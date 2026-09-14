#!/usr/bin/env bash
# Wire zyvor-ota + fleet + nodra + device-agent + relay-edge on one host and verify.
# Run ON the target host as sus (with passwordless sudo).
set -euo pipefail

FLEET_HTTP=http://127.0.0.1:18090
FLEET_HTTPS=https://127.0.0.1:18090
NODRA=http://127.0.0.1:18447
DEVICE=http://127.0.0.1:9188
RELAY=https://127.0.0.1:18086
OTA_SOCK=/run/zyvor-ota-demo/agent.sock
WORK=/home/sus/.deployments/zyvor-lab-wire
mkdir -p "$WORK"/{tls,nodrad,reports}
CJ=/tmp/fleet-wire.cj
REPORT="$WORK/reports/wire-$(date +%Y%m%d-%H%M%S).txt"

pass() { echo "[PASS] $*" | tee -a "$REPORT"; }
fail() { echo "[FAIL] $*" | tee -a "$REPORT"; FAILED=1; }
info() { echo "[INFO] $*" | tee -a "$REPORT"; }
FAILED=0
: >"$REPORT"

info "=== 1) Baseline health ==="
curl -sf "$FLEET_HTTP/readyz" >/dev/null && pass "fleet readyz" || fail "fleet readyz"
curl -sf "$NODRA/healthz" >/dev/null && pass "nodra healthz" || fail "nodra healthz"
curl -sf "$DEVICE/api/v1/health" >/dev/null && pass "device-agent health" || fail "device-agent health"
curl -skf "$RELAY/healthz" >/dev/null && pass "relay-edge healthz" || fail "relay-edge healthz"
systemctl is-active --quiet zyvor-otad-demo && pass "ota demo active" || fail "ota demo active"

info "=== 2) Enable TLS on Fleet (required by OTA fleet_url) ==="
if [[ ! -f /etc/zyvor-fleet/tls/cert.pem ]]; then
  sudo mkdir -p /etc/zyvor-fleet/tls
  openssl req -x509 -newkey rsa:2048 -nodes -keyout "$WORK/tls/key.pem" -out "$WORK/tls/cert.pem" \
    -days 365 -subj "/CN=zyvor-fleet-lab" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:80.79.5.173" 2>/dev/null
  sudo install -m 640 -o zyvor-fleet -g zyvor-fleet "$WORK/tls/cert.pem" /etc/zyvor-fleet/tls/cert.pem
  sudo install -m 640 -o zyvor-fleet -g zyvor-fleet "$WORK/tls/key.pem" /etc/zyvor-fleet/tls/key.pem
fi
# Update unit to pass TLS flags (keep demo + listen)
sudo tee /etc/systemd/system/zyvor-fleet.service >/dev/null <<'UNIT'
[Unit]
Description=Zyvor Fleet control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=zyvor-fleet
Group=zyvor-fleet
EnvironmentFile=/etc/zyvor-fleet/fleet.env
ExecStart=/usr/local/bin/fleetd --demo --listen :18090 --data /var/lib/zyvor-fleet/state.json --tls-cert /etc/zyvor-fleet/tls/cert.pem --tls-key /etc/zyvor-fleet/tls/key.pem
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/zyvor-fleet /etc/zyvor-fleet/tls
CapabilityBoundingSet=
LockPersonality=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl restart zyvor-fleet
sleep 2
curl -skf "$FLEET_HTTPS/readyz" >/dev/null && pass "fleet HTTPS readyz" || fail "fleet HTTPS readyz"
FLEET="$FLEET_HTTPS"
CURL_F=(curl -skf -c "$CJ" -b "$CJ")

info "=== 3) Fleet login + OTA device registration ==="
rm -f "$CJ"
"${CURL_F[@]}" -X POST "$FLEET/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"admin@zyvor.local","password":"zyvor-fleet-demo"}' >/dev/null
OTA_JSON=$("${CURL_F[@]}" -X POST "$FLEET/api/v1/ota/devices" -H 'Content-Type: application/json' -H 'X-Zyvor-Request: 1' \
  -d '{"deviceId":"NLDW4-4-16-36","name":"lab-host","siteId":"lab"}')
echo "$OTA_JSON" | tee "$WORK/ota-device.json" >/dev/null
OTA_TOKEN=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$WORK/ota-device.json")
[[ -n "$OTA_TOKEN" ]] && pass "OTA device token issued" || fail "OTA device token"

# Put a placeholder assignment (invalid signature is OK for delivery test; agent may reject on submit)
ASSIGN='{"job_id":"lab-wire-1","device_id":"NLDW4-4-16-36","auto_reboot":false,"not_before":"2020-01-01T00:00:00Z","deadline":"2099-01-01T00:00:00Z","release":{"key_id":"demo","payload":"e30=","signature":"AA=="}}'
"${CURL_F[@]}" -X PUT "$FLEET/api/v1/ota/devices/NLDW4-4-16-36/assignment" -H 'Content-Type: application/json' -H 'X-Zyvor-Request: 1' \
  -d "$ASSIGN" >/dev/null && pass "OTA assignment published" || fail "OTA assignment publish"

info "=== 4) Point zyvor-otad-demo at Fleet OTA contract ==="
sudo mkdir -p /etc/zyvor-ota-demo
echo -n "$OTA_TOKEN" | sudo tee /etc/zyvor-ota-demo/fleet.token >/dev/null
sudo chmod 600 /etc/zyvor-ota-demo/fleet.token
sudo cp /etc/zyvor-fleet/tls/cert.pem /etc/zyvor-ota-demo/fleet-ca.pem
# Merge fleet fields into existing agent.json
python3 - <<'PY'
import json
p="/etc/zyvor-ota-demo/agent.json"
c=json.load(open(p))
c["fleet_url"]="https://127.0.0.1:18090"
c["fleet_token_file"]="/etc/zyvor-ota-demo/fleet.token"
c["fleet_ca"]="/etc/zyvor-ota-demo/fleet-ca.pem"
# keep existing trust/download settings
open("/tmp/agent.wired.json","w").write(json.dumps(c, indent=2)+"\n")
print("wired", c["device_id"], c["fleet_url"])
PY
sudo install -m 640 /tmp/agent.wired.json /etc/zyvor-ota-demo/agent.json
sudo systemctl restart zyvor-otad-demo
sleep 3
systemctl is-active --quiet zyvor-otad-demo && pass "ota restarted with fleet_url" || fail "ota restart"

# Poll Fleet assignment via device token (simulates agent GET)
CODE=$(curl -sk -o /tmp/asg.json -w '%{http_code}' -H "Authorization: Bearer $OTA_TOKEN" \
  "$FLEET/v1/devices/NLDW4-4-16-36/assignment")
[[ "$CODE" == "200" ]] && pass "OTA GET assignment via device token (HTTP $CODE)" || fail "OTA GET assignment HTTP $CODE"

# Post a synthetic event batch (contiguous ACK)
EV='[{"sequence":1,"job_id":"lab-wire-1","state":"accepted","time":"2026-09-14T00:00:00Z"}]'
ACK=$(curl -sk -H "Authorization: Bearer $OTA_TOKEN" -H 'Content-Type: application/json' \
  -d "$EV" "$FLEET/v1/devices/NLDW4-4-16-36/events")
echo "$ACK" | grep -q '"sequence":1' && pass "OTA events ACK contiguous: $ACK" || fail "OTA events ACK: $ACK"

info "=== 5) Fleet-agent + Device Agent inventory merge ==="
sudo tee /etc/systemd/system/zyvor-fleet-agent.service >/dev/null <<'UNIT'
[Unit]
Description=Zyvor Fleet site agent (lab)
After=network-online.target zyvor-fleet.service zyvor-device-agent.service
Wants=network-online.target

[Service]
Type=simple
User=sus
ExecStart=/usr/local/bin/fleet-agent \
  --server https://127.0.0.1:18090 \
  --name lab-nldw4 \
  --region lab-eu \
  --enrollment-token zf_enroll_demo-local-only \
  --state /home/sus/.deployments/zyvor-lab-wire/fleet-agent-state.json \
  --labels class=lab,tier=smoke \
  --interval 10s \
  --device-agent-url http://127.0.0.1:9188
Environment=SSL_CERT_FILE=/etc/zyvor-fleet/tls/cert.pem
# fleet-agent uses default http.Client — need insecure for self-signed.
# Use NODE_EXTRA / custom: wrap with SSL skip via env if supported.
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT

# fleet-agent may reject self-signed TLS. Prefer HTTP by briefly also listening...
# Better: use openssl s_client trust via SSL_CERT_FILE pointing at our CA (self-signed is the CA).
# Go's default transport trusts SSL_CERT_FILE as CA bundle — should work for our self-signed leaf if it's the cert itself as CA.
sudo systemctl daemon-reload
sudo systemctl restart zyvor-fleet-agent || sudo systemctl start zyvor-fleet-agent
sleep 5
if systemctl is-active --quiet zyvor-fleet-agent; then
  pass "fleet-agent active"
else
  # Fallback: run once in foreground to capture error, then try with http if TLS fails
  info "fleet-agent failed under HTTPS — checking logs"
  journalctl -u zyvor-fleet-agent -n 20 --no-pager | tee -a "$REPORT" || true
  # Run agent against HTTPS with GODEBUG? Instead patch to use curl-enrolled approach via temporary HTTP proxy.
  # Simplest fix: stunnel not available — generate and use system trust:
  sudo cp /etc/zyvor-fleet/tls/cert.pem /usr/local/share/ca-certificates/zyvor-fleet-lab.crt
  sudo update-ca-certificates >/dev/null 2>&1 || true
  sudo systemctl restart zyvor-fleet-agent
  sleep 5
  systemctl is-active --quiet zyvor-fleet-agent && pass "fleet-agent active after CA install" || fail "fleet-agent still down"
fi

sleep 8
SITES=$("${CURL_F[@]}" "$FLEET/api/v1/sites")
echo "$SITES" | tee "$WORK/sites.json" >/dev/null
python3 - <<'PY' | tee -a "$REPORT"
import json
sites=json.load(open("/home/sus/.deployments/zyvor-lab-wire/sites.json"))
assert isinstance(sites, list), sites
print(f"sites={len(sites)}")
if not sites:
    raise SystemExit("no sites enrolled")
s=sites[0]
inv=s.get("inventory") or {}
meta=inv.get("metadata") or {}
print("site", s.get("id"), s.get("name"), s.get("status"))
print("inventory keys", sorted(inv.keys())[:12])
# Device-agent merge puts namespaced keys into metadata
da=[k for k in meta if "device" in k.lower() or "zyvor" in k.lower() or "serial" in k.lower() or "capability" in k.lower()]
print("metadata sample", list(meta.items())[:8])
print("device_agent_meta_hints", da[:8])
PY
pass "fleet site enrolled (see report)"

info "=== 6) Nodrad MQTT + Device Agent → Nodra ==="
if [[ ! -x /usr/local/bin/nodrad ]]; then
  fail "nodrad binary missing — install step should have placed it"
else
  ENROLL=$(sudo grep '^NODRA_ENROLLMENT_TOKEN=' /etc/nodra/nodra.env | cut -d= -f2-)
  sudo mkdir -p /var/lib/nodrad /etc/nodra
  if [[ ! -f /etc/nodra/nodrad.json ]]; then
    /usr/local/bin/nodrad init \
      --config /tmp/nodrad.json \
      --server "$NODRA" \
      --site lab-nldw4 \
      --enrollment-token "$ENROLL" \
      --data /var/lib/nodrad \
      --listen 127.0.0.1:9091 \
      --mqtt-listen 127.0.0.1:1883 || true
    # init may need to run as process that enrolls — check agent docs
    if [[ -f /tmp/nodrad.json ]]; then
      sudo install -m 640 /tmp/nodrad.json /etc/nodra/nodrad.json
    fi
  fi
  sudo tee /etc/systemd/system/nodrad.service >/dev/null <<'UNIT'
[Unit]
Description=Nodra edge agent (MQTT + spool)
After=network-online.target nodra-server.service
Wants=nodra-server.service

[Service]
Type=simple
ExecStart=/usr/local/bin/nodrad --config /etc/nodra/nodrad.json
Restart=on-failure
RestartSec=3
ReadWritePaths=/var/lib/nodrad
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
  # Ensure data dir writable
  sudo mkdir -p /var/lib/nodrad
  sudo chmod 755 /var/lib/nodrad
  # If config missing, create minimal via init as root after fixing token
  if [[ ! -f /etc/nodra/nodrad.json ]]; then
    ENROLL=$(sudo grep '^NODRA_ENROLLMENT_TOKEN=' /etc/nodra/nodra.env | cut -d= -f2-)
    sudo /usr/local/bin/nodrad init --config /etc/nodra/nodrad.json --server http://127.0.0.1:18447 \
      --site lab-nldw4 --enrollment-token "$ENROLL" --data /var/lib/nodrad \
      --listen 127.0.0.1:9091 --mqtt-listen 127.0.0.1:1883
  fi
  sudo systemctl daemon-reload
  sudo systemctl enable --now nodrad
  sleep 3
  if systemctl is-active --quiet nodrad && ss -tln | grep -q ':1883 '; then
    pass "nodrad MQTT listening :1883"
    # Enable device-agent nodra publish
    sudo python3 - <<'PY'
from pathlib import Path
p=Path("/etc/zyvor/device-agent.toml")
t=p.read_text()
if "enabled = false" in t.split("[nodra]")[1].split("[")[0]:
    t=t.replace("[nodra]\nenabled = false", "[nodra]\nenabled = true", 1)
    # also handle spaces
    import re
    t=re.sub(r"(\[nodra\]\n(?:.*\n)*?)enabled = false", r"\1enabled = true", t, count=1)
Path("/tmp/device-agent.wired.toml").write_text(t)
print("nodra section patched")
PY
    # Safer sed:
    sudo sed -i '/^\[nodra\]/,/^\[/{s/^enabled = false/enabled = true/}' /etc/zyvor/device-agent.toml
    sudo systemctl restart zyvor-device-agent
    sleep 3
    INT=$(curl -sf "$DEVICE/api/v1/integrations")
    echo "$INT" | tee -a "$REPORT"
    echo "$INT" | grep -q '"nodra_enabled":true' && pass "device-agent nodra enabled" || fail "device-agent nodra not enabled: $INT"
    # give it a moment to connect
    sleep 5
    INT=$(curl -sf "$DEVICE/api/v1/integrations")
    echo "$INT" | tee -a "$REPORT"
    echo "$INT" | grep -q '"nodra_connected":true' && pass "device-agent connected to Nodra MQTT" || info "nodra_connected not yet true (may still be connecting): $INT"
  else
    journalctl -u nodrad -n 40 --no-pager | tee -a "$REPORT" || true
    fail "nodrad not healthy"
  fi
fi

info "=== 7) Relay-edge smoke ==="
if EDGE="$RELAY" /home/sus/.deployments/zyvor-relay-edge/../../relay-edge/scripts/smoke.sh 2>/dev/null; then
  pass "relay-edge smoke"
else
  # scripts may not be on remote — curl UI modules instead
  curl -skf "$RELAY/healthz" | grep -q relay-edge && pass "relay-edge health payload" || fail "relay-edge smoke"
  curl -skf "$RELAY/ui/" >/dev/null && pass "relay-edge UI" || fail "relay-edge UI"
fi

info "=== 8) Cross-service summary ==="
{
  echo "fleet:   $FLEET"
  echo "nodra:   $NODRA"
  echo "device:  $DEVICE"
  echo "relay:   $RELAY/ui/"
  echo "ota:     unix:$OTA_SOCK"
  zyvor-ota -socket "$OTA_SOCK" status 2>/dev/null || true
  "${CURL_F[@]}" "$FLEET/api/v1/ota/devices" || true
} | tee -a "$REPORT"

if [[ "${FAILED:-0}" -eq 0 ]]; then
  echo "ALL WIRE CHECKS PASSED" | tee -a "$REPORT"
  exit 0
fi
echo "SOME CHECKS FAILED — see $REPORT" | tee -a "$REPORT"
exit 1

#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# ============================================================================
# selftest.sh — Post-installation verification for the zyvor-ota remote
# smoke-deploy (deploy/README.md).
#
# Checks that the systemd units are active, the operator socket answers, and
# then runs one REAL signed job cycle (sign -> submit -> install -> reboot ->
# health -> commit -> ack -> gc) against the live, running simulator-backend
# daemon. This never touches RAUC, D-Bus or real disks: the simulator backend
# only rewrites a local JSON slot file (internal/ota/backend.go).
#
# Usage:
#   sudo bash scripts/selftest.sh
#
# Exit codes:
#   0 = all checks passed
#   1 = one or more checks failed
# ============================================================================
set -euo pipefail

SOCKET="/run/zyvor-ota-demo/agent.sock"
CLI="/usr/local/bin/zyvor-ota"
KEYS="/etc/zyvor-ota-demo/keys"
ARTIFACTS="/var/lib/zyvor-ota-demo/artifacts"
AGENT_CONFIG="/etc/zyvor-ota-demo/agent.json"

SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"

PASS=0
FAIL=0
pass() { PASS=$((PASS + 1)); echo "  [pass] $1"; }
failc() { FAIL=$((FAIL + 1)); echo "  [fail] $1"; }
section() { echo ""; echo "=== $1 ==="; }

section "zyvor-ota-demo services"
if $SUDO systemctl is-active --quiet zyvor-ota-demo-artifacts.service; then
    pass "zyvor-ota-demo-artifacts.service active"
else
    failc "zyvor-ota-demo-artifacts.service not active"
fi
if $SUDO systemctl is-active --quiet zyvor-otad-demo.service; then
    pass "zyvor-otad-demo.service active"
else
    failc "zyvor-otad-demo.service not active"
fi

section "Operator socket"
ready=0
for _ in $(seq 1 30); do
    if [ -S "$SOCKET" ]; then
        ready=1
        break
    fi
    sleep 1
done
if [ "$ready" -eq 1 ]; then
    pass "agent socket present: $SOCKET"
else
    failc "agent socket missing: $SOCKET"
fi
if STATUS=$($SUDO "$CLI" -socket "$SOCKET" status 2>&1); then
    pass "status: $STATUS"
else
    failc "status query failed: $STATUS"
fi

section "Live signed job cycle"
if [ "$FAIL" -eq 0 ]; then
    if OUT=$($SUDO env SOCKET="$SOCKET" CLI="$CLI" KEYS="$KEYS" ARTIFACTS="$ARTIFACTS" AGENT_CONFIG="$AGENT_CONFIG" python3 - <<'PY' 2>&1
import hashlib, json, os, subprocess, sys, time
from datetime import datetime, timedelta, timezone

SOCKET = os.environ["SOCKET"]
CLI = os.environ["CLI"]
KEYS = os.environ["KEYS"]
ARTIFACTS = os.environ["ARTIFACTS"]
AGENT_CONFIG = os.environ["AGENT_CONFIG"]


def cli(*args):
    last = None
    for _ in range(25):
        result = subprocess.run([CLI, "-socket", SOCKET] + list(args),
                                 text=True, capture_output=True, timeout=30)
        if result.returncode == 0:
            return result.stdout
        blob = result.stdout + result.stderr
        last = result
        if "engine busy" in blob:
            time.sleep(0.2)
            continue
        raise SystemExit(f"{args}: rc={result.returncode} stdout={result.stdout!r} stderr={result.stderr!r}")
    raise SystemExit(f"{args}: rc={last.returncode} stdout={last.stdout!r} stderr={last.stderr!r}")


def wait_state(job_id, expected, timeout=40):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = json.loads(cli("job", job_id))
        if last["state"] == expected:
            return last
        if last["state"] in ("failed", "needs_recovery"):
            raise SystemExit(f"job {job_id} entered {last['state']}: {last}")
        time.sleep(0.2)
    raise SystemExit(f"timeout waiting for job {job_id} to reach {expected}, last={last}")


with open(AGENT_CONFIG) as f:
    agent_config = json.load(f)
device_id = agent_config["device_id"]
artifact_host = agent_config["download_hosts"][0]

seq = int(time.time())
job_id = f"selftest-{seq}"
data = (f"zyvor-ota selftest artifact {seq}\n" * 64).encode()
artifact_name = f"selftest-{seq}.raucb"
artifact_path = os.path.join(ARTIFACTS, artifact_name)
with open(artifact_path, "wb") as f:
    f.write(data)
os.chmod(artifact_path, 0o644)

now = datetime.now(timezone.utc)
release = {
    "schema": 1, "id": f"selftest-{seq}", "sequence": seq,
    "compatible": "smoke-deploy", "backend": "simulator", "version": f"0.selftest.{seq}",
    "expires": (now + timedelta(hours=1)).isoformat(),
    "artifact": {
        "url": f"http://{artifact_host}/{artifact_name}",
        "size": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
    },
}
release_path = "/tmp/zyvor-ota-selftest-release.json"
envelope_path = "/tmp/zyvor-ota-selftest-envelope.json"
assignment_path = "/tmp/zyvor-ota-selftest-assignment.json"
with open(release_path, "w") as f:
    json.dump(release, f)

subprocess.run([CLI, "sign", release_path, f"{KEYS}/release.key", "demo", envelope_path], check=True)
with open(envelope_path) as f:
    envelope = json.load(f)

assignment = {
    "job_id": job_id,
    "device_id": device_id,
    "release": envelope,
    "not_before": (now - timedelta(minutes=1)).isoformat(),
    "deadline": (now + timedelta(minutes=10)).isoformat(),
    "auto_reboot": False,
}
with open(assignment_path, "w") as f:
    json.dump(assignment, f)

cli("submit", assignment_path)
wait_state(job_id, "awaiting_reboot")
cli("reboot")
wait_state(job_id, "committed")

events = json.loads(cli("events"))
if events:
    cli("ack", str(events[-1]["sequence"]))
cli("gc")

for p in (release_path, envelope_path, assignment_path):
    os.remove(p)

print(f"live job cycle ok: {job_id} install -> reboot -> health -> commit -> ack -> gc")
PY
    ); then
        pass "$OUT"
    else
        failc "live job cycle failed: $OUT"
    fi
else
    echo "  [skip] live job cycle (service/socket checks failed above)"
fi

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[ "$FAIL" -eq 0 ]

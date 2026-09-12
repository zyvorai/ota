# Tutorial: your first simulator update

This walks the exact lifecycle `make demo` automates (`scripts/e2e.py`), one
command at a time, so you can see what each step actually does. Every command
below was run against a real build of this repository; every JSON block is
literal captured output, not a mock-up (timestamps, keys, and hashes will
differ on your machine).

**This is the simulator backend only.** It never touches RAUC, D-Bus, or real
disks — `internal/ota/backend.go`'s `Simulator.Reboot` just rewrites a local
JSON slot file. For a real board, see [DEVICE-INTEGRATION](DEVICE-INTEGRATION.md)
instead; this tutorial does not qualify anything on hardware.

Requirements: Linux, Go 1.27.1 or a newer supported patch, Python 3.

## 1. Build and set up a workspace

```sh
make build
mkdir -p /tmp/zyvor-ota-tutorial/assets
cd /tmp/zyvor-ota-tutorial
OTA=/path/to/zyvor-ota/bin/zyvor-ota
OTAD=/path/to/zyvor-ota/bin/zyvor-otad
```

## 2. Generate a signing key and a device config

```sh
$OTA keygen ./keys
touch healthy   # the health probe this config checks for
```

`keygen` refuses to run if `./keys` already exists, and writes `release.key`
(0600, private) and `release.pub` (0644, public) — never commit `release.key`.

Embed the printed public key as the trust key, and point `download_hosts` and
the health check at files under this workspace:

```sh
PUBKEY=$(cat keys/release.pub)
cat > agent.json <<JSON
{
  "device_id": "tutorial-1",
  "compatible": "tutorial-board",
  "state_dir": "/tmp/zyvor-ota-tutorial/state",
  "socket": "/tmp/zyvor-ota-tutorial/agent.sock",
  "backend": "simulator",
  "allow_device_writes": false,
  "trust_keys": {"tutorial": "${PUBKEY}"},
  "download_hosts": ["127.0.0.1:8091"],
  "max_artifact_bytes": 1048576,
  "reserve_bytes": 1048576,
  "health_timeout_seconds": 10,
  "health_stable_seconds": 2,
  "checks": [{"kind": "file", "target": "/tmp/zyvor-ota-tutorial/healthy"}]
}
JSON
```

Every field here is documented in the [User Guide](USER-GUIDE.md#configuration-reference-agentjson).

## 3. Start an artifact server and the daemon

```sh
python3 -m http.server 8091 --directory assets --bind 127.0.0.1 &
$OTAD -config agent.json &
sleep 1
$OTA -socket ./agent.sock status
```

```json
{"active":null,"backend":"simulator","device_id":"tutorial-1","event_sequence":0,"high_sequence":0,"pending_events":0,"version":"0.1.0"}
```

## 4. Build and sign a release

```sh
echo "tutorial release payload" > assets/os.raucb
SHA=$(sha256sum assets/os.raucb | awk '{print $1}')
SIZE=$(wc -c < assets/os.raucb | tr -d ' ')

cat > release.json <<JSON
{
  "schema": 1, "id": "tutorial-release-1", "sequence": 1,
  "compatible": "tutorial-board", "backend": "simulator", "version": "1.0.0",
  "expires": "2030-01-01T00:00:00Z",
  "artifact": {"url": "http://127.0.0.1:8091/os.raucb", "sha256": "${SHA}", "size": ${SIZE}}
}
JSON
$OTA sign release.json keys/release.key tutorial envelope.json
```

`sequence` is the anti-replay high-water mark: it must strictly increase
across releases this device will accept. `sign` writes `envelope.json` — the
`key_id`, base64 `payload`, and Ed25519 `signature` that `submit` verifies.

## 5. Submit a signed assignment

Assignments carry their own install window (`not_before`/`deadline`),
independent of the release's own `expires`:

```sh
NOT_BEFORE=$(date -u -d '-1 minute' +%Y-%m-%dT%H:%M:%SZ)   # macOS: date -u -v-1M ...
DEADLINE=$(date -u -d '+10 minutes' +%Y-%m-%dT%H:%M:%SZ)   # macOS: date -u -v+10M ...
python3 -c "
import json
envelope = json.load(open('envelope.json'))
json.dump({'job_id': 'tutorial-job-1', 'device_id': 'tutorial-1', 'release': envelope,
           'not_before': '$NOT_BEFORE', 'deadline': '$DEADLINE', 'auto_reboot': False},
          open('assignment.json', 'w'))
"
$OTA -socket ./agent.sock submit assignment.json
```

The response is the created job at `state: "accepted"`. Poll it — the engine
advances once per second:

```sh
$OTA -socket ./agent.sock job tutorial-job-1
```

Within a few seconds it moves `accepted → downloading → verified →
installing → awaiting_reboot` on its own (downloading the artifact, verifying
its digest, and writing it to the inactive slot). At `awaiting_reboot`:

```json
{"...":"...","state":"awaiting_reboot","old_slot":"rootfs.0","old_version":"factory","target_slot":"rootfs.1","boot_id":"boot-0","reboot_requested":false,"updated":"..."}
```

## 6. Reboot and watch it commit

`auto_reboot` was `false`, so nothing reboots until you ask:

```sh
$OTA -socket ./agent.sock reboot
```

Poll `job tutorial-job-1` again. It moves `checking_health → committing →
committed` once the configured health check (the `healthy` file) has passed
continuously for `health_stable_seconds`:

```json
{"...":"...","state":"committed","old_slot":"rootfs.0","target_slot":"rootfs.1","boot_id":"boot-0x","reboot_requested":true,"updated":"..."}
```

## 7. See a rollback happen

Submit a second, higher-sequence release the same way (`sequence: 2`,
`job_id: tutorial-job-2`), wait for `awaiting_reboot`, then remove the health
marker *before* rebooting so the new slot never becomes healthy:

```sh
rm -f healthy
$OTA -socket ./agent.sock reboot
```

After `health_timeout_seconds` (10s here) with no passing health check, the
job moves `checking_health → rollback_pending → rolled_back` — the backend
restores the old slot automatically, no operator action needed:

```json
{"...":"state":"rolled_back","error":"health verification deadline exceeded","old_slot":"rootfs.1","old_version":"1.0.0","target_slot":"rootfs.0","boot_id":"boot-0xx","...":"..."}
```

Resubmitting either `assignment.json` again is rejected — its release
sequence is no longer above the device's high-water mark. This is the same
check that rejects a captured-and-replayed assignment.

## 8. Acknowledge events and clean the cache

```sh
$OTA -socket ./agent.sock events                 # every state transition since the last ack
$OTA -socket ./agent.sock ack 18                  # the last sequence number from `events`
$OTA -socket ./agent.sock events                  # -> null: nothing unacknowledged left
$OTA -socket ./agent.sock gc                      # removes cached bundles now that no job is active
```

A real Fleet server does the `ack` step after it durably stores the events
(see [FLEET](FLEET.md)); this tutorial does it by hand since there's no Fleet
server here.

## 9. Clean up

```sh
kill %1 %2   # the artifact server and the daemon started with `&` above
rm -rf /tmp/zyvor-ota-tutorial
```

## Next steps

- [User Guide](USER-GUIDE.md) — every CLI subcommand and config field in one place.
- [Device deployment](https://github.com/zyvorai/ota#device-deployment) — the same lifecycle against a real board with the RAUC backend.
- [Remote smoke-deploy](https://github.com/zyvorai/ota/blob/main/deploy/README.md) — run this same simulator lifecycle as an installed systemd service on another host over SSH.
- [Operations](OPERATIONS.md) — the runbook for `NeedsRecovery`, trust rotation, and journal limits once this isn't a toy workspace anymore.

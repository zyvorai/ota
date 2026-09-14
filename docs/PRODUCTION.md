---
hero:
  eyebrow: PRODUCTION
  title: Production operations runbook
---

Companion to [OPERATIONS.md](OPERATIONS.md) for the Minewing GW1 r1 production path.
For a multi-product Linux lab (simulator OTA + Fleet + Nodra + Device Agent),
see [LAB.md](LAB.md) first — that path does **not** qualify a board image.

## Current maturity (2026-09-15)

| Claim | Status |
|---|---|
| Host software matrix + CI lab-substitute | green (`make qualify`, verify←lab-substitute, CodeQL) |
| Simulator / Fleet contract path | production-capable for **simulator backend** |
| Generic QEMU+RAUC lab (`zyvor-ota-qemu-lab`) | **complete** — KVM SSH, `rauc status`, install, power-loss, 3 reboots; [`qemu-lab/CHECKLIST.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/qemu-lab/CHECKLIST.md); HIL `20260914T192634Z` |
| Minewing / claimable RAUC power-loss HIL | **unsigned** — needs BSP/`OTA_HIL_MINEWING=1` or physical board |
| Hardware checklist (Minewing) | **not signed** — do not claim board OS OTA |

**Sign gates** ([HIL.md](HIL.md)):

- Lab-only: `OTA_HIL_SIGN=1` + `OTA_HIL_SIGN_QEMU_LAB=1` → `qemu-lab/CHECKLIST.md` when `qemu_lab_complete=true`.
- Minewing: `OTA_HIL_SIGN=1` + `OTA_HIL_MINEWING=1` → `hardware-checklist.md` only when `minewing_rauc_claimable=true`.

**Verdict:** agent software is **engineering-preview production-hardening**. Generic
QEMU RAUC lab HIL is **complete** (not Minewing). **Minewing board-production** still
requires BSP image or physical board + Minewing HIL sign-off.

## Preconditions

1. Board profile [`boards/minewing-gw1-r1`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/BOARD.md)
   baked into the factory image (`scripts/bake-rootfs-overlay.sh` + PARTUUIDs
   from `board.env`).
2. Software matrix green: `make qualify` →
   `evidence/qualification/software-matrix.json`.
3. Hardware checklist signed for the exact image hash:
   [`evidence/qualification/hardware-checklist.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hardware-checklist.md).
4. Fleet server implements [FLEET.md](FLEET.md):
   - **Production / lab control plane:** Zyvor Fleet (`zyvorai/fleet`)
     `/v1/devices/...` and `/api/v1/ota/...` — see Fleet
     `docs/OTA_CONTRACT.md`.
   - **Bring-up without Fleet:** `zyvor-fleet-ref` in this repository.
5. Offline Ed25519 release keys and RAUC X.509 keys never stored on devices.
6. `fleet_url` is HTTPS with a CA the agent trusts (`fleet_ca` or the system
   store). HTTP is rejected at config validation.

## Health probes (Mark-good gate)

Production agent config must include device-specific checks. Example set used by
the Minewing profile:

| Kind | Target | Purpose |
|---|---|---|
| `systemd` | `zyvor-device-agent.service` | Core device agent running |
| `http` | `http://127.0.0.1:9091/healthz` | Loopback application health (often `nodrad` ingest) |
| `file` | `/var/lib/zyvor-device/healthy` | Latch written after app init |

See [`examples/health/`](../examples/health/) for copy-paste check fragments.
Do not mark an OS good until essential device and application probes pass.

## Wiring to Fleet

1. Register the device: `POST /api/v1/ota/devices` (Fleet admin).
2. Install the returned bearer token as `fleet_token_file` (mode `0600`/`640`,
   readable only by the agent user).
3. Set `fleet_url` to the Fleet HTTPS base (no path suffix).
4. Publish signed Assignment JSON with `PUT …/assignment`. Job `device_id`,
   release `compatible`/`backend`/`sequence`, and trust `key_id` must all match.
5. After a terminal job, `DELETE …/assignment` so the agent stops re-polling
   the same envelope.
6. Monitor `pending_events` vs Fleet `ackedSequence`. ACKs are contiguous —
   a gap freezes the outbox until repaired.

## Monitoring

- Scrape the Unix-socket `/metrics` endpoint from a node-local exporter.
- Alert on `zyvor_ota_pending_events` growth (Fleet ACK lag) and stuck non-terminal jobs.
- Track Fleet assignment delivery failures separately from local rollback success.
- On Fleet, watch `/api/v1/ota/devices/{id}/events` alongside site health.

## NeedsRecovery

Follow [OPERATIONS.md](OPERATIONS.md#needsrecovery). Never edit `state.json` to
clear an interlock or lower the anti-replay sequence.

## Key rotation

Provision a new Ed25519 public key alongside the old key via a trusted image or
config update, restart the agent, sign with the new key ID, then remove the old
key after the fleet has the new trust configuration.

## Journal retention

At 10,000 jobs there is no online compaction in v0.1. Retire/reprovision under
an audited maintenance process with a trusted sequence baseline.

## Release artifacts

Tag `v*` → GitHub Actions builds, checksums, Cosign-signs `SHA256SUMS`, and
publishes a GitHub Release. Verify:

```sh
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity https://github.com/zyvorai/ota/.github/workflows/release.yml@refs/tags/vX.Y.Z \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS
sha256sum -c SHA256SUMS
```

Bind the board image SBOM digest in signed release metadata separately from the
source SPDX file.

---
hero:
  eyebrow: HIL
  title: RAUC / power-loss HIL
---

Host `make qualify` never claims hardware rows. There are **two separate tracks**:

| Track | Compatible | Status | How to exercise |
|---|---|---|---|
| Generic QEMU lab | `zyvor-ota-qemu-lab` | **Complete** (lab evidence) | QEMU guest + RAUC; CI soft-smoke via `make ci-rauc-qemu` |
| Minewing GW1 r1 | `minewing-gw1-r1` | **Unsigned** | BSP QEMU image **or** physical silicon — never auto-signed |

**QEMU vs Minewing silicon:** Track A proves RAUC A/B install / mid-install power-loss /
reboots on the **generic** lab disk only. Track B Minewing power-loss is **not** signed
unless an operator runs the harness with `OTA_HIL_MINEWING=1` and
`minewing_rauc_claimable=true`. Soft-skip CI (`scripts/ci/rauc-qemu-smoke.sh`) never
claims either track as newly signed.

Never treat the generic lab image as a Minewing silicon claim.

## Track A — Generic QEMU lab (complete)

Built and exercised on lab host `80.79.5.173` (2026-09-14):

| Step | Result |
|---|---|
| Image build | [`scripts/hil/build-qemu-rauc-lab.sh`](https://github.com/zyvorai/ota/blob/main/scripts/hil/build-qemu-rauc-lab.sh) → `/home/sus/zyvor-qemu-lab/disk.img` |
| Guest | KVM + DHCP + SSH; live `rauc status` (booted rootfs.A) |
| Signed install | `INSTALL_RC=0` to inactive slot |
| Bad signature | Rejected (`BAD_RC=1`) |
| Power-loss | `kill -9` qemu mid-install; previous slot still boots |
| Reboots | Three healthy guest restarts with `rauc status` |

Evidence:

- Checklist: [`evidence/qualification/qemu-lab/CHECKLIST.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/qemu-lab/CHECKLIST.md) (`qemu_lab_complete=true`)
- HIL stamp: [`evidence/qualification/hil/20260914T192634Z/`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hil/20260914T192634Z/)
- Notes: [`evidence/qualification/qemu-lab/README.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/qemu-lab/README.md)
- Disk copy checked 2026-09-21 at `/home/sus/zyvor-qemu-lab/disk.img` and `$HOME/zyvor-qemu-lab/disk.img` (`7b8ce8ecbce8b43f…`). Presence smoke only. Not re-booted, not a release asset, not Minewing.

Sign **lab-only** checklist (never Minewing hardware-checklist):

```bash
QUALIFY_QEMU_IMAGE=/path/to/disk.img \
OTA_HIL_ENV=qemu \
OTA_HIL_SKU=zyvor-ota-qemu-lab \
OTA_HIL_COMPATIBLE=zyvor-ota-qemu-lab \
OTA_HIL_SSH='-o StrictHostKeyChecking=no -p 2222 -i lab_ssh_key root@127.0.0.1' \
OTA_HIL_BUNDLE=/path/to/bundle.raucb \
OTA_HIL_LOG_*=… \
OTA_HIL_SIGN=1 OTA_HIL_SIGN_QEMU_LAB=1 \
./scripts/hil/run-rauc-powerloss-hil.sh
```

## Track B — Minewing GW1 r1 (open)

**Status:** unsigned. A BSP QEMU-bootable A/B image (`compatible=minewing-gw1-r1`)
or a physical GW1 r1 board is required. This repository does **not** ship a
prebuilt Minewing disk (not in git, not a GitHub release asset). The generic
`$HOME/zyvor-qemu-lab/disk.img` is Track A only and must never be passed as a
Minewing image.

Board profile: [`boards/minewing-gw1-r1/`](https://github.com/zyvorai/ota/tree/main/boards/minewing-gw1-r1)
([`QEMU.md`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/QEMU.md),
[`BOARD.md`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/BOARD.md)).

### Operator runbook (bake → HIL → sign)

1. **PARTUUIDs.** Copy `boards/minewing-gw1-r1/board.env.example` → `board.env`
   and fill UUIDs from the BSP factory layout. Render RAUC config:
   `scripts/render-board-rauc.sh boards/minewing-gw1-r1 /path/to/etc/rauc`.
2. **Cross-build.** `make dist` (or `make build` on the target arch).
3. **Bake.** Install primary CLI `otactl`, compat alias `zyvor-ota`, and
   `zyvor-otad` into the BSP rootfs:
   ```bash
   scripts/bake-rootfs-overlay.sh \
     --rootfs /path/to/rootfs \
     --board boards/minewing-gw1-r1 \
     --agent-config boards/minewing-gw1-r1/agent.json \
     --arch arm64
   ```
4. **Image location.** Point `QUALIFY_QEMU_IMAGE` at the BSP-produced
   `minewing-ab.img` (suggested lab path: `$HOME/zyvor-minewing-lab/minewing-ab.img`).
   Do not use the generic qemu-lab disk.
5. **Boot and smoke.** Boot slot A. Confirm `rauc status` and
   `otactl status json` over SSH (`zyvor-ota` is the same binary).
6. **Artifacts.** Serve a signed `.raucb` + OTA assignment (`otactl submit` or
   `zyvor-fleet-ref`). Capture operator logs for commit, bad signature,
   power-loss write, power-loss boot selection, NeedsRecovery, and three reboots
   (`OTA_HIL_LOG_*` env vars — see harness SUMMARY).
7. **HIL harness (unsigned run first).** Attach logs; leave `OTA_HIL_SIGN` unset
   until every required row is `pass`.
8. **Sign only when claimable.** When `minewing_rauc_claimable=true`:

```bash
QUALIFY_QEMU_IMAGE=/path/to/minewing-ab.img \
OTA_HIL_ENV=qemu \
OTA_HIL_MINEWING=1 \
OTA_HIL_COMPATIBLE=minewing-gw1-r1 \
OTA_HIL_SSH='root@guest' \
OTA_HIL_BUNDLE=/path/to/os.raucb \
OTA_HIL_LOG_COMMIT=… \
OTA_HIL_LOG_BADSIG=… \
OTA_HIL_LOG_PLOSS_WRITE=… \
OTA_HIL_LOG_PLOSS_BOOT=… \
OTA_HIL_LOG_NEEDS_RECOVERY=… \
OTA_HIL_LOG_REBOOTS=… \
OTA_HIL_SIGN=1 \
./scripts/hil/run-rauc-powerloss-hil.sh
```

Use `OTA_HIL_ENV=physical` for silicon with the same `OTA_HIL_MINEWING=1` gate.

`OTA_HIL_SIGN=1` updates
[`evidence/qualification/hardware-checklist.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hardware-checklist.md)
**only** when `minewing_rauc_claimable=true` (Minewing-compatible image + all
required rows pass). Commit the new `evidence/qualification/hil/<stamp>/`
directory with the checklist update. Until that happens, production docs must
keep Minewing **unsigned**.

Dry-run (`OTA_HIL_ENV=dry-run`) never sets `minewing_rauc_claimable`.
## GitHub CI (lab substitute + QEMU soft-smoke)

When neither image is available, CI still runs
[`scripts/ci/lab-substitute.py`](https://github.com/zyvorai/ota/blob/main/scripts/ci/lab-substitute.py)
(`make ci-lab`, job `lab-substitute`):

| CI row | What it proves |
|---|---|
| `ci_agent_crash_during_install` | Daemon restart during `installing` → `needs_recovery` |
| `ci_bad_signature_reject` | Wrong signing key rejected before install |
| `ci_fleet_ref_https_commit` | HTTPS `zyvor-fleet-ref` → simulator `committed` |
| `ci_hil_harness_dry_run` | HIL evidence layout (never claimable) |

Beyond lab-substitute, [`scripts/ci/rauc-qemu-smoke.sh`](https://github.com/zyvorai/ota/blob/main/scripts/ci/rauc-qemu-smoke.sh)
(`make ci-rauc-qemu`) soft-skips when `QUALIFY_QEMU_IMAGE` / `QEMU_LAB_DIR/disk.img`
is missing (exit 0). When the generic lab image is present it records a presence
(or optional live `rauc status` / full HIL) check for Track A tooling — still
**not** a Minewing power-loss signature.

CI substitutes and soft-skips **do not** close Track B (Minewing) hardware claims.

## Dry-run harness

```bash
OTA_HIL_ENV=dry-run OTA_HIL_SKIP_QUALIFY=1 ./scripts/hil/run-rauc-powerloss-hil.sh
```

Omit `OTA_HIL_SKIP_QUALIFY` to also run `make qualify` (up to five minutes).
Dry-run never sets `minewing_rauc_claimable`. Power-loss steps are documented in
each run’s `POWERLOSS_PROCEDURE.md`.

---
hero:
  eyebrow: HIL
  title: RAUC / power-loss HIL
---

Host `make qualify` never claims hardware rows. There are **two separate tracks**:

| Track | Compatible | Status |
|---|---|---|
| Generic QEMU lab | `zyvor-ota-qemu-lab` | **Complete** — see below |
| Minewing GW1 r1 | `minewing-gw1-r1` | **Unsigned** — needs BSP image or physical board |

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

Requires a **BSP** QEMU-bootable A/B image with `compatible=minewing-gw1-r1`
([`boards/minewing-gw1-r1/QEMU.md`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/QEMU.md)),
or a physical board. Then:

```bash
QUALIFY_QEMU_IMAGE=/path/to/minewing-ab.img \
OTA_HIL_ENV=qemu \   # or physical
OTA_HIL_MINEWING=1 \
OTA_HIL_COMPATIBLE=minewing-gw1-r1 \
OTA_HIL_SSH=root@guest \
OTA_HIL_BUNDLE=/path/to/os.raucb \
OTA_HIL_LOG_*=… \
OTA_HIL_SIGN=1 \
./scripts/hil/run-rauc-powerloss-hil.sh
```

`OTA_HIL_SIGN=1` updates
[`evidence/qualification/hardware-checklist.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hardware-checklist.md)
**only** when `minewing_rauc_claimable=true` (Minewing-compatible image + all required rows pass).

## GitHub CI (lab substitute)

When neither image is available, CI still runs
[`scripts/ci/lab-substitute.py`](https://github.com/zyvorai/ota/blob/main/scripts/ci/lab-substitute.py)
(`make ci-lab`, job `lab-substitute`):

| CI row | What it proves |
|---|---|
| `ci_agent_crash_during_install` | Daemon restart during `installing` → `needs_recovery` |
| `ci_bad_signature_reject` | Wrong signing key rejected before install |
| `ci_fleet_ref_https_commit` | HTTPS `zyvor-fleet-ref` → simulator `committed` |
| `ci_hil_harness_dry_run` | HIL evidence layout (never claimable) |

CI substitutes **do not** close Track A or Track B hardware claims.

## Dry-run harness

```bash
OTA_HIL_ENV=dry-run ./scripts/hil/run-rauc-powerloss-hil.sh
```

Power-loss steps are documented in each run’s `POWERLOSS_PROCEDURE.md`.

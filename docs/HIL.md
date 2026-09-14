---
hero:
  eyebrow: HIL
  title: RAUC / power-loss HIL — Minewing GW1 r1
---

Host `make qualify` never claims these rows. Use the runner after a real RAUC
image exists ([`boards/minewing-gw1-r1/QEMU.md`](../boards/minewing-gw1-r1/QEMU.md)).

## Runner

```bash
# Dry-run (harness + host qualify only):
OTA_HIL_ENV=dry-run ./scripts/hil/run-rauc-powerloss-hil.sh

# QEMU / physical once the image boots:
QUALIFY_QEMU_IMAGE=/path/to/minewing-ab.img \
OTA_HIL_ENV=qemu \
OTA_HIL_SSH=root@127.0.0.1 \
OTA_HIL_BUNDLE=/path/to/os.raucb \
OTA_HIL_ASSIGNMENT=/path/to/assignment.json \
OTA_HIL_LOG_COMMIT=/path/to/commit.log \
OTA_HIL_LOG_BADSIG=/path/to/badsig.log \
OTA_HIL_LOG_PLOSS_WRITE=/path/to/ploss-write.log \
OTA_HIL_LOG_PLOSS_BOOT=/path/to/ploss-boot.log \
OTA_HIL_LOG_NEEDS_RECOVERY=/path/to/needs-recovery.log \
OTA_HIL_LOG_REBOOTS=/path/to/reboots.log \
./scripts/hil/run-rauc-powerloss-hil.sh

# Sign checklist only when minewing_rauc_claimable=true:
OTA_HIL_SIGN=1 OTA_HIL_ENV=qemu ./scripts/hil/run-rauc-powerloss-hil.sh
```

Power-loss steps are documented in each run’s `POWERLOSS_PROCEDURE.md`.

# Power-loss procedure — Minewing GW1 r1 (qemu)

1. Boot slot A; capture `rauc status` and `zyvor-ota status`.
2. Start a valid signed install; when RAUC is writing the inactive slot,
   interrupt power (QEMU: kill -9 the qemu PID / `system_powerdown` mid-write).
3. Restore power; confirm **previous slot still boots** and agent does not
   auto-commit a half-written candidate.
4. Repeat for boot-selection change window (BOOT_ORDER / attempt counters).
5. Save console + `journalctl -u rauc -u zyvor-otad` to the env vars listed in SUMMARY.
6. Re-run this script with logs attached; then `OTA_HIL_SIGN=1` only if claimable.

See `boards/minewing-gw1-r1/QEMU.md` and `docs/QUALIFICATION.md`.

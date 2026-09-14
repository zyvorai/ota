# Hardware / QEMU qualification checklist — Minewing GW1 r1

**Status:** harness ready (`scripts/hil/run-rauc-powerloss-hil.sh` + `docs/HIL.md`);
**unsigned** until `QUALIFY_QEMU_IMAGE` + power-loss logs make
`minewing_rauc_claimable=true`. Dry-run evidence under
`evidence/qualification/hil/` is **not** a production claim.

| Field | Value |
|---|---|
| Operator | |
| Date (UTC) | |
| Image hash (rootfs A) | |
| Image hash (bundle under test) | |
| Device serial / QEMU ID | |
| Power supply notes | |
| RAUC version | |
| zyvor-ota version | |

## Results

Mark each row `pass` / `fail` / `blocked` and attach log paths.

| Test | Environment (qemu/physical) | Result | Log / evidence path |
|---|---|---|---|
| Valid bundle + healthy commit | | | |
| Wrong board or signature | | | |
| Wrong outer digest/size | | | |
| Invalid inner RAUC signature | | | |
| Network loss during download | | | |
| Power loss during target writes | | | |
| Power loss while boot selection changes | | | |
| Invalid kernel/DTB + watchdog | | | |
| Userspace health failure rollback | | | |
| Agent crash during install → NeedsRecovery | | | |
| Agent crash before/after mark-good | | | |
| Three ordinary healthy reboots | | | |
| Shared-data schema rollback | | | |
| Fleet outage through reboot | | | |
| Full or failing persistent storage | | | |
| systemd / D-Bus / polkit packaging | | | |

## Sign-off

I confirm these tests were executed on a recoverable lab device with a known
recovery image, and that failures (if any) are documented above.

- Name:
- Signature / commit SHA of signed notes:

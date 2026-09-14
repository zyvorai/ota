# QEMU RAUC lab image evidence

Built on `80.79.5.173` via `scripts/hil/build-qemu-rauc-lab.sh`.

| Artifact | Location on lab |
|---|---|
| Disk (4G) | `/home/sus/zyvor-qemu-lab/disk.img` |
| Signed bundle | `/home/sus/zyvor-qemu-lab/bundle.raucb` |
| Keys | `/home/sus/zyvor-qemu-lab/keys/` |

**Compatible:** `zyvor-ota-qemu-lab` — generic lab image, **not** Minewing GW1 r1 BSP.

## Completed (2026-09-14T192634Z)

- Guest SSH over KVM + `rauc status` (booted rootfs.A)
- Signed bundle install succeeded (`INSTALL_RC=0`)
- Bad signature rejected (`BAD_RC=1`)
- Mid-install `kill -9` qemu; previous slot still boots
- Three healthy guest reboots with `rauc status`
- Lab checklist signed: [`CHECKLIST.md`](CHECKLIST.md) (`qemu_lab_complete=true`)

**Still not claimed:** Minewing silicon (`minewing_rauc_claimable=false`). Needs BSP image or physical board + `OTA_HIL_MINEWING=1`.

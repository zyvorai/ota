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

## Disk copy checked 2026-09-21

The same generic image is at `/home/sus/zyvor-qemu-lab/disk.img` on `80.79.5.173` and at `$HOME/zyvor-qemu-lab/disk.img` on the operator machine. It is not in git and not a GitHub release asset.

| File | SHA-256 |
|---|---|
| `disk.img` | `7b8ce8ecbce8b43fce12b2571dfe79644115955ebea4fb5c54ccb0f09be31ed6` |
| `bundle.raucb` | `fd4317d1099a40ec51db9d8985ccb1e03f30d6270d564bb5b2c90565276d6e5a` |

`SHA256SUMS` in this directory is the earlier recorded disk line. The live disk hash above is the image after the 2026-09-14 lab run. The bundle hash is unchanged. `rauc-qemu-smoke` on the lab host reported the image present. It did not boot the guest again.

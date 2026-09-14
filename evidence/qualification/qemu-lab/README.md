# QEMU RAUC lab image evidence

Built on `80.79.5.173` via `scripts/hil/build-qemu-rauc-lab.sh`.

| Artifact | Location on lab |
|---|---|
| Disk (4G) | `/home/sus/zyvor-qemu-lab/disk.img` |
| Signed bundle | `/home/sus/zyvor-qemu-lab/bundle.raucb` |
| Keys | `/home/sus/zyvor-qemu-lab/keys/` |

**Compatible:** `zyvor-ota-qemu-lab` — generic lab image, **not** Minewing GW1 r1 BSP.

**Proven:** host `rauc info` verifies signed bundle; corrupted bundle fails CMS verify;
direct-kernel boot reached multi-user (TCG).

**Not proven / blocked:** guest `rauc status` over SSH (banner timeout under TCG without
KVM access for `sus`); full mid-install power-loss with slot boot selection; Minewing
silicon claim (`minewing_rauc_claimable=false`).

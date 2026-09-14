# QEMU RAUC lab image

- compatible: `zyvor-ota-qemu-lab` (generic lab — **not** Minewing GW1 r1 BSP)
- disk: `/home/sus/zyvor-qemu-lab/disk.img`
- rootA UUID: f2c87333-4a47-43f6-a959-613fdab07be2
- rootB UUID: 1c8392f8-2955-4010-8871-127a7d4dc656
- SSH: `ssh -p 2222 -i lab_ssh_key root@127.0.0.1` after `./run-qemu.sh`
- Bundle: `bundle.raucb`

Minewing silicon claims still require the board profile image from the BSP.

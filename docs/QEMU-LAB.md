---
hero:
  eyebrow: QEMU LAB
  title: Generic QEMU image recipe
  lead: Reproducible build steps for the signed generic lab. The disk is not a GitHub release asset.
---

The generic lab (`compatible=zyvor-ota-qemu-lab`) is already qualified. See [HIL.md](HIL.md) Track A and `evidence/qualification/qemu-lab/`. Minewing silicon is a different track and stays unsigned.

The disk image is large and depends on the host's debootstrap or image inputs, so the release workflow does **not** attach a prebuilt disk. Build it locally and record the checksum.

```sh
./scripts/hil/build-qemu-rauc-lab.sh /path/to/out
sha256sum /path/to/out/disk.img /path/to/out/*.raucb > qemu-lab-SHA256SUMS
```

Boot with the command printed by that script (KVM when available, TCG otherwise). Then:

```sh
./scripts/ota-demo up --profile qemu
```

If the image is absent, `--profile qemu` exits with the recipe above. It does not download an unverified disk and it does not set `minewing_rauc_claimable`.

CI job `rauc-qemu-smoke` only checks that a disk is present when `QUALIFY_QEMU_IMAGE` is set. A missing image is a skip, not a Minewing pass.

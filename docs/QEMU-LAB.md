---
hero:
  eyebrow: QEMU LAB
  title: Generic QEMU image recipe
  lead: Reproducible build steps for the signed generic lab. The disk is not a GitHub release asset.
---

The generic lab (`compatible=zyvor-ota-qemu-lab`) is already qualified. See [HIL.md](HIL.md) Track A and `evidence/qualification/qemu-lab/`. Minewing silicon is a different track and stays unsigned.

The disk image is large and depends on the host's debootstrap or image inputs, so the release workflow does **not** attach a prebuilt disk. A copy of the generic lab disk already exists outside git:

| Copy | Path |
|---|---|
| Lab host `80.79.5.173` | `/home/sus/zyvor-qemu-lab/disk.img` |
| Operator machine | `$HOME/zyvor-qemu-lab/disk.img` |

Checked 2026-09-21. `disk.img` is 4 GiB (`7b8ce8ecbce8b43fce12b2571dfe79644115955ebea4fb5c54ccb0f09be31ed6`). `bundle.raucb` is `fd4317d1099a40ec51db9d8985ccb1e03f30d6270d564bb5b2c90565276d6e5a`. The bundle hash matches the 2026-09-14 evidence file. The disk hash is the image after that lab run, so it does not match the older line in `evidence/qualification/qemu-lab/SHA256SUMS`. On the lab host, `QUALIFY_QEMU_IMAGE` pointing at that disk passed the presence check in `scripts/ci/rauc-qemu-smoke.sh`. That check does not boot the guest and does not sign Minewing.

Point the demo profile at the local copy. It still refuses to download a disk:

```sh
export QUALIFY_QEMU_IMAGE="$HOME/zyvor-qemu-lab/disk.img"
```

To rebuild from scratch, record the new checksum:

```sh
./scripts/hil/build-qemu-rauc-lab.sh /path/to/out
sha256sum /path/to/out/disk.img /path/to/out/*.raucb > qemu-lab-SHA256SUMS
```

Boot with the command printed by that script (KVM when available, TCG otherwise). Then:

```sh
./scripts/ota-demo up --profile qemu
```

If the image is absent, `--profile qemu` exits with the recipe above. It does not download an unverified disk and it does not set `minewing_rauc_claimable`.

CI job `rauc-qemu-smoke` checks `QUALIFY_QEMU_IMAGE` when set, otherwise `$HOME/zyvor-qemu-lab/disk.img`. A missing image is a skip, not a Minewing pass.

---
hero:
  eyebrow: BUILDROOT
  title: Buildroot integration
  lead: Same bake contract as Yocto. A template for a RAUC image, not a certified board.
---

Buildroot images that use RAUC follow [DEVICE-INTEGRATION.md](DEVICE-INTEGRATION.md). This page does not ship a defconfig and does not claim a board.

## Rootfs overlay

Build static binaries with `make dist`, then:

```sh
scripts/bake-rootfs-overlay.sh output/target
```

Run that from a `post-build.sh` after packages are installed. Install:

- `zyvor-otad` and `zyvor-ota` under `/usr/local/bin`
- `packaging/systemd/zyvor-otad.service`
- D-Bus and polkit examples only after you review them for your init and policy stack
- `/etc/zyvor-ota/agent.json` with the board `compatible`, pinned trust key, and download host allowlist

Keep `/var/lib/zyvor-ota` on storage that both A/B slots mount. The journal must not live on the rootfs that an update replaces.

## Bundle and enroll

Produce `rootfs.ext4` and the matching boot image from Buildroot, then `rauc bundle` as in [YOCTO.md](YOCTO.md). Enroll the device against Fleet or `zyvor-fleet-ref` using the [tutorial](TUTORIAL.md). Hardware rows stay unsigned until the checklist in `evidence/qualification/hardware-checklist.md` is filled on the real board.

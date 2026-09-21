---
hero:
  eyebrow: YOCTO
  title: Yocto integration
  lead: Bake the agent into a RAUC image. This is a template, not a certified BSP, and it does not sign Minewing silicon.
---

Use this with [DEVICE-INTEGRATION.md](DEVICE-INTEGRATION.md) and the Minewing profile at `boards/minewing-gw1-r1/`. The generic reference under `boards/reference/` is not a product image.

## What the image must already have

- Two rootfs slots plus grouped kernel/DTB, RAUC 1.13 or newer, and a bootloader attempt counter
- A persistent `/var/lib/zyvor-ota` mounted from both slots
- RAUC trust anchors and the OTA Ed25519 public key provisioned in the image
- No second updater and no unconditional mark-good service

## Bake the agent

From the OTA repository, after `make dist` or `make build`:

```sh
# Render PARTUUIDs from the BSP into RAUC config. See boards/minewing-gw1-r1/board.env.example.
scripts/bake-rootfs-overlay.sh /path/to/rootfs-work
```

Copy the overlay into the Yocto rootfs (a `ROOTFS_POSTPROCESS_COMMAND` or a small recipe that installs the two binaries, `packaging/systemd/zyvor-otad.service`, the D-Bus policy, and `/etc/zyvor-ota/agent.json`). Pin the `zyvor-ota` user UID/GID so both slots agree.

Do not invent a machine configuration here. Point `compatible` at the BSP machine (`minewing-gw1-r1` only when that board is actually being built).

## Bundle

```sh
rauc bundle --cert=release-cert.pem --key=release-key.pem bundle-input gateway.raucb
sha256sum gateway.raucb
```

Sign the OTA envelope separately with `otactl sign`. Both signatures are required. Qualification is [QUALIFICATION.md](QUALIFICATION.md); the generic QEMU lab does not close Minewing rows.

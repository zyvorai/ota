# Minewing GW1 revision r1

**Selected production SKU for Zyvor OTA v0.1 board bring-up.**

| Field | Value |
|---|---|
| SKU | `minewing-gw1` |
| Hardware revision | `r1` |
| RAUC / OTA `compatible` | `minewing-gw1-r1` |
| Target arch | `linux/arm64` |
| Bootloader contract | U-Boot + RAUC `BOOT_ORDER` / attempt counters |
| Update backend | `rauc` (RAUC ≥ 1.13 with D-Bus) |
| Persistent OTA state | `/var/lib/zyvor-ota` (shared across A/B) |
| Persistent RAUC data | `/var/lib/rauc` |

This directory is the board profile for image bake and qualification. It is
**not** a prebuilt flashable image. Partition PARTUUIDs come from the BSP
image build; copy `board.env.example` to `board.env`, fill UUIDs from the
factory layout, then render RAUC config with `scripts/render-board-rauc.sh`.

## Factory image requirements

1. Redundant rootfs A/B and boot A/B partitions with stable PARTUUIDs.
2. Shared persistent partitions mounted before `zyvor-otad` and `rauc`.
3. Hardware watchdog able to reset a hung candidate slot.
4. Recovery path (UART / known recovery image) documented by the BSP owner.
5. RAUC X.509 trust anchor at `/etc/rauc/trusted-root.pem`.
6. Ed25519 OTA public key(s) in `/etc/zyvor-ota/agent.json` (`trust_keys`).
7. Agent UID/GID `zyvor-ota` consistent across A and B images.

## Bake into a rootfs

From a built `dist/` (or `bin/` on the build host):

```sh
make dist
scripts/bake-rootfs-overlay.sh \
  --rootfs /path/to/rootfs \
  --board boards/minewing-gw1-r1 \
  --agent-config boards/minewing-gw1-r1/agent.json \
  --arch arm64
```

Then sign real `.raucb` bundles with the board's RAUC cert/key, fill release
metadata digests, and sign the OTA envelope with the offline Ed25519 key.

## Qualification

Software-simulatable rows: `make qualify`.
Hardware / QEMU RAUC rows: [docs/QUALIFICATION.md](../../docs/QUALIFICATION.md).

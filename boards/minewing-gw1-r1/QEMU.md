# QEMU path for Minewing GW1 r1

Run the [release qualification matrix](../../docs/QUALIFICATION.md) against a
**real RAUC image** in QEMU before physical boards. A mock D-Bus server or the
OTA simulator does **not** satisfy these rows.

## Prerequisites

- Board BSP produces a QEMU-bootable image with A/B slots and RAUC configured
  from this profile (`system.conf` rendered from `system.conf.in`).
- `rauc` ≥ 1.13 on the guest; `zyvor-otad` baked via `scripts/bake-rootfs-overlay.sh`.
- Lab signing keys (RAUC X.509 + Ed25519) provisioned only in the lab trust store.

## Suggested flow

1. Boot slot A; confirm `rauc status` and `zyvor-ota status`.
2. Serve a signed `.raucb` + OTA assignment (CLI submit or `zyvor-fleet-ref`).
3. Execute software-adjacent rows (bad signature, digest, network loss) in QEMU.
4. For power-loss rows, interrupt the QEMU process or cut emulated power at the
   documented flash / boot-selection points; prove the previous slot remains bootable.
5. Record image hashes, guest serial, commands, and logs under
   `evidence/qualification/` using the hardware checklist template.

`make qualify` covers **host software** matrix rows only. Point
`QUALIFY_QEMU_IMAGE=/path/to/image` when a board image exists; the qualify
script will refuse to claim hardware pass without an operator-signed checklist.

## Generic lab builder (not Minewing BSP)

For a **generic** x86_64 A/B + RAUC disk (`compatible=zyvor-ota-qemu-lab`), run
[`scripts/hil/build-qemu-rauc-lab.sh`](../../scripts/hil/build-qemu-rauc-lab.sh)
on a Linux amd64 host. That image is for lab crypto/boot bring-up evidence only —
it does **not** satisfy Minewing GW1 r1 hardware claims. See
[`evidence/qualification/qemu-lab/`](../../evidence/qualification/qemu-lab/).

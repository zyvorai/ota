# QEMU path for Minewing GW1 r1

This file is the **Minewing BSP** QEMU track only. For the completed **generic**
lab image (`zyvor-ota-qemu-lab`), see [docs/HIL.md](../../docs/HIL.md) Track A
and [`evidence/qualification/qemu-lab/`](../../evidence/qualification/qemu-lab/).

Run the [release qualification matrix](../../docs/QUALIFICATION.md) Minewing rows
against a **BSP-produced** RAUC image in QEMU before physical boards. A mock
D-Bus server, the OTA simulator, and the generic qemu-lab disk do **not**
satisfy Minewing claimable rows.

## Prerequisites

- Board BSP produces a QEMU-bootable image with A/B slots and RAUC configured
  from this profile (`system.conf` rendered from `system.conf.in`).
- `compatible=minewing-gw1-r1` on guest and bundles.
- `rauc` ≥ 1.13 on the guest; `zyvor-otad` baked via `scripts/bake-rootfs-overlay.sh`.
- Lab signing keys (RAUC X.509 + Ed25519) provisioned only in the lab trust store.

## Suggested flow

1. Boot slot A; confirm `rauc status` and `zyvor-ota status`.
2. Serve a signed `.raucb` + OTA assignment (CLI submit or `zyvor-fleet-ref`).
3. Execute software-adjacent rows (bad signature, digest, network loss) in QEMU.
4. For power-loss rows, interrupt the QEMU process or cut emulated power at the
   documented flash / boot-selection points; prove the previous slot remains bootable.
5. Record image hashes, guest serial, commands, and logs under
   `evidence/qualification/` and sign with `OTA_HIL_MINEWING=1` only when
   `minewing_rauc_claimable=true`.

`make qualify` covers **host software** matrix rows only.

## Generic lab builder (not this SKU)

```bash
./scripts/hil/build-qemu-rauc-lab.sh /path/to/out
```

Produces `compatible=zyvor-ota-qemu-lab`. Useful for RAUC crypto/install/power-loss
bring-up on x86_64 KVM. **Does not** close Minewing hardware-checklist rows.

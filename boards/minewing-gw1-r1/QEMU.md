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
- `rauc` ≥ 1.13 on the guest; `otactl` + `zyvor-otad` baked via
  `scripts/bake-rootfs-overlay.sh` (`zyvor-ota` is installed as a compat alias).
- Lab signing keys (RAUC X.509 + Ed25519) provisioned only in the lab trust store.
- No prebuilt Minewing disk is in this repository or on GitHub Releases. Place
  the BSP image at a lab path such as `$HOME/zyvor-minewing-lab/minewing-ab.img`
  and export `QUALIFY_QEMU_IMAGE` to that path.

## Operator runbook (ordered)

Full Track B checklist (sign gates, log env vars): [docs/HIL.md](../../docs/HIL.md)
Track B. Summary:

1. Copy `board.env.example` → `board.env`; fill PARTUUIDs from the BSP layout.
2. `scripts/render-board-rauc.sh boards/minewing-gw1-r1 /path/to/etc/rauc`.
3. `make dist` then bake into the BSP rootfs:
   ```sh
   scripts/bake-rootfs-overlay.sh \
     --rootfs /path/to/rootfs \
     --board boards/minewing-gw1-r1 \
     --agent-config boards/minewing-gw1-r1/agent.json \
     --arch arm64
   ```
4. Boot slot A; confirm `rauc status` and `otactl status json`.
5. Serve a signed `.raucb` + OTA assignment (`otactl submit` or `zyvor-fleet-ref`).
6. Run software-adjacent rows (bad signature, digest, network loss) in QEMU.
7. For power-loss rows, interrupt QEMU or cut emulated power at the documented
   flash / boot-selection points; prove the previous slot remains bootable.
8. Attach logs via `OTA_HIL_LOG_*`, set `OTA_HIL_MINEWING=1`, and run
   `scripts/hil/run-rauc-powerloss-hil.sh`. Use `OTA_HIL_SIGN=1` **only** when
   `minewing_rauc_claimable=true`.

`make qualify` covers **host software** matrix rows only. Dry-run HIL never
claims Minewing.

## Generic lab builder (not this SKU)

```bash
./scripts/hil/build-qemu-rauc-lab.sh /path/to/out
```

Produces `compatible=zyvor-ota-qemu-lab`. Useful for RAUC crypto/install/power-loss
bring-up on x86_64 KVM. **Does not** close Minewing hardware-checklist rows.

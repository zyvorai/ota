---
hero:
  eyebrow: DEVICE INTEGRATION
  title: RAUC board integration and qualification
---

## Required board information

The production bring-up SKU for this repository is **Minewing GW1 revision r1**
(`compatible=minewing-gw1-r1`). See [`boards/minewing-gw1-r1/BOARD.md`](../boards/minewing-gw1-r1/BOARD.md).
Obtain its SoC, RAM/storage layout, BSP/build system, bootloader version, boot
environment storage, recovery connector, signed-boot settings, watchdog behavior,
and vendor firmware procedure. Do not infer these from ARM64 support alone.

Templates in `boards/reference` remain generic placeholders. The Minewing profile
uses `system.conf.in` / `manifest.raucm.in` rendered from `board.env` (PARTUUIDs
from the BSP). No script in this repository repartitions or flashes a device
automatically. Bake the agent with `scripts/bake-rootfs-overlay.sh`.

## Image layout

| Region | Purpose |
|---|---|
| Bootloader/environment | Provisioned by the board factory process |
| rootfs A + boot A | Current OS, kernel and DTB as a consistent slot group |
| rootfs B + boot B | Inactive update target |
| Persistent data | OTA journal/cache, RAUC status, application data |
| Recovery | Board-specific known recovery mechanism |

Both OS versions must mount the same persistent `/var/lib/zyvor-ota` before OTA
starts, and the same RAUC data directory before RAUC starts. Keep the agent account
UID/GID consistent across images. Provision enough space for one full bundle plus
partial download and reserve. The downloader reuses one partial rather than
keeping a second complete copy; GC retained bundles between rollout waves.

Use stable partition references qualified for the board. Bind the rootfs and boot
images using RAUC's `parent` slot setting. OTA checks rootfs slot identity while
RAUC handles the group. The package must use a unique bundle version, identical
to the outer release `version`. Reusing a version across images is rejected.

## RAUC service and bootloader

Target RAUC 1.13 or newer with D-Bus enabled; qualify the actual vendor version.
Set trusted bundle certificates in the system keyring. Leave compatible checking
and signature checking enabled. Configure activation of the installed slot.
Disable alternative RAUC pollers/updaters and unconditional mark-good services:
OTA must be the sole update requester and health confirmation owner.

For U-Boot, implement the RAUC boot-selection contract (`BOOT_ORDER`, slot attempt
counters) and decrement attempts before boot. Prove that environment writes and
slot selection remain recoverable under power loss. The precise boot script and
environment offsets depend on the board and are intentionally not invented here.

Configure a hardware watchdog able to reset an unbootable/hung candidate. Set
timeouts long enough for expected boot and filesystem recovery, but bounded.
Initial factory image boot confirmation must be handled by the factory image
until the first OTA transaction establishes known versions in the journal.

## Bundle production

From your Yocto/Buildroot/vendor BSP build, obtain `rootfs.ext4` and a boot partition
image containing the matching kernel/DTB. Place them with `manifest.raucm` in a
bundle-input directory, then use RAUC's native builder:

```sh
rauc bundle --cert=release-cert.pem --key=release-key.pem bundle-input os.raucb
rauc info --keyring=trusted-root.pem os.raucb
sha256sum os.raucb
stat -c %s os.raucb
```

Fill the OTA release metadata with that digest, size, compatible and version;
sign it with `zyvor-ota sign`. The Ed25519 envelope authenticates the OTA policy
metadata and artifact digest. RAUC independently verifies the `.raucb` signature
against its X.509 trust anchor at install time. Both checks are required.

## Install the agent

Create a dedicated system account `zyvor-ota`. Prefer baking with
`scripts/bake-rootfs-overlay.sh` into the rootfs, or install equivalently: the
two built executables under `/usr/local/bin`, the config under
`/etc/zyvor-ota/agent.json`, and the unit under `/etc/systemd/system/zyvor-otad.service`.
Provision the supplied D-Bus and polkit policy examples through your image build
after review against distro policy. The daemon has no direct block-device
permission; RAUC runs separately as root.

```sh
systemctl daemon-reload
systemctl enable --now zyvor-otad.service
journalctl -u zyvor-otad.service -f
```

The policy files were not installed in the build environment. Qualify them on the
actual distribution. Socket access is restricted to the agent UID and root.

## Release qualification matrix

Record image hashes, firmware versions, device serial, power supply and test logs.
Each destructive test uses a recoverable lab device with a known recovery image.
Host software rows are automated by `make qualify` (see [QUALIFICATION.md](QUALIFICATION.md)).
Hardware / QEMU RAUC rows use `evidence/qualification/hardware-checklist.md`.

| Test | Required outcome |
|---|---|
| Valid bundle and healthy services | New slot boots, passes health, commits |
| Wrong board or signature | Reject without slot writes |
| Wrong outer digest/size | Reject before RAUC request |
| Invalid inner RAUC signature | RAUC rejects; operator inspects recovery state |
| Network loss during download | Resume or restart transfer, reverify full hash |
| Power loss during target writes | Previous slot remains bootable |
| Power loss while boot selection changes | At least one bootable, consistent group |
| Invalid kernel/DTB | Bounded boot attempts and watchdog return to previous OS |
| Userspace health failure | Previous slot activated and rebooted |
| Agent crash during install | NeedsRecovery; no automatic second install |
| Agent crash before/after mark-good | Reconcile without reinstalling |
| Three ordinary healthy reboots | Current slot stays bootable and counters reset |
| Shared-data schema rollback | Previous OS can still read its required data |
| Fleet outage through reboot | Local health/rollback works; events retained |
| Full or failing persistent storage | No silent journal reset or false commit |

Run these in QEMU with a real RAUC image first, then on the exact physical board.
A mock D-Bus server and simulator do not substitute for these tests.

## Primary references

- [RAUC integration](https://rauc.readthedocs.io/en/latest/integration.html)
- [RAUC D-Bus and configuration reference](https://rauc.readthedocs.io/en/latest/reference.html)
- [RAUC bundle usage](https://rauc.readthedocs.io/en/latest/using.html)
- [Cosign verification](https://docs.sigstore.dev/cosign/verifying/verify/)

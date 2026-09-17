---
hero:
  eyebrow: QUALIFICATION
  title: Production qualification matrix
---

Software rows are automated by `make qualify`. Real-RAUC rows split into two tracks
([HIL.md](HIL.md)):

| Track | Environment | Evidence | Status |
|---|---|---|---|
| Generic QEMU lab (`zyvor-ota-qemu-lab`) | **QEMU guest** (KVM/TCG), not silicon | [`qemu-lab/CHECKLIST.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/qemu-lab/CHECKLIST.md), HIL `20260914T192634Z` (`qemu_lab_complete=true`) | **Complete** (lab image only) |
| Minewing GW1 r1 (`minewing-gw1-r1`) | **BSP QEMU or physical silicon** | [`hardware-checklist.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hardware-checklist.md) via `OTA_HIL_MINEWING=1` | **Unsigned** — power-loss not CI-signed |

When neither image is available, GitHub CI job **`lab-substitute`**
(`make ci-lab`) still proves agent crash→NeedsRecovery, bad signature reject,
HTTPS fleet-ref commit, and HIL harness dry-run. Soft-smoke
[`scripts/ci/rauc-qemu-smoke.sh`](https://github.com/zyvorai/ota/blob/main/scripts/ci/rauc-qemu-smoke.sh)
(`make ci-rauc-qemu`) exit-0 skips when the QEMU disk is absent. Those CI rows
never claim Track B Minewing power-loss, and do not re-sign Track A.

A multi-product **simulator** stack on a shared Linux host is documented in
[LAB.md](LAB.md). That complements the software matrix; it does **not** close
Minewing silicon.

## Board under qualification

| Field | Value |
|---|---|
| SKU / revision | Minewing GW1 / r1 |
| Profile | [`boards/minewing-gw1-r1`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/BOARD.md) |
| Compatible | `minewing-gw1-r1` |

## Software (host) rows — `make qualify`

| ID | Expected |
|---|---|
| `unit_protocol_fleetref` | Go unit/protocol + Fleet reference server tests pass |
| `simulator_e2e_commit_rollback_replay` | Real daemon/CLI simulator E2E passes |
| `render_rauc_system_conf` | Board PARTUUID render produces `minewing-gw1-r1` system.conf |
| `bake_rootfs_overlay_minewing` | Overlay bake installs agent + packaging into a rootfs tree |
| `release_schema_json_parse` / `openapi_yaml_parse` | API schemas parse |

These prove agent, Fleet contract client, and bake tooling. They **do not** prove
RAUC flash, bootloader attempt counters, or power-loss recovery.

## Cross-product lab rows (optional evidence)

Run after each product's `deploy-remote.sh` (see [LAB.md](LAB.md)):

| ID | Expected |
|---|---|
| `remote_smoke_deploy` | `scripts/deploy-remote.sh` selftest 5/5 on the lab host |
| `fleet_ota_signed_commit` | Fleet `PUT` assignment → agent `committed` (simulator) |
| `fleet_ota_event_ack` | Contiguous ACK drains agent `pending_events` to 0 |
| `device_agent_nodra_mqtt` | Device Agent `nodra_connected=true` against `nodrad` |
| `fleet_agent_inventory_merge` | Site metadata includes `zyvor.device_agent.*` |

Record host, date, and job IDs next to `evidence/qualification/` when you
repeat the lab on a new machine.

## Generic QEMU lab rows — signed (not Minewing)

Completed for `compatible=zyvor-ota-qemu-lab` on 2026-09-14: live `rauc status`,
signed install, bad-signature reject, mid-install power-loss, three healthy
reboots. See [HIL.md](HIL.md) Track A. This does **not** sign Minewing.

CI soft-smoke (`make ci-rauc-qemu` / `scripts/ci/rauc-qemu-smoke.sh`) only checks
image presence (or optional live SSH) when a disk is available; missing images
soft-skip. It does **not** re-sign Track A or claim Minewing power-loss.

## Minewing / physical rows — operator signed

Run first with a **Minewing BSP** QEMU image
([`boards/minewing-gw1-r1/QEMU.md`](https://github.com/zyvorai/ota/blob/main/boards/minewing-gw1-r1/QEMU.md)),
then on the exact physical board. Fill
[`hardware-checklist.md`](https://github.com/zyvorai/ota/blob/main/evidence/qualification/hardware-checklist.md)
only when `minewing_rauc_claimable=true`. **Minewing power-loss is not CI-
automated and is not signed by lab-substitute or rauc-qemu-smoke.**

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
| Packaging (systemd / D-Bus / polkit) | Qualified on the target distribution |

A mock D-Bus server and the OTA simulator do **not** substitute for these tests.

## Fleet lab integration

**Preferred:** Zyvor Fleet control plane with TLS
([FLEET.md](FLEET.md), Fleet `docs/OTA_CONTRACT.md`, [LAB.md](LAB.md)).

**Bring-up without Fleet:**

```sh
make build
bin/zyvor-fleet-ref -token lab-secret -devices minewing-gw1-lab-001 \
  -assignment /path/to/assignment.json
```

Point `fleet_url` / `fleet_token_file` / `fleet_ca` in the agent config at that
server. Unit coverage lives in `internal/fleetref` and is run by `make qualify`.

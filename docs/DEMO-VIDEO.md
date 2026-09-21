---
hero:
  eyebrow: DEMO
  title: Five-minute demo script
  lead: Record this against scripts/ota-demo. Do not publish a generated stand-in.
---

| Time | On screen | Caption |
|---|---|---|
| 0:00 | Terminal, repository root | Safest open OTA runtime for Linux edge fleets |
| 0:20 | `./scripts/ota-demo up` | Local Fleet reference, one enrolled simulator device |
| 1:20 | Healthy release commits | Signed update, reboot, health, commit |
| 2:30 | Broken health probe | Automatic rollback to the previous slot |
| 3:30 | Interrupt the daemon mid-download | Resume, or NeedsRecovery if the outcome is ambiguous |
| 4:20 | Printed report path | Downloadable proof of what the device did |
| 4:50 | Qualification page | Minewing silicon is still unsigned |

Say explicitly: Fleet chooses the devices; the agent verifies and installs; this demo is the simulator unless `--profile qemu` was used with a locally built disk.

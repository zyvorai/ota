<div align="center">
<img src="docs/assets/readme/zyvor-logo.svg" alt="Zyvor" width="72" height="72"/>

# Zyvor OTA

### Signed, recoverable device OS updates for the Zyvor Platform.

[Tutorial](docs/TUTORIAL.md) · [User Guide](docs/USER-GUIDE.md) · [Architecture](docs/ARCHITECTURE.md) · [Operations](docs/OPERATIONS.md)

[![CI](https://github.com/zyvorai/ota/actions/workflows/ci.yml/badge.svg)](https://github.com/zyvorai/ota/actions/workflows/ci.yml)
[![Release](https://github.com/zyvorai/ota/actions/workflows/release.yml/badge.svg)](https://github.com/zyvorai/ota/actions/workflows/release.yml)
[![Go Reference](https://img.shields.io/badge/go-1.27.1-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

<br/>

<img src="docs/assets/readme/ota-lifecycle.svg" alt="Fleet assigns a signed release; the agent verifies it, installs to the inactive A/B slot, reboots, checks health, then commits or rolls back automatically. An ambiguous crash instead leaves the job needing operator recovery." width="880"/>

</div>

**v0.1.0 — engineering preview, Apache-2.0.** Working Go agent and CLI, native
RAUC D-Bus adapter, simulator, automated tests, and CI lab-substitutes.
**Generic QEMU RAUC lab HIL is complete** (`zyvor-ota-qemu-lab`); **Minewing
silicon qualification remains a separate, unsigned gate**. See
[verification evidence](docs/TEST-REPORT.md) and [HIL.md](docs/HIL.md).

Fleet decides **which devices and when**. OTA verifies and installs a device OS
release. RAUC writes the inactive slot and integrates with the board bootloader.

## Is this for you?

Zyvor OTA is a small, single-purpose, open-source (Apache-2.0) **device-side**
update agent: it verifies a signed release and installs it via RAUC's A/B slot
mechanism, with automatic health-check-gated rollback. It is not a fleet
dashboard, not a targeting/rollout controller, and not a general configuration
manager — Zyvor Fleet (or whatever system you point it at) decides which
devices get which release and when; OTA only ever verifies and installs.

| | **Zyvor OTA** | Mender | SWUpdate | balena | Roll-your-own (dpkg/apt + scripts) |
|---|---|---|---|---|---|
| Scope | Signed A/B install agent only | Agent + optional management server | Embedded update framework (daemon only) | Full fleet + container-based OS update, via balenaCloud | Whatever you script |
| Update mechanism | RAUC-managed A/B slots | A/B or single-partition | A/B, single-copy, or custom | balenaOS + container layer swap | Package manager, no atomicity guarantee |
| Rollback | Automatic, health-check gated, with an explicit `NeedsRecovery` interlock for ambiguous crashes | Automatic (A/B mode) | Depends on integration | Automatic (balenaOS) | Usually none |
| License | Apache-2.0 | Apache-2.0 core + commercial Enterprise | GPL-2.0 | Apache-2.0 agent + proprietary balenaCloud | N/A |
| Fleet/targeting | Separate concern — Zyvor Fleet implements `docs/FLEET.md` (`/v1/devices` + `/api/v1/ota`); lab stack in `docs/LAB.md` | Mender server (open or hosted) | Not included — typically paired with hawkBit or a custom backend | balenaCloud (proprietary) | You build it |

*(General characterizations as of writing — verify current licensing/features
against each project's own docs. Eclipse hawkBit and Uptane are not included
as direct rows: hawkBit is primarily a fleet/backend component comparable to
Fleet+OTA combined, and Uptane is a security framework some of the above
implement, not a competing product — Zyvor OTA does not currently claim
Uptane conformance.)*

**Maturity, stated honestly**: **v0.1.0 engineering preview** — working agent,
CLI, RAUC adapter, simulator, CI, and **generic QEMU RAUC lab HIL**
([docs/HIL.md](docs/HIL.md) Track A). **Minewing GW1 r1 silicon** is still
unsigned ([docs/PRODUCTION.md](docs/PRODUCTION.md)). If you need Minewing
board-qualified OTA today, evaluate accordingly.

New here? [`docs/FAQ.md`](docs/FAQ.md) covers licensing, support, and
production-readiness questions; [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)
covers real operational issues with their documented fix.

## Contents

- [Is this for you?](#is-this-for-you)
- [FAQ](docs/FAQ.md) · [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Lab stack (Fleet + OTA + Device Agent)](docs/LAB.md)
- [Tutorial: your first simulator update](docs/TUTORIAL.md)
- [User guide: CLI and config reference](docs/USER-GUIDE.md)
- [Run the complete simulator demonstration](#run-the-complete-simulator-demonstration)
- [Implemented](#implemented)
- [Scope and support matrix](#scope-and-support-matrix)
- [Device deployment](#device-deployment)
- [Remote smoke-deploy](#remote-smoke-deploy)
- [Repository guide](#repository-guide)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Run the complete simulator demonstration

Requirements: Linux, Go 1.27.1 or a newer supported patch (Go 1.24 is the language minimum),
Python 3 for the demo, GCC for race tests, and `dbus-daemon` for protocol tests.

```sh
make build
make test
make race
make vet
make demo
```

The demo generates temporary signing keys, starts a local artifact server and
the **real daemon and CLI**, installs two simulated releases, restarts the daemon
before reboot, commits the first release, rolls back the unhealthy second release,
rejects replay, acknowledges events, and cleans the cache. It never writes disks,
calls system reboot, or contacts a Fleet deployment. Temporary keys are deleted.

Use `make dist` to cross-compile static Linux AMD64 and ARM64 executables.
An ARM64 build is not evidence of execution on ARM64 hardware.

## Implemented

- Ed25519-signed exact-byte release envelopes; pinned device trust keys.
- SHA-256 and signed artifact sizes, exact board/backend compatibility, expiry,
  monotonic release sequence, job idempotency, and maintenance windows.
- HTTPS downloads with host allowlists, safe redirect checks, bounded size,
  disk reserve, resumable ranges, full digest verification, and cache cleanup.
- Durable single-writer state and event outbox using fsync + atomic rename.
- RAUC `InstallBundle`, `Completed`, `GetSlotStatus`, `GetPrimary`, and `Mark`
  over D-Bus. No shell commands or arbitrary update scripts are accepted by OTA.
- A/B install, controlled reboot, local health window, mark-good and rollback.
- Recovery interlock when an installation's outcome is ambiguous after a crash.
- Health confirmation on later normal boots of known installed versions.
- File, systemd-service and loopback HTTP health probes.
- Unix-socket operator API, CLI, and Prometheus text metrics.
- Outbound Fleet contract with TLS, optional custom CA/mTLS, token-file auth,
  ordered event acknowledgment, retry by polling, and no inbound cloud port.
- systemd and policy examples, API schema, CI, release build/SBOM/Cosign workflow.

## Scope and support matrix

| Capability | Status |
|---|---|
| Simulator install/reboot/rollback | Implemented and software tested |
| RAUC D-Bus wire protocol | Implemented; tested against a test service on a real private bus |
| Real signed RAUC bundles on physical devices | Adapter implemented; board qualification required |
| Kernel/rootfs/device tree/BSP | Delivered together through a board-built RAUC OS bundle |
| SWUpdate `.swu` | Not implemented in v0.1 |
| Standalone model/config/container/MCU updates | Not implemented in v0.1 |
| Bootloader update | Not enabled by this project; requires a qualified board recovery design |
| Fleet integration | Contract and transport implemented; existing Fleet server needs matching endpoints |
| QEMU bootable image | Not included; board integration examples are templates |
| Hardware power-loss testing | Not performed in this build environment |

## Device deployment

Read [device integration](docs/DEVICE-INTEGRATION.md) before using the RAUC backend.
The initial image must already provide redundant slots, a working bootloader
fallback policy, a shared persistent OTA state directory, RAUC trust anchors,
and a recovery method. This project does not repartition existing devices.

1. Build a board image and real `.raucb` with the BSP's image build system
   (Minewing GW1 r1 profile: `boards/minewing-gw1-r1/`).
2. Give the RAUC bundle and signed OTA release the same unique version.
3. Provision the pinned Ed25519 public key and the RAUC X.509 trust anchor.
4. Configure `boards/minewing-gw1-r1/agent.json` (or `examples/agent.rauc.json`) for the board and artifact host.
5. Bake binaries/unit/D-Bus/polkit with `scripts/bake-rootfs-overlay.sh`, or install equivalently.
6. Start the agent and submit a signed assignment through its Unix socket or Fleet (`zyvor-fleet-ref` for lab).
7. Qualify reboot, rollback and power interruption per `docs/QUALIFICATION.md`.

The daemon defaults to the configured backend; the example development flow
explicitly uses `simulator`. RAUC requires `allow_device_writes: true` and at least
one local health check. Simulator releases cannot be installed by the RAUC backend.

```sh
zyvor-ota keygen ./release-keys
# Fill examples/release.json with the actual artifact URL, SHA-256, size and expiry.
zyvor-ota sign release.json release-keys/release.key production-1 envelope.json
# Embed envelope.json as the release field in assignment.json.
zyvor-ota -socket /run/zyvor-ota/agent.sock submit assignment.json
zyvor-ota -socket /run/zyvor-ota/agent.sock job job-001
zyvor-ota -socket /run/zyvor-ota/agent.sock reboot
```

Keep the signing private key off the device. Examples contain placeholders,
not working production credentials or pre-signed firmware.

## Remote smoke-deploy

`scripts/deploy-remote.sh` pushes a build to an arbitrary Linux host over SSH,
runs `make check && make demo` there, and installs it as a systemd service
running the simulator backend, then drives one real signed install/reboot/
commit job cycle against that live daemon. It never touches RAUC, D-Bus, or
real disks, and it does **not** replace the device deployment steps above —
see [`deploy/README.md`](deploy/README.md) for what it does and does not do.

```sh
scripts/deploy-remote.sh HOST USER --key
make deploy-remote H=HOST U=USER
```

## Repository guide

| Directory | Purpose |
|---|---|
| `cmd/` | Operator CLI and device daemon |
| `internal/ota/` | Engine, journal, verification, downloader, backends, API, Fleet |
| `api/` | OpenAPI and release JSON schema |
| `examples/` | Config/release/assignment templates |
| `boards/minewing-gw1-r1/` | Selected production SKU profile (Minewing GW1 r1), bake inputs |
| `boards/reference/` | Generic RAUC templates, not a certified image |
| `packaging/` | systemd, D-Bus and polkit policy examples |
| `deploy/` | Remote smoke-deploy tooling (simulator backend, not board deployment) |
| `scripts/` | Daemon E2E, qualify matrix, bake/render, checksums, remote deploy |
| `docs/` | Architecture, security, recovery, device qualification and test evidence |

See [architecture](docs/ARCHITECTURE.md), [Fleet contract](docs/FLEET.md),
[lab stack](docs/LAB.md), [operations](docs/OPERATIONS.md), the
[tutorial](docs/TUTORIAL.md), and the [user guide](docs/USER-GUIDE.md). There
is intentionally no second fleet dashboard or competing application rollout
controller in this repository.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): Apache-2.0 SPDX headers, board profiles
scoped to a tested SKU/revision pair, and required evidence for any change to
boot selection, slot grouping, signing or recovery behavior. Run `make check`,
`make demo`, and `make dist` before opening a pull request.

## Security

See [SECURITY.md](SECURITY.md) for the threat model this agent defends and how
to report a suspected vulnerability. Never post signing keys, device
credentials, or exploitable production details in a public issue.

## License

### Open source (Apache-2.0)

This repository is licensed under the [Apache License, Version 2.0](LICENSE).
You may use, modify, and run it for personal, lab, and commercial production
use at no charge, subject to Apache-2.0 (preserve notices / NOTICE where required).
Original Zyvor OTA source is Apache-2.0. Vendored dependencies retain their own licenses. RAUC is an external runtime dependency and is not relicensed by this project.

### Enterprise

Production support, SLAs, and Zyvor Enterprise products are licensed separately.
Contact [sales@zyvor.dev](mailto:sales@zyvor.dev) or see [zyvor.dev](https://zyvor.dev).

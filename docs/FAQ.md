---
hero:
  eyebrow: FAQ
  title: FAQ
---

Questions people evaluating Zyvor OTA actually ask, before they've decided
to adopt it. If you've already decided, [the tutorial](TUTORIAL.md) is a
better starting point.

## Licensing & cost

**Is it really free?** Yes. Apache-2.0 — use, modify, and run it for
personal, lab, and commercial production use at no charge, subject to
preserving notices (see [`NOTICE`](https://github.com/zyvorai/ota/blob/main/NOTICE)). RAUC (an external runtime
dependency) is not relicensed by this project — check its own license
separately. See the README's [License](https://github.com/zyvorai/ota#license) section.

**What does "Enterprise" mean here?** Operational leverage on top of the same
agent: Fleet campaign controls, SSO and approvals, hosted site-relay
operations, compliance evidence, certified board enablement, SLA and LTS.
Contact sales@zyvor.dev. The site relay client, rollback, signature
verification, the simulator, the QEMU lab recipe, and the basic Fleet
contract stay in this Apache-2.0 repository. Nothing here requires a paid
license to update a device safely.

## Community and enterprise

| Community (this repository) | Enterprise |
|---|---|
| Agent, CLI, RAUC backend, simulator, local API | Fleet dashboard and campaign orchestration |
| Signed releases, rollback, schema 2 payloads | SSO/RBAC, approvals, and immutable audit |
| QEMU reference recipe and the site relay client | Certified board enablement and fleet analytics |
| Offline campaign, journal backup, Prometheus metrics | Compliance packaging and premium integrations |
| Optional trust metadata (not a conformance claim) | SLA, LTS releases, and emergency response |
| Basic Fleet contract | Multi-tenancy and a policy engine |

## Support

**What if I find a bug?** Open a GitHub issue.

**What if I find a security vulnerability?** See [`SECURITY.md`](https://github.com/zyvorai/ota/blob/main/SECURITY.md)
for private reporting instructions and the documented threat model — note
its own text says private reporting requires the repository's private
vulnerability reporting feature to be enabled by the maintainers first.

## Production readiness

**Is this production-ready?** Be precise. **v0.2.0 — engineering preview** means:

- **Yes (software):** Go agent, `otactl` CLI (`zyvor-ota` alias), RAUC D-Bus
  adapter, simulator, CI (`lab-substitute`), and Fleet contract path — see
  [TEST-REPORT.md](TEST-REPORT.md) and [PRODUCTION.md](PRODUCTION.md).
- **Yes (generic QEMU lab):** real RAUC A/B install, bad-signature reject,
  mid-install power-loss, and three reboots for `zyvor-ota-qemu-lab` —
  [HIL.md](HIL.md) Track A (`qemu_lab_complete=true`).
- **No (Minewing board OS OTA):** physical / BSP Minewing HIL is still
  unsigned (`minewing_rauc_claimable=false`). Follow the Track B operator
  runbook in [HIL.md](HIL.md) when a BSP image or board is available. Do not
  deploy to Minewing production silicon on the strength of the qemu-lab track
  alone.

**Does it work with Zyvor Fleet today?** Yes — Zyvor Fleet (`zyvorai/fleet`)
implements the server side of [`docs/FLEET.md`](FLEET.md) (`/v1/devices/...`
and `/api/v1/ota/...`). See Fleet's `docs/OTA_CONTRACT.md`. OTA's client is
tested against that contract; a lab reference server (`zyvor-fleet-ref`) in
this repo is for bring-up without a full Fleet deploy. A recorded multi-product
lab (simulator OTA + Fleet TLS, non-demo) is in [`docs/LAB.md`](LAB.md). Always verify
your own Fleet version and TLS trust path before production.

## Security

**How are updates verified?** Ed25519-signed release envelopes with pinned
trust keys and SHA-256 content verification. RAUC is only handed a bundle
after that check. Optional `trust_dir` metadata adds a threshold root,
delegated target types, and snapshot/timestamp binding. That is not a
conformance claim, and this binary has no TPM driver. See
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md), [`docs/SUPPLY-CHAIN.md`](SUPPLY-CHAIN.md),
and [`SECURITY.md`](https://github.com/zyvorai/ota/blob/main/SECURITY.md).
The on-disk anti-replay high-water mark is not a TPM or RPMB counter.

**Can it run arbitrary update scripts?** No — "No shell commands or
arbitrary update scripts are accepted." Install is RAUC D-Bus calls only
(`InstallBundle`/`GetSlotStatus`/`Mark`).

**What happens if a device crashes mid-update?** A `NeedsRecovery`
interlock catches ambiguous post-crash states rather than guessing — see
[`docs/OPERATIONS.md`](OPERATIONS.md)'s "NeedsRecovery" section for the
exact recovery procedure. It is deliberately not fully automatic in the
ambiguous case.

## Hardware

**What boards are supported?** The bring-up SKU is **Minewing GW1 revision r1**
(`boards/minewing-gw1-r1/`). It is a profile for BSP bake and qualification, not
a prebuilt flashable image — see [`docs/DEVICE-INTEGRATION.md`](DEVICE-INTEGRATION.md)
and [`docs/QUALIFICATION.md`](QUALIFICATION.md). `boards/reference/` remains generic
templates. Build targets are Linux amd64/arm64; CI also executes the ARM64 CLI
under qemu-user. An arm64 build existing is not
evidence it's been run on arm64 hardware.

**Does it require RAUC specifically?** OS slot updates use the RAUC D-Bus adapter. There is no SWUpdate adapter. Schema 2 container, config, and model targets are verified file copies and do not add another install backend.

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

**What does "Enterprise" mean here?** Production support, SLAs, and
Zyvor's other commercial products are licensed separately from this
open-source agent. Contact sales@zyvor.dev. Nothing in this repository
requires it.

## Support

**What if I find a bug?** Open a GitHub issue.

**What if I find a security vulnerability?** See [`SECURITY.md`](https://github.com/zyvorai/ota/blob/main/SECURITY.md)
for private reporting instructions and the documented threat model — note
its own text says private reporting requires the repository's private
vulnerability reporting feature to be enabled by the maintainers first.

## Production readiness

**Is this production-ready?** Be precise about what "v0.1.0 — engineering
preview" means here: a working Go agent, CLI, native RAUC D-Bus adapter,
simulator, and automated tests exist and are exercised in CI (see
[`docs/TEST-REPORT.md`](TEST-REPORT.md) for exact coverage/results). What's
explicitly **not** done yet: physical board qualification — the README
states this is "a separate release gate," and `TEST-REPORT.md` itself lists
"Production qualification still required" items, including hardware
power-loss testing. Don't deploy to production hardware on the strength of
this README alone — read the test report and qualify your own board first.

**Does it work with Zyvor Fleet today?** [`docs/FLEET.md`](FLEET.md) is
explicit: it's "a proposed server contract implemented by the OTA client.
No existing Zyvor Fleet repository or API has been modified or assumed
compatible." OTA's client side of that contract is real and tested; whether
your Fleet deployment implements the server side is a separate integration
question you need to verify, not something to assume from this repo alone.

## Security

**How are updates verified?** Ed25519-signed release envelopes with pinned
trust keys and SHA-256 content verification — RAUC is only ever handed a
bundle after signature verification passes. See
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md) and [`SECURITY.md`](https://github.com/zyvorai/ota/blob/main/SECURITY.md)
for the full threat model, including explicit non-claims (e.g. the on-disk
anti-replay high-water mark is *not* a TPM/RPMB hardware counter — read the
threat model before assuming a stronger guarantee than is actually made).

**Can it run arbitrary update scripts?** No — "No shell commands or
arbitrary update scripts are accepted." Install is RAUC D-Bus calls only
(`InstallBundle`/`GetSlotStatus`/`Mark`).

**What happens if a device crashes mid-update?** A `NeedsRecovery`
interlock catches ambiguous post-crash states rather than guessing — see
[`docs/OPERATIONS.md`](OPERATIONS.md)'s "NeedsRecovery" section for the
exact recovery procedure. It is deliberately not fully automatic in the
ambiguous case.

## Hardware

**What boards are supported?** None are named/certified yet.
`boards/reference/` holds templates, explicitly documented as "not a
certified image" — see [`docs/DEVICE-INTEGRATION.md`](DEVICE-INTEGRATION.md)
for board bring-up/qualification steps you'd need to do yourself. Build
targets today are Linux amd64/arm64; an arm64 build existing is not
evidence it's been run on arm64 hardware.

**Does it require RAUC specifically?** Yes — the current adapter talks to
RAUC over D-Bus. There's no SWUpdate or other backend adapter in this
repository today.

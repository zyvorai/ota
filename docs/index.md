# Zyvor OTA

**Signed, recoverable device OS updates for the Zyvor Platform.**

**v0.1.0 — engineering preview, Apache-2.0.** Zyvor OTA is a small,
single-purpose, open-source **device-side** update agent: it verifies a
signed release and installs it via RAUC's A/B slot mechanism, with automatic
health-check-gated rollback. It is not a fleet dashboard, not a
targeting/rollout controller, and not a general configuration manager — Zyvor
Fleet (or whatever system you point it at) decides which devices get which
release and when; OTA only ever verifies and installs. This repository
contains a working Go agent and CLI, a native RAUC D-Bus adapter, an isolated
simulator, and automated tests — physical board qualification is a separate
release gate.

See the full [README on GitHub](https://github.com/zyvorai/ota) for the
project overview, comparison table, and license details.

## Start here

- [Tutorial](TUTORIAL.md) — walk the full simulator lifecycle one command at a time.
- [User Guide](USER-GUIDE.md) — the CLI, daemon, and `agent.json` reference.
- [Architecture](ARCHITECTURE.md) — invariants and the update lifecycle.
- [FAQ](FAQ.md) — licensing, support, production-readiness, and security questions people ask before adopting it.
- [Troubleshooting](TROUBLESHOOTING.md) — real operational issues and their documented fixes.

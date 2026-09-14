# Changelog

## Unreleased

- Document production maturity — software green, Minewing HIL still unsigned.

- GitHub CI `lab-substitute` job (verify downloads its artifact so `ci_*` qualify rows pass)
- CodeQL workflow for Go (`make ci-lab`): agent crash→NeedsRecovery,
  bad-signature reject, HTTPS fleet-ref commit, HIL harness dry-run — covers
  lab-blocked rows without claiming Minewing RAUC/power-loss.

## 0.1.0 — 2026-09-14

First public engineering-preview release of Zyvor OTA.

- Signed release verify + RAUC A/B install agent (`zyvor-ota` / `zyvor-otad`)
- Simulator path with automated E2E; Fleet assignment/events contract
- Minewing GW1 r1 board profile + bake/render tooling
- `make qualify` software matrix; Cosign-signed release artifacts
- RAUC/power-loss HIL harness (`scripts/hil/`) — silicon sign-off still operator-gated
- Lab wiring docs (`docs/LAB.md`)

**Not claimed in this release:** physical Minewing RAUC/power-loss qualification.

# Changelog

## Unreleased

## 0.2.0 — 2026-09-22

Engineering preview. Generic QEMU RAUC lab HIL remains complete. Minewing GW1 r1
silicon stays unsigned. No Uptane or TPM claim.

- Primary operator CLI is `otactl`. `zyvor-ota` remains a byte-identical compat
  alias in `bin/`, `dist/`, packages, bake overlay, and remote smoke-deploy.
  Docs and lab scripts prefer `otactl`. Daemon stays `zyvor-otad`; paths stay
  under `/run/zyvor-ota` and `/etc/zyvor-ota`. CI asserts the alias match and
  that HIL dry-run never sets `minewing_rauc_claimable`.
- Minewing Track B operator runbook in [docs/HIL.md](docs/HIL.md) and
  [boards/minewing-gw1-r1/QEMU.md](boards/minewing-gw1-r1/QEMU.md): bake → HIL →
  sign. No prebuilt Minewing disk; checklist stays unsigned until a claimable
  operator run.
- CI lab-substitute crash path mutates the SQLite `ota.db` snapshot (not legacy
  `state.json`) so install→NeedsRecovery and fleet-ref commit stay green.
- Document schema 2 file-copy targets, campaign export and import, and the checked generic QEMU disk path. SWUpdate, MCU handlers, Minewing silicon, and a TPM driver stay out.
- Record the generic QEMU lab disk copy at `$HOME/zyvor-qemu-lab/disk.img` and on lab host `80.79.5.173`, with the 2026-09-21 checksum. The image stays out of git and off the GitHub release. Minewing stays unsigned.
- SQLite journal with terminal-job archive, `otactl backup`, and `otactl archive`. A legacy `state.json` is imported once.
- Schema 2 targets (`os.rauc`, `container.oci`, `config.bundle`, `model.oci`) commit or roll back as one set. No shell handlers. MCU firmware is not implemented.
- Offline campaign export/import, local media, download window, bandwidth cap, jitter, and a site relay client.
- Optional `trust_dir` metadata: threshold root, delegated target types, snapshot and timestamp binding, signed SBOM, reject list, and an append-only release log. No TPM driver and no conformance claim. A release that requires measured boot is refused unless a quote checker is linked.
- Operator commands retry a short `engine busy` response. The smoke deploy waits for the demo socket as root.
- Docs aligned with the journal, schema 2 payloads, relay, and optional trust metadata. The 12 September test census in [docs/TEST-REPORT.md](docs/TEST-REPORT.md) is unchanged.
- `make help`, `make ci` (gofmt, vet, race, build), and `make status`.
- CI soft-smoke for generic QEMU RAUC lab: `scripts/ci/rauc-qemu-smoke.sh`
  (`make ci-rauc-qemu`) — exit 0 soft-skip when the QEMU disk is absent;
  does **not** sign Minewing silicon power-loss (see `docs/HIL.md`).
- Docs refresh: HIL/QUALIFICATION/LAB/TEST-REPORT/FAQ/README split generic
  `zyvor-ota-qemu-lab` (complete) from Minewing silicon (unsigned); lab topology
  updated for Fleet non-demo, Nodra HTTPS, relay-edge auth.
- Complete generic QEMU RAUC lab HIL (KVM SSH, install, power-loss, reboots); Minewing still unsigned.
- Add QEMU RAUC lab image builder and record HIL evidence (Minewing still unsigned).
- Document production maturity — software green, Minewing HIL still unsigned.
- GitHub CI `lab-substitute` job (verify downloads its artifact so `ci_*` qualify rows pass)
- CodeQL workflow for Go (`make ci-lab`): agent crash→NeedsRecovery,
  bad-signature reject, HTTPS fleet-ref commit, HIL harness dry-run — covers
  lab-blocked rows without claiming Minewing RAUC/power-loss.

**Not claimed in this release:** physical Minewing RAUC/power-loss qualification.

## 0.1.0 — 2026-09-14

First public engineering-preview release of Zyvor OTA.

- Signed release verify + RAUC A/B install agent (`zyvor-ota` / `zyvor-otad`)
- Simulator path with automated E2E; Fleet assignment/events contract
- Minewing GW1 r1 board profile + bake/render tooling
- `make qualify` software matrix; Cosign-signed release artifacts
- RAUC/power-loss HIL harness (`scripts/hil/`) — silicon sign-off still operator-gated
- Lab wiring docs (`docs/LAB.md`)

**Not claimed in this release:** physical Minewing RAUC/power-loss qualification.

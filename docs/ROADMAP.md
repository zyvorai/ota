---
hero:
  eyebrow: ROADMAP
  title: From engine to customer outcome
  lead: The agent verifies, installs, and rolls back. The trial, Fleet contract, schema 2 payloads, journal, relay, and optional trust metadata are in this repository. The operator CLI is otactl. Minewing silicon, the five-minute recording, and a TPM driver are not.
---

Positioning:

> The safest open OTA runtime for Linux edge fleets—signed updates, automatic rollback, offline operation, and verifiable deployment evidence without cloud lock-in.

Zyvor Fleet decides which devices and when. This repository verifies and installs. Rollback and signature checks stay in the Apache-2.0 agent. Enterprise is operational leverage, not withheld safety. This project does **not** claim Uptane conformance.

## Five features

1. **15-minute golden path** — `scripts/ota-demo up` on the simulator, plus a QEMU lab recipe. Minewing GW1 r1 physical qualification stays unsigned until an operator records Track B in [HIL.md](HIL.md).
2. **Safe rollout in Fleet** — canary then wave, pause thresholds, per-device timeline, guided `NeedsRecovery`. Policy is not reimplemented in the agent.
3. **Typed release graph** — schema 2 targets (`os.rauc`, `container.oci`, `config.bundle`, `model.oci`) commit together or return to the previous consistent set. No shell handlers.
4. **Supply chain** — optional threshold root, delegated target types, snapshot and timestamp binding, signed SBOM, and an append-only release log. Documented in [SUPPLY-CHAIN.md](SUPPLY-CHAIN.md). No TPM driver. This project does **not** claim Uptane conformance.
5. **Intermittent networks** — content-addressed cache, download window, bandwidth cap, offline campaign, USB or local media with the same digest check, and Zyvor Relay.

## 90-day exit criteria

| Phase | Delivery | Exit criterion |
|---|---|---|
| Weeks 1–3 | Reference path and one-command demo | A new engineer completes a signed update and rollback in about 15 minutes on the simulator or generic QEMU lab |
| Weeks 2–5 | Fleet OTA canary waves | 100 simulated devices complete a controlled wave, or the rollout pauses on the configured threshold |
| Weeks 4–7 | SQLite journal, retention, metrics | Long-running retention and a documented corruption restore pass without hand-editing state |
| Weeks 6–10 | Relay, offline campaigns, bandwidth | 100 clients share one upstream artifact download |
| Weeks 8–12 | Typed payloads and release DAG | An OS + payload release commits or rolls back as one set |
| After v1 | Threshold signing, TPM, delegated roles | Metadata checks and adversarial tests cover the root threshold, delegation, snapshot, timestamp, SBOM signature, and release log. No TPM driver and no conformance claim. |

## Not in this repository

- Uptane conformance, or any wording that implies it
- A TPM or secure-element quote driver. `requires_measured_boot` is refused unless a checker is linked
- Arbitrary root scripts or MCU firmware handlers
- Signing Minewing silicon from CI or from the generic QEMU lab
- A prebuilt Minewing BSP disk image
- A second cloud scheduler inside the agent

## Review

Pull requests should say whether they claim a capability on this page. If the code does not implement it, the claim does not land. A periodic readiness review compares `main` to this file: README wording, [QUALIFICATION.md](QUALIFICATION.md), Fleet OTA rollouts, the journal, and metrics. It does not auto-merge and it does not widen product scope.

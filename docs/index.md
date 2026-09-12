---
hero:
  eyebrow: ZYVOR OTA
  title: Signed, recoverable device OS updates
  lead: A small, single-purpose, open-source device-side agent that verifies a signed release and installs it via RAUC's A/B slot mechanism, with automatic health-check-gated rollback.
  swatches:
    - {label: "v0.1.0 · engineering preview"}
    - {label: "Apache-2.0"}
    - {label: "Ed25519 + SHA-256"}
    - {label: "RAUC A/B"}
  highlights:
    - {value: "2", label: "Bootable A/B rootfs slots per device", footnote: "1"}
    - {value: "71", label: "Passing Go test/subtest records, 0 failures", footnote: "2"}
    - {value: "10,000", label: "Retained jobs & outbox events, bounded by design", footnote: "3"}
    - {value: "0", label: "Shell commands accepted by the install path", footnote: "4"}
  hub_bands:
    - {icon: "📘", title: "Tutorial", description: "Walk the exact lifecycle make demo automates, one command at a time.", href: TUTORIAL.md}
    - {icon: "📖", title: "User Guide", description: "A single-page reference for the zyvor-ota CLI, the zyvor-otad daemon, and every agent.json field.", href: USER-GUIDE.md}
    - {icon: "🏗️", title: "Architecture", description: "The full job state machine and the invariants that guard it, from acceptance to commit or automatic rollback.", href: ARCHITECTURE.md}
    - {icon: "❓", title: "FAQ", description: "Licensing, support, and production-readiness questions people ask before adopting it.", href: FAQ.md}
    - {icon: "🛠️", title: "Troubleshooting", description: "Real operational issues, with the documented fix — not a generic checklist.", href: TROUBLESHOOTING.md}
footnotes:
  - {marker: "1", text: "Exactly two bootable rootfs slots are supported per device; kernel/DTB partitions are grouped under their rootfs in RAUC.", href: ARCHITECTURE.md, href_label: "See installation and boot."}
  - {marker: "2", text: "71 passing Go test/subtest records with the race detector (42 top-level tests plus 29 subtests), 0 failures, 1 skipped for an environment limitation.", href: TEST-REPORT.md, href_label: "See the verification report."}
  - {marker: "3", text: "10,000 retained jobs and 10,000 unacknowledged events; new jobs are rejected once the outbox exceeds 9,000 — no unacknowledged event is dropped to make space.", href: ARCHITECTURE.md, href_label: "See the persistence invariants."}
  - {marker: "4", text: "Install is RAUC D-Bus calls only (InstallBundle / GetSlotStatus / Mark) — no shell commands or arbitrary update scripts are accepted by OTA.", href: FAQ.md, href_label: "See the security FAQ."}
---

It is **not** a fleet dashboard, not a targeting/rollout controller, and not
a general configuration manager — Zyvor Fleet (or whatever system you point
it at) decides which devices get which release and when; OTA only ever
verifies and installs. This repository contains a working Go agent and CLI,
a native RAUC D-Bus adapter, an isolated simulator, and automated tests —
physical board qualification is a separate release gate.

See the full [README on GitHub](https://github.com/zyvorai/ota) for the
project overview, comparison table, and license details.

## Take a closer look

=== "Signing & verification"

    Every release is an Ed25519-signed envelope checked against a pinned
    device trust key. The artifact is verified by SHA-256 digest and size
    after download, and the verified cached bytes are hashed again
    immediately before installation — RAUC is only ever handed a bundle
    after signature verification passes.

=== "Install & rollback"

    RAUC owns the flash operation onto the inactive `rootfs` slot. Health
    checks run after reboot; a healthy boot commits, and a deadline exceeded
    or failed health check triggers automatic rollback to the previous slot.
    A crash that leaves the outcome ambiguous enters `NeedsRecovery` instead
    of guessing, and blocks further jobs until an operator resolves it.

=== "Fleet contract"

    A proposed HTTPS contract: Fleet delivers signed assignments and
    consumes acknowledged events. No existing Zyvor Fleet repository or API
    has been modified or assumed compatible — the client side of this
    contract is implemented and tested; your Fleet deployment's server side
    is a separate integration to verify.

## What's implemented

<div class="icon-badge-list" markdown="1">

- 🔐 Ed25519-signed release envelopes, pinned device trust keys
- 🧮 SHA-256 digest and size verification, re-checked before install
- 🔁 Automatic, health-check-gated rollback
- 🧯 `NeedsRecovery` interlock for ambiguous post-crash states
- 🗂️ Durable single-writer state and event outbox (fsync + atomic rename)
- 📊 Unix-socket operator API, CLI, and Prometheus text metrics

</div>

---
hero:
  eyebrow: ZYVOR OTA
  title: Signed, recoverable device OS updates
  lead: The safest open OTA runtime for Linux edge fleets — signed updates, automatic rollback, and verifiable evidence, without cloud lock-in.
  swatches:
    - {label: "v0.2.0 · engineering preview"}
    - {label: "Apache-2.0"}
    - {label: "Ed25519 + SHA-256"}
    - {label: "RAUC A/B"}
  highlights:
    - {value: "2", label: "Bootable A/B rootfs slots per device", footnote: "1"}
    - {value: "71", label: "Test records in the 12 September report", footnote: "2"}
    - {value: "10,000", label: "Hot jobs before terminal history is archived", footnote: "3"}
    - {value: "0", label: "Shell commands accepted by the install path", footnote: "4"}
  hub_bands:
    - {icon: "📘", title: "15-minute trial", description: "Enroll one device, ship a signed release, and watch commit or rollback.", href: TUTORIAL.md}
    - {icon: "🗺️", title: "Roadmap", description: "What the agent implements, and which hardware claims stay unsigned.", href: ROADMAP.md}
    - {icon: "📖", title: "User Guide", description: "A single-page reference for the otactl CLI (zyvor-ota alias), the zyvor-otad daemon, and every agent.json field.", href: USER-GUIDE.md}
    - {icon: "🏗️", title: "Architecture", description: "The full job state machine and the invariants that guard it, from acceptance to commit or automatic rollback.", href: ARCHITECTURE.md}
    - {icon: "❓", title: "FAQ", description: "Licensing, support, and production-readiness questions people ask before adopting it.", href: FAQ.md}
    - {icon: "🧪", title: "Lab stack", description: "Deploy and wire OTA with Fleet, Nodra, Device Agent, and relay-edge on one Linux host.", href: LAB.md}
    - {icon: "🛠️", title: "Troubleshooting", description: "Real operational issues, with the documented fix — not a generic checklist.", href: TROUBLESHOOTING.md}
footnotes:
  - {marker: "1", text: "Exactly two bootable rootfs slots are supported per device; kernel/DTB partitions are grouped under their rootfs in RAUC.", href: ARCHITECTURE.md, href_label: "See installation and boot."}
  - {marker: "2", text: "The 12 September 2026 report counted 71 passing Go test/subtest records with the race detector. Later commits added journal, relay, campaign, and supply-chain tests. See the report for that run; do not treat 71 as a current census.", href: TEST-REPORT.md, href_label: "See the verification report."}
  - {marker: "3", text: "The SQLite journal keeps 10,000 hot jobs. Older terminal jobs are archived in the same transaction. Unacknowledged events are not dropped.", href: ARCHITECTURE.md, href_label: "See persistence."}
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

    HTTPS contract: Fleet delivers signed assignments and consumes
    acknowledged events. Zyvor Fleet implements the server APIs; this
    repository ships the client and a lab reference server
    (`zyvor-fleet-ref`). See [FLEET.md](FLEET.md) and [LAB.md](LAB.md).

## What's implemented

<div class="icon-badge-list" markdown="1">

- 🔐 Ed25519-signed release envelopes, pinned device trust keys
- 🧮 SHA-256 digest and size verification, re-checked before install
- 🔁 Automatic, health-check-gated rollback
- 🧯 `NeedsRecovery` interlock for ambiguous post-crash states
- 🗂️ Durable single-writer SQLite journal (WAL); terminal jobs archive, unacknowledged events stay
- 📦 Schema 2 targets for OS, container, config, and model bytes, committed or rolled back as one set
- 📡 Site relay, offline campaign, and local media, all checked by the same digest
- 📊 Unix-socket operator API, CLI, backup, archive, and Prometheus text metrics
- 🔏 Optional threshold root and delegated keys when `trust_dir` is set. Not a conformance claim

</div>

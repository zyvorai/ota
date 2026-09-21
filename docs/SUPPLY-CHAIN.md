---
hero:
  eyebrow: SUPPLY CHAIN
  title: Threat model for what is not v1
  lead: Ed25519 envelopes, pinned keys, and RAUC signatures are what the agent does today. Threshold signing, TPM identity, and delegated roles are specified here and not implemented. This is not an Uptane conformance claim.
---

## What the agent does now

- Rejects a release whose Ed25519 signature does not match a pinned trust key.
- Rejects the wrong board, an expired envelope, or a release sequence at or below the on-disk high-water mark.
- Checks artifact size and SHA-256 before install, including on the file:// and USB import path.
- Speaks TLS to Fleet. Artifact hosts must be on the allowlist. Relay peers must present the relay token.
- Does not run shell from release JSON. Typed payload handlers copy verified bytes into the agent state directory.
- Treats Cosign on the GitHub release as a checksum signature for the published binaries, not as trust for a RAUC bundle.
- Records an optional SBOM digest. It does not scan for vulnerabilities and does not refuse a release because of a CVE.

The on-disk sequence is not a TPM or RPMB counter. A root user who can edit `ota.db` can edit that counter. [SECURITY.md](https://github.com/zyvorai/ota/blob/main/SECURITY.md) is the operational policy.

## What large customers will ask, and what we will not pretend

| Question | v1 answer |
|---|---|
| What if the online signing key leaks? | You cannot rotate a root without a config or image update today. Threshold and offline root are post-v1. |
| Can a site sign only its own config? | No delegated role exists. One pinned key signs the envelope. |
| Does the device prove measured boot before a sensitive rollout? | No. |
| Is this Uptane? | No. |

## Post-v1 design, not a claim

These roles are the ones to implement later, mapped only loosely onto TUF/Uptane names so a later spec review has somewhere to start. Shipping them requires a threat-model review and adversarial tests. Until that file exists and those tests pass, documentation and the agent must not say "Uptane".

- **Root.** Offline keys, a threshold (for example 2 of 3) before the root statement changes. The device pins the current root and refuses a root update that does not meet the threshold.
- **Targets.** Says which artifact digests belong to a release. May be delegated per product, site, or customer so a site key cannot sign an OS bundle.
- **Snapshot.** Names the current targets and timestamp versions so a freeze of an old targets file is visible.
- **Timestamp.** Short-lived signature over the snapshot so a replay of last month's metadata expires.
- **Rotation and revocation.** A signed metadata update, not a new OS image, retires a targets key. Emergency revocation still needs a device that can fetch or import that metadata offline.
- **Device identity.** TPM 2.0 or a secure element holds a device key. Enrollment binds that key to the Fleet device id. The agent does not ship a TPM quote path now.
- **Measured boot.** A later gate can refuse a sensitive campaign when a measured-boot quote does not match a policy. It is not a substitute for the A/B health check.
- **Provenance.** SLSA attestations and a signed SBOM can be required as extra digests inside the envelope. The agent already has an optional `sbom_sha256`. Enforcing a signature over the SBOM is future work.
- **Transparency.** An append-only log of published releases, plus a per-release vulnerability policy. Neither log nor policy gate exists in this repository.

`internal/ota` does not contain a Uptane metadata state machine. Adaptive OS updates stay off unless a board profile sets `allow_adaptive`, and even then this project does not define its own delta format.

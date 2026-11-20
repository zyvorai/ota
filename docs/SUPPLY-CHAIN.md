---
hero:
  eyebrow: SUPPLY CHAIN
  title: Threat model, not a conformance claim
  lead: Ed25519 envelopes stay the release check. Optional trust metadata adds a threshold root, delegated keys, snapshot and timestamp binding, a signed SBOM, and an append-only release log. This is not a conformance claim.
---

## What the agent does now

- Rejects a release whose Ed25519 signature does not match a pinned trust key.
- Rejects the wrong board, an expired envelope, or a release sequence at or below the on-disk high-water mark.
- Checks artifact size and SHA-256 before install, including on the file:// and USB import path.
- When `trust_dir` and `root_sha256` are set, requires a threshold signature to change the root, rejects a root rollback, and rejects a delegated key that signs a target type outside its role. A timestamp must be unexpired and must match the snapshot. The snapshot must match the targets metadata. The release payload must be listed there.
- When a release pins `sbom_sha256`, requires an `sbom` artifact with that same digest and checks those bytes before install. A `sbom_signature` must verify with a pinned key. `require_sbom_signature` refuses a release that does not carry one. The agent does not scan for vulnerabilities. A digest listed in the signed targets `reject_sha256` list is refused.
- Records each accepted release payload in `transparency.log`. A broken chain, or the same digest under a second release id, stops the next accept.
- Speaks TLS to Fleet. Artifact hosts must be on the allowlist. Relay peers must present the relay token.
- Does not run shell from release JSON. Typed payload handlers copy verified bytes into the agent state directory.
- Treats Cosign on the GitHub release as a checksum signature for the published binaries, not as trust for a RAUC bundle.
- Refuses `requires_measured_boot` unless the process has a quote checker. This binary does not include a TPM driver, so a normal agent refuses that release.

The on-disk sequence is not a TPM or RPMB counter. A root user who can edit `ota.db` can edit that counter. [SECURITY.md](https://github.com/zyvorai/ota/blob/main/SECURITY.md) is the operational policy.

## What large customers will ask, and what we will not pretend

| Question | Answer |
|---|---|
| What if the online signing key leaks? | With `trust_dir` set, retiring a targets key is a root update signed by the root threshold. It does not require a new OS image. Without `trust_dir`, rotation is still a config or image update. |
| Can a site sign only its own config? | Yes, when the root delegation lists that key only for `config.bundle`. That key cannot sign `os.rauc`. |
| Does the device prove measured boot before a sensitive rollout? | Only if a quote checker is linked. This binary has none, and it refuses the release instead of skipping the check. |
| Is this Uptane? | No. |

## What is still not in this binary

- A TPM 2.0 or secure-element quote path. `requires_measured_boot` is a gate, not a driver.
- A vulnerability scanner. The reject list is an explicit set of digests in the signed targets metadata.
- Any conformance statement. The role names are local to this repository.

`internal/ota` does not contain a conformance metadata state machine. Adaptive OS updates stay off unless a board profile sets `allow_adaptive`, and even then this project does not define its own delta format.

# Security policy

This is an engineering preview. Report suspected vulnerabilities privately to
the Zyvor maintainers through the repository's private vulnerability reporting
feature once enabled. Do not post signing keys, device credentials or exploitable
production details in public issues. Repository owners must enable this feature
before inviting security reports; no security email address is invented here.

## Threat model

The agent rejects unauthorized/tampered artifacts, wrong-board releases, expired
metadata and replayed release sequences. TLS authenticates Fleet/artifact servers;
pinned Ed25519 public keys authenticate release policy; RAUC independently verifies
native bundle signatures. Host allowlists constrain artifact destinations.

Assume the release signer, root-owned configuration, local root account, kernel,
RAUC service and bootloader provisioning are trusted. A compromised root account
can alter the journal and the trust root. The on-disk high-water mark is not a
TPM or RPMB anti-rollback counter. This binary does not include a TPM driver.
A release that sets `requires_measured_boot` is refused unless a quote checker
is linked. Secure Boot, encrypted storage, and hardware-protected rollback
counters stay outside this release.

Keep signing keys offline or in a managed signer. Optional `trust_dir` metadata
can rotate a targets key with a threshold root signature; that is not a
conformance claim. Do not run arbitrary scripts from release JSON. Native RAUC
bundles may contain privileged hooks: the RAUC signer is therefore a highly
trusted authority and must review bundle contents.

The outer format is Ed25519, not a Cosign verification bundle. Cosign in the
release workflow signs the published executable checksums and the source SBOM
distribution. Do not assume a Cosign-signed binary makes a firmware bundle
trusted by RAUC.

Time-based expiry requires a trustworthy device clock. An SBOM digest, and a
signature over those bytes when one is present, binds the named document. It
does not establish that the described software is free of vulnerabilities. A
digest in the signed targets reject list is refused. No vulnerability-scan
result is claimed unless recorded in docs/TEST-REPORT.md.

Only expose the mode-0600 Unix socket to trusted operators. Simulation loopback
HTTP is a development-only interface, deliberately disallowed with RAUC.
Never expose it via reverse proxy. The simulator never calls real reboot/flash.

Use the latest supported Go patch release for shipped binaries and review
dependency and GitHub Actions updates. External RAUC, systemd and BSP packages
must follow the device vendor's security maintenance process.

---
hero:
  eyebrow: RELEASING
  title: Release process
---

1. Update the version constant, Makefile and source SBOM version together.
2. Use a current supported Go patch release; CI pins Go 1.27.1 for this revision.
3. Run formatting, tests/race/vet, simulator E2E and both architecture builds.
4. Run govulncheck and review RAUC/BSP image advisories separately.
5. Keep board qualification status explicit in the release notes.
6. Push a reviewed version tag when ready. The release workflow builds artifacts,
   generates a source-distribution SBOM, checksums them and signs SHA256SUMS with
   Cosign. It uploads a workflow artifact; it does not automatically publish a
   GitHub Release or deploy to devices.

After downloading the tagged workflow artifact, verify the checksum signature
against the exact repository/workflow/tag identity, then the file checksums:

```sh
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity https://github.com/zyvorai/ota/.github/workflows/release.yml@refs/tags/v0.1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS
sha256sum -c SHA256SUMS
```

Change the identity if the actual repository or tag differs. Never loosen the
identity to accept arbitrary workflow signers. The supplied archive has not gone
through GitHub's tag-signing workflow and does not pretend to contain its signature.

The included SPDX file inventories the source distribution and vendored module.
It is not a complete runtime SBOM for the statically linked Go executables or the
board OS. Generate the latter from the actual image/binary build with your SBOM
tooling, and bind the image SBOM's SHA-256 in the signed release metadata.

Actions are version-tagged; review updates and pin action SHAs according to the
organization's release policy before enabling privileged release automation.

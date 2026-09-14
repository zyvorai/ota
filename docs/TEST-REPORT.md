---
hero:
  eyebrow: TEST REPORT
  title: Verification report — Zyvor OTA v0.1.0
---

Recorded 12 September 2026 UTC. This report describes local verification of the
delivered source. It is not a hardware certification or a GitHub Actions run.

## Results

| Check | Result |
|---|---|
| Final toolchain | Go 1.27.1, Linux AMD64 |
| Go unit/protocol tests with race detector | 71 passing test/subtest records; 0 failures |
| Top-level tests | 42 passed; 1 skipped |
| Skipped test | `TestAPIOverRealUnixSocket`: runtime returns EPERM for Unix sockets |
| Core package statement coverage | 70.7% |
| `go vet ./...` | Passed, no diagnostics |
| Real daemon + CLI simulator E2E | Passed |
| Linux AMD64 CLI and daemon | Built and executed locally |
| Linux ARM64 CLI and daemon | Cross-compiled; not executed on ARM64 |
| `govulncheck` v1.8.0 | No vulnerabilities found in the Go application at scan time |
| Workflow/OpenAPI YAML and JSON syntax | Parsed successfully |
| Full OpenAPI/JSON-Schema conformance validation | Not run; validator not installed |
| Source SBOM and release checksums | Generated and included |
| Physical RAUC install / actual `.raucb` boot test | Not run |
| QEMU kernel/rootfs boot and hardware power interruption | Not run |
| Installed systemd/D-Bus/polkit policy behavior | Requires target distribution qualification |
| Existing Zyvor Fleet server integration | Lab reference (`zyvor-fleet-ref`) unit-tested; live Fleet TLS + OTA contract exercised on lab host (see [LAB.md](LAB.md)); hardware RAUC path still open |
| GitHub CI/release/Cosign workflow | Included; release workflow publishes a GitHub Release on tag |
| Software qualification matrix (`make qualify`) | Host software rows; see `evidence/qualification/` |
| ARM64 binary execution | Cross-build locally; CI runs CLI under qemu-user |

The 71 passing records count 42 top-level tests and 29 named subtests. This is not
a claim of 71 independent hardware tests. CLI/daemon main packages are exercised
by the separate process E2E script and are not instrumented in the unit coverage
profile. Whole-module unit coverage is consequently lower than core coverage;
see `evidence/coverage-functions.txt` for the exact report.

## What the tests establish

- Signed release policy rejects bad signatures, untrusted keys, wrong board or
  backend, expiration, invalid digest/size/sequence, unknown fields and trailing JSON.
- Concurrent/repeated assignments remain one job; conflicting IDs and replay fail.
- Maintenance windows and preinstall expiration are enforced.
- Downloads resume with validated Content-Range, restart when ranges are ignored,
  reject corruption/oversize/truncation/encoding surprises, obey disk reserve,
  reject untrusted redirects and production HTTP, and refuse partial-file symlinks.
- File locks prevent two store owners; state persists across reopen; invalid state
  fails startup; aborted transactions do not become visible; failed writes poison
  the store instead of reporting success.
- Healthy updates commit only after health checks. Failed health and boot fallback
  return to the previous slot. Crash during install blocks on NeedsRecovery.
- Agent restart before health and during commit does not reinstall the image.
- Reboot during commit forces fresh health checks. Normal boot confirmation runs
  health checks and does not bounce back into an already failed fallback slot.
- API parsing rejects oversized/invalid requests and incorrect methods.
- Fleet TLS rejects an untrusted server; delivery/acknowledgment works against test
  TLS servers; failed/invalid ACKs retain the outbox; signed assignments are accepted.
- The RAUC adapter serializes and exchanges real D-Bus messages for status,
  InstallBundle/Completed, Mark and reboot against a test service. Failure, timeout,
  busy and compatibility responses are tested. The isolated bus uses loopback TCP
  because this environment blocks AF_UNIX; production uses the system Unix bus.

The simulator E2E launches the actual executables, generates ephemeral signing
keys, signs metadata, serves an artifact, restarts the daemon before reboot,
commits one update, rolls back a failed-health update, rejects replay, acknowledges
events and cleans the artifact cache. All flash and boot actions in this test are
simulated file operations. No host reboot was requested.

## Evidence included

`evidence/go-test.jsonl`, `go-vet.txt`, `e2e.txt`, `build.txt`, `cross-build.txt`,
`govulncheck.txt`, `toolchain.txt`, `binary-formats.txt`, `coverage-functions.txt`,
and `schema-syntax.txt`. Empty `go-vet.txt` means the command emitted no diagnostics.

Run `make check`, `make demo`, and `make dist` to reproduce on standard Linux.
Standard Linux CI also runs the Unix-socket test rather than skipping it when
AF_UNIX is available. The tag workflow must be run on the actual repository to
produce an identity-bound Cosign signature; none is fabricated for this archive.

## Production qualification still required

Use `DEVICE-INTEGRATION.md` / `QUALIFICATION.md` on Minewing GW1 r1. Confirm
real native bundle verification, grouped kernel/rootfs/DTB installation, boot
attempt limits, watchdog recovery, shared-data rollback, power interruption,
and device-specific health probes. Sign
`evidence/qualification/hardware-checklist.md` before production. SWUpdate,
standalone MCU/model/config update backends, and a prebuilt QEMU board image
are not included in this v0.1 scope.

A multi-product **simulator** lab (Fleet TLS, Device Agent, Nodra, relay-edge)
was recorded on 14 September 2026 — see [LAB.md](LAB.md). That stack proves
wiring and the Fleet OTA contract against the simulator backend; it does not
close hardware rows.

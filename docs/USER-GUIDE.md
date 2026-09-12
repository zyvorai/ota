---
hero:
  eyebrow: USER GUIDE
  title: 'User guide: CLI and configuration reference'
---

A single-page reference for the `zyvor-ota` operator CLI, the `zyvor-otad`
daemon, and every `agent.json` field. For a narrated first run, see the
[Tutorial](TUTORIAL.md). For what each step in the lifecycle actually does,
see [Architecture](ARCHITECTURE.md); for incident procedures, see
[Operations](OPERATIONS.md).

## `zyvor-ota` CLI

Global flags: `-socket PATH` (default `/run/zyvor-ota/agent.sock`) and the
demo-only `-simulation-url http://127.0.0.1:PORT` (used by `make demo`, never
in production — see `cmd/zyvor-ota/main.go`).

| Command | Effect |
|---|---|
| `version` | Prints the build version. No socket needed. |
| `keygen DIR` | Generates an Ed25519 keypair. `DIR` must **not** already exist; writes `DIR/release.key` (0600) and `DIR/release.pub` (0644). |
| `sign RELEASE KEY KEY_ID OUT` | Signs the release JSON at `RELEASE` with the private key at `KEY`, tagged `KEY_ID`, and writes the signed envelope to `OUT`. |
| `status` | Current version, backend, device ID, the active job (or `null`), and event/sequence counters. |
| `job ID` | The full job record for `ID` — state, old/target slot, boot ID, timestamps. |
| `submit ASSIGNMENT` | Submits a signed assignment. Returns the created (or already-accepted) job. Rejects a replayed or downgraded release sequence. |
| `events` | Unacknowledged state-transition events, oldest first. `null` when there are none. |
| `ack SEQUENCE` | Acknowledges every event up to and including `SEQUENCE`, removing them locally. Rejects an ack past the latest event. |
| `reboot` | Requests reboot into the newly installed slot. Only valid while a job is `awaiting_reboot` and inside its assignment window. |
| `recover-abort` | Operator-only: marks an ambiguous install target `bad` and restores the original slot as next boot target. Only valid in `needs_recovery`, with the backend idle on the original slot. |
| `gc` | Deletes cached/partial artifact files. Refused while a job is active. |

## `zyvor-otad` daemon flags

| Flag | Default | Notes |
|---|---|---|
| `-config PATH` | `/etc/zyvor-ota/agent.json` | The `Config` described below. |
| `-simulation-listen ADDR` | (unset) | Demo-only loopback HTTP listener; requires `backend: simulator` and a `127.0.0.1` address. Never set this in production. |

## Job states

`accepted → downloading → verified → installing → awaiting_reboot →
checking_health → committing → committed`, with `rollback_pending →
rolled_back` when health fails or times out, and `needs_recovery` when a
crash makes the outcome ambiguous (see [Operations §NeedsRecovery](OPERATIONS.md#needsrecovery)).
`committed`, `rolled_back`, and `failed` are terminal — `gc` only runs when
no job is in a non-terminal state.

## Configuration reference (`agent.json`)

Every field below is validated by `Config.Validate()` in
`internal/ota/model.go` at daemon startup; an invalid config refuses to
start rather than run with a relaxed setting.

| Field | Type | Required | Constraint |
|---|---|---|---|
| `device_id` | string | yes | `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`; must match the assignment's `device_id`. |
| `compatible` | string | yes | Same pattern; must match the release's `compatible` and the backend's actual board/image compatibility string. |
| `state_dir` | string | yes | Absolute path. Single-writer journal directory — never tmpfs/NFS, never shared with another agent instance. |
| `socket` | string | yes | Absolute path to the operator Unix socket. |
| `backend` | string | yes | `simulator` or `rauc`. |
| `allow_device_writes` | bool | conditionally | Must be `true` when `backend: rauc`. |
| `trust_keys` | map[string]string | yes, ≥1 entry | `key_id → base64 Ed25519 public key`. A release's envelope `key_id` must match an entry here. |
| `download_hosts` | []string | yes, ≥1 entry | `host:port` allowlist, no scheme — the artifact URL's host must match exactly. |
| `max_artifact_bytes` | int64 | yes | `1` – `1<<40` (1 TiB). |
| `reserve_bytes` | uint64 | no | Disk space kept free during download; ≤ `1<<50`. |
| `health_timeout_seconds` | int | yes | `1` – `3600`. |
| `health_stable_seconds` | int | yes | `0` – (`health_timeout_seconds` − 1): how long health must hold continuously before commit. |
| `checks` | []Check | conditionally | `{"kind": "...", "target": "..."}`; required (≥1) when `backend: rauc`. See below. |
| `fleet_url` | string | no | Plain HTTPS base URL — no userinfo, query string, or fragment. |
| `fleet_token_file` | string | conditionally | Required when `fleet_url` is set, unless both `fleet_cert` and `fleet_key` are given (mTLS). |
| `fleet_ca` | string | no | Custom CA bundle path for the Fleet connection. |
| `fleet_cert` / `fleet_key` | string | conditionally | mTLS client cert/key pair; both required together. |

### Health check kinds

| `kind` | `target` must be |
|---|---|
| `file` | An absolute path; the check passes while the file exists. |
| `systemd` | A unit name matching the same identifier pattern as `device_id`. |
| `http` | A URL parsed as `http://127.0.0.1` or `http://[::1]`, no userinfo — loopback only. |

## Examples

`examples/agent.simulator.json` and `examples/agent.rauc.json` are complete,
runnable templates for each backend; `examples/release.json` and
`examples/assignment.json` are minimal release/assignment shapes. All contain
placeholder values — see the [Tutorial](TUTORIAL.md) for a config built and
run end to end.

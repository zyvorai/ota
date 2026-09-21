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
| `backup DEST` | Writes a `VACUUM INTO` copy of the journal to an absolute path that must not already exist. Stop the agent before replacing `ota.db` with that copy. |
| `archive` | Prints terminal jobs that have left the hot journal. Hot jobs are omitted. |

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
| `bandwidth_bytes_per_sec` | int64 | no | `0` means no cap. A positive value limits the artifact read. |
| `download_jitter_seconds` | int | no | `0`–`3600`. `0` starts the download on the next engine step. Otherwise the device waits `FNV(device_id) % (n+1)` seconds. |
| `download_window_start` / `download_window_end` | string | no | Both empty, or both `HH:MM` local time. Outside the window the job stays deferred. |
| `local_media_dir` | string | no | Absolute directory of `{sha256}.raucb` files for offline install. |
| `relay_url` | string | no | Site relay base URL. Empty skips the relay. HTTPS, or HTTP on `127.0.0.1` / `::1`, with an empty path. The agent asks the relay for the signed digest before the artifact URL. A miss falls through. A digest mismatch does not. |
| `relay_token_file` | string | conditionally | Bearer token for `relay_url`. Required when the URL is set. |
| `trust_dir` | string | no | Absolute directory of `root.json`, `timestamp.json`, `snapshot.json`, and `targets.json`. Empty keeps the single pinned release key. |
| `root_sha256` | string | conditionally | SHA-256 of the initial root payload. Required with `trust_dir`. Later root versions must meet the previous root's signature threshold. |
| `require_sbom_signature` | bool | no | Off unless set. A release must then carry `sbom_key_id` and `sbom_signature` over the SBOM bytes. |
| `allow_adaptive` | bool | no | Off unless set. Refuses a release that sets `adaptive` when this is false. |
| `capabilities` | []string | no | Capability names a schema 2 target may list in `requires`. |
| `fleet_url` | string | no | Plain **HTTPS** base URL — no userinfo, query string, or fragment. HTTP is rejected at config validation. Use `fleet_ca` (or the system trust store) for lab self-signed Fleet certs. |
| `fleet_token_file` | string | conditionally | Required when `fleet_url` is set, unless both `fleet_cert` and `fleet_key` are given (mTLS). Mode `0600`/`640`; readable by the agent user. |
| `fleet_ca` | string | no | Custom CA bundle path for the Fleet connection. |
| `fleet_cert` / `fleet_key` | string | conditionally | mTLS client cert/key pair; both required together. |
| `otlp_endpoint` | string | no | OpenTelemetry collector base URL. Empty disables traces. HTTPS, or HTTP on `127.0.0.1` / `::1`. Path must be empty or `/v1/traces`. Prometheus on `/metrics` stays available either way. |
| `otlp_token_file` | string | no | Bearer token for the collector. Requires `otlp_endpoint`. |

For a multi-product lab (Fleet TLS + simulator OTA), see [LAB.md](LAB.md).
For bring-up without Fleet, run `zyvor-fleet-ref` (see [QUALIFICATION.md](QUALIFICATION.md)).

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

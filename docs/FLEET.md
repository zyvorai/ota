---
hero:
  eyebrow: FLEET
  title: Fleet adapter contract v1
---

This is the HTTPS adapter contract between Zyvor OTA (client) and a Fleet
control plane (server).

**Zyvor Fleet** (`zyvorai/fleet`) implements this contract on the control
plane: `/v1/devices/{device_id}/assignment` and `/events`, with digest-bound
device tokens and operator APIs under `/api/v1/ota/...`. See Fleet's
`docs/OTA_CONTRACT.md`. A **lab reference server** also ships in this OTA
repository as `zyvor-fleet-ref` for bring-up without a full Fleet deployment.
A recorded multi-product lab is in [LAB.md](LAB.md).

All requests use HTTPS to the configured Fleet base URL. Authentication is a
token read from a local file at startup, mTLS, or both. Redirects are rejected.
Device IDs must be provisioned and bound to authenticated identity by the server;
the server must not authorize a device merely because its ID appears in the path.

## Receive work

`GET /v1/devices/{device_id}/assignment`

- `204`: no assignment.
- `200`: one Assignment JSON (same shape as `examples/assignment.json`).
- Other codes: deferred; client will retry on the next poll.

Assignments include a unique job ID, device ID, signed release envelope,
not-before/deadline and auto-reboot permission. The authenticated Fleet connection
authorizes the assignment/window. The release publisher signature authorizes the
artifact and its compatibility/sequence. A job must not be altered after delivery.
Deliver the same assignment until its accepted/terminal events are acknowledged.

## Receive events

`POST /v1/devices/{device_id}/events`

Body: array of at most 100 Event objects. Persist them idempotently using
`(device_id, sequence)` as the key. Return HTTP 200 with:

```json
{"sequence": 123}
```

The sequence must be the highest contiguous event durably accepted by the server,
never higher than the last event in that request. A lost response causes replay
of the batch; duplicate insertion must be harmless. The agent rejects an ACK for
unsent events. Events are removed locally only after a valid acknowledgment.

The initial state snapshot is exposed through the local API. Fleet consumes the
event stream and should retain full job/release data from its own assignments.

## Rollout policy stays in Fleet

For 50 gateways, start 2 canaries, then 10 additional devices, then the remaining
38. Promotion requires committed events, a Fleet-side observation period, healthy
application/device telemetry, and configured failure thresholds. An unreachable
device is unknown, not successful. Pause new assignments on uncertainty. OTA does
not implement a second cloud scheduler or automatically assign the next wave.

Local boot health should not require WAN access unless the device genuinely
cannot operate offline. Fleet connectivity can be a separate promotion criterion.

## Deferred capabilities

Fleet push or websocket transport is not in the v1 agent contract. Bandwidth
limits and download windows are agent settings (`bandwidth_bytes_per_sec`,
`download_window_start` / `download_window_end`), separate from the assignment
maintenance window. Staged OS, container, config, and model payloads are schema
2 targets in the agent. Canary and wave policy for OTA devices lives in Zyvor
Fleet, not in this repository.

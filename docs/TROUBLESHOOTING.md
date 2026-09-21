---
hero:
  eyebrow: TROUBLESHOOTING
  title: Troubleshooting
---

Real operational issues, with the documented fix — not a generic checklist.
If your symptom isn't here, check [`docs/OPERATIONS.md`](OPERATIONS.md) in
full, then [open an issue](https://github.com/zyvorai/ota/issues).

## A job is stuck in `NeedsRecovery` after a crash

This is the agent correctly refusing to guess after an ambiguous crash, not
a bug. Follow the exact procedure in
[`docs/OPERATIONS.md`](OPERATIONS.md#needsrecovery):

1. Inspect `otactl job JOB_ID` and `rauc status --detailed --output-format=json`.
2. Confirm RAUC is idle — do not kill a running flash operation.
3. If the original slot is still running, `otactl recover-abort` marks
   the ambiguous target bad and restores the old slot as the next boot
   target.
4. If another slot is running or storage is damaged, use the board's
   recovery path instead.
5. Fix the root cause and issue a newly signed sequence with a new version
   and job ID.

Never hand-edit `ota.db` or a leftover `state.json` to clear the interlock or
lower the accepted sequence number. That defeats the anti-replay protection
this interlock exists to preserve.

## `otactl gc` refuses to run

By design — it refuses during an active job so a failed artifact stays
available for inspection rather than being silently deleted. Wait for the
job to reach a terminal state first.

## Fleet doesn't see event acknowledgements

Fleet must acknowledge only contiguous event sequences it has durably
stored. If you're operating without a Fleet integration at all, export
`events`, store the export durably yourself, then run
`otactl ack LAST_SEQUENCE` — `ack` does not erase job history, so this is
safe to do after the fact.

**Lab gotcha:** posting a synthetic low `sequence` (for example `1`) into
Fleet before a real job whose outbox starts higher creates a hole. The agent
keeps `pending_events` until Fleet's `ackedSequence` is aligned with the
contiguous prefix the agent actually emitted. See [LAB.md](LAB.md).

## Job journal is approaching 10,000 entries

Terminal jobs above 10,000 move into the SQLite `archive_jobs` table in the
same transaction. Read them with `otactl archive`. Copy the live journal
with `otactl backup /var/backups/ota.db` (absolute path, and it will not
overwrite an existing file). Stop the agent before replacing `ota.db`.
Unacknowledged events are not deleted to make room. **Do not** delete the
journal to reclaim space — that invalidates replay protection.

## Rotating the signing key

Without `trust_dir`, provision the new public key alongside the old one
through a separately trusted configuration or image update, restart the agent
to load it, then start signing new releases under the new key ID. Only remove
the old key once the whole fleet has the new trust configuration — removing it
early causes preinstall jobs signed with the old key to fail their next
verification step.

With `trust_dir` set, a later root document signed by the configured threshold
retires a targets key without a new OS image. The initial root payload must
match `root_sha256`. The private signing key itself must never live on a
production device; it is a separate trust domain from Cosign build-signing
identities.

## A restored journal snapshot causes verification failures

The live journal is `ota.db`. A legacy `state.json` is imported once and
renamed `state.json.migrated`. Restoring an older `ota.db` without its
anti-replay high-water mark makes the next release look like a replay, or
lets an old sequence through if the mark moved backward. Stop the agent,
replace `ota.db` only with a `otactl backup` copy, and reconcile against
RAUC's actual slot state before starting again. Do not hand-edit rows.

## `docs/FLEET.md`'s contract doesn't match my Fleet deployment's API

Zyvor Fleet (`zyvorai/fleet`) ships the server APIs described in
[FLEET.md](FLEET.md) (see Fleet `docs/OTA_CONTRACT.md`). Older or forked
Fleet builds may not. Confirm your deployed version exposes
`/v1/devices/{id}/assignment` and `/events`, and that `fleet_url` is HTTPS
with a CA the agent trusts. For bring-up without Fleet, use `zyvor-fleet-ref`.
Recorded lab wiring: [LAB.md](LAB.md).

## Nothing here matches

Check [`docs/OPERATIONS.md`](OPERATIONS.md) and
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md) for the full runbook and design
invariants, then [open an issue](https://github.com/zyvorai/ota/issues)
with your `otactl job JOB_ID` output and relevant logs (redact signing
keys and device credentials).

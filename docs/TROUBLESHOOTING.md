# Troubleshooting

Real operational issues, with the documented fix — not a generic checklist.
If your symptom isn't here, check [`docs/OPERATIONS.md`](OPERATIONS.md) in
full, then [open an issue](https://github.com/zyvorai/ota/issues).

## A job is stuck in `NeedsRecovery` after a crash

This is the agent correctly refusing to guess after an ambiguous crash, not
a bug. Follow the exact procedure in
[`docs/OPERATIONS.md`](OPERATIONS.md#needsrecovery):

1. Inspect `zyvor-ota job JOB_ID` and `rauc status --detailed --output-format=json`.
2. Confirm RAUC is idle — do not kill a running flash operation.
3. If the original slot is still running, `zyvor-ota recover-abort` marks
   the ambiguous target bad and restores the old slot as the next boot
   target.
4. If another slot is running or storage is damaged, use the board's
   recovery path instead.
5. Fix the root cause and issue a newly signed sequence with a new version
   and job ID.

Never hand-edit `state.json` to clear the interlock or lower the accepted
sequence number — that defeats the anti-replay protection this interlock
exists to preserve.

## `zyvor-ota gc` refuses to run

By design — it refuses during an active job so a failed artifact stays
available for inspection rather than being silently deleted. Wait for the
job to reach a terminal state first.

## Fleet doesn't see event acknowledgements

Fleet must acknowledge only contiguous event sequences it has durably
stored. If you're operating without a Fleet integration at all, export
`events`, store the export durably yourself, then run
`zyvor-ota ack LAST_SEQUENCE` — `ack` does not erase job history, so this is
safe to do after the fact.

## Job journal is approaching 10,000 entries

There is no online compaction command in v0.1 by design — the journal is
deliberately bounded. At that scale, retire/reprovision the device identity
under an audited maintenance process that retains history and a trusted
release-sequence baseline. **Do not** delete the journal to reclaim space —
that invalidates replay protection.

## Rotating the signing key

Provision the new public key alongside the old one through a separately
trusted configuration/image update, restart the agent to load it, then
start signing new releases under the new key ID. Only remove the old key
once the whole fleet has the new trust configuration — removing it early
causes preinstall jobs signed with the old key to fail their next
verification step. The private signing key itself must never live on a
production device; it's a separate trust domain from Cosign build-signing
identities.

## A restored `state.json` snapshot causes verification failures

Expected if you restored an older snapshot without also restoring an
equal-or-higher trusted anti-replay high-water mark — the two must move
together. Reconcile against RAUC's actual slot state before restarting the
agent after any manual state recovery.

## `docs/FLEET.md`'s contract doesn't match my Fleet deployment's API

That contract is explicitly documented as "proposed... No existing Zyvor
Fleet repository or API has been modified or assumed compatible" — treat it
as a spec for the client side OTA implements, not a guarantee your specific
Fleet deployment already speaks it. Verify server-side support before
relying on it.

## Nothing here matches

Check [`docs/OPERATIONS.md`](OPERATIONS.md) and
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md) for the full runbook and design
invariants, then [open an issue](https://github.com/zyvorai/ota/issues)
with your `zyvor-ota job JOB_ID` output and relevant logs (redact signing
keys and device credentials).

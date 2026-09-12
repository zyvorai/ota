---
hero:
  eyebrow: OPERATIONS
  title: Operator runbook
---

## Monitor

`zyvor-ota status` shows the active job, sequence high-water mark and outbox count.
`zyvor-ota job JOB_ID` shows retained job history. `zyvor-ota events` shows ordered
unacknowledged transitions. The socket exposes `/metrics` for local scraping.
Errors from download and Fleet transport are sanitized to avoid printing signed
URLs or bearer tokens. Full job data is visible only to trusted socket operators.

Fleet retries every ten seconds; the engine advances once per second. Downloads
have a 30-minute cap, Fleet requests 20 seconds, and local health probes three
seconds each. A transient download failure retains the partial file and retries
until the assignment expires. This version does not implement jitter or bandwidth
shaping; schedule fleet cohorts to avoid synchronized large downloads.

## Reboot

`auto_reboot: true` authorizes reboot inside the assignment window once the bundle
is installed. Otherwise use `zyvor-ota reboot`. The intent is persisted before
requesting reboot. If the agent died after recording intent but before invoking
reboot, reissue the command inside the window. The command checks the observed
boot ID, slot and idle backend before requesting a reboot again.

After the window expires, an installed pending release is held for inspection;
the CLI does not extend its deadline. Schedule windows with sufficient headroom.
A qualified operator can use the board's RAUC/bootloader tooling to select the old
slot or perform a maintenance reboot, then let OTA reconcile the actual boot.

## NeedsRecovery

1. Inspect `zyvor-ota job JOB_ID` and `rauc status --detailed --output-format=json`.
2. Confirm RAUC is idle. Do not kill a running flash operation.
3. If the original slot is running, `zyvor-ota recover-abort` marks the ambiguous
   target bad and restores the old slot as the next boot target.
4. If another slot is running or storage is damaged, use the board recovery path.
5. Fix the cause and issue a newly signed sequence with a new version and job ID.

Never edit `state.json` to clear an interlock or lower the accepted sequence.
For a journal/storage error, stop the agent, preserve logs and state, repair the
filesystem, and reconcile against RAUC before restarting. A stale `.ota-*` temp
file is not authoritative; `state.json` is. Do not restore old state snapshots
without also restoring an equal-or-higher trusted anti-replay high-water mark.

## Cache and outbox

After a job is terminal, `zyvor-ota gc` removes cached bundles and partial files.
It refuses to run during an active job. This is explicit rather than automatic,
so a fleet operator can retain a failed artifact for inspection.

Fleet must acknowledge only contiguous event sequences durably stored by its
server. If operating without Fleet, export `events`, store the export durably,
then run `zyvor-ota ack LAST_SEQUENCE`. ACK does not erase job history.

The job journal is deliberately bounded and has no online compaction command in
v0.1. At 10,000 jobs, retire/reprovision the device identity under an audited
maintenance process with retained history and a trusted release-sequence baseline.
Deleting the journal to regain space invalidates replay protection.

## Trust rotation

Provision a new public key alongside the old key through a separately trusted
configuration/image update. Restart the agent to load its root-owned config,
then start signing releases under the new key ID. Remove the old key only after
the fleet has the new trust configuration. Key removal rejects preinstall jobs
signed by the old key when their next verification step runs.

The OTA private signing key must never reside on a production device. RAUC bundle
signing keys and Cosign build-signing identities are separate trust domains.

## Data compatibility

A/B rollback restores OS partitions, not arbitrary shared application data.
Keep schema migrations backward-compatible across the current and fallback OS,
or implement a board/application-specific transactional data recovery scheme.
Do not mark an OS good until essential device and application probes pass.

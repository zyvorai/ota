---
hero:
  eyebrow: OPERATIONS
  title: Operator runbook
---

## Monitor

`otactl status` prints a Cilium-style colorful logo (agent, backend, active job, events, sequence, feature rows). `otactl status json` keeps the raw agent JSON. `zyvor-ota` is the same binary.
`otactl job JOB_ID` shows retained job history. `otactl events` shows ordered
unacknowledged transitions. The socket exposes `/metrics` for local scraping.
Set `otlp_endpoint` to also export one OpenTelemetry span per job transition
and per Fleet sync. Leave it empty to keep traces off. A collector failure
does not change the job outcome.
Errors from download and Fleet transport are sanitized to avoid printing signed
URLs or bearer tokens. Full job data is visible only to trusted socket operators.

Fleet retries every ten seconds; the engine advances once per second. Downloads
have a 30-minute cap, Fleet requests 20 seconds, and local health probes three
seconds each. A transient download failure retains the partial file and retries
until the assignment expires. `bandwidth_bytes_per_sec` caps a download when it is
greater than zero. `download_jitter_seconds` spreads the moment devices in one
wave start fetching; zero leaves that off. The offset is a stable function of
`device_id`, so a retry waits out the same delay instead of rolling a new one.
Local media that already matches the signed digest does not wait.
`relay_url` asks a site relay for that digest before the signed URL. The relay
may fetch the signed URL only for hosts in its `-origin-hosts` list. The agent
still checks the digest. A relay miss uses the signed URL. A mismatched body does not.

## Reboot

`auto_reboot: true` authorizes reboot inside the assignment window once the bundle
is installed. Otherwise use `otactl reboot`. The intent is persisted before
requesting reboot. If the agent died after recording intent but before invoking
reboot, reissue the command inside the window. The command checks the observed
boot ID, slot and idle backend before requesting a reboot again.

After the window expires, an installed pending release is held for inspection;
the CLI does not extend its deadline. Schedule windows with sufficient headroom.
A qualified operator can use the board's RAUC/bootloader tooling to select the old
slot or perform a maintenance reboot, then let OTA reconcile the actual boot.

## NeedsRecovery

1. Inspect `otactl job JOB_ID` and `rauc status --detailed --output-format=json`.
2. Confirm RAUC is idle. Do not kill a running flash operation.
3. If the original slot is running, `otactl recover-abort` marks the ambiguous
   target bad and restores the old slot as the next boot target.
4. If another slot is running or storage is damaged, use the board recovery path.
5. Fix the cause and issue a newly signed sequence with a new version and job ID.

Never edit `ota.db` or a leftover `state.json` to clear an interlock or lower
the accepted sequence. For a journal or storage error, stop the agent, preserve
logs and state, repair the filesystem, and reconcile against RAUC before
restarting. A stale `.ota-*` temp file is not authoritative; `ota.db` is.
Restore it only from a `otactl backup` copy taken while the agent was
healthy, and do not move the anti-replay high-water mark backward.

## Offline campaign

Copy a signed assignment and its digest-named artifacts onto removable media, then onto the device. The signed URLs stay as they are. Set `local_media_dir` to the absolute media directory. When `{sha256}.raucb` is present and matches, the agent uses that file and skips the download window. A file that is present but does not match the signed digest is refused.

```sh
otactl campaign-export assignment.json ./artifacts /media/usb/campaign
otactl campaign-import /media/usb/campaign /var/lib/zyvor-ota/media
otactl -socket /run/zyvor-ota/agent.sock submit /media/usb/campaign/assignment.json
```

`campaign-import` prints the assignment path. Submit that file. Do not edit the envelope to point the URLs at `file://`.

## Cache and outbox

After a job is terminal, `otactl gc` removes cached bundles and partial files.
It refuses to run during an active job. This is explicit rather than automatic,
so a fleet operator can retain a failed artifact for inspection.

Fleet must acknowledge only contiguous event sequences durably stored by its
server. If operating without Fleet, export `events`, store the export durably,
then run `otactl ack LAST_SEQUENCE`. ACK does not erase job history.

The hot journal keeps up to 10,000 jobs. Older terminal jobs move into the
SQLite archive instead of rejecting the next update. Unacknowledged events are
still not dropped. If the outbox exceeds 9,000 events, new jobs pause until
Fleet (or `otactl ack`) drains them. A corrupt `ota.db` fails startup;
restore the last `VACUUM INTO` backup. Take that copy while the agent is
running with `otactl backup /var/backups/ota.db`. Stop the agent before
replacing `ota.db`. `otactl archive` prints terminal jobs that have already
left the hot journal. Do not delete the journal to reset the
release sequence.

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

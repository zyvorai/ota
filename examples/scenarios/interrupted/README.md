# Interrupted update

`./scripts/ota-demo up` stops the daemon after the job reaches `awaiting_reboot`, then starts it again.

Expected state: the job is still `awaiting_reboot` (resume), then `committed` after reboot. If you kill the daemon while the state is `installing`, the next start is `needs_recovery` and the only built-in resolution is `recover-abort` plus a newly signed sequence. See [OPERATIONS.md](../../docs/OPERATIONS.md).

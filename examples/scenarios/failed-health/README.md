# Failed health check

`./scripts/ota-demo up` removes the health file before reboot on the second release.

Expected state: `rolled_back`. The previous slot is booted again. A retry needs a newly signed sequence.

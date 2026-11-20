# Health check fragments

Paste the JSON array from `minewing-gw1-r1.checks.json` into the agent
`checks` field. All `http` probes must target loopback (`127.0.0.1` or `::1`).
Systemd unit names must match `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`.
File probes require absolute paths present only after the new OS is healthy.

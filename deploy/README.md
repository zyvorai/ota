# Deploy

This directory and `scripts/deploy-remote.sh` give zyvor-ota an SSH-based remote
deploy path shaped like `../guestkit/scripts/deploy-remote.sh` +
`../guestkit/deploy/`. It is **not** the same kind of deployment guestkit does,
and it does not replace [`docs/DEVICE-INTEGRATION.md`](../docs/DEVICE-INTEGRATION.md).

## Two different meanings of "deploy" in this repo

1. **Real device deployment** (production): a board-image build system bakes
   `zyvor-otad`/`zyvor-ota`, `packaging/systemd/zyvor-otad.service`,
   `packaging/dbus/zyvor-ota.conf` and RAUC trust anchors into a board OS image
   with real A/B slots. That process is described end-to-end in
   `docs/DEVICE-INTEGRATION.md` and cannot be automated from a generic host —
   it requires the board's own BSP/image tooling. Nothing here changes that.

2. **Remote smoke-deploy** (this directory + `scripts/deploy-remote.sh`): push
   the built binaries to an arbitrary Linux host over SSH, install them as a
   systemd service running the **simulator backend**, and run a real signed
   install → reboot → health → commit/rollback job cycle against that live,
   running daemon. This proves the built artifacts work end-to-end on that
   host/arch. It intentionally never touches RAUC, D-Bus, or real disks —
   `internal/ota/backend.go`'s `Simulator.Reboot` only rewrites a local JSON
   slot file, it never calls `reboot(2)` — so it's safe to run on a normal
   cloud/VM host that has no RAUC-managed A/B rootfs.

## Layout

| Path | Purpose |
|---|---|
| `deploy/config/agent.simulator.remote.json` | Documented template for the remote demo config (script fills in a freshly generated trust key) |
| `deploy/systemd/zyvor-otad-demo.service` | Simulator-backend unit (no `rauc.service` dependency, no D-Bus) |
| `deploy/systemd/zyvor-ota-demo-artifacts.service` | Loopback-only static file server the demo daemon downloads releases from |
| `scripts/deploy-remote.sh` | Driver: rsync → build → `make check && make demo` → install → verify |
| `scripts/selftest.sh` | Remote verification: checks the installed units, then runs one real signed job (submit → reboot → commit) against the deployed daemon |

## Usage

```sh
scripts/deploy-remote.sh <host> <user> --key
make deploy-remote H=<host> U=<user>
```

See `scripts/deploy-remote.sh --help` for all profiles/flags
(`--quick`, `--verify-only`, `--preflight-only`, `--uninstall`, `--dry-run`,
`--fleet hosts.txt`).

## Worked example against a fresh VM

Prerequisites on the target: a Linux host reachable over SSH with an
already-authorized key (`ssh-copy-id user@host`; password auth works via
`sshpass` but is deprecated — see `--help`), passwordless `sudo` if not
logging in as root, and `systemd`. Nothing else needs to be preinstalled —
`--key` full profile installs Go, `dbus-daemon`, and `gcc` itself.

```sh
scripts/deploy-remote.sh HOST USER --key
```

This runs, in order: rsync the source to `~/.deployments/zyvor-ota` on the
host; install system packages; install a pinned Go toolchain if the host's
doesn't match; run `make check` (test, race, vet, build) and `make demo`
(`scripts/e2e.py`'s full commit/rollback/replay cycle) as a build gate
identical to CI; install the binaries, a dedicated `zyvor-ota-demo` system
user, and both systemd units (picking a free loopback port for the artifact
server rather than assuming one is free — the host may already run other
services); then run `scripts/selftest.sh`, which checks both units are
active, queries the operator socket, and runs one more real signed job cycle
against the now-installed daemon. A representative pass looks like:

```
=== zyvor-ota-demo services ===
  [pass] zyvor-ota-demo-artifacts.service active
  [pass] zyvor-otad-demo.service active

=== Operator socket ===
  [pass] agent socket present: /run/zyvor-ota-demo/agent.sock
  [pass] status: {"active":null,"backend":"simulator","device_id":"...","event_sequence":9,"high_sequence":...,"pending_events":0,"version":"0.1.0"}

=== Live signed job cycle ===
  [pass] live job cycle ok: selftest-... install -> reboot -> health -> commit -> ack -> gc

Results: 5 passed, 0 failed
```

Useful follow-ups against the same host:

```sh
scripts/deploy-remote.sh HOST USER --key --verify-only   # re-run selftest without rebuilding
scripts/deploy-remote.sh HOST USER --key --uninstall     # remove the demo service/binaries/user/keys
```

`--uninstall` removes everything it installed (units, `/usr/local/bin/zyvor-ota*`,
`/etc/zyvor-ota-demo`, `/var/lib/zyvor-ota-demo`, the `zyvor-ota-demo` user, and
the rsynced staging directory) and leaves every other service on a shared host
untouched.

## What this does *not* do

- It does not qualify reboot/rollback/power-loss behavior on real hardware —
  that is a board qualification step per the README's support matrix.
- It does not install a RAUC backend config or D-Bus policy — production
  device deployment still follows `docs/DEVICE-INTEGRATION.md` exactly.
- It never uploads or provisions a real Fleet signing key; the demo trust key
  is generated fresh on the target host and only signs throwaway demo releases.

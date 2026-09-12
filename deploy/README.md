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

## What this does *not* do

- It does not qualify reboot/rollback/power-loss behavior on real hardware —
  that is a board qualification step per the README's support matrix.
- It does not install a RAUC backend config or D-Bus policy — production
  device deployment still follows `docs/DEVICE-INTEGRATION.md` exactly.
- It never uploads or provisions a real Fleet signing key; the demo trust key
  is generated fresh on the target host and only signs throwaway demo releases.

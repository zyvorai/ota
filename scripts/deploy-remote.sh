#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# ─────────────────────────────────────────────────────────────
# zyvor-ota — Remote smoke-deploy (SSH + rsync)
#
# This is NOT real device deployment. Real device deployment bakes the agent
# into a board OS image with a real RAUC backend — see docs/DEVICE-INTEGRATION.md
# and cannot be automated from a generic host. This script instead:
#
#   1. syncs source to a remote Linux host
#   2. builds it there and runs `make check && make demo` (the same gate CI runs)
#   3. installs the binaries as a systemd service running the SIMULATOR backend
#   4. runs one real signed install -> reboot -> health -> commit job cycle
#      against that live, running daemon (scripts/selftest.sh)
#
# The simulator backend never touches RAUC, D-Bus, or real disks — see
# internal/ota/backend.go: Simulator.Reboot only rewrites a local JSON file.
# It is safe to run on a normal cloud/VM host with no A/B rootfs.
#
# Profiles:
#   default   Sync source -> install system deps/Go -> build+check+demo -> install+verify
#   --quick   Sync source -> build+install only (assumes deps/Go already present)
#
# Auth: SSH keys (recommended, --key). Password via sshpass is supported but deprecated.
# ─────────────────────────────────────────────────────────────
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION="0.1.0"
GO_VERSION="1.27.1"
REMOTE_DIR=""
DEPLOY_PROFILE="full"
DEPLOY_LOG="${ZYVOR_OTA_DEPLOY_LOG:-${HOME}/.zyvor-ota/deploy-$(date +%Y%m%d-%H%M%S).log}"

QUICK_MODE=false
UNINSTALL=false
FLEET_FILE=""
KEY_AUTH=false
DRY_RUN=false
SKIP_SYNC=false
SKIP_VERIFY=false
VERIFY_ONLY=false
PREFLIGHT_ONLY=false
VERBOSE=false
SSH_RETRIES="${ZYVOR_OTA_SSH_RETRIES:-3}"
POSITIONAL=()

usage() {
    cat <<EOF
zyvor-ota remote smoke-deploy v${VERSION}

Usage:
  $0 <host> <user> [options]
  $0 user@host [options]
  $0 --fleet hosts.txt

Profiles:
  (default)   Full: system deps + Go toolchain + build/check/demo + install + verify
  --quick     Rsync + remote build/install only (skip system deps / Go install)

Options:
  --help              Show this help
  --dry-run           Print steps without SSH/rsync/build
  --preflight-only    SSH + disk/sudo/systemd checks, then exit
  --verify-only       Run remote selftest only (no rebuild/reinstall)
  --skip-sync         Skip rsync (sources already on host)
  --skip-verify       Skip remote selftest
  --key               SSH key auth (clear password)
  --uninstall         Remove the demo service/binaries from host
  -v, --verbose       Verbose rsync

Environment:
  ZYVOR_OTA_DEPLOY_LOG      Log file path
  ZYVOR_OTA_SSH_RETRIES     SSH retry count (default: 3)
  DEPLOY_DIR                Override remote staging dir (default: ~/.deployments/zyvor-ota)

Examples:
  $0 10.0.0.5 root --key
  $0 root@10.0.0.5 --quick
  $0 10.0.0.5 root --verify-only
  make deploy-remote H=10.0.0.5 U=root

Fleet file (one host per line):
  host user [password] [options]
  user@host root --quick
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)        usage; exit 0 ;;
        --quick)          QUICK_MODE=true; DEPLOY_PROFILE="quick"; shift ;;
        --uninstall)      UNINSTALL=true; shift ;;
        --key)            KEY_AUTH=true; shift ;;
        --dry-run)        DRY_RUN=true; shift ;;
        --skip-sync)      SKIP_SYNC=true; shift ;;
        --skip-verify)    SKIP_VERIFY=true; shift ;;
        --verify-only)    VERIFY_ONLY=true; shift ;;
        --preflight-only) PREFLIGHT_ONLY=true; shift ;;
        -v|--verbose)     VERBOSE=true; shift ;;
        --fleet)
            shift
            FLEET_FILE="${1:?--fleet requires a hosts file path}"
            shift
            ;;
        *)
            POSITIONAL+=("$1")
            shift
            ;;
    esac
done

TARGET_HOST="${POSITIONAL[0]:-}"
TARGET_USER="${POSITIONAL[1]:-root}"
TARGET_PASS="${POSITIONAL[2]:-}"

if [ "$KEY_AUTH" = true ]; then
    TARGET_PASS=""
fi

if [[ -n "${TARGET_HOST}" && "${TARGET_HOST}" == *"@"* ]]; then
    TARGET_USER="${TARGET_HOST%%@*}"
    TARGET_HOST="${TARGET_HOST#*@}"
fi

_use_color() { [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; }
if _use_color; then
    C_OK=$'\033[32m'; C_FAIL=$'\033[31m'; C_INFO=$'\033[36m'; C_WARN=$'\033[33m'
    C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'; C_MAG=$'\033[35m'; C_CYAN=$'\033[96m'; C_RST=$'\033[0m'
else
    C_OK= C_FAIL= C_INFO= C_WARN= C_DIM= C_BOLD= C_MAG= C_CYAN= C_RST=
fi

_log_file() { mkdir -p "$(dirname "$DEPLOY_LOG")" 2>/dev/null || true; echo "[$(date -Iseconds)] $*" >>"$DEPLOY_LOG" 2>/dev/null || true; }
ok()   { echo "${C_OK}  [ok] $*${C_RST}"; _log_file "OK $*"; }
fail() { echo "${C_FAIL}  [fail] $*${C_RST}" >&2; _log_file "FAIL $*"; exit 1; }
info() { echo "${C_INFO}  [info] $*${C_RST}"; _log_file "INFO $*"; }
warn() { echo "${C_WARN}  [warn] $*${C_RST}"; _log_file "WARN $*"; }
dry()  { echo "${C_MAG}  [dry-run] $*${C_RST}"; _log_file "DRY $*"; }

profile_label() {
    if [ "$UNINSTALL" = true ]; then echo "uninstall"; return; fi
    if [ "$VERIFY_ONLY" = true ]; then echo "verify-only"; return; fi
    if [ "$PREFLIGHT_ONLY" = true ]; then echo "preflight"; return; fi
    echo "${DEPLOY_PROFILE}"
}

print_banner() {
    local target
    target="${TARGET_USER}@${TARGET_HOST}"
    [ -z "${TARGET_HOST}" ] && target="(fleet mode)"
    echo ""
    echo "${C_CYAN}${C_BOLD}  ============================================================${C_RST}"
    echo "${C_CYAN}${C_BOLD}  zyvor-ota remote smoke-deploy  v${VERSION}${C_RST}"
    echo "${C_CYAN}${C_BOLD}  ${target}  ·  profile: $(profile_label)${C_RST}"
    [ "$DRY_RUN" = true ] && echo "${C_MAG}${C_BOLD}  DRY-RUN — no remote changes${C_RST}"
    [ -n "${FLEET_FILE}" ] && echo "${C_CYAN}${C_BOLD}  fleet: ${FLEET_FILE}${C_RST}"
    echo "${C_CYAN}${C_BOLD}  ============================================================${C_RST}"
    echo ""
}

STEP_T0=0
STEP_IDX=0

step_begin() {
    STEP_IDX=$((STEP_IDX + 1))
    STEP_T0=$(date +%s)
    echo ""
    echo "${C_BOLD}${C_CYAN}  -- Step ${STEP_IDX}: $*${C_RST}"
    echo "${C_DIM}  ------------------------------------------------------------${C_RST}"
    _log_file "STEP ${STEP_IDX}: $*"
}

step_end() {
    echo "${C_DIM}  --${C_RST} ${C_OK}done in $(( $(date +%s) - STEP_T0 ))s${C_RST}"
}

run_step() {
    step_begin "$1"; shift
    if [ "$DRY_RUN" = true ]; then dry "would run: $*"; step_end; return 0; fi
    "$@"; step_end
}

SSH_OPTS="-o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=15 -o ServerAliveInterval=30"
if [ -z "${TARGET_PASS}" ]; then
    SSH_OPTS+=" -o BatchMode=yes -o PreferredAuthentications=publickey"
fi

_ssh_once() {
    if [ -n "${TARGET_PASS}" ] && command -v sshpass &>/dev/null; then
        export SSHPASS="${TARGET_PASS}"
        sshpass -e ssh ${SSH_OPTS} "${TARGET_USER}@${TARGET_HOST}" "$@"
    else
        ssh ${SSH_OPTS} "${TARGET_USER}@${TARGET_HOST}" "$@"
    fi
}

_ssh() {
    local attempt=1 max="${SSH_RETRIES}"
    while [ "$attempt" -le "$max" ]; do
        if _ssh_once "$@"; then
            return 0
        fi
        attempt=$((attempt + 1))
        if [ "$attempt" -le "$max" ]; then
            local _d=$(( 2 * (attempt - 1) )); _d=$(( _d < 2 ? 2 : _d > 30 ? 30 : _d ))
            warn "SSH retry ${attempt}/${max}" && sleep "${_d}"
        fi
    done
    return 1
}

_rsync() {
    local opts="-az --delete"
    [ "$VERBOSE" = true ] && opts+=" --progress"
    if [ -n "${TARGET_PASS}" ] && command -v sshpass &>/dev/null; then
        export SSHPASS="${TARGET_PASS}"
        rsync ${opts} -e "sshpass -e ssh ${SSH_OPTS}" "$@"
    else
        rsync ${opts} -e "ssh ${SSH_OPTS}" "$@"
    fi
}

validate() {
    [ -n "${TARGET_HOST}" ] || { usage; exit 1; }
    [ -f "${PROJECT_DIR}/go.mod" ] || fail "Not in zyvor-ota repo: ${PROJECT_DIR}"
    if [ -n "${TARGET_PASS}" ]; then
        warn "Password auth is deprecated. Prefer: ssh-copy-id ${TARGET_USER}@${TARGET_HOST}"
        command -v sshpass &>/dev/null || fail "sshpass required for password auth (dnf/apt install sshpass)"
    fi
}

check_connectivity() {
    info "SSH -> ${TARGET_USER}@${TARGET_HOST}  log: ${DEPLOY_LOG}"
    if [ "$DRY_RUN" = true ]; then
        REMOTE_DIR="${DEPLOY_DIR:-${HOME}/.deployments/zyvor-ota}"
        return 0
    fi
    _ssh "echo ok" &>/dev/null || fail "SSH failed — try: ssh-copy-id ${TARGET_USER}@${TARGET_HOST}"
    ok "SSH connected"
    local remote_home
    remote_home=$(_ssh "echo \$HOME" 2>/dev/null | tr -d '\r')
    remote_home="${remote_home:-/home/${TARGET_USER}}"
    REMOTE_DIR="${DEPLOY_DIR:-${remote_home}/.deployments/zyvor-ota}"
    info "Remote path: ${REMOTE_DIR}"
}

preflight_remote() {
    info "Preflight on ${TARGET_HOST}..."
    if [ "$DRY_RUN" = true ]; then return 0; fi
    _ssh bash <<'REMOTE' || fail "Preflight failed"
set -e
echo "  host: $(hostname -f 2>/dev/null || hostname)"
echo "  os:   $(. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" || uname -s)"
echo "  arch: $(uname -m)"
echo "  mem:  $(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo n/a)"
echo "  disk: $(df -h / 2>/dev/null | awk 'NR==2{print $4 " free on " $1}' || echo n/a)"
AVAIL=$(df -BG / 2>/dev/null | awk 'NR==2{gsub(/G/,"",$4); print $4}' || echo 99)
if [ "${AVAIL}" -lt 4 ] 2>/dev/null; then
    echo "  warn: less than 4G free on / — build may fail"
fi
if [ "$(id -u)" -ne 0 ]; then
    if ! sudo -n true 2>/dev/null; then
        echo "  FAIL: non-root user needs passwordless sudo to install a system service"
        exit 1
    fi
    echo "  ok: passwordless sudo"
else
    echo "  ok: running as root"
fi
command -v systemctl >/dev/null || { echo "  FAIL: systemd not found"; exit 1; }
echo "  ok: systemd present"
command -v go >/dev/null && echo "  go: $(go version)" || echo "  go: not installed (will be installed)"
command -v dbus-daemon >/dev/null && echo "  ok: dbus-daemon present" || echo "  dbus-daemon: not installed (will be installed)"
REMOTE
    ok "Preflight passed"
}

sync_files() {
    if [ "$SKIP_SYNC" = true ]; then
        info "Skipping rsync (--skip-sync)"
        return 0
    fi
    _ssh "mkdir -p '${REMOTE_DIR}'"
    local excludes=(
        --exclude '.git'
        --exclude 'bin'
        --exclude 'dist'
        --exclude 'coverage.out'
        --exclude '*.log'
    )
    _rsync "${excludes[@]}" "${PROJECT_DIR}/" "${TARGET_USER}@${TARGET_HOST}:${REMOTE_DIR}/"
    ok "Source synced to ${REMOTE_DIR}"
}

install_system_deps() {
    _ssh bash <<'REMOTE'
set -euo pipefail
SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"

. /etc/os-release 2>/dev/null || true
_ID="${ID:-}" _ID_LIKE="${ID_LIKE:-}"
if [[ "$_ID" == "debian" || "$_ID" == "ubuntu" || "$_ID_LIKE" == *"debian"* || "$_ID_LIKE" == *"ubuntu"* ]]; then
    $SUDO apt-get update -qq
    $SUDO apt-get install -y -qq dbus gcc python3 curl tar
elif command -v dnf &>/dev/null; then
    $SUDO dnf install -y dbus-daemon gcc python3 curl tar
elif command -v yum &>/dev/null; then
    $SUDO yum install -y dbus-daemon gcc python3 curl tar
elif command -v apt-get &>/dev/null; then
    $SUDO apt-get update -qq
    $SUDO apt-get install -y -qq dbus gcc python3 curl tar
else
    echo "ERROR: unsupported package manager"
    exit 1
fi
echo "System dependencies installed"
REMOTE
}

ensure_go_remote() {
    _ssh env GO_VERSION="${GO_VERSION}" bash <<'REMOTE'
set -e
SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"
if command -v go &>/dev/null && go version | grep -q "go${GO_VERSION}"; then
    echo "Go: $(go version)"
    exit 0
fi
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  GOARCH=amd64 ;;
    aarch64) GOARCH=arm64 ;;
    *) echo "ERROR: unsupported arch $ARCH"; exit 1 ;;
esac
TARBALL="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
curl -fsSL "https://go.dev/dl/${TARBALL}" -o "/tmp/${TARBALL}"
$SUDO rm -rf /usr/local/go
$SUDO tar -C /usr/local -xzf "/tmp/${TARBALL}"
rm -f "/tmp/${TARBALL}"
echo 'export PATH=$PATH:/usr/local/go/bin' | $SUDO tee /etc/profile.d/zyvor-ota-go.sh >/dev/null
echo "Go installed: $(/usr/local/go/bin/go version)"
REMOTE
}

build_and_check_remote() {
    _ssh env REMOTE_STAGING="${REMOTE_DIR}" bash <<'REMOTE'
set -e
# internal/ota/store.go rejects group/world-writable state dirs (real security
# check, not umask-aware). Go's t.TempDir() requests mode 0777, which under a
# common Debian/Ubuntu per-user-group umask of 002 becomes 0775 (group-writable)
# and trips that check spuriously. umask 022 here matches what CI runners use,
# and only affects this build subshell — the deployed daemon always requests
# state dirs at 0700 explicitly regardless of umask, so this changes nothing
# about runtime behavior.
umask 022
export PATH="$PATH:/usr/local/go/bin"
cd "${REMOTE_STAGING}"
make check
make demo
REMOTE
}

build_only_remote() {
    _ssh env REMOTE_STAGING="${REMOTE_DIR}" bash <<'REMOTE'
set -e
umask 022
export PATH="$PATH:/usr/local/go/bin"
cd "${REMOTE_STAGING}"
make build
REMOTE
}

install_and_configure_remote() {
    _ssh env REMOTE_STAGING="${REMOTE_DIR}" bash <<'REMOTE'
set -e
SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"
cd "${REMOTE_STAGING}"

$SUDO install -m755 bin/otactl /usr/local/bin/otactl
$SUDO install -m755 bin/zyvor-ota /usr/local/bin/zyvor-ota
$SUDO install -m755 bin/zyvor-otad /usr/local/bin/zyvor-otad

if ! id zyvor-ota-demo &>/dev/null; then
    $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin zyvor-ota-demo
fi

$SUDO mkdir -p /etc/zyvor-ota-demo /var/lib/zyvor-ota-demo/artifacts

# `otactl keygen DIR` uses os.Mkdir (not MkdirAll) and refuses to run
# against a directory that already exists, so this must not pre-create it.
# zyvor-ota is the same binary as otactl.
if [ ! -f /etc/zyvor-ota-demo/keys/release.pub ]; then
    $SUDO rm -rf /etc/zyvor-ota-demo/keys
    $SUDO /usr/local/bin/otactl keygen /etc/zyvor-ota-demo/keys
fi
$SUDO chmod 700 /etc/zyvor-ota-demo/keys
$SUDO chmod 600 /etc/zyvor-ota-demo/keys/release.key /etc/zyvor-ota-demo/keys/release.pub

PUBKEY="$($SUDO cat /etc/zyvor-ota-demo/keys/release.pub | tr -d '\r\n')"
DEVICE_ID="$(hostname -s 2>/dev/null || hostname)"

# This host may already run other services on common ports (it does: a shared
# box can have zyvor-api/zyvor-fleet/docker-proxy etc. bound to fixed ports),
# so the artifact server must not hardcode one — pick a free loopback port.
ARTIFACT_PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
echo "${ARTIFACT_PORT}" | $SUDO tee /etc/zyvor-ota-demo/artifacts.port >/dev/null

$SUDO tee /etc/zyvor-ota-demo/agent.json >/dev/null <<JSON
{
  "device_id": "${DEVICE_ID}",
  "compatible": "smoke-deploy",
  "state_dir": "/var/lib/zyvor-ota-demo",
  "socket": "/run/zyvor-ota-demo/agent.sock",
  "backend": "simulator",
  "allow_device_writes": false,
  "trust_keys": {"demo": "${PUBKEY}"},
  "download_hosts": ["127.0.0.1:${ARTIFACT_PORT}"],
  "max_artifact_bytes": 1048576,
  "reserve_bytes": 1048576,
  "health_timeout_seconds": 30,
  "health_stable_seconds": 2,
  "checks": [{"kind": "file", "target": "/etc/os-release"}]
}
JSON

$SUDO chown -R zyvor-ota-demo:zyvor-ota-demo /var/lib/zyvor-ota-demo
$SUDO chmod 755 /etc/zyvor-ota-demo

sed "s/8090/${ARTIFACT_PORT}/" deploy/systemd/zyvor-ota-demo-artifacts.service | $SUDO tee /etc/systemd/system/zyvor-ota-demo-artifacts.service >/dev/null
$SUDO chmod 644 /etc/systemd/system/zyvor-ota-demo-artifacts.service
$SUDO install -m644 deploy/systemd/zyvor-otad-demo.service /etc/systemd/system/zyvor-otad-demo.service
$SUDO systemctl daemon-reload
$SUDO systemctl enable zyvor-ota-demo-artifacts.service zyvor-otad-demo.service
# Always restart, never just enable --now: agent.json/keys/port are rewritten
# on every deploy, and an already-active unit would otherwise keep running
# with whatever config it loaded at its own last start, silently drifting
# from what's now on disk (this is exactly what caused a stale-trust-key
# "invalid release signature" failure during initial testing of this script).
$SUDO systemctl restart zyvor-ota-demo-artifacts.service
$SUDO systemctl restart zyvor-otad-demo.service
ready=0
for _ in $(seq 1 30); do
    if $SUDO test -S /run/zyvor-ota-demo/agent.sock; then
        ready=1
        break
    fi
    sleep 1
done
if [ "$ready" -ne 1 ]; then
    echo "agent socket did not appear within 30s" >&2
    $SUDO journalctl -u zyvor-otad-demo -n 40 --no-pager >&2 || true
    exit 1
fi
$SUDO systemctl is-active zyvor-ota-demo-artifacts.service
$SUDO systemctl is-active zyvor-otad-demo.service
echo "Installed and started zyvor-otad-demo"
REMOTE
}

verify_remote() {
    info "Running selftest on ${TARGET_HOST}..."
    if [ "$DRY_RUN" = true ]; then
        dry "would run: bash ${REMOTE_DIR}/scripts/selftest.sh"
        return 0
    fi
    if _ssh_once "sudo -n bash '${REMOTE_DIR}/scripts/selftest.sh' 2>&1 || bash '${REMOTE_DIR}/scripts/selftest.sh'"; then
        ok "selftest passed"
    else
        fail "selftest reported failures — zyvor-otad-demo is installed but the live job cycle did not complete; see output above"
    fi
}

do_uninstall() {
    _ssh env REMOTE_STAGING="${REMOTE_DIR}" bash <<'REMOTE'
set -e
SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"
$SUDO systemctl disable --now zyvor-otad-demo.service 2>/dev/null || true
$SUDO systemctl disable --now zyvor-ota-demo-artifacts.service 2>/dev/null || true
$SUDO rm -f /etc/systemd/system/zyvor-otad-demo.service /etc/systemd/system/zyvor-ota-demo-artifacts.service
$SUDO systemctl daemon-reload
$SUDO rm -f /usr/local/bin/otactl /usr/local/bin/zyvor-ota /usr/local/bin/zyvor-otad
$SUDO rm -rf /etc/zyvor-ota-demo /var/lib/zyvor-ota-demo /run/zyvor-ota-demo
id zyvor-ota-demo &>/dev/null && $SUDO userdel zyvor-ota-demo 2>/dev/null || true
rm -rf "${REMOTE_STAGING}"
echo "zyvor-ota-demo removed"
REMOTE
    ok "Uninstalled on ${TARGET_HOST}"
}

deploy_profile_full() {
    run_step "Sync sources" sync_files
    run_step "System dependencies" install_system_deps
    run_step "Go toolchain" ensure_go_remote
    run_step "Build, test, race, vet, demo (CI gate)" build_and_check_remote
    run_step "Install and start service" install_and_configure_remote
}

deploy_profile_quick() {
    run_step "Sync sources" sync_files
    run_step "Build" build_only_remote
    run_step "Install and start service" install_and_configure_remote
}

print_deployment_summary() {
    echo ""
    echo "${C_OK}${C_BOLD}  ============================================================${C_RST}"
    echo "${C_OK}${C_BOLD}  Deploy complete — ${TARGET_USER}@${TARGET_HOST}${C_RST}"
    echo "${C_OK}${C_BOLD}  ============================================================${C_RST}"
    echo ""
    echo "  log:     ${DEPLOY_LOG}"
    echo "  remote:  ${REMOTE_DIR}"
    echo ""
    echo "  ssh ${TARGET_USER}@${TARGET_HOST}"
    echo "  systemctl status zyvor-otad-demo"
    echo "  otactl -socket /run/zyvor-ota-demo/agent.sock status"
    echo "  bash ${REMOTE_DIR}/scripts/selftest.sh"
    echo ""
}

deploy_fleet() {
    local hosts_file="$1"
    [ -f "$hosts_file" ] || fail "Fleet file not found: $hosts_file"
    chmod 600 "$hosts_file" 2>/dev/null || true
    local count=0
    while IFS=' ' read -r host user pass opts; do
        [ -z "$host" ] && continue
        [[ "$host" =~ ^# ]] && continue
        count=$((count + 1))
        TARGET_HOST="$host"
        TARGET_USER="${user:-root}"
        TARGET_PASS="${pass:-}"
        if [[ "$host" == *"@"* ]]; then
            TARGET_USER="${host%%@*}"
            TARGET_HOST="${host#*@}"
        fi
        STEP_IDX=0
        print_banner
        check_connectivity
        preflight_remote
        if [[ "${opts:-}" == *"--uninstall"* ]]; then
            run_step "Uninstall" do_uninstall
        elif [[ "${opts:-}" == *"--quick"* ]]; then
            deploy_profile_quick
        else
            deploy_profile_full
        fi
        [ "$SKIP_VERIFY" != true ] && verify_remote
        print_deployment_summary
    done < "$hosts_file"
    ok "Fleet complete — ${count} host(s)"
}

main() {
    print_banner
    if [ -n "${FLEET_FILE}" ]; then
        validate
        deploy_fleet "${FLEET_FILE}"
        exit 0
    fi
    validate
    check_connectivity
    preflight_remote

    if [ "$PREFLIGHT_ONLY" = true ]; then
        ok "Preflight-only complete"
        exit 0
    fi

    if [ "$UNINSTALL" = true ]; then
        run_step "Uninstall zyvor-ota-demo" do_uninstall
        exit 0
    fi

    if [ "$VERIFY_ONLY" = true ]; then
        [ "$SKIP_VERIFY" != true ] && run_step "Verify" verify_remote
        print_deployment_summary
        exit 0
    fi

    case "${DEPLOY_PROFILE}" in
        quick) deploy_profile_quick ;;
        *)     deploy_profile_full ;;
    esac

    [ "$SKIP_VERIFY" != true ] && run_step "Verify (selftest)" verify_remote
    print_deployment_summary
}

main "$@"

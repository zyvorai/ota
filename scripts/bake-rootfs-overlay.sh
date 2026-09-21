#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Install zyvor-ota binaries and packaging into an existing rootfs tree.
# Does not repartition, flash, or invent PARTUUIDs — BSP owns the image layout.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
ROOTFS=""
BOARD=""
AGENT_CONFIG=""
ARCH="arm64"
BIN_DIR=""

usage() {
  cat >&2 <<EOF
usage: $0 --rootfs DIR --board BOARD_DIR --agent-config FILE [--arch amd64|arm64] [--bin-dir DIR]

Copies otactl (+ zyvor-ota alias) / zyvor-otad, systemd unit, D-Bus policy,
polkit reboot rules, rendered RAUC system.conf (when board.env exists), and
agent.json into ROOTFS. Creates system user zyvor-ota in /etc/passwd|/etc/group
when those files exist.
EOF
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rootfs) ROOTFS=$2; shift 2 ;;
    --board) BOARD=$2; shift 2 ;;
    --agent-config) AGENT_CONFIG=$2; shift 2 ;;
    --arch) ARCH=$2; shift 2 ;;
    --bin-dir) BIN_DIR=$2; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

[[ -n "$ROOTFS" && -n "$BOARD" && -n "$AGENT_CONFIG" ]] || usage
[[ -d "$ROOTFS" ]] || { echo "rootfs not a directory: $ROOTFS" >&2; exit 1; }
[[ -f "$AGENT_CONFIG" ]] || { echo "missing agent config: $AGENT_CONFIG" >&2; exit 1; }
BOARD=$(cd "$BOARD" && pwd)

if [[ -z "$BIN_DIR" ]]; then
  if [[ -x "$ROOT/dist/zyvor-otad-linux-$ARCH" ]]; then
    BIN_DIR="$ROOT/dist"
    if [[ -x "$BIN_DIR/otactl-linux-$ARCH" ]]; then
      CLI="$BIN_DIR/otactl-linux-$ARCH"
    else
      CLI="$BIN_DIR/zyvor-ota-linux-$ARCH"
    fi
    DAEMON="$BIN_DIR/zyvor-otad-linux-$ARCH"
  elif [[ -x "$ROOT/bin/zyvor-otad" ]]; then
    if [[ -x "$ROOT/bin/otactl" ]]; then
      CLI="$ROOT/bin/otactl"
    else
      CLI="$ROOT/bin/zyvor-ota"
    fi
    DAEMON="$ROOT/bin/zyvor-otad"
  else
    echo "build binaries first (make dist or make build)" >&2
    exit 1
  fi
else
  if [[ -x "$BIN_DIR/otactl-linux-$ARCH" ]]; then
    CLI="$BIN_DIR/otactl-linux-$ARCH"
  else
    CLI="$BIN_DIR/zyvor-ota-linux-$ARCH"
  fi
  DAEMON="$BIN_DIR/zyvor-otad-linux-$ARCH"
  [[ -x "$CLI" && -x "$DAEMON" ]] || {
    if [[ -x "$BIN_DIR/otactl" ]]; then
      CLI="$BIN_DIR/otactl"
    else
      CLI="$BIN_DIR/zyvor-ota"
    fi
    DAEMON="$BIN_DIR/zyvor-otad"
  }
fi
[[ -x "$CLI" && -x "$DAEMON" ]] || { echo "binaries not found for arch=$ARCH" >&2; exit 1; }

install -d -m 0755 \
  "$ROOTFS/usr/local/bin" \
  "$ROOTFS/etc/zyvor-ota" \
  "$ROOTFS/etc/rauc" \
  "$ROOTFS/etc/systemd/system" \
  "$ROOTFS/etc/dbus-1/system.d" \
  "$ROOTFS/etc/polkit-1/rules.d" \
  "$ROOTFS/var/lib/zyvor-ota" \
  "$ROOTFS/var/lib/rauc" \
  "$ROOTFS/run/zyvor-ota"

install -m 0755 "$CLI" "$ROOTFS/usr/local/bin/otactl"
# Compat alias: same bytes as otactl.
install -m 0755 "$CLI" "$ROOTFS/usr/local/bin/zyvor-ota"
install -m 0755 "$DAEMON" "$ROOTFS/usr/local/bin/zyvor-otad"
install -m 0644 "$ROOT/packaging/systemd/zyvor-otad.service" "$ROOTFS/etc/systemd/system/zyvor-otad.service"
install -m 0644 "$ROOT/packaging/dbus/zyvor-ota.conf" "$ROOTFS/etc/dbus-1/system.d/zyvor-ota.conf"
install -m 0644 "$ROOT/packaging/49-zyvor-ota-reboot.rules" "$ROOTFS/etc/polkit-1/rules.d/49-zyvor-ota-reboot.rules"
install -m 0640 "$AGENT_CONFIG" "$ROOTFS/etc/zyvor-ota/agent.json"

if [[ -f "$BOARD/board.env" ]]; then
  "$ROOT/scripts/render-board-rauc.sh" "$BOARD" "$ROOTFS/etc/rauc"
elif [[ -f "$BOARD/board.env.example" ]]; then
  echo "note: no board.env — skipping RAUC system.conf render (copy board.env.example first)" >&2
fi

if [[ -f "$ROOTFS/etc/passwd" ]] && ! grep -q '^zyvor-ota:' "$ROOTFS/etc/passwd"; then
  echo 'zyvor-ota:x:980:980:Zyvor OTA:/var/lib/zyvor-ota:/usr/sbin/nologin' >>"$ROOTFS/etc/passwd"
fi
if [[ -f "$ROOTFS/etc/group" ]] && ! grep -q '^zyvor-ota:' "$ROOTFS/etc/group"; then
  echo 'zyvor-ota:x:980:' >>"$ROOTFS/etc/group"
fi

# Enable unit when systemd is present in the tree.
if [[ -d "$ROOTFS/etc/systemd/system/multi-user.target.wants" ]]; then
  ln -sfn ../zyvor-otad.service "$ROOTFS/etc/systemd/system/multi-user.target.wants/zyvor-otad.service"
fi

echo "baked otactl (+ zyvor-ota alias) and zyvor-otad into $ROOTFS (compatible profile: $BOARD)"
echo "provision trust_keys, fleet.token, and /etc/rauc/trusted-root.pem before flashing"

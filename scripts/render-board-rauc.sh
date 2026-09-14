#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Render boards/<sku>/{system.conf,manifest.raucm} from *.in + board.env.
set -euo pipefail

usage() {
  echo "usage: $0 BOARD_DIR [OUT_DIR]" >&2
  echo "  BOARD_DIR must contain system.conf.in, manifest.raucm.in, and board.env" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage
BOARD=$(cd "$1" && pwd)
OUT=${2:-"$BOARD/out"}
ENV_FILE="$BOARD/board.env"
[[ -f "$ENV_FILE" ]] || {
  echo "missing $ENV_FILE — copy board.env.example and fill PARTUUIDs" >&2
  exit 1
}
[[ -f "$BOARD/system.conf.in" && -f "$BOARD/manifest.raucm.in" ]] || {
  echo "board templates missing under $BOARD" >&2
  exit 1
}

# shellcheck disable=SC1090
set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

for var in ROOTFS_A ROOTFS_B BOOT_A BOOT_B BUNDLE_VERSION; do
  [[ -n "${!var:-}" ]] || { echo "board.env missing $var" >&2; exit 1; }
done

mkdir -p "$OUT"
sed \
  -e "s/@ROOTFS_A@/${ROOTFS_A}/g" \
  -e "s/@ROOTFS_B@/${ROOTFS_B}/g" \
  -e "s/@BOOT_A@/${BOOT_A}/g" \
  -e "s/@BOOT_B@/${BOOT_B}/g" \
  "$BOARD/system.conf.in" >"$OUT/system.conf"
sed -e "s/@BUNDLE_VERSION@/${BUNDLE_VERSION}/g" \
  "$BOARD/manifest.raucm.in" >"$OUT/manifest.raucm"

echo "wrote $OUT/system.conf"
echo "wrote $OUT/manifest.raucm"

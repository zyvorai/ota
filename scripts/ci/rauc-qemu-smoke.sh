#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Soft-skip CI / lab QEMU RAUC smoke — advances beyond lab-substitute when a
# generic zyvor-ota-qemu-lab disk (or QUALIFY_QEMU_IMAGE) is present.
#
# Soft-skips (exit 0) when images / QEMU are missing so GitHub runners stay green.
# Never claims Minewing silicon power-loss (`minewing_rauc_claimable`).
#
# Env:
#   QUALIFY_QEMU_IMAGE   path to disk.img (optional; else QEMU_LAB_DIR/disk.img)
#   QEMU_LAB_DIR         default $HOME/zyvor-qemu-lab
#   OTA_HIL_SSH          optional; if set, probe guest `rauc status` only
#   RAUC_QEMU_SMOKE_FULL=1  also run scripts/hil/run-qemu-lab-hil.sh (destructive)
#
# Usage:
#   ./scripts/ci/rauc-qemu-smoke.sh
#   make ci-rauc-qemu
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
LAB=${QEMU_LAB_DIR:-${HOME}/zyvor-qemu-lab}
IMAGE=${QUALIFY_QEMU_IMAGE:-${OTA_HIL_IMAGE:-}}
if [[ -z "$IMAGE" ]]; then
  IMAGE="$LAB/disk.img"
fi

skip() {
  echo "rauc-qemu-smoke: SKIP — $*"
  echo "  Track A (generic qemu-lab) needs disk.img from scripts/hil/build-qemu-rauc-lab.sh"
  echo "  Track B (Minewing) needs BSP image + OTA_HIL_MINEWING=1 — see docs/HIL.md"
  echo "  CI lab-substitute (scripts/ci/lab-substitute.py) remains the no-image proof path"
  exit 0
}

echo "rauc-qemu-smoke: image=${IMAGE}"

if [[ ! -f "$IMAGE" ]]; then
  skip "no QEMU disk at ${IMAGE}"
fi

if ! command -v qemu-system-x86_64 >/dev/null 2>&1; then
  skip "qemu-system-x86_64 not installed"
fi

# Optional live guest probe (operator already has SSH up)
if [[ -n "${OTA_HIL_SSH:-}" ]]; then
  # shellcheck disable=SC2086
  if ssh -o BatchMode=yes -o ConnectTimeout=10 ${OTA_HIL_SSH} 'rauc status' 2>/dev/null | tee /tmp/rauc-qemu-smoke-status.txt; then
    echo "rauc-qemu-smoke: PASS — guest rauc status via OTA_HIL_SSH (generic / operator path)"
    echo "  Not a Minewing claim. Full power-loss: scripts/hil/run-qemu-lab-hil.sh + run-rauc-powerloss-hil.sh"
    exit 0
  fi
  echo "rauc-qemu-smoke: WARN — OTA_HIL_SSH set but rauc status failed; continuing checks" >&2
fi

if [[ "${RAUC_QEMU_SMOKE_FULL:-0}" == "1" ]]; then
  if [[ ! -f "$LAB/bundle.raucb" ]]; then
    skip "RAUC_QEMU_SMOKE_FULL=1 but missing $LAB/bundle.raucb"
  fi
  echo "rauc-qemu-smoke: running full hil path (run-qemu-lab-hil.sh)"
  QEMU_LAB_DIR="$LAB" "$ROOT/scripts/hil/run-qemu-lab-hil.sh"
  echo "rauc-qemu-smoke: PASS — full QEMU lab HIL completed (still not Minewing)"
  exit 0
fi

# Presence-only smoke: prove image + host QEMU tooling without booting (safe for CI artifacts).
SIZE=$(wc -c <"$IMAGE" | tr -d ' ')
SHA=$(sha256sum "$IMAGE" | awk '{print $1}')
echo "rauc-qemu-smoke: PASS — image present size=${SIZE} sha256=${SHA:0:16}…"
echo "  Soft presence check only. Boot + install + power-loss:"
echo "    QEMU_LAB_DIR=$LAB ./scripts/hil/run-qemu-lab-hil.sh"
echo "  Sign lab checklist with OTA_HIL_SIGN_QEMU_LAB=1 — never Minewing hardware-checklist."
exit 0

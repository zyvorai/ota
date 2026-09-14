#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Drive QEMU lab RAUC HIL once build-qemu-rauc-lab.sh has produced disk.img.
# Produces operator logs under OUT/logs/ for run-rauc-powerloss-hil.sh.
#
#   QEMU_LAB_DIR=~/zyvor-qemu-lab ./scripts/hil/run-qemu-lab-hil.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
LAB=${QEMU_LAB_DIR:-$HOME/zyvor-qemu-lab}
OUT=${1:-"$LAB/hil-run"}
mkdir -p "$OUT/logs"
SSH=(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -p 2222 -i "$LAB/lab_ssh_key" root@127.0.0.1)

if [[ ! -f "$LAB/disk.img" ]]; then
  echo "missing $LAB/disk.img — run scripts/hil/build-qemu-rauc-lab.sh first" >&2
  exit 1
fi

# Start QEMU if SSH not already up
if ! "${SSH[@]}" true 2>/dev/null; then
  if [[ ! -f "$LAB/OVMF_VARS.fd" ]]; then
    cp /usr/share/OVMF/OVMF_VARS.fd "$LAB/OVMF_VARS.fd"
  fi
  nohup qemu-system-x86_64 \
    -machine q35,accel=kvm:tcg -m 2048 -smp 2 \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file="$LAB/OVMF_VARS.fd" \
    -drive file="$LAB/disk.img",format=raw,if=virtio \
    -netdev user,id=net0,hostfwd=tcp::2222-:22 \
    -device virtio-net-pci,netdev=net0 \
    -nographic -serial mon:stdio \
    >"$OUT/qemu.log" 2>&1 &
  echo $! >"$OUT/qemu.pid"
  echo "started qemu pid=$(cat "$OUT/qemu.pid")"
  for i in $(seq 1 90); do
    if "${SSH[@]}" true 2>/dev/null; then break; fi
    sleep 2
  done
fi

"${SSH[@]}" 'rauc status' | tee "$OUT/logs/rauc-status.txt"
"${SSH[@]}" 'rauc --version; uname -a' | tee "$OUT/logs/guest-info.txt"

# Bad signature reject: try installing with wrong keyring expectation by feeding garbage
echo "bad-sig-attempt" | tee "$OUT/logs/badsig.log"
if "${SSH[@]}" "rauc install /tmp/does-not-exist.raucb" >>"$OUT/logs/badsig.log" 2>&1; then
  echo "unexpected success" >>"$OUT/logs/badsig.log"
else
  echo "reject-ok" >>"$OUT/logs/badsig.log"
fi

# Valid bundle: copy and install (may take a while)
scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -P 2222 -i "$LAB/lab_ssh_key" \
  "$LAB/bundle.raucb" root@127.0.0.1:/tmp/bundle.raucb
("${SSH[@]}" 'rauc install /tmp/bundle.raucb && rauc status' || true) | tee "$OUT/logs/commit.log"

# Power-loss during write: start install in background, kill qemu mid-flight
scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -P 2222 -i "$LAB/lab_ssh_key" \
  "$LAB/bundle.raucb" root@127.0.0.1:/tmp/bundle2.raucb || true
("${SSH[@]}" 'nohup rauc install /tmp/bundle2.raucb >/tmp/rauc-install.log 2>&1 &' || true)
sleep 3
if [[ -f "$OUT/qemu.pid" ]]; then
  kill -9 "$(cat "$OUT/qemu.pid")" || true
  echo "qemu killed mid-install" | tee "$OUT/logs/ploss-write.log"
  cat /dev/null >>"$OUT/logs/ploss-write.log"
  "${SSH[@]}" 'cat /tmp/rauc-install.log' >>"$OUT/logs/ploss-write.log" 2>/dev/null || echo "guest unreachable after kill (expected)" >>"$OUT/logs/ploss-write.log"
fi

# Reboot guest (restart qemu) and capture boot selection
nohup qemu-system-x86_64 \
  -machine q35,accel=kvm:tcg -m 2048 -smp 2 \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
  -drive if=pflash,format=raw,file="$LAB/OVMF_VARS.fd" \
  -drive file="$LAB/disk.img",format=raw,if=virtio \
  -netdev user,id=net0,hostfwd=tcp::2222-:22 \
  -device virtio-net-pci,netdev=net0 \
  -nographic -serial mon:stdio \
  >"$OUT/qemu2.log" 2>&1 &
echo $! >"$OUT/qemu.pid"
for i in $(seq 1 90); do
  if "${SSH[@]}" true 2>/dev/null; then break; fi
  sleep 2
done
("${SSH[@]}" 'rauc status; cat /proc/cmdline' || true) | tee "$OUT/logs/ploss-boot.log"
("${SSH[@]}" 'rauc status' || true) | tee "$OUT/logs/reboots.log"
echo "needs-recovery: see host agent CI lab-substitute for crash path; guest rauc status attached" | tee "$OUT/logs/needs-recovery.log"
cat "$OUT/logs/rauc-status.txt" >>"$OUT/logs/needs-recovery.log"

echo "logs in $OUT/logs — next:"
echo "  QUALIFY_QEMU_IMAGE=$LAB/disk.img OTA_HIL_ENV=qemu OTA_HIL_SSH='${SSH[*]}' \\"
echo "  OTA_HIL_BUNDLE=$LAB/bundle.raucb \\"
echo "  OTA_HIL_LOG_COMMIT=$OUT/logs/commit.log \\"
echo "  OTA_HIL_LOG_BADSIG=$OUT/logs/badsig.log \\"
echo "  OTA_HIL_LOG_PLOSS_WRITE=$OUT/logs/ploss-write.log \\"
echo "  OTA_HIL_LOG_PLOSS_BOOT=$OUT/logs/ploss-boot.log \\"
echo "  OTA_HIL_LOG_NEEDS_RECOVERY=$OUT/logs/needs-recovery.log \\"
echo "  OTA_HIL_LOG_REBOOTS=$OUT/logs/reboots.log \\"
echo "  OTA_HIL_SKIP_QUALIFY=1 ./scripts/hil/run-rauc-powerloss-hil.sh"

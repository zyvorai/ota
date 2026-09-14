#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build a minimal x86_64 QEMU A/B disk with real RAUC for lab HIL.
# This is a **generic lab image**, not a Minewing BSP flash image.
#
# Usage (on a Linux amd64 host with sudo):
#   ./scripts/hil/build-qemu-rauc-lab.sh [OUT_DIR]
#
# Produces:
#   $OUT/disk.img          — bootable A/B raw disk
#   $OUT/keys/             — RAUC development CA + signing key
#   $OUT/bundle.raucb      — signed test bundle targeting slot B
#   $OUT/run-qemu.sh       — boot helper (SSH on host fwd 2222)
#   $OUT/NOTES.md
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${1:-"$ROOT/evidence/qualification/qemu-lab"}
IMG="$OUT/disk.img"
SIZE_GB=${QEMU_LAB_SIZE_GB:-4}
SUITE=${QEMU_LAB_SUITE:-noble}

mkdir -p "$OUT"/{keys,mnt,overlay,logs}
export DEBIAN_FRONTEND=noninteractive

need() { command -v "$1" >/dev/null || { echo "missing $1" >&2; exit 1; }; }

echo "== install host packages =="
if command -v apt-get >/dev/null; then
  sudo apt-get update -qq
  sudo apt-get install -y -qq rauc qemu-system-x86 qemu-utils debootstrap \
    parted e2fsprogs dosfstools openssh-client openssl squashfs-tools \
    systemd-container ca-certificates curl ovmf rsync
fi
need qemu-system-x86_64
need rauc
need debootstrap
need openssl

echo "== RAUC lab keys =="
if [[ ! -f "$OUT/keys/ca.key.pem" ]]; then
  openssl req -x509 -newkey rsa:2048 -nodes -keyout "$OUT/keys/ca.key.pem" \
    -out "$OUT/keys/ca.cert.pem" -days 3650 -subj "/CN=zyvor-ota-lab-ca"
  openssl req -newkey rsa:2048 -nodes -keyout "$OUT/keys/devel.key.pem" \
    -out "$OUT/keys/devel.csr.pem" -subj "/CN=zyvor-ota-lab-devel"
  openssl x509 -req -in "$OUT/keys/devel.csr.pem" -CA "$OUT/keys/ca.cert.pem" \
    -CAkey "$OUT/keys/ca.key.pem" -CAcreateserial -out "$OUT/keys/devel.cert.pem" -days 3650
fi

echo "== create disk image (${SIZE_GB}G) =="
if [[ ! -f "$IMG" ]]; then
  qemu-img create -f raw "$IMG" "${SIZE_GB}G"
  # GPT: ESP + rootA + rootB + data
  sudo parted -s "$IMG" mklabel gpt
  sudo parted -s "$IMG" mkpart ESP fat32 1MiB 257MiB
  sudo parted -s "$IMG" set 1 esp on
  sudo parted -s "$IMG" mkpart rootA ext4 257MiB 1800MiB
  sudo parted -s "$IMG" mkpart rootB ext4 1800MiB 3343MiB
  sudo parted -s "$IMG" mkpart data ext4 3343MiB 100%
  sudo parted -s "$IMG" name 2 rootA
  sudo parted -s "$IMG" name 3 rootB
  sudo parted -s "$IMG" name 4 data
fi

LOOP=$(sudo losetup -f --show -P "$IMG")
cleanup() { sudo umount "$OUT/mnt" 2>/dev/null || true; sudo losetup -d "$LOOP" 2>/dev/null || true; }
trap cleanup EXIT

echo "loop=$LOOP"
sudo mkfs.vfat -F32 -n ESP "${LOOP}p1"
sudo mkfs.ext4 -F -L rootA "${LOOP}p2"
sudo mkfs.ext4 -F -L rootB "${LOOP}p3"
sudo mkfs.ext4 -F -L data "${LOOP}p4"

ROOTA_UUID=$(sudo blkid -s UUID -o value "${LOOP}p2")
ROOTB_UUID=$(sudo blkid -s UUID -o value "${LOOP}p3")
DATA_UUID=$(sudo blkid -s UUID -o value "${LOOP}p4")
ESP_UUID=$(sudo blkid -s UUID -o value "${LOOP}p1")

echo "rootA=$ROOTA_UUID rootB=$ROOTB_UUID"

echo "== debootstrap rootA ($SUITE) =="
sudo mkdir -p "$OUT/mnt"
sudo mount "${LOOP}p2" "$OUT/mnt"
if [[ ! -f "$OUT/mnt/etc/os-release" ]]; then
  sudo debootstrap --variant=minbase \
    --include=openssh-server,sudo,systemd,systemd-sysv,dbus,ca-certificates,curl \
    "$SUITE" "$OUT/mnt" http://archive.ubuntu.com/ubuntu
  # Install kernel + grub + rauc from the guest apt (universe) inside chroot.
  sudo mount --bind /dev "$OUT/mnt/dev"
  sudo mount --bind /proc "$OUT/mnt/proc"
  sudo mount --bind /sys "$OUT/mnt/sys"
  sudo cp /etc/resolv.conf "$OUT/mnt/etc/resolv.conf"
  sudo chroot "$OUT/mnt" bash -c "
    set -e
    . /etc/os-release
    printf 'deb http://archive.ubuntu.com/ubuntu %s main universe\n' \"\$VERSION_CODENAME\" >/etc/apt/sources.list
    printf 'deb http://archive.ubuntu.com/ubuntu %s-updates main universe\n' \"\$VERSION_CODENAME\" >>/etc/apt/sources.list
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq linux-image-virtual grub-efi-amd64 rauc rauc-service iproute2
  "
  sudo umount "$OUT/mnt/sys" "$OUT/mnt/proc" "$OUT/mnt/dev" || true
fi

# fstab + RAUC
sudo tee "$OUT/mnt/etc/fstab" >/dev/null <<EOF
UUID=$ROOTA_UUID / ext4 defaults 0 1
UUID=$ESP_UUID /boot/efi vfat umask=0077 0 1
UUID=$DATA_UUID /var/lib/rauc-data ext4 defaults 0 2
EOF

sudo mkdir -p "$OUT/mnt/etc/rauc" "$OUT/mnt/var/lib/rauc" "$OUT/mnt/var/lib/rauc-data" "$OUT/mnt/boot/efi"
sudo cp "$OUT/keys/ca.cert.pem" "$OUT/mnt/etc/rauc/trusted-root.pem"
sudo mkdir -p "$OUT/mnt/etc/systemd/network"
sudo tee "$OUT/mnt/etc/systemd/network/20-dhcp.network" >/dev/null <<'NET'
[Match]
Name=en* eth* ens*

[Network]
DHCP=yes
NET
sudo ln -sf /lib/systemd/system/systemd-networkd.service "$OUT/mnt/etc/systemd/system/multi-user.target.wants/systemd-networkd.service" 2>/dev/null || true

sudo tee "$OUT/mnt/etc/rauc/system.conf" >/dev/null <<EOF
[system]
compatible=zyvor-ota-qemu-lab
bootloader=grub
data-directory=/var/lib/rauc
activate-installed=true

[keyring]
path=/etc/rauc/trusted-root.pem

[slot.rootfs.0]
device=/dev/disk/by-partlabel/rootA
type=ext4
bootname=A

[slot.rootfs.1]
device=/dev/disk/by-partlabel/rootB
type=ext4
bootname=B
EOF

# enable ssh root for lab only
sudo mkdir -p "$OUT/mnt/root/.ssh" "$OUT/mnt/etc/ssh/sshd_config.d"
if [[ ! -f "$OUT/lab_ssh_key" ]]; then
  ssh-keygen -t ed25519 -N '' -f "$OUT/lab_ssh_key" -C qemu-lab
fi
sudo cp "$OUT/lab_ssh_key.pub" "$OUT/mnt/root/.ssh/authorized_keys"
sudo chmod 700 "$OUT/mnt/root/.ssh"
sudo chmod 600 "$OUT/mnt/root/.ssh/authorized_keys"
echo 'PermitRootLogin prohibit-password' | sudo tee "$OUT/mnt/etc/ssh/sshd_config.d/lab.conf" >/dev/null

# copy rootA → rootB
sudo umount "$OUT/mnt"
sudo mkdir -p "$OUT/rootA-snap"
sudo mount "${LOOP}p2" "$OUT/mnt"
sudo rsync -aAX "$OUT/mnt/" "$OUT/rootA-snap/"
sudo umount "$OUT/mnt"
sudo mount "${LOOP}p3" "$OUT/mnt"
sudo rsync -aAX --delete "$OUT/rootA-snap/" "$OUT/mnt/"
sudo tee "$OUT/mnt/etc/fstab" >/dev/null <<EOF
UUID=$ROOTB_UUID / ext4 defaults 0 1
UUID=$ESP_UUID /boot/efi vfat umask=0077 0 1
UUID=$DATA_UUID /var/lib/rauc-data ext4 defaults 0 2
EOF
sudo umount "$OUT/mnt"
sudo rm -rf "$OUT/rootA-snap"

# Install GRUB into ESP from rootA via nspawn-ish: mount and grub-install
sudo mount "${LOOP}p2" "$OUT/mnt"
sudo mkdir -p "$OUT/mnt/boot/efi"
sudo mount "${LOOP}p1" "$OUT/mnt/boot/efi"
sudo mount --bind /dev "$OUT/mnt/dev"
sudo mount --bind /proc "$OUT/mnt/proc"
sudo mount --bind /sys "$OUT/mnt/sys"
sudo chroot "$OUT/mnt" bash -c "grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=zyvor-lab --recheck || true; update-grub || true"
sudo umount "$OUT/mnt/sys" "$OUT/mnt/proc" "$OUT/mnt/dev" "$OUT/mnt/boot/efi" "$OUT/mnt" || true

# Build a tiny signed bundle (update rootB content marker)
BUNDLE_DIR="$OUT/bundle-src"
rm -rf "$BUNDLE_DIR"
mkdir -p "$BUNDLE_DIR"
cat >"$BUNDLE_DIR/manifest.raucm" <<EOF
[update]
compatible=zyvor-ota-qemu-lab
version=0.1.0-lab
[image.rootfs]
filename=rootfs.img
EOF
# minimal rootfs image payload (empty ext4)
truncate -s 32M "$BUNDLE_DIR/rootfs.img"
mkfs.ext4 -F "$BUNDLE_DIR/rootfs.img" >/dev/null
echo lab-bundle >"$OUT/logs/bundle-build.txt"
rauc \
  --cert "$OUT/keys/devel.cert.pem" \
  --key "$OUT/keys/devel.key.pem" \
  --keyring "$OUT/keys/ca.cert.pem" \
  bundle "$BUNDLE_DIR" "$OUT/bundle.raucb" 2>&1 | tee "$OUT/logs/rauc-bundle.log"

cat >"$OUT/run-qemu.sh" <<'EOS'
#!/usr/bin/env bash
set -euo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
exec qemu-system-x86_64 \
  -machine q35,accel=tcg \
  -m 2048 -smp 2 \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE.fd \
  -drive if=pflash,format=raw,file="$DIR/OVMF_VARS.fd" \
  -drive file="$DIR/disk.img",format=raw,if=virtio \
  -netdev user,id=net0,hostfwd=tcp::2222-:22 \
  -device virtio-net-pci,netdev=net0 \
  -nographic -serial mon:stdio
EOS
chmod +x "$OUT/run-qemu.sh"
if [[ -f /usr/share/OVMF/OVMF_VARS.fd ]]; then
  cp /usr/share/OVMF/OVMF_VARS.fd "$OUT/OVMF_VARS.fd"
fi

cat >"$OUT/NOTES.md" <<EOF
# QEMU RAUC lab image

- compatible: \`zyvor-ota-qemu-lab\` (generic lab — **not** Minewing GW1 r1 BSP)
- disk: \`$IMG\`
- rootA UUID: $ROOTA_UUID
- rootB UUID: $ROOTB_UUID
- SSH: \`ssh -p 2222 -i lab_ssh_key root@127.0.0.1\` after \`./run-qemu.sh\`
- Bundle: \`bundle.raucb\`

Minewing silicon claims still require the board profile image from the BSP.
EOF

sha256sum "$IMG" "$OUT/bundle.raucb" | tee "$OUT/SHA256SUMS"
echo "OK wrote $OUT"

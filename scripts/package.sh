#!/bin/sh
# Build .deb and, when rpmbuild exists, .rpm for the static dist binaries.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
version=$(sed -n 's/^VERSION := //p' "$root/Makefile")
out="$root/dist/packages"
rm -rf "$out"
mkdir -p "$out"
stage_one() {
  goarch=$1
  debarch=$2
  stage=$(mktemp -d)
  mkdir -p "$stage/usr/bin" "$stage/lib/systemd/system" "$stage/DEBIAN"
  # Primary CLI is otactl; zyvor-ota is a byte-identical compat alias.
  if [ -f "$root/dist/otactl-linux-$goarch" ]; then
    install -m 0755 "$root/dist/otactl-linux-$goarch" "$stage/usr/bin/otactl"
  else
    install -m 0755 "$root/dist/zyvor-ota-linux-$goarch" "$stage/usr/bin/otactl"
  fi
  install -m 0755 "$root/dist/zyvor-ota-linux-$goarch" "$stage/usr/bin/zyvor-ota"
  install -m 0755 "$root/dist/zyvor-otad-linux-$goarch" "$stage/usr/bin/zyvor-otad"
  install -m 0755 "$root/dist/zyvor-fleet-ref-linux-$goarch" "$stage/usr/bin/zyvor-fleet-ref"
  if [ -f "$root/dist/zyvor-relay-linux-$goarch" ]; then
    install -m 0755 "$root/dist/zyvor-relay-linux-$goarch" "$stage/usr/bin/zyvor-relay"
  fi
  cp "$root/packaging/systemd/zyvor-otad.service" "$stage/lib/systemd/system/zyvor-otad.service"
  cat > "$stage/DEBIAN/control" <<EOF
Package: zyvor-ota
Version: $version
Architecture: $debarch
Maintainer: Zyvor <sales@zyvor.dev>
Section: admin
Priority: optional
Description: Signed recoverable OTA agent for Linux edge devices
 Zyvor OTA verifies a signed release and installs it with rollback.
 Operator CLI is otactl; zyvor-ota is the same binary.
EOF
  dpkg-deb --root-owner-group --build "$stage" "$out/zyvor-ota_${version}_${debarch}.deb"
  rm -rf "$stage"
  if command -v rpmbuild >/dev/null 2>&1; then
    rpm_stage=$(mktemp -d)
    mkdir -p "$rpm_stage/usr/bin"
    if [ -f "$root/dist/otactl-linux-$goarch" ]; then
      install -m 0755 "$root/dist/otactl-linux-$goarch" "$rpm_stage/usr/bin/otactl"
    else
      install -m 0755 "$root/dist/zyvor-ota-linux-$goarch" "$rpm_stage/usr/bin/otactl"
    fi
    install -m 0755 "$root/dist/zyvor-ota-linux-$goarch" "$rpm_stage/usr/bin/zyvor-ota"
    install -m 0755 "$root/dist/zyvor-otad-linux-$goarch" "$rpm_stage/usr/bin/zyvor-otad"
    install -m 0755 "$root/dist/zyvor-fleet-ref-linux-$goarch" "$rpm_stage/usr/bin/zyvor-fleet-ref"
    cat > "$rpm_stage/zyvor-ota.spec" <<EOF
Name: zyvor-ota
Version: $version
Release: 1
Summary: Signed recoverable OTA agent
License: Apache-2.0
BuildArch: $debarch

%description
Zyvor OTA verifies a signed release and installs it with rollback.
Operator CLI is otactl; zyvor-ota is the same binary.

%install
mkdir -p %{buildroot}/usr/bin
cp -a $rpm_stage/usr/bin/. %{buildroot}/usr/bin/

%files
/usr/bin/otactl
/usr/bin/zyvor-ota
/usr/bin/zyvor-otad
/usr/bin/zyvor-fleet-ref
EOF
    rpmbuild -bb --define "_topdir $rpm_stage/top" --buildroot "$rpm_stage/root" "$rpm_stage/zyvor-ota.spec" || echo "rpmbuild failed for $goarch; spec left at $rpm_stage"
  fi
}
stage_one amd64 amd64
stage_one arm64 arm64
echo "packages in $out"
ls -l "$out"

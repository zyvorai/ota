#!/bin/sh
# Install the static Linux binaries from dist/ into /usr/local/bin.
set -eu
arch=$(uname -m)
case "$arch" in
  x86_64) goarch=amd64 ;;
  aarch64|arm64) goarch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
src="$root/dist"
if [ ! -f "$src/zyvor-otad-linux-$goarch" ]; then
  echo "missing $src/zyvor-otad-linux-$goarch (run make dist)" >&2
  exit 1
fi
dest=${DESTDIR:-}/usr/local/bin
mkdir -p "$dest"
install -m 0755 "$src/zyvor-ota-linux-$goarch" "$dest/zyvor-ota"
install -m 0755 "$src/zyvor-otad-linux-$goarch" "$dest/zyvor-otad"
install -m 0755 "$src/zyvor-fleet-ref-linux-$goarch" "$dest/zyvor-fleet-ref"
if [ -f "$src/zyvor-relay-linux-$goarch" ]; then
  install -m 0755 "$src/zyvor-relay-linux-$goarch" "$dest/zyvor-relay"
fi
echo "installed zyvor-ota ($goarch) to $dest"

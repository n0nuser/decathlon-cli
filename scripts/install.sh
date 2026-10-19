#!/bin/sh
# Installs the decathlon release binary.
#
#   curl -fsSL https://raw.githubusercontent.com/n0nuser/decathlon-cli/main/scripts/install.sh | sh
#
# VERSION pins a release (VERSION=1.2.3), DECATHLON_INSTALL_DIR or PREFIX
# chooses the target directory (default ~/.local/bin).
set -eu

repo="n0nuser/decathlon-cli"
bin="decathlon"

if [ -n "${DECATHLON_INSTALL_DIR:-}" ]; then
  dir="$DECATHLON_INSTALL_DIR"
elif [ -n "${PREFIX:-}" ]; then
  dir="$PREFIX/bin"
else
  dir="$HOME/.local/bin"
fi

for tool in curl tar mktemp; do
  command -v "$tool" >/dev/null 2>&1 || { echo "install.sh needs $tool on PATH" >&2; exit 1; }
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) echo "unsupported os: $os (use a release archive or go install)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

if [ -n "${VERSION:-}" ]; then
  version=${VERSION#v}
  tag="v$version"
else
  tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
  [ -n "$tag" ] || { echo "could not resolve the latest release; set VERSION" >&2; exit 1; }
  version=${tag#v}
fi

asset="${bin}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# checksums.txt first, so the archive is verified before anything is extracted.
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
curl -fsSL "$base/$asset" -o "$tmp/$asset"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null) ||
    { echo "checksum verification failed for $asset" >&2; exit 1; }
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $asset\$" checksums.txt | shasum -a 256 -c - >/dev/null) ||
    { echo "checksum verification failed for $asset" >&2; exit 1; }
else
  echo "install.sh needs sha256sum or shasum to verify the download" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp" "$bin"
mkdir -p "$dir"
install -m 0755 "$tmp/$bin" "$dir/$bin"

echo "installed $bin $version to $dir/$bin"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "add $dir to your PATH" ;;
esac

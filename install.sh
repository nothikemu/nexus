#!/bin/sh
# Install the nexus CLI.
#
#   curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | sh
#
# Downloads the latest release for your OS and CPU from GitHub, verifies its
# SHA-256 checksum and installs it into $NEXUS_INSTALL_DIR (default
# ~/.local/bin, or /usr/local/bin when that's writable). If there's no release
# for your platform yet and Go is installed, it builds from source instead.
#
# Options (environment variables):
#   NEXUS_VERSION      a tag such as v0.2.0 (default: latest)
#   NEXUS_INSTALL_DIR  where to put the binary

set -eu

REPO="nothikemu/nexus"
VERSION="${NEXUS_VERSION:-latest}"

say()  { printf '  \033[38;2;164;139;255m✦\033[0m %s\n' "$1"; }
ok()   { printf '  \033[38;2;91;228;155m◈\033[0m %s\n' "$1"; }
fail() { printf '  \033[38;2;255;107;122m✕\033[0m %s\n' "$1" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os=windows ;;
  *) fail "unsupported OS: $os" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported CPU: $arch" ;;
esac

if [ -n "${NEXUS_INSTALL_DIR:-}" ]; then
  dir="$NEXUS_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

fetch() { # url dest
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -qO "$2" "$1"
  else fail "need curl or wget"; fi
}

printf '\n'
say "installing nexus for $os/$arch"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
ext=tar.gz
[ "$os" = windows ] && ext=zip
asset="nexus_${os}_${arch}.$ext"

if fetch "$base/$asset" "$tmp/$asset" 2>/dev/null && fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
  if have sha256sum; then got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
  else got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1); fi
  [ -n "$want" ] && [ "$want" = "$got" ] || fail "checksum mismatch for $asset — not installing"
  if [ "$ext" = zip ]; then (cd "$tmp" && unzip -q "$asset"); else tar -xzf "$tmp/$asset" -C "$tmp"; fi
  bin=nexus
  [ "$os" = windows ] && bin=nexus.exe
  install -m 0755 "$tmp/$bin" "$dir/$bin" 2>/dev/null || { cp "$tmp/$bin" "$dir/$bin" && chmod 0755 "$dir/$bin"; }
  ok "installed $("$dir/$bin" version --plain 2>/dev/null | head -1 | sed 's/^\* //') to $dir"
elif have go; then
  say "no prebuilt release found — building from source with $(go version | cut -d' ' -f3)"
  ref="$VERSION"; [ "$ref" = latest ] && ref=main
  GOBIN="$dir" go install "github.com/$REPO/cmd/nexus@$ref"
  ok "installed nexus to $dir"
else
  fail "no prebuilt release for $os/$arch yet, and Go isn't installed. Install Go 1.24+ (https://go.dev/dl) and run this again."
fi

case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "add $dir to your PATH:  export PATH=\"$dir:\$PATH\"" ;;
esac

if ! have pg_ctl && ! have docker; then
  say "nexus runs your local database with PostgreSQL or Docker — neither was found."
  case "$os" in
    darwin) say "  brew install postgresql@16" ;;
    linux)  say "  sudo apt install postgresql   (or your distro's package)" ;;
  esac
fi
printf '\n'
ok "you're set. try:  nexus init my-app"
printf '\n'

#!/bin/sh
# braids installer.
#
#   curl -fsSL https://braids.chat/install.sh | sh
#
# Downloads the release build for this machine, checks it against the
# published checksums, and puts one binary on your PATH. Nothing else: no
# daemon, no configuration file, no phoning home afterwards.
#
# Set BRAIDS_VERSION to pin a version, BRAIDS_BIN_DIR to choose where it lands.
set -eu

REPO="Ashes47/braids"
VERSION="${BRAIDS_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required and was not found"; }

need uname
need tar
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  read_url() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  read_url() { wget -qO- "$1"; }
else
  die "curl or wget is required"
fi

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *)      die "no release build for $(uname -s). Install with Go instead: go install github.com/$REPO/cmd/braids@latest" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *)             die "no release build for $(uname -m). Install with Go instead: go install github.com/$REPO/cmd/braids@latest" ;;
esac

if [ "$VERSION" = latest ]; then
  # The tag, read from the redirect the releases page serves, so this needs no
  # API token and no jq.
  VERSION=$(read_url "https://api.github.com/repos/$REPO/releases/latest" |
            sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$VERSION" ] || die "could not work out the latest version. Set BRAIDS_VERSION=vX.Y.Z"
fi
number="${VERSION#v}"

# Where it goes: somewhere already on PATH that does not need a password if
# possible, and /usr/local/bin only when it is writable.
if [ -n "${BRAIDS_BIN_DIR:-}" ]; then
  bindir="$BRAIDS_BIN_DIR"
elif [ -w /usr/local/bin ] 2>/dev/null; then
  bindir=/usr/local/bin
else
  bindir="$HOME/.local/bin"
fi
mkdir -p "$bindir"

archive="braids_${number}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

say "braids $VERSION — $os/$arch"
fetch "$base/$archive" "$tmp/$archive" || die "could not download $base/$archive"

# Verified, because a binary fetched over the network and put on your PATH is
# exactly the thing worth checking.
if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  want=$(sed -n "s/^\\([0-9a-f]\\{64\\}\\)  *$archive\$/\\1/p" "$tmp/checksums.txt" | head -1)
  if [ -n "$want" ]; then
    if command -v shasum >/dev/null 2>&1; then
      got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
    elif command -v sha256sum >/dev/null 2>&1; then
      got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
    else
      got=""
      say "  checksum: skipped, no shasum or sha256sum on this machine"
    fi
    if [ -n "$got" ]; then
      [ "$got" = "$want" ] || die "checksum mismatch for $archive — refusing to install"
      say "  checksum: ok"
    fi
  else
    say "  checksum: $archive is not listed in checksums.txt, continuing"
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/braids" ] || die "the archive did not contain a braids binary"
chmod +x "$tmp/braids"
mv "$tmp/braids" "$bindir/braids"
say "  installed: $bindir/braids"

case ":$PATH:" in
  *":$bindir:"*) ;;
  *) say ""
     say "  $bindir is not on your PATH. Add it:"
     say "    echo 'export PATH=\"$bindir:\$PATH\"' >> ~/.zshrc"
     ;;
esac

say ""
say "Next:"
say "  braids index    # read every transcript under ~/.claude"
say "  braids          # open the map"

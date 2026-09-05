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

# stamp records that somebody checked, which braids reads to decide whether to
# mention updates. It is written even when this run installs nothing: braids
# cannot see what the newest version is, so a notice that only cleared on a
# real update would keep asking forever in the one case where there is nothing
# to do. Failing to write it is never a reason to fail an install.
stamp() {
  home="$HOME/.braids"
  mkdir -p "$home" 2>/dev/null || return 0
  chmod 700 "$home" 2>/dev/null || true
  date -u +%Y-%m-%dT%H:%M:%SZ > "$home/checked" 2>/dev/null || true
}

# The same mark `braids help` prints, so the install and the program greet you
# the same way. Accent colour only when stdout is a terminal: with `curl | sh`
# it is the pipe that is on stdin, so this is usually true, but a log file
# should not collect escape codes.
mark() {
  if [ -t 1 ]; then a=$(printf '\033[38;5;208m'); z=$(printf '\033[0m')
  else a=""; z=""; fi
  printf '%s' "$a"
  cat <<'ART'
  ___.                        __       .___
  \_ |__   ______   _____    |__|   __| _/   ______
   | __ \  \_  __ \ \__  \    |    / __ |   /  ___/
   | \_\ \  |  | \/  / __ \_  |   / /_/ |   \___  \
   |___  /  |__|    (____  / |__| \____ |  /____  /
       \/                \/            \/       \/
ART
  printf '%s    conversations as a graph\n\n' "$z"
}
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required and was not found"; }

mark
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

# Already on it? Ask the binary this run would replace, rather than whatever
# is first on PATH, because those are not always the same file and replacing
# the wrong one is how you end up with two braids and PATH choosing.
here=""
if [ -x "$bindir/braids" ]; then
  here=$("$bindir/braids" version 2>/dev/null | sed -n 's/^braids \([0-9][^ ]*\).*/\1/p' | head -1)
fi
if [ -n "$here" ] && [ "$here" = "$number" ]; then
  say "braids $VERSION is already installed at $bindir/braids"
  stamp
  onpath=$(command -v braids 2>/dev/null || true)
  if [ -n "$onpath" ] && [ "$onpath" != "$bindir/braids" ]; then
    say ""
    say "  note: $onpath comes first on your PATH and is a different file"
  fi
  exit 0
fi

archive="braids_${number}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

if [ -n "$here" ]; then
  say "braids $here -> $VERSION, $os/$arch"
else
  say "braids $VERSION, $os/$arch"
fi
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
      [ "$got" = "$want" ] || die "checksum mismatch for $archive, refusing to install"
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

stamp

say ""
say "  what changed: https://github.com/$REPO/releases/tag/$VERSION"

# A first install has no index, and braids will not make one on a read: a
# command that quietly creates an empty index answers a mistyped --db with "no
# matches", which is a wrong answer wearing the shape of a right one. So the
# one command that is allowed to create it runs here, once.
#
# An upgrade does not. The index is already there, braids keeps it current
# every time the map opens, and re-reading a large history is a slow surprise
# in the middle of installing something. If a release ever changes the index
# format, braids notices that on its own and re-reads then.
if [ -z "$here" ]; then
  say ""
  say "  reading your transcripts, once:"
  "$bindir/braids" index 2>&1 | sed 's/^/    /' || \
    say "    could not index yet. Run: braids index"
fi

say ""
say "Next:"
say "  braids          # open the map"

#!/bin/sh
# Install or upgrade plasma-settings-migrator on Linux, from its latest GitHub release:
#
#   curl -fsSL https://raw.githubusercontent.com/fluffynuts/plasma-settings-migrator/master/install.sh | sh
#
# Downloads the release zip for this machine into a temporary folder, checks
# it against the release's SHA256SUMS, unpacks it into another, and runs
# plasma-settings-migrator --install from there. Anything after "sh -s --" is passed on to
# --install, should it ever grow options:
#
#   curl -fsSL .../install.sh | sh -s -- <options>
#
# POSIX sh on purpose: "| sh" is dash on Debian and Ubuntu, not bash.
# Once plasma-settings-migrator is installed, plasma-settings-migrator --upgrade does the same job.

set -eu

RELEASES="${PLASMA_SETTINGS_MIGRATOR_RELEASES:-https://github.com/fluffynuts/plasma-settings-migrator/releases}"

say() { printf 'plasma-settings-migrator-install: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}

[ "$(uname -s)" = Linux ] || die "plasma-settings-migrator manages KDE Plasma settings, so it only runs on Linux"
os=linux
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "no plasma-settings-migrator release for a $(uname -m) machine (only x86-64 and ARM64)" ;;
esac
asset="plasma-settings-migrator-$os-$arch.zip"

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    die "needs curl or wget to download the release"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    die "needs sha256sum or shasum to check the download"
  fi
}

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t plasma-settings-migrator-install)"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

say "downloading $asset"
fetch "$RELEASES/latest/download/$asset" "$tmp/$asset" || die "couldn't download $RELEASES/latest/download/$asset"
fetch "$RELEASES/latest/download/SHA256SUMS" "$tmp/SHA256SUMS" || die "couldn't download the release's SHA256SUMS"

want="$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$want" ] || die "SHA256SUMS has no entry for $asset"
got="$(sha256 "$tmp/$asset")"
# (A release published between the two downloads above would make them
# disagree too: running this again settles that.)
[ "$got" = "$want" ] || die "$asset doesn't match its checksum (got $got, want $want) — not installing it"
say "checked $asset against SHA256SUMS"

mkdir "$tmp/unpacked"
if command -v unzip >/dev/null 2>&1; then
  unzip -q "$tmp/$asset" -d "$tmp/unpacked"
elif command -v python3 >/dev/null 2>&1; then
  python3 -m zipfile -e "$tmp/$asset" "$tmp/unpacked"
else
  die "needs unzip (or python3) to unpack the release — install unzip and run this again"
fi
bundle="$(find "$tmp/unpacked" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
[ -n "$bundle" ] && [ -f "$bundle/plasma-settings-migrator" ] || die "the release zip doesn't hold a plasma-settings-migrator binary"
chmod +x "$bundle/plasma-settings-migrator" # python's zipfile doesn't keep the executable bit

say "running plasma-settings-migrator --install from $(basename "$bundle")"
"$bundle/plasma-settings-migrator" --install "$@"

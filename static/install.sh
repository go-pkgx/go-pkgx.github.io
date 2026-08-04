#!/bin/sh
# pkgm installer — the pure-Go pkgx package manager.
#
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh
#
# Downloads the static, dependency-free pkgm binary for your os/arch from the
# latest GitHub release, verifies it against the release SHA256SUMS, and installs
# it to ${PKGM_INSTALL:-$HOME/.local/bin}.
#
# Env:
#   PKGM_INSTALL   install directory (default: $HOME/.local/bin)
#
# BSD-3-Clause © the go-pkgx authors.
set -eu

REPO="go-pkgx/pkgm"
BASE="https://github.com/${REPO}/releases/latest/download"
INSTALL_DIR="${PKGM_INSTALL:-$HOME/.local/bin}"

err() { printf 'pkgm-install: %s\n' "$1" >&2; exit 1; }

# --- detect platform ---------------------------------------------------------
os=$(uname -s)
case "$os" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS '$os' (this installer supports Linux and macOS; on Windows use install.ps1)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported architecture '$arch' (supported: x86_64/amd64, aarch64/arm64)" ;;
esac

asset="pkgm-${os}-${arch}"
url="${BASE}/${asset}"

# --- pick a downloader -------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
else
  err "need curl or wget to download"
fi

# --- pick a sha256 tool ------------------------------------------------------
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  err "need sha256sum or shasum to verify the download"
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/pkgm-install.XXXXXX") || err "cannot create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'pkgm-install: downloading %s\n' "$asset" >&2
dl "$url" "$tmp/$asset" || err "download failed: $url"

printf 'pkgm-install: verifying checksum\n' >&2
dl "${BASE}/SHA256SUMS" "$tmp/SHA256SUMS" || err "could not download SHA256SUMS"

want=$(grep " ${asset}\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
[ -n "$want" ] || err "no checksum for ${asset} in SHA256SUMS"
got=$(sha256 "$tmp/$asset")
if [ "$want" != "$got" ]; then
  err "checksum mismatch for ${asset}
  expected $want
  got      $got"
fi

# --- install -----------------------------------------------------------------
mkdir -p "$INSTALL_DIR" || err "cannot create $INSTALL_DIR"
chmod +x "$tmp/$asset"
mv -f "$tmp/$asset" "$INSTALL_DIR/pkgm" || err "cannot install to $INSTALL_DIR"

printf 'pkgm-install: installed pkgm to %s/pkgm\n' "$INSTALL_DIR" >&2

# --- PATH hint ---------------------------------------------------------------
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    # SC2016: the literal $PATH is intentional — it is text we print for the user.
    # shellcheck disable=SC2016
    printf '\npkgm-install: %s is not on your PATH. Add it with:\n\n    export PATH="%s:$PATH"\n\n' \
      "$INSTALL_DIR" "$INSTALL_DIR" >&2
    ;;
esac

printf '\nDone. Next:  pkgm install lz4.org\n' >&2
printf '(installs verify against the signed registry by default)\n' >&2

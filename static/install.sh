#!/bin/sh
# pkgm installer — the pure-Go pkgx package manager.
#
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh
#
# Downloads the static, dependency-free pkgm binary for your os/arch from a
# GitHub release, verifies it against the release SHA256SUMS, and installs it to
# ${PKGM_INSTALL:-$HOME/.local/bin}. Idempotent: re-running is the updater — it
# resolves the target version and skips the download if that version is already
# installed (so `curl … | sh` on a cron is a safe self-update).
#
# Env:
#   PKGM_INSTALL   install directory (default: $HOME/.local/bin)
#   PKGM_VERSION   install a specific version (e.g. v0.1.0 or 0.1.0);
#                  default: the latest release
#   PKGM_FORCE     set to 1 to re-download/reinstall even if already current
#
# BSD-3-Clause © the go-pkgx authors.
set -eu

REPO="go-pkgx/pkgm"
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

# --- pick a downloader -------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
  # Resolve the latest tag by following the /releases/latest redirect (no API
  # rate limit, and curl is already required for the pipe-to-sh one-liner).
  latest_tag() { curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" | sed -n 's|.*/tag/||p'; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
  latest_tag() { wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1; }
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

# --- resolve the target version ----------------------------------------------
if [ -n "${PKGM_VERSION:-}" ]; then
  tag=$PKGM_VERSION
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac  # normalise to vX.Y.Z
else
  tag=$(latest_tag) || err "could not resolve the latest pkgm version"
  [ -n "$tag" ] || err "could not resolve the latest pkgm version"
fi
want_ver=${tag#v}
BASE="https://github.com/${REPO}/releases/download/${tag}"
url="${BASE}/${asset}"

# --- skip if already at the target version (unless forced) -------------------
if [ "${PKGM_FORCE:-0}" != "1" ] && [ -x "$INSTALL_DIR/pkgm" ]; then
  cur=$("$INSTALL_DIR/pkgm" --version 2>/dev/null | awk 'NR==1{print $NF}') || cur=
  if [ -n "$cur" ] && [ "$cur" = "$want_ver" ]; then
    printf 'pkgm-install: pkgm %s already installed at %s/pkgm (PKGM_FORCE=1 to reinstall)\n' "$want_ver" "$INSTALL_DIR" >&2
    exit 0
  fi
  [ -n "$cur" ] && printf 'pkgm-install: updating pkgm %s -> %s\n' "$cur" "$want_ver" >&2
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/pkgm-install.XXXXXX") || err "cannot create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'pkgm-install: downloading %s %s\n' "$asset" "$tag" >&2
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

printf 'pkgm-install: installed pkgm %s to %s/pkgm\n' "$want_ver" "$INSTALL_DIR" >&2

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

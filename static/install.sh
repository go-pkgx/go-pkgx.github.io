#!/bin/sh
# go-pkgx installer — the pure-Go pkgx family (pkgm, pkgx, mirror).
#
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh -s -- pkgm v0.1.1
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh -s -- pkgm latest
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh            # pkgm, latest
#   curl -fsSL https://go-pkgx.github.io/install.sh | sh -s -- pkgx # another tool
#
# Selects one of {pkgm, pkgx, mirror}, downloads its static, dependency-free
# binary for your os/arch from a GitHub release, verifies it against the release
# SHA256SUMS, and installs it. Idempotent: re-running is the updater — it
# resolves the target version and skips the download if that version is already
# installed (so `curl … | sh` on a cron is a safe self-update).
#
# Tool selection (default: pkgm, so the bare one-liner is unchanged):
#   sh -s -- <tool>   positional argument (pkgm | pkgx | mirror)
#   PKGX_TOOL=<tool>  environment variable (or TOOL=<tool>)
#
# Version selection (second positional argument, or the env knob below):
#   sh -s -- <tool> v0.1.1   a NAMED release — what the docs pin, so a line
#                            copied today and the same line copied in six
#                            months install the same bytes, and a bad release
#                            does not reach everyone who installs that hour
#   sh -s -- <tool> latest   the newest release, said out loud
#   sh -s -- <tool>          the newest release (unchanged; what a bare
#                            `| sh` has always done, and existing pipelines
#                            keep doing)
#
# Env knobs (per-tool prefix, e.g. PKGM_*, PKGX_*, MIRROR_*; a tool-agnostic
# TOOL_* is honoured as a fallback):
#   <TOOL>_INSTALL / TOOL_INSTALL   install directory (default: $HOME/.local/bin)
#   <TOOL>_VERSION / TOOL_VERSION   install a specific version (e.g. v0.1.0 or
#                                   0.1.0), or `latest`; default: the latest
#                                   release. The positional argument wins over
#                                   this, so a pinned one-liner cannot be
#                                   silently redirected by an exported variable.
#   <TOOL>_FORCE   / TOOL_FORCE     set to 1 to re-download/reinstall even if
#                                   already current
# (PKGM_INSTALL / PKGM_VERSION / PKGM_FORCE keep working for the default tool.)
#
# BSD-3-Clause © the go-pkgx authors.
set -eu

# --- select the tool ---------------------------------------------------------
tool="${1:-${PKGX_TOOL:-${TOOL:-pkgm}}}"
case "$tool" in
  pkgm|pkgx|mirror) ;;
  *) printf 'go-pkgx-install: unknown tool %s (choose one of: pkgm, pkgx, mirror)\n' "'$tool'" >&2; exit 1 ;;
esac
tool_upper=$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]')

REPO="go-pkgx/${tool}"

err() { printf '%s-install: %s\n' "$tool" "$1" >&2; exit 1; }

# Resolve a per-tool env knob: <TOOL_UPPER>_<SUFFIX> first (so PKGM_VERSION,
# PKGX_FORCE, MIRROR_INSTALL keep working), then a tool-agnostic TOOL_<SUFFIX>.
tool_env() {
  # shellcheck disable=SC2154  # 'v' is assigned inside the eval'd expression
  eval "v=\${${tool_upper}_$1:-}"
  [ -n "$v" ] || eval "v=\${TOOL_$1:-}"
  printf '%s' "$v"
}

INSTALL_DIR=$(tool_env INSTALL)
[ -n "$INSTALL_DIR" ] || INSTALL_DIR="$HOME/.local/bin"

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

asset="${tool}-${os}-${arch}"

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
# The second positional argument beats the env knob, which beats "latest". A
# pinned one-liner must mean what it says: if an exported <TOOL>_VERSION could
# override it, the line a reader copied and the version they got would differ,
# which is the whole failure this pin exists to prevent.
want_version="${2:-}"
[ -n "$want_version" ] || want_version=$(tool_env VERSION)
case "$want_version" in
  ""|latest|LATEST)
    tag=$(latest_tag) || err "could not resolve the latest ${tool} version"
    [ -n "$tag" ] || err "could not resolve the latest ${tool} version"
    ;;
  v[0-9]*|[0-9]*)
    tag=$want_version
    case "$tag" in v*) ;; *) tag="v$tag" ;; esac  # normalise to vX.Y.Z
    ;;
  *)
    # Refuse rather than prefix a "v" onto whatever this is: "vmain" or
    # "vstable" would 404 on the download, three steps from here, and read as
    # a network problem instead of a typo.
    err "'$want_version' is not a version (use a release like v0.1.1, or 'latest')"
    ;;
esac
want_ver=${tag#v}
BASE="https://github.com/${REPO}/releases/download/${tag}"
url="${BASE}/${asset}"

# --- skip if already at the target version (unless forced) -------------------
if [ "$(tool_env FORCE)" != "1" ] && [ -x "$INSTALL_DIR/$tool" ]; then
  cur=$("$INSTALL_DIR/$tool" --version 2>/dev/null | awk 'NR==1{print $NF}') || cur=
  if [ -n "$cur" ] && [ "$cur" = "$want_ver" ]; then
    printf '%s-install: %s %s already installed at %s/%s (%s_FORCE=1 to reinstall)\n' \
      "$tool" "$tool" "$want_ver" "$INSTALL_DIR" "$tool" "$tool_upper" >&2
    exit 0
  fi
  [ -n "$cur" ] && printf '%s-install: updating %s %s -> %s\n' "$tool" "$tool" "$cur" "$want_ver" >&2
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/${tool}-install.XXXXXX") || err "cannot create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf '%s-install: downloading %s %s\n' "$tool" "$asset" "$tag" >&2
dl "$url" "$tmp/$asset" || err "download failed: $url"

printf '%s-install: verifying checksum\n' "$tool" >&2
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
mv -f "$tmp/$asset" "$INSTALL_DIR/$tool" || err "cannot install to $INSTALL_DIR"

printf '%s-install: installed %s %s to %s/%s\n' "$tool" "$tool" "$want_ver" "$INSTALL_DIR" "$tool" >&2

# --- PATH hint ---------------------------------------------------------------
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    # SC2016: the literal $PATH is intentional — it is text we print for the user.
    # shellcheck disable=SC2016
    printf '\n%s-install: %s is not on your PATH. Add it with:\n\n    export PATH="%s:$PATH"\n\n' \
      "$tool" "$INSTALL_DIR" "$INSTALL_DIR" >&2
    ;;
esac

# --- per-tool next step ------------------------------------------------------
case "$tool" in
  pkgm)
    printf '\nDone. Next:  pkgm install lz4.org\n' >&2
    printf '(installs verify against the signed registry by default)\n' >&2
    ;;
  pkgx)
    printf '\nDone. Next:  pkgx node@22 --version\n' >&2
    printf '(runs packages on the fly; verifies against the signed registry by default)\n' >&2
    ;;
  mirror)
    printf '\nDone. Next:  mirror agwa.name/git-crypt --dest ./m\n' >&2
    printf '(mirrors pkgx bottles for local/offline serving)\n' >&2
    ;;
esac

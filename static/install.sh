#!/bin/sh
# go-zpkg installer — the pure-Go pkgx family (zpkgm, zpkgx, zpkgs, mirror).
#
#   curl -fsSL https://go-zpkg.github.io/install.sh | sh -s -- zpkgm v0.9.0
#   curl -fsSL https://go-zpkg.github.io/install.sh | sh -s -- zpkgm latest
#   curl -fsSL https://go-zpkg.github.io/install.sh | sh            # zpkgm, latest
#   curl -fsSL https://go-zpkg.github.io/install.sh | sh -s -- zpkgx # another tool
#
# Selects one of {zpkgm, zpkgx, zpkgs, mirror}, downloads its static,
# dependency-free binary for your os/arch from a GitHub release, verifies it
# against the release SHA256SUMS, and installs it. Idempotent: re-running is
# the updater — it resolves the target version and skips the download if that
# version is already installed (so `curl … | sh` on a cron is a safe
# self-update).
#
# Tool selection (default: zpkgm, so the bare one-liner installs the installer):
#   sh -s -- <tool>   positional argument (zpkgm | zpkgx | zpkgs | mirror)
#
# zpkgx and zpkgs are the two halves of one program and ship from ONE release:
# zpkgx RUNS a package, zpkgs ANSWERS about one (ls, search, why, catalog,
# store, browse, compat, env, completion). Install both, or zpkgs's verbs
# have nowhere to go.
#   ZPKG_TOOL=<tool>  environment variable (or TOOL=<tool>)
#
# Version selection (second positional argument, or the env knob below):
#   sh -s -- <tool> v0.1.3   a NAMED release — what the docs pin, so a line
#                            copied today and the same line copied in six
#                            months install the same bytes, and a bad release
#                            does not reach everyone who installs that hour
#   sh -s -- <tool> latest   the newest release, said out loud
#   sh -s -- <tool>          the newest release (unchanged; what a bare
#                            `| sh` has always done, and existing pipelines
#                            keep doing)
#
# Env knobs (per-tool prefix, e.g. ZPKGM_*, ZPKGX_*, MIRROR_*; a tool-agnostic
# TOOL_* is honoured as a fallback):
#   <TOOL>_INSTALL / TOOL_INSTALL   install directory (default: $HOME/.local/bin)
#   <TOOL>_VERSION / TOOL_VERSION   install a specific version (e.g. v0.1.0 or
#                                   0.1.0), or `latest`; default: the latest
#                                   release. The positional argument wins over
#                                   this, so a pinned one-liner cannot be
#                                   silently redirected by an exported variable.
#   <TOOL>_FORCE   / TOOL_FORCE     set to 1 to re-download/reinstall even if
#                                   already current
#
# BSD-3-Clause © the go-zpkg authors.
set -eu

# --- select the tool ---------------------------------------------------------
#
# ⛔⛔ THIS FILE IS SERVED, SO IT BREAKS BEFORE ANYTHING ELSE DOES. Every
# README's one-liner curls it, and it is the first thing a new user runs. The
# go-zpkg → go-zpkg rename moved all twelve repositories; until this landed,
# https://go-zpkg.github.io/install.sh answered 200 and then fetched releases
# from `go-zpkg/<tool>`, an organisation that now holds no repositories at all.
# A rule table could not see it: the repository name is ASSEMBLED at run time
# from a variable, and there is no string `go-zpkg/pkgm` anywhere to match.
tool="${1:-${ZPKG_TOOL:-${TOOL:-zpkgm}}}"
case "$tool" in
  zpkgm|zpkgx|zpkgs|mirror) ;;
  *) printf 'go-zpkg-install: unknown tool %s (choose one of: zpkgm, zpkgx, zpkgs, mirror)\n' "'$tool'" >&2; exit 1 ;;
esac
tool_upper=$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]')

# ⛔ THE REPOSITORY IS NOT ALWAYS THE TOOL'S NAME. Three of the four are
# their own repository; `zpkgs` ships from go-zpkg/zpkgx, because it is the
# other half of the same program — zpkgx runs packages, zpkgs answers about
# them, and they share one resolver. Deriving the repo from the tool name
# alone would have fetched from go-zpkg/zpkgs, which does not exist, and the
# failure a user would see is a 404 on a release rather than "that is not
# where it lives".
case "$tool" in
  zpkgs) REPO="go-zpkg/zpkgx" ;;
  *)     REPO="go-zpkg/${tool}" ;;
esac

err() { printf '%s-install: %s\n' "$tool" "$1" >&2; exit 1; }

# Resolve a per-tool env knob: <TOOL_UPPER>_<SUFFIX> first (so ZPKGM_VERSION,
# ZPKG_FORCE, MIRROR_INSTALL keep working), then a tool-agnostic TOOL_<SUFFIX>.
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
    err "'$want_version' is not a version (use a release like v0.1.3, or 'latest')"
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
#
# ⛔ THESE ARMS MUST SPELL THE TOOLS THE VALIDATION ACCEPTS. They are matched
# against the same `$tool` the `case` above has already restricted, so an arm
# naming a tool that cannot reach here is unreachable — and it fails SILENTLY,
# by printing nothing at all, which is the one failure mode a `| sh` one-liner
# gives a user no way to notice. Leaving `pkgm)` here after the rename would
# have done exactly that.
case "$tool" in
  zpkgm)
    printf '\nDone. Next:  zpkgm install lz4.org\n' >&2
    printf '(installs verify against the signed registry by default)\n' >&2
    ;;
  zpkgx)
    printf '\nDone. Next:  zpkgx node@22 --version\n' >&2
    printf '(runs packages on the fly; verifies against the signed registry by default)\n' >&2
    printf 'To ask ABOUT packages, install its other half:  … | sh -s -- zpkgs\n' >&2
    ;;
  zpkgs)
    printf '\nDone. Next:  zpkgs catalog update  &&  zpkgs search rg\n' >&2
    printf '(answers about packages — ls, search, why, store, browse, env)\n' >&2
    printf 'To RUN one, install its other half:  … | sh -s -- zpkgx\n' >&2
    ;;
  mirror)
    printf '\nDone. Next:  mirror agwa.name/git-crypt --dest ./m\n' >&2
    printf '(mirrors pkgx bottles for local/offline serving)\n' >&2
    ;;
  *)
    printf '\n%s-install: no next step known for %s — the validation above and\n' "$tool" "$tool" >&2
    printf 'this list have drifted apart.\n' >&2
    ;;
esac

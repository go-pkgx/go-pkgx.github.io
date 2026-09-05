<#
.SYNOPSIS
  go-pkgx installer for Windows — the pure-Go pkgx family (pkgm, pkgx, mirror).

.DESCRIPTION
  Run:

      $env:PKGM_VERSION='v0.1.3'; irm https://go-pkgx.github.io/install.ps1 | iex
      irm https://go-pkgx.github.io/install.ps1 | iex                 # pkgm, latest
      $env:PKGX_TOOL='pkgx'; irm https://go-pkgx.github.io/install.ps1 | iex

  A piped script takes no arguments, so on Windows the version is pinned with
  the environment variable above. Saved to disk, both are positional:

      .\install.ps1 pkgx v0.1.4
      .\install.ps1 pkgx latest

  Selects one of {pkgm, pkgx, mirror}, downloads its static <tool>.exe for your
  architecture from a GitHub release, verifies it against the release
  SHA256SUMS, installs it to $env:LOCALAPPDATA\Programs\go-pkgx, and adds that
  directory to the user PATH. Idempotent: re-running is the updater — it
  resolves the target version and skips the download if that version is already
  installed.

  Tool selection (default: pkgm, so the bare one-liner is unchanged):
    .\install.ps1 <tool>   positional argument (pkgm | pkgx | mirror)
    $env:PKGX_TOOL=<tool>  environment variable (or $env:TOOL=<tool>)

  Env knobs (per-tool prefix PKGM_*, PKGX_*, MIRROR_*; a tool-agnostic TOOL_*
  is honoured as a fallback):
    <TOOL>_INSTALL / TOOL_INSTALL   install directory (default:
                                    $env:LOCALAPPDATA\Programs\go-pkgx)
    <TOOL>_VERSION / TOOL_VERSION   install a specific version (e.g. v0.1.0 or
                                    0.1.0), or 'latest'; default: the latest
                                    release. The -Version argument wins over
                                    this, so a pinned command line cannot be
                                    silently redirected by an exported variable.
    <TOOL>_FORCE   / TOOL_FORCE     set to 1 to re-download/reinstall even if
                                    already current
  (PKGM_VERSION / PKGM_FORCE keep working for the default tool.)

  BSD-3-Clause (c) the go-pkgx authors.
#>
param([string]$Tool, [string]$Version)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# --- select the tool ---------------------------------------------------------
if (-not $Tool) {
  if ($env:PKGX_TOOL)  { $Tool = $env:PKGX_TOOL }
  elseif ($env:TOOL)   { $Tool = $env:TOOL }
  else                 { $Tool = 'pkgm' }
}
if ($Tool -notin @('pkgm', 'pkgx', 'mirror')) {
  Write-Error "go-pkgx-install: unknown tool '$Tool' (choose one of: pkgm, pkgx, mirror)"
  exit 1
}
$toolUpper = $Tool.ToUpper()
$repo = "go-pkgx/$Tool"

function Fail([string]$msg) {
  Write-Error "${Tool}-install: $msg"
  exit 1
}

# Resolve a per-tool env knob: <TOOL_UPPER>_<SUFFIX> first (so PKGM_VERSION,
# PKGX_FORCE, MIRROR_INSTALL keep working), then a tool-agnostic TOOL_<SUFFIX>.
function Get-ToolEnv([string]$suffix) {
  $v = [Environment]::GetEnvironmentVariable("${toolUpper}_$suffix")
  if (-not $v) { $v = [Environment]::GetEnvironmentVariable("TOOL_$suffix") }
  return $v
}

# --- detect architecture -----------------------------------------------------
$rawArch = $env:PROCESSOR_ARCHITECTURE
switch ($rawArch) {
  'AMD64' { $arch = 'amd64' }
  'ARM64' { $arch = 'arm64' }
  default { Fail "unsupported architecture '$rawArch' (supported: AMD64, ARM64)" }
}

$asset = "$Tool-windows-$arch.exe"
$installDir = Get-ToolEnv 'INSTALL'
if (-not $installDir) { $installDir = Join-Path $env:LOCALAPPDATA 'Programs\go-pkgx' }
$dest = Join-Path $installDir "$Tool.exe"

# --- resolve the target version ----------------------------------------------
# The -Version argument beats the env knob, which beats "latest". A pinned
# command must mean what it says: if an exported <TOOL>_VERSION could override
# it, the line a reader copied and the version they got would differ, which is
# the whole failure this pin exists to prevent.
$wantVersion = $Version
if (-not $wantVersion) { $wantVersion = Get-ToolEnv 'VERSION' }
if ($wantVersion -and $wantVersion -ne 'latest') {
  # Refuse rather than prefix a "v" onto whatever this is: "vmain" or "vstable"
  # would 404 on the download, three steps from here, and read as a network
  # problem instead of a typo.
  if ($wantVersion -notmatch '^v?[0-9]') {
    Fail "'$wantVersion' is not a version (use a release like v0.1.3, or 'latest')"
  }
  $tag = $wantVersion
  if ($tag -notmatch '^v') { $tag = "v$tag" }  # normalise to vX.Y.Z
} else {
  try {
    $tag = (Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing).tag_name
  } catch {
    Fail "could not resolve the latest $Tool version ($($_.Exception.Message))"
  }
}
if (-not $tag) { Fail "could not resolve the target $Tool version" }
$wantVer = $tag.TrimStart('v')
$base = "https://github.com/$repo/releases/download/$tag"

# --- skip if already at the target version (unless forced) -------------------
if ((Get-ToolEnv 'FORCE') -ne '1' -and (Test-Path -LiteralPath $dest)) {
  $cur = $null
  try { $cur = (& $dest --version 2>$null | Select-Object -First 1).Split(' ')[-1] } catch { $cur = $null }
  if ($cur -eq $wantVer) {
    Write-Host "${Tool}-install: $Tool $wantVer already installed at $dest (set ${toolUpper}_FORCE=1 to reinstall)"
    return
  }
  if ($cur) { Write-Host "${Tool}-install: updating $Tool $cur -> $wantVer" }
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("$Tool-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $binTmp = Join-Path $tmp $asset
  $sumsTmp = Join-Path $tmp 'SHA256SUMS'

  Write-Host "${Tool}-install: downloading $asset $tag"
  try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $binTmp -UseBasicParsing
  } catch {
    Fail "download failed: $base/$asset ($($_.Exception.Message))"
  }

  Write-Host "${Tool}-install: verifying checksum"
  try {
    Invoke-WebRequest -Uri "$base/SHA256SUMS" -OutFile $sumsTmp -UseBasicParsing
  } catch {
    Fail "could not download SHA256SUMS ($($_.Exception.Message))"
  }

  $want = $null
  foreach ($line in Get-Content -LiteralPath $sumsTmp) {
    # SHA256SUMS lines look like: <hex>  <tool>-windows-amd64.exe
    $parts = $line -split '\s+', 2
    if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $asset) {
      $want = $parts[0].Trim().ToLower()
      break
    }
  }
  if (-not $want) { Fail "no checksum for $asset in SHA256SUMS" }

  $got = (Get-FileHash -LiteralPath $binTmp -Algorithm SHA256).Hash.ToLower()
  if ($want -ne $got) {
    Fail "checksum mismatch for $asset`n  expected $want`n  got      $got"
  }

  # --- install ---------------------------------------------------------------
  New-Item -ItemType Directory -Path $installDir -Force | Out-Null
  Move-Item -LiteralPath $binTmp -Destination $dest -Force
  Write-Host "${Tool}-install: installed $Tool $wantVer to $dest"

  # --- add to user PATH ------------------------------------------------------
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not $userPath) { $userPath = '' }
  $onPath = $userPath.Split(';') | Where-Object { $_.TrimEnd('\') -eq $installDir.TrimEnd('\') }
  if (-not $onPath) {
    $newPath = if ($userPath.Length -gt 0) { "$userPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "${Tool}-install: added $installDir to your user PATH (restart your shell to pick it up)"
  }

  Write-Host ''
  switch ($Tool) {
    'pkgm' {
      Write-Host 'Done. Next:  pkgm install lz4.org'
      Write-Host '(installs verify against the signed registry by default)'
    }
    'pkgx' {
      Write-Host 'Done. Next:  pkgx node@22 --version'
      Write-Host '(runs packages on the fly; verifies against the signed registry by default)'
    }
    'mirror' {
      Write-Host 'Done. Next:  mirror agwa.name/git-crypt --dest ./m'
      Write-Host '(mirrors pkgx bottles for local/offline serving)'
    }
  }
} finally {
  Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
}

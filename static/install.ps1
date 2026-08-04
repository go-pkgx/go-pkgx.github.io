<#
.SYNOPSIS
  pkgm installer for Windows — the pure-Go pkgx package manager.

.DESCRIPTION
  Run:

      irm https://go-pkgx.github.io/install.ps1 | iex

  Downloads the static pkgm.exe for your architecture from the latest GitHub
  release, verifies it against the release SHA256SUMS, installs it to
  $env:LOCALAPPDATA\Programs\go-pkgx, and adds that directory to the user PATH.

  BSD-3-Clause (c) the go-pkgx authors.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = 'go-pkgx/pkgm'
$base = "https://github.com/$repo/releases/latest/download"

function Fail([string]$msg) {
  Write-Error "pkgm-install: $msg"
  exit 1
}

# --- detect architecture -----------------------------------------------------
$rawArch = $env:PROCESSOR_ARCHITECTURE
switch ($rawArch) {
  'AMD64' { $arch = 'amd64' }
  'ARM64' { $arch = 'arm64' }
  default { Fail "unsupported architecture '$rawArch' (supported: AMD64, ARM64)" }
}

$asset = "pkgm-windows-$arch.exe"
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\go-pkgx'
$dest = Join-Path $installDir 'pkgm.exe'

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("pkgm-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $binTmp = Join-Path $tmp $asset
  $sumsTmp = Join-Path $tmp 'SHA256SUMS'

  Write-Host "pkgm-install: downloading $asset"
  try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $binTmp -UseBasicParsing
  } catch {
    Fail "download failed: $base/$asset ($($_.Exception.Message))"
  }

  Write-Host 'pkgm-install: verifying checksum'
  try {
    Invoke-WebRequest -Uri "$base/SHA256SUMS" -OutFile $sumsTmp -UseBasicParsing
  } catch {
    Fail "could not download SHA256SUMS ($($_.Exception.Message))"
  }

  $want = $null
  foreach ($line in Get-Content -LiteralPath $sumsTmp) {
    # SHA256SUMS lines look like: <hex>  pkgm-windows-amd64.exe
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
  Write-Host "pkgm-install: installed pkgm to $dest"

  # --- add to user PATH ------------------------------------------------------
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not $userPath) { $userPath = '' }
  $onPath = $userPath.Split(';') | Where-Object { $_.TrimEnd('\') -eq $installDir.TrimEnd('\') }
  if (-not $onPath) {
    $newPath = if ($userPath.Length -gt 0) { "$userPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "pkgm-install: added $installDir to your user PATH (restart your shell to pick it up)"
  }

  Write-Host ''
  Write-Host 'Done. Next:  pkgm install lz4.org'
  Write-Host '(installs verify against the signed registry by default)'
} finally {
  Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
}

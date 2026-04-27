# kpot installer for Windows (PowerShell 5.0+).
#
# Usage:
#   irm https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.ps1 | iex
#
# Environment overrides:
#   $env:KPOT_VERSION      — pin to a specific tag (e.g. v0.5.0). Default: latest release.
#   $env:KPOT_INSTALL_DIR  — install destination. Default: %USERPROFILE%\bin

#Requires -Version 5.0
$ErrorActionPreference = 'Stop'

$Repo = 'Shin-R2un/kpot'
$InstallDir = if ($env:KPOT_INSTALL_DIR) { $env:KPOT_INSTALL_DIR } else { Join-Path $env:USERPROFILE 'bin' }

function Info($msg)  { Write-Host "→ $msg" -ForegroundColor Cyan }
function Ok($msg)    { Write-Host "✓ $msg" -ForegroundColor Green }
function Warn($msg)  { Write-Host "! $msg" -ForegroundColor Yellow }
function Fail($msg)  { Write-Host "error: $msg" -ForegroundColor Red; exit 1 }

# --- Detect arch ---
# kpot ships windows_amd64 only at the moment.
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    default { Fail "unsupported arch: $env:PROCESSOR_ARCHITECTURE (kpot ships windows amd64 only)" }
}

# --- Resolve version ---
$ver = if ($env:KPOT_VERSION) {
    $env:KPOT_VERSION
} else {
    Info "Resolving latest release tag..."
    try {
        (Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest").tag_name
    } catch {
        Fail "could not resolve latest tag from GitHub API: $_"
    }
}
$verNoV = $ver.TrimStart('v')

$archiveName = "kpot_${verNoV}_windows_${arch}.zip"
$url         = "https://github.com/$Repo/releases/download/$ver/$archiveName"
$sumsUrl     = "https://github.com/$Repo/releases/download/$ver/checksums.txt"

Info "Installing kpot $ver (windows/$arch) -> $InstallDir\kpot.exe"

# --- Download & verify ---
$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "kpot-$([guid]::NewGuid())")
try {
    $zip  = Join-Path $tmp $archiveName
    $sums = Join-Path $tmp 'checksums.txt'

    try {
        Invoke-WebRequest -Uri $url      -OutFile $zip  -UseBasicParsing
        Invoke-WebRequest -Uri $sumsUrl  -OutFile $sums -UseBasicParsing
    } catch {
        Fail "download failed: $_"
    }

    $expected = (Select-String -Path $sums -Pattern " $([regex]::Escape($archiveName))$" `
                 | Select-Object -First 1).Line.Split(' ')[0]
    if (-not $expected) { Fail "no checksum entry for $archiveName in checksums.txt" }

    $actual = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
        Fail "checksum mismatch (expected $expected, got $actual)"
    }
    Ok "checksum verified ($expected)"

    Expand-Archive -Path $zip -DestinationPath $tmp -Force

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir | Out-Null
    }
    Move-Item -Force (Join-Path $tmp 'kpot.exe') (Join-Path $InstallDir 'kpot.exe')

    Ok "kpot $ver installed to $InstallDir\kpot.exe"

    # --- PATH check ---
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $onPath = ($env:Path -split ';' | Where-Object { $_ -ieq $InstallDir }).Count -gt 0 `
           -or ($userPath -and ($userPath -split ';' | Where-Object { $_ -ieq $InstallDir }).Count -gt 0)

    if (-not $onPath) {
        Warn "$InstallDir is not in your PATH."
        Write-Host "  Run this once (then open a new shell):" -ForegroundColor Yellow
        Write-Host "    [Environment]::SetEnvironmentVariable('Path', '$InstallDir;' + [Environment]::GetEnvironmentVariable('Path','User'), 'User')" -ForegroundColor Yellow
    } else {
        & (Join-Path $InstallDir 'kpot.exe') version
    }
} finally {
    Remove-Item -Recurse -Force $tmp
}

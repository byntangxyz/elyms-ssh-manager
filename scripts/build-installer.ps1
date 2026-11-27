#Requires -Version 5.1
<#
.SYNOPSIS
    Builds the Windows x64 elyms binary and packages it with Inno Setup 6.

.DESCRIPTION
    Fails fast when the Go toolchain or the Inno Setup compiler is unavailable,
    runs the test suite, builds dist\elyms.exe, then compiles
    installer\elyms.iss into dist\elyms-setup.exe.

.EXAMPLE
    pwsh -File scripts\build-installer.ps1
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Fail {
    param([Parameter(Mandatory)][string]$Message)

    Write-Host "error: $Message" -ForegroundColor Red
    exit 1
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $repoRoot 'dist'
$binaryPath = Join-Path $distDir 'elyms.exe'
$issPath = Join-Path $repoRoot 'installer\elyms.iss'
$installerPath = Join-Path $distDir 'elyms-setup.exe'

# --- Discover required tools -------------------------------------------------

$goPath = $null
$goCmd = Get-Command go -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($goCmd) {
    $goPath = $goCmd.Source
}
if (-not $goPath) {
    Fail "Go toolchain not found. Install Go 1.25+ and make sure 'go' is on PATH."
}

$isccPath = $null
$isccCmd = Get-Command ISCC.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($isccCmd) {
    $isccPath = $isccCmd.Source
}
else {
    $bases = @(
        ${env:ProgramFiles(x86)},
        $env:ProgramFiles,
        (Join-Path $env:LOCALAPPDATA 'Programs')
    ) | Where-Object { $_ }

    $isccPath = $bases |
        ForEach-Object { Join-Path $_ 'Inno Setup 6\ISCC.exe' } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Select-Object -First 1
}
if (-not $isccPath) {
    Fail "Inno Setup 6 compiler (ISCC.exe) not found. Install it from https://jrsoftware.org/isdl.php or add it to PATH."
}

if (-not (Test-Path -LiteralPath $issPath)) {
    Fail "Installer script not found: $issPath"
}

# --- Build environment -------------------------------------------------------

$env:GOTOOLCHAIN = 'local'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

New-Item -ItemType Directory -Path $distDir -Force | Out-Null

Push-Location $repoRoot
try {
    Write-Host '==> go test ./...' -ForegroundColor Cyan
    & $goPath test ./...
    if ($LASTEXITCODE -ne 0) {
        Fail "go test ./... failed (exit code $LASTEXITCODE); aborting before build."
    }

    Write-Host '==> go build dist\elyms.exe' -ForegroundColor Cyan
    & $goPath build -trimpath -ldflags '-s -w' -o $binaryPath ./cmd/elyms
    if ($LASTEXITCODE -ne 0) {
        Fail "go build failed (exit code $LASTEXITCODE)."
    }
    if (-not (Test-Path -LiteralPath $binaryPath)) {
        Fail "Build reported success but $binaryPath is missing."
    }

    Write-Host '==> ISCC.exe installer\elyms.iss' -ForegroundColor Cyan
    & $isccPath $issPath
    if ($LASTEXITCODE -ne 0) {
        Fail "Inno Setup compilation failed (exit code $LASTEXITCODE)."
    }
    if (-not (Test-Path -LiteralPath $installerPath)) {
        Fail "Inno Setup reported success but $installerPath is missing."
    }
}
finally {
    Pop-Location
}

$installer = Get-Item -LiteralPath $installerPath
$sizeMb = [Math]::Round($installer.Length / 1MB, 2)
Write-Host ''
Write-Host "Installer: $($installer.FullName)" -ForegroundColor Green
Write-Host "Size:      $sizeMb MB ($($installer.Length) bytes)" -ForegroundColor Green

#!/usr/bin/env pwsh
# scripts/build.ps1 — Full release build for Trangly
# Builds linux/amd64 + linux/arm64 binaries, packages them as .deb + .rpm,
# and writes dist/build-info.json with version, commit, date, and checksums.
#
# Usage:
#   .\scripts\build.ps1                  # auto-detects version from git tag
#   .\scripts\build.ps1 -Version 1.2.0  # override version

param(
    [string]$Version = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Resolve build metadata ────────────────────────────────────────────────────
if ($Version -eq "") {
    $v = git describe --tags --always --dirty 2>$null
    $Version = if ($v) { $v } else { "dev" }
}
$c = git rev-parse --short HEAD 2>$null
$Commit    = if ($c) { $c } else { "unknown" }
$BuildDate = (Get-Date -Format "yyyy-MM-dd")
$Ldflags   = "-s -w -X github.com/udaypankhaniya/trangly/pkg/version.Version=$Version -X github.com/udaypankhaniya/trangly/pkg/version.Commit=$Commit -X github.com/udaypankhaniya/trangly/pkg/version.BuildDate=$BuildDate"

Write-Host ""
Write-Host "  Building Trangly $Version ($Commit) on $BuildDate" -ForegroundColor Cyan
Write-Host ""

# ── Ensure dist/ exists ────────────────────────────────────────────────────────
New-Item -ItemType Directory -Force -Path dist | Out-Null

# ── Build binaries ────────────────────────────────────────────────────────────
$archs = @(
    @{ GOARCH = "amd64"; Out = "dist/trangly-linux-amd64" },
    @{ GOARCH = "arm64"; Out = "dist/trangly-linux-arm64" }
)

foreach ($a in $archs) {
    Write-Host "  [build] linux/$($a.GOARCH)..." -NoNewline
    $env:GOOS = "linux"; $env:GOARCH = $a.GOARCH; $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags="$Ldflags" -o $a.Out ./cmd/trangly
    if ($LASTEXITCODE -ne 0) { throw "Build failed for $($a.GOARCH)" }
    $env:GOOS = ""; $env:GOARCH = ""; $env:CGO_ENABLED = ""
    Write-Host " done" -ForegroundColor Green
}

# ── Package with nfpm ─────────────────────────────────────────────────────────
$packagers = @("deb", "rpm")
$builtFiles = @()

foreach ($a in $archs) {
    Copy-Item $a.Out dist/trangly-linux -Force
    $env:VERSION = $Version
    $env:GOARCH  = $a.GOARCH

    foreach ($pkg in $packagers) {
        Write-Host "  [package] $pkg linux/$($a.GOARCH)..." -NoNewline
        $out = nfpm package --config nfpm.yaml --packager $pkg -t dist/ 2>&1
        if ($LASTEXITCODE -ne 0) { throw "nfpm $pkg failed: $out" }
        # nfpm prints "created package: dist/xxx" — extract filename
        $line = ($out | Where-Object { $_ -match "created package:" }) | Select-Object -Last 1
        if ($line -match "created package:\s+(.+)") {
            $builtFiles += $Matches[1].Trim()
        }
        Write-Host " done" -ForegroundColor Green
    }
}

Remove-Item dist/trangly-linux -ErrorAction SilentlyContinue

# ── Compute SHA-256 checksums ─────────────────────────────────────────────────
$checksums = @{}
foreach ($f in $builtFiles) {
    $hash = (Get-FileHash $f -Algorithm SHA256).Hash.ToLower()
    $checksums[(Split-Path $f -Leaf)] = "sha256:$hash"
}

# ── Write dist/build-info.json ────────────────────────────────────────────────
$info = [ordered]@{
    version    = $Version
    commit     = $Commit
    build_date = $BuildDate
    files      = ($builtFiles | ForEach-Object { Split-Path $_ -Leaf })
    checksums  = $checksums
}
$json = $info | ConvertTo-Json -Depth 4
Set-Content -Path dist/build-info.json -Value $json -Encoding UTF8

# ── Summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  Build complete" -ForegroundColor Cyan
Write-Host ""
$builtFiles + @("dist/build-info.json") | ForEach-Object {
    $size = "{0:N1} MB" -f ((Get-Item $_).Length / 1MB)
    Write-Host ("  {0,-45} {1}" -f (Split-Path $_ -Leaf), $size)
}
Write-Host ""

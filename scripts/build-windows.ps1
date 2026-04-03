param(
    [string]$Version = "",
    [switch]$SkipZip
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Resolve build metadata
if ($Version -eq "") {
    $v = git describe --tags --always --dirty 2>$null
    $Version = if ($v) { $v -replace "^v", "" } else { "dev" }
}
$c = git rev-parse --short HEAD 2>$null
$Commit    = if ($c) { $c } else { "unknown" }
$BuildDate = (Get-Date -Format "yyyy-MM-dd")
$Ldflags   = "-s -w " +
             "-X github.com/udaypankhaniya/trangly/pkg/version.Version=$Version " +
             "-X github.com/udaypankhaniya/trangly/pkg/version.Commit=$Commit " +
             "-X github.com/udaypankhaniya/trangly/pkg/version.BuildDate=$BuildDate"

Write-Host ""
Write-Host "  Building Trangly $Version ($Commit) for Windows - $BuildDate" -ForegroundColor Cyan
Write-Host ""

New-Item -ItemType Directory -Force -Path dist | Out-Null

$archs = @(
    @{ GOARCH = "amd64"; Suffix = "windows-amd64" },
    @{ GOARCH = "arm64"; Suffix = "windows-arm64" }
)

$builtFiles = @()
$zipFiles   = @()

foreach ($a in $archs) {
    $exePath = "dist\trangly-$($a.Suffix).exe"

    Write-Host "  [build] windows/$($a.GOARCH)..." -NoNewline
    $env:GOOS        = "windows"
    $env:GOARCH      = $a.GOARCH
    $env:CGO_ENABLED = "0"

    go build -trimpath -ldflags="$Ldflags" -o $exePath ./cmd/trangly
    if ($LASTEXITCODE -ne 0) { throw "Build failed for windows/$($a.GOARCH)" }

    $env:GOOS = ""; $env:GOARCH = ""; $env:CGO_ENABLED = ""
    Write-Host " done" -ForegroundColor Green
    $builtFiles += $exePath

    if (-not $SkipZip) {
        $zipPath = "dist\trangly-$($a.Suffix)-v$Version.zip"
        Write-Host "  [zip]   $(Split-Path $zipPath -Leaf)..." -NoNewline

        $staging = "dist\_staging_$($a.GOARCH)"
        New-Item -ItemType Directory -Force -Path $staging | Out-Null

        Copy-Item $exePath "$staging\trangly.exe"

        foreach ($extra in @("README.md", "LICENSE", "CHANGELOG.md")) {
            if (Test-Path $extra) { Copy-Item $extra "$staging\" }
        }

        $note  = "Trangly $Version - Windows Build`r`n"
        $note += "================================`r`n`r`n"
        $note += "Trangly is designed to run on a Linux VPS.`r`n"
        $note += "This Windows build is provided for development and testing only.`r`n`r`n"
        $note += "Usage:`r`n"
        $note += "  trangly.exe setup`r`n"
        $note += "  trangly.exe start`r`n`r`n"
        $note += "Docs: https://github.com/udaypankhaniya/trangly#readme`r`n`r`n"
        $note += "NOTE: Docker Desktop (WSL2 backend) must be running for pipelines to work on Windows.`r`n"
        [System.IO.File]::WriteAllText("$staging\INSTALL.txt", $note, [System.Text.Encoding]::ASCII)

        Compress-Archive -Path "$staging\*" -DestinationPath $zipPath -Force
        Remove-Item -Recurse -Force $staging

        Write-Host " done" -ForegroundColor Green
        $zipFiles += $zipPath
    }
}

$allOutputs = $builtFiles + $zipFiles
$checksums  = @{}
foreach ($f in $allOutputs) {
    $hash = (Get-FileHash $f -Algorithm SHA256).Hash.ToLower()
    $checksums[(Split-Path $f -Leaf)] = "sha256:$hash"
}

$info = [ordered]@{
    version    = $Version
    commit     = $Commit
    build_date = $BuildDate
    platform   = "windows"
    files      = ($allOutputs | ForEach-Object { Split-Path $_ -Leaf })
    checksums  = $checksums
}
$jsonPath = "dist\build-info-windows.json"
$info | ConvertTo-Json -Depth 4 | Set-Content -Path $jsonPath -Encoding UTF8

Write-Host ""
Write-Host "  Windows build complete" -ForegroundColor Cyan
Write-Host ""
($allOutputs + @($jsonPath)) | ForEach-Object {
    if (Test-Path $_) {
        $size = "{0:N1} MB" -f ((Get-Item $_).Length / 1MB)
        Write-Host ("  {0,-52} {1}" -f (Split-Path $_ -Leaf), $size)
    }
}
Write-Host ""

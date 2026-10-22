# Cross-compile Linux binaries on Windows (for systemd deployment, no Docker needed).
#
#   .\scripts\build-linux.ps1                 # build amd64 + arm64 into dist\
#   .\scripts\build-linux.ps1 -Arch amd64     # amd64 only
#
# Output is a fully static binary (CGO_ENABLED=0) with the web UI embedded:
# copy it to the server and run it directly.
#
# Version comes from web/package.json plus the current git commit (same rules as
# scripts/version.sh) and is injected via -ldflags, so it shows up in the startup
# log, the UI footer and GET /healthz.
param(
    [ValidateSet('amd64', 'arm64', 'both')]
    [string]$Arch = 'both',
    [string]$OutDir = (Join-Path (Resolve-Path "$PSScriptRoot\..") 'dist')
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
Push-Location $root
try {
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'linux'

    # ---- Version: web/package.json version + git commit (timestamp fallback) ----
    $base = '0.0.0'
    $pkg = Join-Path $root 'web\package.json'
    if (Test-Path $pkg) {
        $base = (Get-Content $pkg -Raw | ConvertFrom-Json).version
        if (-not $base) { $base = '0.0.0' }
    }

    $sha = ''
    $dirty = ''
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $sha = (& git -C $root rev-parse --short=7 HEAD 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -ne 0) { $sha = '' }
        if ($sha) {
            $status = (& git -C $root status --porcelain 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -eq 0 -and $status) { $dirty = 'dirty' }
        }
    }

    if ($sha) {
        $version = "$base+sha.$sha"
        if ($dirty) { $version = "${version}.${dirty}" }
    }
    else {
        $version = "$base+build.$((Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmss'))"
    }
    $commit = if ($sha) { $sha } else { 'unknown' }
    $buildTime = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $ldflags = "-s -w -X main.version=$version -X main.commit=$commit -X main.buildTime=$buildTime"

    if ($Arch -eq 'both') { $archs = @('amd64', 'arm64') } else { $archs = @($Arch) }

    foreach ($a in $archs) {
        $env:GOARCH = $a
        $out = Join-Path $OutDir "course-hub-linux-$a"
        Write-Host "==> building linux/$a"
        & go build -trimpath -ldflags $ldflags -o $out .
        if ($LASTEXITCODE -ne 0) { throw "build failed (linux/$a)" }
        $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-Host "    $out  ($size MB)"
    }
    Write-Host ""
    Write-Host "Version: $version (commit $commit, built $buildTime)"
    Write-Host "Done. On the server:"
    Write-Host "  chmod +x course-hub-linux-amd64 && ./course-hub-linux-amd64"
}
finally {
    Pop-Location
}

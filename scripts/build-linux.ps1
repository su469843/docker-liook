# Cross-compile Linux binaries on Windows (for systemd deployment, no Docker needed).
#
#   .\scripts\build-linux.ps1                 # build amd64 + arm64 into dist\
#   .\scripts\build-linux.ps1 -Arch amd64     # amd64 only
#
# Output is a fully static binary (CGO_ENABLED=0) with the web UI embedded:
# copy it to the server and run it directly.
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

    if ($Arch -eq 'both') { $archs = @('amd64', 'arm64') } else { $archs = @($Arch) }

    foreach ($a in $archs) {
        $env:GOARCH = $a
        $out = Join-Path $OutDir "course-hub-linux-$a"
        Write-Host "==> building linux/$a"
        & go build -trimpath -ldflags='-s -w' -o $out .
        if ($LASTEXITCODE -ne 0) { throw "build failed (linux/$a)" }
        $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-Host "    $out  ($size MB)"
    }
    Write-Host ""
    Write-Host "Done. On the server:"
    Write-Host "  chmod +x course-hub-linux-amd64 && ./course-hub-linux-amd64"
}
finally {
    Pop-Location
}

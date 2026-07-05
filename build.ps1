# Build Hull's CLI + daemon into bin\. Counterpart to build.sh on Unix.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# Produces bin\hull.exe and bin\hulld.exe.
# Requires: Go.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Version stamped into the binaries (matches the Unix build.sh/install.sh).
$VER = (git describe --tags --always --dirty 2>$null); if (-not $VER) { $VER = 'dev' }
$COMMIT = (git rev-parse --short HEAD 2>$null); if (-not $COMMIT) { $COMMIT = 'none' }
$verFlags = "-X github.com/CavenRE/hull/internal/version.Version=$VER -X github.com/CavenRE/hull/internal/version.Commit=$COMMIT"

Write-Host 'Building daemon + CLI...' -ForegroundColor Cyan
$bin = Join-Path $root 'bin'
New-Item -ItemType Directory -Force -Path $bin | Out-Null
go build -ldflags "-s -w $verFlags" -o (Join-Path $bin 'hulld.exe') ./cmd/hulld
go build -ldflags "-s -w $verFlags" -o (Join-Path $bin 'hull.exe')  ./cmd/hull

Write-Host ''
Write-Host "Built bin\hull.exe + bin\hulld.exe ($VER)" -ForegroundColor Green
Write-Host 'Add bin\ to PATH, or copy both exes somewhere on PATH.'

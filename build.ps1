# Build the Hull binary into bin\hull.exe. Counterpart to build.sh on Unix.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# There is one binary: hull.exe is the CLI, the daemon (hull daemon run), and
# its own installer (hull install). The Hull icon is embedded via the committed
# resource in cmd\hull. Requires: Go.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Version stamped into the binary (matches the Unix build.sh/install.sh).
$VER = (git describe --tags --always --dirty 2>$null); if (-not $VER) { $VER = 'dev' }
$COMMIT = (git rev-parse --short HEAD 2>$null); if (-not $COMMIT) { $COMMIT = 'none' }
$verFlags = "-X github.com/CavenRE/hull/internal/version.Version=$VER -X github.com/CavenRE/hull/internal/version.Commit=$COMMIT"

Write-Host 'Building hull...' -ForegroundColor Cyan
$bin = Join-Path $root 'bin'
New-Item -ItemType Directory -Force -Path $bin | Out-Null
go build -ldflags "-s -w $verFlags" -o (Join-Path $bin 'hull.exe') ./cmd/hull

Write-Host ''
Write-Host "Built bin\hull.exe ($VER)" -ForegroundColor Green
Write-Host 'Run it: bin\hull.exe install   (installs to %LOCALAPPDATA%\Hull + PATH)'

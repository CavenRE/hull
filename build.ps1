# Build Hull's CLI + daemon into bin\. Counterpart to build.sh on Unix.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1              # CLI only
#   powershell -ExecutionPolicy Bypass -File build.ps1 -Installer   # also dist\Hull.exe
#
# Produces bin\hull.exe and bin\hulld.exe. With -Installer it also builds the
# self-contained Windows installer dist\Hull.exe (a compiled exe that embeds
# both binaries; avoids the AMSI blocking that hits script installers).
# Requires: Go.

param([switch]$Installer)

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

if ($Installer) {
  Write-Host ''
  Write-Host 'Staging installer payload...' -ForegroundColor Cyan
  $setupDir = Join-Path $root 'cmd\hull-setup'
  $payload = Join-Path $setupDir 'payload.zip'
  Remove-Item $payload -Force -ErrorAction SilentlyContinue
  Compress-Archive -Path (Join-Path $bin 'hull.exe'), (Join-Path $bin 'hulld.exe') -DestinationPath $payload -CompressionLevel Optimal

  Write-Host 'Building installer dist\Hull.exe...' -ForegroundColor Cyan
  $dist = Join-Path $root 'dist'
  New-Item -ItemType Directory -Force -Path $dist | Out-Null
  go build -tags installer -ldflags "-s -w -X main.version=$VER" -o (Join-Path $dist 'Hull.exe') ./cmd/hull-setup
  Remove-Item $payload -Force -ErrorAction SilentlyContinue

  $out = Join-Path $dist 'Hull.exe'
  Write-Host ''
  Write-Host "Installer: $out ($([math]::Round((Get-Item $out).Length/1MB,1)) MB)" -ForegroundColor Green
}

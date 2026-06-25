# Build Hull — one command, one output.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# Produces a single installer at  bin\Hull-Setup.exe  that bundles the GUI,
# the daemon (hulld), and the CLI (hull). Double-click it to install all three.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Toolchain for this machine: tauri-cli + MinGW on PATH; the network needs
# HTTP/1.1 for crates.io/NSIS downloads.
$env:PATH = "$env:USERPROFILE\.cargo\bin;$env:USERPROFILE\.hull-toolchain\mingw64\bin;$env:PATH"
$env:CARGO_HTTP_MULTIPLEXING = 'false'
$env:CARGO_HTTP_TIMEOUT = '180'
$env:CARGO_NET_RETRY = '10'

$triple   = 'x86_64-pc-windows-gnu'
$binaries = Join-Path $root 'gui\src-tauri\binaries'
New-Item -ItemType Directory -Force -Path $binaries | Out-Null

Write-Host 'Building daemon + CLI (sidecars)...' -ForegroundColor Cyan
go build -ldflags '-s -w' -o (Join-Path $binaries "hulld-$triple.exe") ./cmd/hulld
go build -ldflags '-s -w' -o (Join-Path $binaries "hull-$triple.exe")  ./cmd/hull

Write-Host 'Stopping any running Hull (file locks)...' -ForegroundColor Cyan
Get-Process hull-gui, hulld, 'Hull_0.1.0_x64-setup' -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2

Write-Host 'Bundling installer (cargo tauri build)...' -ForegroundColor Cyan
Push-Location (Join-Path $root 'gui\src-tauri')
try { cargo tauri build } finally { Pop-Location }

$src = Join-Path $root 'gui\src-tauri\target\release\bundle\nsis\Hull_0.1.0_x64-setup.exe'
$out = Join-Path $root 'bin\Hull-Setup.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $out) | Out-Null
Copy-Item -LiteralPath $src -Destination $out -Force

Write-Host ''
Write-Host "Installer ready:  $out" -ForegroundColor Green
Write-Host 'Double-click it to install Hull (GUI + daemon + CLI).'

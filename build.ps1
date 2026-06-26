# Build Hull , one command, one self-contained installer (NO NSIS).
#
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# Produces bin\Hull-Setup.exe: our own installer that embeds the GUI, daemon,
# and CLI, installs them, and wires uninstall to `hull uninstall`. It never
# relaunches from %TEMP%, so SRP/AppLocker policies can't block it.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Toolchain for this machine: MinGW on PATH; the network needs HTTP/1.1.
$env:PATH = "$env:USERPROFILE\.cargo\bin;$env:USERPROFILE\.hull-toolchain\mingw64\bin;$env:PATH"
$env:CARGO_HTTP_MULTIPLEXING = 'false'
$env:CARGO_HTTP_TIMEOUT = '180'
$env:CARGO_NET_RETRY = '10'

# Version stamped into the binaries (matches the Linux build.sh/install.sh).
$VER = (git describe --tags --always --dirty 2>$null); if (-not $VER) { $VER = 'dev' }
$COMMIT = (git rev-parse --short HEAD 2>$null); if (-not $COMMIT) { $COMMIT = 'none' }
$verFlags = "-X github.com/CavenRE/hull/internal/version.Version=$VER -X github.com/CavenRE/hull/internal/version.Commit=$COMMIT"

$payloadDir = Join-Path $root 'cmd\hull-setup\payload'
Remove-Item $payloadDir -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $payloadDir | Out-Null

Write-Host 'Building daemon + CLI...' -ForegroundColor Cyan
go build -ldflags "-s -w $verFlags" -o (Join-Path $payloadDir 'hulld.exe') ./cmd/hulld
go build -ldflags "-s -w $verFlags" -o (Join-Path $payloadDir 'hull.exe')  ./cmd/hull

Write-Host 'Building GUI (cargo build --release, no bundling)...' -ForegroundColor Cyan
Get-Process hull-gui, hulld -ErrorAction SilentlyContinue | Stop-Process -Force
Push-Location (Join-Path $root 'gui\src-tauri')
try { cargo build --release } finally { Pop-Location }

$rel = Join-Path $root 'gui\src-tauri\target\release'
Copy-Item (Join-Path $rel 'hull-gui.exe') $payloadDir -Force
$wv = Join-Path $rel 'WebView2Loader.dll'
if (Test-Path $wv) { Copy-Item $wv $payloadDir -Force }
else { Write-Host '  WARN: WebView2Loader.dll not found , relying on the system WebView2 runtime' -ForegroundColor Yellow }

Write-Host 'Packing payload...' -ForegroundColor Cyan
$zip = Join-Path $root 'cmd\hull-setup\payload.zip'
Remove-Item $zip -Force -ErrorAction SilentlyContinue
Compress-Archive -Path (Join-Path $payloadDir '*') -DestinationPath $zip -CompressionLevel Optimal

Write-Host 'Building installer (GUI app, embeds payload)...' -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path (Join-Path $root 'bin') | Out-Null
# -H windowsgui => no console window ever (it's a graphical installer).
go build -tags installer -ldflags '-s -w -H windowsgui' -o (Join-Path $root 'bin\Hull-Setup.exe') ./cmd/hull-setup

# Tidy intermediates (keep bin\Hull-Setup.exe only).
Remove-Item $payloadDir -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item $zip -Force -ErrorAction SilentlyContinue

$out = Join-Path $root 'bin\Hull-Setup.exe'
Write-Host ''
Write-Host "Installer ready:  $out  ($([math]::Round((Get-Item $out).Length/1MB,1)) MB)" -ForegroundColor Green
Write-Host 'Double-click it to install Hull (GUI + daemon + CLI). Uninstall via Apps & Features or `hull uninstall`.'

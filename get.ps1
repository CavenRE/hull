<#
Hull bootstrap for Windows , install or uninstall the CLI straight from GitHub.
Counterpart to get.sh on Linux/macOS.

  Install (one line):
    irm https://raw.githubusercontent.com/CavenRE/hull/master/get.ps1 | iex

  With options (create a scriptblock so you can pass args):
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/CavenRE/hull/master/get.ps1))) -Uninstall

It clones the Hull repo, builds the CLI + daemon from source (needs Go and git),
installs them to %LOCALAPPDATA%\Hull, and adds that to your PATH. No admin needed.
There are no prebuilt CLI releases yet, so this always builds from source.

  -Uninstall     remove Hull instead of installing
  -Purge         with -Uninstall: also remove ~/.hull (config, CA, service data)
  -Prefix DIR    install location (default: %LOCALAPPDATA%\Hull)
  -Branch NAME   branch to build from (default: master)
#>
param(
  [switch]$Uninstall,
  [switch]$Purge,
  [string]$Prefix = (Join-Path $env:LOCALAPPDATA 'Hull'),
  [string]$Branch = 'master'
)

$ErrorActionPreference = 'Stop'
$repo = 'https://github.com/CavenRE/hull.git'

function Info($m) { Write-Host $m -ForegroundColor Cyan }
function Ok($m)   { Write-Host "  $([char]0x2714) $m" -ForegroundColor Green }
function Warn($m) { Write-Host "  ! $m" -ForegroundColor Yellow }
function Die($m)  { Write-Host $m -ForegroundColor Red; exit 1 }

function Add-ToUserPath($dir) {
  $p = [Environment]::GetEnvironmentVariable('Path', 'User')
  $parts = @()
  if ($p) { $parts = $p.Split(';') | Where-Object { $_ -ne '' } }
  if ($parts -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', (($parts + $dir) -join ';'), 'User')
    $env:Path = "$env:Path;$dir"
    Ok "added $dir to your PATH (open a new terminal to pick it up)"
  } else {
    Ok "$dir already on PATH"
  }
}

function Remove-FromUserPath($dir) {
  $p = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($p) {
    $parts = $p.Split(';') | Where-Object { $_ -ne '' -and $_ -ne $dir }
    [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
  }
}

if ($Uninstall) {
  Info 'Uninstalling Hull...'
  $hull = Join-Path $Prefix 'hull.exe'
  if (Test-Path $hull) {
    try {
      if ($Purge) { & $hull uninstall --quiet --purge-data } else { & $hull uninstall --quiet }
    } catch { Warn "hull uninstall reported: $_" }
  }
  Remove-FromUserPath $Prefix
  if (Test-Path $Prefix) { Remove-Item $Prefix -Recurse -Force -ErrorAction SilentlyContinue }
  Ok 'Done.'
  return
}

# --- install ---
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Die 'git is required (https://git-scm.com/download/win)' }
if (-not (Get-Command go  -ErrorAction SilentlyContinue)) { Die 'Go is required to build from source (https://go.dev/dl/)' }

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('hull-get-' + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Info "Fetching Hull ($Branch)..."
  git clone --depth 1 --branch $Branch $repo $tmp 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Die 'git clone failed' }

  $ver = (git -C $tmp describe --tags --always --dirty 2>$null); if (-not $ver) { $ver = 'source' }
  $commit = (git -C $tmp rev-parse --short HEAD 2>$null); if (-not $commit) { $commit = 'none' }
  $ldflags = "-s -w -X github.com/CavenRE/hull/internal/version.Version=$ver -X github.com/CavenRE/hull/internal/version.Commit=$commit"

  Info 'Building hull + hulld...'
  New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
  Push-Location $tmp
  try {
    go build -ldflags $ldflags -o (Join-Path $Prefix 'hull.exe')  ./cmd/hull
    if ($LASTEXITCODE -ne 0) { Die 'building hull failed (is a Hull daemon running and locking the file? run `hull stop` and retry)' }
    go build -ldflags $ldflags -o (Join-Path $Prefix 'hulld.exe') ./cmd/hulld
    if ($LASTEXITCODE -ne 0) { Die 'building hulld failed (is a Hull daemon running? run `hull stop` and retry)' }
  } finally { Pop-Location }
  Ok "installed hull + hulld to $Prefix ($ver)"

  Add-ToUserPath $Prefix

  Write-Host ''
  Info 'Next steps:'
  Write-Host '  hull setup     # enable the router + DNS and trust the local CA (may prompt for elevation)'
  Write-Host '  hull doctor    # verify Docker, ports, DNS, certs'
  Write-Host '  hulld          # start the daemon, then: hull new demo laravel'
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

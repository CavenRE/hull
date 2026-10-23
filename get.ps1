<#
Hull bootstrap for Windows , install or uninstall the CLI straight from GitHub.
Counterpart to get.sh on Linux/macOS.

  Install (one line):
    irm https://raw.githubusercontent.com/CavenRE/hull/master/get.ps1 | iex

  With options (create a scriptblock so you can pass args):
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/CavenRE/hull/master/get.ps1))) -Uninstall

It downloads the newest release binary, checks it against the release's
SHA256SUMS, installs it to %LOCALAPPDATA%\Hull, adds that to your PATH, and runs
`hull setup`. No admin needed, and no Go or git. -FromSource builds instead.

  -Uninstall     remove Hull instead of installing
  -Purge         with -Uninstall: also remove ~/.hull (config, CA, service data)
  -Prefix DIR    install location (default: %LOCALAPPDATA%\Hull)
  -FromSource    clone and build with Go and git, instead of downloading
  -Branch NAME   with -FromSource: branch or tag to build (default: the newest release tag)
#>
param(
  [switch]$Uninstall,
  [switch]$Purge,
  [string]$Prefix = (Join-Path $env:LOCALAPPDATA 'Hull'),
  [switch]$FromSource,
  [string]$Branch = ''
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
  foreach ($f in 'hull.exe', 'hull.exe.new', 'hull.exe.old') {
    Remove-Item (Join-Path $Prefix $f) -Force -ErrorAction SilentlyContinue
  }
  if ((Test-Path $Prefix) -and -not (Get-ChildItem -Force $Prefix)) { Remove-Item $Prefix -Force }
  Ok 'Done.'
  return
}

# --- install ---
function Install-Release {
  $base = 'https://github.com/CavenRE/hull/releases/latest/download'
  $asset = 'hull-windows-x64.exe'
  New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
  $new = Join-Path $Prefix 'hull.exe.new'
  Info 'Downloading Hull...'
  $ProgressPreference = 'SilentlyContinue'
  Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile $new
  $sums = (Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS").Content
  if ($sums -is [byte[]]) { $sums = [System.Text.Encoding]::UTF8.GetString($sums) }
  $want = ($sums -split "`n" | Where-Object { $_ -match "^([0-9a-f]{64})\s+\*?$([regex]::Escape($asset))\s*$" } | ForEach-Object { $Matches[1] } | Select-Object -First 1)
  $got = (Get-FileHash -Algorithm SHA256 $new).Hash.ToLower()
  if (-not $want -or $got -ne $want) {
    Remove-Item $new -Force -ErrorAction SilentlyContinue
    Die "the download does not match the release's SHA256SUMS; nothing was installed"
  }
  # A running daemon locks hull.exe, but Windows lets a locked file be renamed.
  $exe = Join-Path $Prefix 'hull.exe'
  $old = Join-Path $Prefix 'hull.exe.old'
  Remove-Item $old -Force -ErrorAction SilentlyContinue
  if (Test-Path $exe) { Move-Item $exe $old -Force }
  Move-Item $new $exe -Force
  Ok "installed hull to $Prefix ($(& $exe --version))"
}

if (-not $FromSource) {
  Install-Release
  Add-ToUserPath $Prefix
  $hullExe = Join-Path $Prefix 'hull.exe'
  Write-Host ''
  Info 'Running setup (this may prompt for elevation)...'
  & $hullExe setup
  if ($LASTEXITCODE -ne 0) {
    Warn 'setup did not finish. Open a new terminal and run: hull setup'
  } else {
    & $hullExe doctor
    Write-Host ''
    Ok 'Hull is installed and running. In a NEW terminal try: hull new demo laravel'
  }
  return
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Die 'git is required (https://git-scm.com/download/win)' }
if (-not (Get-Command go  -ErrorAction SilentlyContinue)) { Die 'Go is required to build from source (https://go.dev/dl/)' }

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('hull-get-' + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  if (-not $Branch) {
    # The newest release tag, not master: a bootstrap builds what was released.
    $Branch = (git ls-remote --tags --refs --sort=-v:refname $repo 'v*' | Select-Object -First 1) -replace '.*refs/tags/', ''
    if (-not $Branch) { Die 'could not find a Hull release tag (pass -Branch to choose one)' }
  }
  Info "Fetching Hull ($Branch)..."
  git clone --depth 1 --branch $Branch $repo $tmp 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Die 'git clone failed' }

  $ver = (git -C $tmp describe --tags --always --dirty 2>$null); if (-not $ver) { $ver = 'source' }
  $commit = (git -C $tmp rev-parse --short HEAD 2>$null); if (-not $commit) { $commit = 'none' }
  $ldflags = "-s -w -X github.com/CavenRE/hull/internal/version.Version=$ver -X github.com/CavenRE/hull/internal/version.Commit=$commit"

  Info 'Building hull...'
  New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
  Push-Location $tmp
  try {
    go build -trimpath -ldflags $ldflags -o (Join-Path $Prefix 'hull.exe')  ./cmd/hull
    if ($LASTEXITCODE -ne 0) { Die 'building hull failed (is a Hull daemon running and locking the file? run `hull stop` and retry)' }
  } finally { Pop-Location }
  Ok "installed hull to $Prefix ($ver)"

  Add-ToUserPath $Prefix

  # Finish the job rather than printing homework. install.sh has always run
  # setup for Linux/macOS users; Windows used to stop here, which left a fresh
  # install with no router, no hosts block, and no daemon: nothing was served.
  # Setup provisions the CA and DNS, starts the daemon, and enables it at login.
  $hullExe = Join-Path $Prefix 'hull.exe'
  Write-Host ''
  Info 'Running setup (this may prompt for elevation)...'
  & $hullExe setup
  if ($LASTEXITCODE -ne 0) {
    Warn 'setup did not finish. Open a new terminal and run: hull setup'
  } else {
    & $hullExe doctor
    Write-Host ''
    Ok 'Hull is installed and running. In a NEW terminal try: hull new demo laravel'
  }
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

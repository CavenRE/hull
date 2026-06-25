# Hull — clean Windows uninstall.
#
# A reliable fallback for when the NSIS uninstaller won't launch (common with
# unsigned builds + Defender). Removes the program files, the Apps & Features
# entry, the PATH entry, and shortcuts. Pass -PurgeData to also move ~/.hull
# aside (to ~/.hull.bak) for a truly fresh slate.
#
#   powershell -ExecutionPolicy Bypass -File uninstall.ps1
#   powershell -ExecutionPolicy Bypass -File uninstall.ps1 -PurgeData

[CmdletBinding()]
param(
  [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Hull'),
  [switch]$PurgeData
)

$ErrorActionPreference = 'Continue'

Write-Host '== stopping Hull processes ==' -ForegroundColor Cyan
Get-Process hull-gui, hulld, hull -ErrorAction SilentlyContinue | ForEach-Object {
  Stop-Process -Id $_.Id -Force; Write-Host "  stopped $($_.Name)"
}
Start-Sleep -Milliseconds 600

Write-Host '== removing program files ==' -ForegroundColor Cyan
if (Test-Path -LiteralPath $InstallDir) {
  Remove-Item -LiteralPath $InstallDir -Recurse -Force
  Write-Host "  removed $InstallDir"
} else { Write-Host "  not found: $InstallDir" }

Write-Host '== removing Apps & Features entry ==' -ForegroundColor Cyan
$rk = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Hull'
if (Test-Path -LiteralPath $rk) { Remove-Item -LiteralPath $rk -Recurse -Force; Write-Host '  removed registry entry' }
else { Write-Host '  no registry entry' }

Write-Host '== cleaning per-user PATH ==' -ForegroundColor Cyan
$key  = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$kind = $key.GetValueKind('Path')
$raw  = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$new  = (($raw -split ';') | Where-Object { $_ -ne '' -and $_.TrimEnd('\') -ne $InstallDir.TrimEnd('\') }) -join ';'
if ($new -ne $raw) { $key.SetValue('Path', $new, $kind); Write-Host '  removed Hull from PATH' } else { Write-Host '  no Hull entry in PATH' }
$key.Close()

Write-Host '== removing shortcuts ==' -ForegroundColor Cyan
@(
  (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Hull.lnk'),
  (Join-Path $env:USERPROFILE 'Desktop\Hull.lnk')
) | ForEach-Object {
  if (Test-Path -LiteralPath $_) { Remove-Item -LiteralPath $_ -Force; Write-Host "  removed $_" }
}

if ($PurgeData) {
  Write-Host '== backing up ~/.hull -> ~/.hull.bak ==' -ForegroundColor Cyan
  $hull = Join-Path $env:USERPROFILE '.hull'
  if (Test-Path -LiteralPath $hull) {
    $bak = Join-Path $env:USERPROFILE '.hull.bak'
    if (Test-Path -LiteralPath $bak) { Remove-Item -LiteralPath $bak -Recurse -Force }
    Move-Item -LiteralPath $hull -Destination $bak
    Write-Host '  config/certs/services moved aside (restore by renaming back)'
  } else { Write-Host '  no ~/.hull' }
} else {
  Write-Host 'Kept ~/.hull (config, certs, services). Re-run with -PurgeData to wipe it.' -ForegroundColor Yellow
}

Write-Host 'Done. Open a new terminal so the PATH change takes effect.' -ForegroundColor Green

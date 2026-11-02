# Larvyn installer for Windows.
#
#   irm https://raw.githubusercontent.com/larvyn/releases/main/install.ps1 | iex
#
# Options (environment variables, set them before running):
#   $env:LARVYN_VERSION = "v0.1.0"      install that release instead of the newest one
#   $env:LARVYN_REPO = "owner/repo"     download releases from another GitHub repository
#   $env:LARVYN_UNINSTALL = "1"         remove Larvyn instead of installing it
#   $env:DO_NOT_TRACK = "1"             do not count this install (see the end of the script)
#
# Running it again updates Larvyn. Nothing needs administrator rights.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is far slower with the progress bar
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Say($msg) { Write-Host "larvyn " -ForegroundColor Magenta -NoNewline; Write-Host $msg }
function Fail($msg) { Write-Host "larvyn " -ForegroundColor Red -NoNewline; Write-Host $msg; throw "larvyn install failed" }

$repo = if ($env:LARVYN_REPO) { $env:LARVYN_REPO } else { 'larvyn/releases' }
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\Larvyn'
$exe = Join-Path $installDir 'larvyn.exe'
$startMenu = Join-Path ([Environment]::GetFolderPath('Programs')) 'Larvyn.lnk'
$desktop = Join-Path ([Environment]::GetFolderPath('Desktop')) 'Larvyn.lnk'
$asset = 'larvyn-windows-amd64.exe'

if (Get-Process -Name larvyn -ErrorAction SilentlyContinue) {
    Fail 'Larvyn is running. Close it and run this again.'
}

if ($env:LARVYN_UNINSTALL -eq '1') {
    Remove-Item -Recurse -Force $installDir -ErrorAction SilentlyContinue
    Remove-Item -Force $startMenu, $desktop -ErrorAction SilentlyContinue
    Say "uninstalled. Your settings stay in $env:APPDATA\larvyn; delete that folder to remove them too."
    return
}

if (-not [Environment]::Is64BitOperatingSystem) { Fail 'Larvyn needs 64-bit Windows.' }

# The newest release, prereleases included, unless a version was asked for.
$version = $env:LARVYN_VERSION
if (-not $version) {
    $releases = Invoke-RestMethod "https://api.github.com/repos/$repo/releases?per_page=1" -Headers @{ 'User-Agent' = 'larvyn-installer' }
    if (-not $releases) { Fail "could not find a release in github.com/$repo" }
    $version = @($releases)[0].tag_name
}

$base = "https://github.com/$repo/releases/download/$version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("larvyn-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Say "downloading $version ($asset)"
    Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) { Fail "$asset is missing from checksums.txt" }
    $expected = ($line -split ' ')[0].ToLower()
    $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { Fail "checksum mismatch for $asset (expected $expected, got $actual)" }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Move-Item -Force (Join-Path $tmp $asset) $exe
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# Start menu shortcut (and a desktop one on the first install).
$shell = New-Object -ComObject WScript.Shell
$firstInstall = -not (Test-Path $startMenu)
foreach ($path in @($startMenu) + $(if ($firstInstall) { @($desktop) } else { @() })) {
    $link = $shell.CreateShortcut($path)
    $link.TargetPath = $exe
    $link.WorkingDirectory = $installDir
    $link.IconLocation = "$exe,0"
    $link.Description = 'The Laravel workbench'
    $link.Save()
}

# Count the install: GitHub counts downloads of each release file, and only the
# install scripts fetch installs.txt (the in-app updater does not), so its count
# is the number of installs. Nothing about you or your machine is sent; older
# releases have no such file, and a failure never matters.
if (-not $env:DO_NOT_TRACK) {
    try { Invoke-WebRequest "$base/installs.txt" -UseBasicParsing -TimeoutSec 5 | Out-Null } catch { }
}

Say "installed $version to $exe"
Say 'open Larvyn from the Start menu'

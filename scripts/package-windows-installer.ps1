#Requires -Version 7.0
param(
    [Parameter(Mandatory)][string]$Stage,
    [Parameter(Mandatory)][string]$Output,
    [string]$MakeNSIS = 'makensis.exe'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$Stage = (Resolve-Path -LiteralPath $Stage).Path
$manifest = Get-Content -LiteralPath (Join-Path $Stage 'release-manifest.json') -Raw | ConvertFrom-Json
if ($manifest.platform -ne 'windows/amd64/v1') { throw 'Expected Windows package' }
$version = [string]$manifest.version
if ($version -notmatch '^v(\d+\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?$') { throw 'Invalid release version' }
$numeric = $Matches[1]
$files = @($manifest.files.PSObject.Properties.Name) + 'release-manifest.json'
$cache = Join-Path (Split-Path $Stage -Parent) 'installer-build'
New-Item -ItemType Directory -Path $cache -Force | Out-Null
$install = @(); $uninstall = @()
foreach ($file in $files) {
    if ($file -match '["$:\r\n]' -or $file.Split('/') -contains '..' -or $file.StartsWith('/')) { throw 'Unsafe manifest path' }
    $source = Join-Path $Stage $file
    if ($file -ne 'release-manifest.json' -and (Get-FileHash -LiteralPath $source).Hash.ToLowerInvariant() -ne $manifest.files.$file) { throw "Payload hash mismatch: $file" }
    $relative = $file.Replace('/', '\')
    $parent = Split-Path $relative -Parent
    $install += ('SetOutPath "$INSTDIR' + $(if ($parent) { '\' + $parent }) + '"')
    $install += ('File "${PAYLOAD}\' + $relative + '"')
    $uninstall += ('Delete "$INSTDIR\' + $relative + '"')
}
$installPath = Join-Path $cache 'install.nsh'; $uninstallPath = Join-Path $cache 'uninstall.nsh'
[IO.File]::WriteAllLines($installPath, $install, [Text.UTF8Encoding]::new($true))
[IO.File]::WriteAllLines($uninstallPath, $uninstall, [Text.UTF8Encoding]::new($true))
& $MakeNSIS /V2 "/DVERSION=$version" "/DNUMVERSION=$numeric" "/DPAYLOAD=$Stage" "/DOUTPUT=$([IO.Path]::GetFullPath($Output))" "/DICON=$PSScriptRoot\..\winres\installer.ico" "/DINSTALL_FILES=$installPath" "/DUNINSTALL_FILES=$uninstallPath" "$PSScriptRoot\windows-installer.nsi"
if ($LASTEXITCODE -ne 0) { throw 'Windows installer compilation failed' }
Write-Output "Installer: $Output"

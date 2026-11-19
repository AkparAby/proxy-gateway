#Requires -Version 7.0
# Runs a silent install/upgrade/uninstall in a new, isolated directory. No app launch.
param([Parameter(Mandatory)][string]$Installer)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'Windows is required' }
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$root = Split-Path $PSScriptRoot -Parent
$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\ProxyGateway'
$menu = Join-Path ([Environment]::GetFolderPath('Programs')) 'Proxy Gateway'
$desktop = Join-Path ([Environment]::GetFolderPath('Desktop')) 'Proxy Gateway.lnk'
foreach ($existing in @($key, $menu, $desktop)) {
    if (Test-Path -LiteralPath $existing) { throw "Existing installation/shortcut must not be modified by this test: $existing" }
}
$temp = Join-Path $root ('.release-cache\installer-smoke-' + [guid]::NewGuid().ToString('N'))
$target = Join-Path $temp 'Proxy Gateway'
New-Item -ItemType Directory -Path $temp | Out-Null
function Processes {
    @(Get-CimInstance Win32_Process -Filter "Name='proxy-gateway.exe' OR Name='gateway-manager.exe' OR Name='mihomo.exe'" |
        Where-Object ExecutablePath | Select-Object ProcessId,ExecutablePath | Sort-Object ProcessId | ConvertTo-Json -Compress)
}
$before = Processes
$configPath = Join-Path $root 'config.json'
$configBefore = if (Test-Path $configPath) { (Get-FileHash $configPath).Hash } else { '' }
function Run-Installer([string]$Exe, [string]$Arguments, [bool]$Success = $true) {
    $process = Start-Process -FilePath $Exe -ArgumentList $Arguments -WindowStyle Hidden -PassThru
    if (-not $process.WaitForExit(180000)) { throw "Installer timed out; inspect process $($process.Id)" }
    if ($Success -and $process.ExitCode -ne 0) { throw "Installer returned $($process.ExitCode)" }
    if (-not $Success -and $process.ExitCode -eq 0) { throw 'Installer unexpectedly succeeded' }
}
function Verify-Payload {
    $manifest = Get-Content -LiteralPath "$target\release-manifest.json" -Raw | ConvertFrom-Json
    foreach ($file in $manifest.files.PSObject.Properties) {
        if ($file.Name -match '(^|/)(config\.json|.*\.log|integrations-state\.json|selection\.json)$') { throw 'Local data in release manifest' }
        if ((Get-FileHash -LiteralPath (Join-Path $target $file.Name)).Hash.ToLowerInvariant() -ne $file.Value) { throw "Installed hash mismatch: $($file.Name)" }
    }
    if ((Get-ItemProperty -LiteralPath $key).InstallLocation -ne $target) { throw 'Incorrect registered installation directory' }
    if (-not (Test-Path -LiteralPath "$menu\Proxy Gateway.lnk")) { throw 'Start menu shortcut missing' }
}
Run-Installer $Installer "/S /D=$target"
Verify-Payload
Write-Output 'PASS silent per-user installation, registration, shortcuts and all payload hashes'
$sentinel = '{"schemaVersion":2,"proxyPort":18765}'
[IO.File]::WriteAllText("$target\config.json", $sentinel)
[IO.File]::WriteAllText("$target\runtime\smoke.log", 'preserve-log')
[IO.File]::WriteAllText("$target\runtime\integrations-state.json", '{}')
Run-Installer $Installer "/S /D=$target"
Verify-Payload
if ([IO.File]::ReadAllText("$target\config.json") -ne $sentinel) { throw 'Upgrade overwrote settings' }
if ([IO.File]::ReadAllText("$target\runtime\smoke.log") -ne 'preserve-log') { throw 'Upgrade overwrote logs' }
Write-Output 'PASS upgrade preserves configuration and logs'
$uninstaller = Join-Path $temp 'uninstall-test.exe'
Copy-Item -LiteralPath "$target\Uninstall.exe" -Destination $uninstaller
# Force a restoration failure without touching any actual system integration.
$restoreScript = "$target\scripts\integrations.ps1"
$restoreBytes = [IO.File]::ReadAllBytes($restoreScript)
[IO.File]::WriteAllText($restoreScript, 'exit 23')
[IO.File]::WriteAllText("$target\runtime\integrations-state.json", '{"test":{"enabled":true}}')
try {
    Run-Installer $uninstaller "/S _?=$target" $false
    if (-not (Test-Path -LiteralPath "$target\proxy-gateway.exe") -or -not (Test-Path -LiteralPath $key)) { throw 'Uninstall removed files despite failed restoration' }
    Run-Installer $Installer "/S /D=$target" $false
    if ([IO.File]::ReadAllText($restoreScript) -ne 'exit 23') { throw 'Upgrade overwrote files despite failed restoration' }
    Write-Output 'PASS failed restoration blocks both upgrade and uninstall'
} finally {
    [IO.File]::WriteAllBytes($restoreScript, $restoreBytes)
    [IO.File]::WriteAllText("$target\runtime\integrations-state.json", '{}')
}
Run-Installer $uninstaller "/S _?=$target"
if (Test-Path -LiteralPath $key) { throw 'Uninstall registry entry remains' }
if (Test-Path -LiteralPath "$target\proxy-gateway.exe") { throw 'Executable remains after uninstall' }
if (Test-Path -LiteralPath $menu) { throw 'Start menu entries remain' }
if ([IO.File]::ReadAllText("$target\config.json") -ne $sentinel -or [IO.File]::ReadAllText("$target\runtime\smoke.log") -ne 'preserve-log') { throw 'Uninstall removed user data' }
if ((Processes) -ne $before) { throw 'Another gateway process was affected' }
if ($configBefore -and (Get-FileHash $configPath).Hash -ne $configBefore) { throw 'Workspace configuration was modified' }
Write-Output 'PASS uninstall removes program files, retains user data and leaves other gateway instances untouched'
Write-Output "Test data retained: $temp"

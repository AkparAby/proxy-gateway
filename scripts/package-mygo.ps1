param(
    [Parameter(Mandatory = $true)][string]$BasePackage,
    [string]$Output = 'dist/mygo-preview',
    [string]$Config,
    [string]$Version = 'dev-mygo'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$base = (Resolve-Path -LiteralPath $BasePackage).Path
$stage = [IO.Path]::GetFullPath($Output, $root)
$core = Join-Path $base 'runtime/mihomo.exe'
$coreLicense = Join-Path $base 'licenses/Mihomo-GPL-3.0.txt'
foreach ($file in @($core, $coreLicense)) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing base package file: $file" }
}
if ($stage.TrimEnd('\') -eq $base.TrimEnd('\')) { throw 'Use a separate output directory for the MyGo version.' }
foreach ($dir in @($stage, "$stage/runtime", "$stage/scripts", "$stage/licenses")) {
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
}

# Copy only distributable files, never the running gateway's restoration state.
foreach ($file in @('Start-Gateway.cmd', 'Start-Gateway.ps1', 'Restore-Proxy.cmd', 'Restore-Proxy.ps1', 'README.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md', 'scripts/integrations.ps1', 'scripts/wsl-integration.sh', 'scripts/installer-maintenance.ps1')) {
    Copy-Item -LiteralPath (Join-Path $root $file) -Destination (Join-Path $stage $file) -Force
}
$utf8 = [Text.UTF8Encoding]::new($false)
$utf8Bom = [Text.UTF8Encoding]::new($true)
$integration = Join-Path $stage 'scripts/integrations.ps1'
[IO.File]::WriteAllText($integration, [IO.File]::ReadAllText($integration), $utf8Bom)
$wsl = Join-Path $stage 'scripts/wsl-integration.sh'
[IO.File]::WriteAllText($wsl, [IO.File]::ReadAllText($wsl).Replace("`r`n", "`n"), $utf8)
Copy-Item -LiteralPath $core -Destination "$stage/runtime/mihomo.exe" -Force
Copy-Item -LiteralPath $coreLicense -Destination "$stage/licenses/Mihomo-GPL-3.0.txt" -Force
Copy-Item -LiteralPath "$root/assets/NotoSansSC-OFL.txt" -Destination "$stage/licenses/NotoSansSC-OFL.txt" -Force
Copy-Item -LiteralPath "$root/desktop/mygo/README.md" -Destination "$stage/MyGo-README.md" -Force
if ($Config -and -not (Test-Path -LiteralPath "$stage/config.json")) {
    Copy-Item -LiteralPath $Config -Destination "$stage/config.json"
}

$savedEnv = @{}
foreach ($key in @('CGO_ENABLED', 'GOOS', 'GOARCH', 'GOAMD64')) {
    $savedEnv[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}
Push-Location $root
try {
    $env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'
    & go build -trimpath -buildvcs=false -ldflags "-H=windowsgui -X main.version=$Version" -o "$stage/runtime/gateway-manager.exe" .
    if ($LASTEXITCODE -ne 0) { throw 'Gateway manager build failed.' }
    Set-Location "$root/desktop/mygo"
    & go build -trimpath -buildvcs=false -ldflags "-H=windowsgui -X main.version=$Version" -o "$stage/proxy-gateway.exe" .
    if ($LASTEXITCODE -ne 0) { throw 'MyGo desktop build failed.' }
    & go run ../../scripts/collect-go-licenses.go "$stage/licenses/Go-dependencies.txt" .
    if ($LASTEXITCODE -ne 0) { throw 'MyGo dependency license collection failed.' }
    $goRoot = & go env GOROOT
    if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go runtime license.' }
    Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination "$stage/licenses/Go-BSD.txt" -Force
} finally {
    Pop-Location
    foreach ($key in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($key, $savedEnv[$key], 'Process') }
}
Write-Output "MyGo portable build: $stage/proxy-gateway.exe"
Write-Output 'Choose Restore and exit in the old version before opening this build.'

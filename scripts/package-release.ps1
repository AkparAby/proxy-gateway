#Requires -Version 7.0
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$')]
    [string]$Version,
    [string]$MakeNSIS = 'makensis.exe'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'Build this Windows package on Windows.' }
$root = Split-Path $PSScriptRoot -Parent
$lock = Get-Content (Join-Path $PSScriptRoot 'mihomo.lock.json') -Raw | ConvertFrom-Json
$utf8 = [Text.UTF8Encoding]::new($false)

function Get-VerifiedDownload($item, $cache) {
    $path = Join-Path $cache $item.name
    if (-not (Test-Path -LiteralPath $path)) {
        Write-Host "Downloading $($item.name)..."
        Invoke-WebRequest -Uri $item.url -OutFile $path
    }
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($hash -ne $item.sha256) { throw "SHA-256 mismatch: $path. Remove this cached file and retry." }
    return $path
}

function Copy-ZipEntry($archive, $entryName, $destination) {
    $zip = [IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $entry = $zip.GetEntry($entryName)
        if (-not $entry) { throw "Missing archive entry: $entryName" }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $destination, $false)
    } finally { $zip.Dispose() }
}

Push-Location $root
$oldEnv = @{}
try {
    $changes = @(& git status --porcelain)
    if ($LASTEXITCODE -ne 0 -or $changes.Count -ne 0) { throw 'Commit source changes before creating a release package.' }
    $commit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve source commit.' }
    $goVersion = (& go version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Go is required to build the manager.' }
    $goRoot = (& go env GOROOT).Trim()

    $name = "proxy-gateway-$Version-windows-amd64"
    $out = Join-Path $root "dist\$Version"
    if (Test-Path -LiteralPath $out) { throw "Output already exists: $out. Keep it or move it before rebuilding." }
    $stage = Join-Path $out $name
    $cache = Join-Path $root '.release-cache'
    foreach ($dir in @($cache, $stage, "$stage\runtime", "$stage\scripts", "$stage\licenses")) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    $binaryZip = Get-VerifiedDownload $lock.binary $cache
    $sourceZip = Get-VerifiedDownload $lock.source $cache

    # Explicit allowlist: never copy the user's runtime directory or configuration.
    $payload = @(
        'Start-Gateway.cmd', 'Start-Gateway.ps1', 'Restore-Proxy.cmd', 'Restore-Proxy.ps1',
        'README.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md',
        'scripts/integrations.ps1', 'scripts/wsl-integration.sh', 'scripts/installer-maintenance.ps1'
    )
    foreach ($file in $payload) {
        Copy-Item -LiteralPath (Join-Path $root $file) -Destination (Join-Path $stage $file)
    }
    # Windows PowerShell 5.1 needs a BOM for the Chinese integration messages.
    $integrationFile = Join-Path $stage 'scripts\integrations.ps1'
    $integrationText = [IO.File]::ReadAllText($integrationFile)
    [IO.File]::WriteAllText($integrationFile, $integrationText, [Text.UTF8Encoding]::new($true))
    # WSL scripts must retain Unix line endings even in a Windows checkout.
    $wslFile = Join-Path $stage 'scripts\wsl-integration.sh'
    [IO.File]::WriteAllText($wslFile, [IO.File]::ReadAllText($wslFile).Replace("`r`n", "`n"), $utf8)

    Copy-ZipEntry $binaryZip $lock.binary.entry (Join-Path $stage 'runtime\mihomo.exe')
    Copy-ZipEntry $sourceZip $lock.source.licenseEntry (Join-Path $stage 'licenses\Mihomo-GPL-3.0.txt')
    Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $stage 'licenses\Go-BSD.txt')

    foreach ($key in @('GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED')) {
        $oldEnv[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
    }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'; $env:CGO_ENABLED = '0'
    & go build -trimpath -buildvcs=false -ldflags "-H=windowsgui -s -w -X main.version=$Version -X main.commit=$commit" -o (Join-Path $stage 'runtime\gateway-manager.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }

    $env:CGO_ENABLED = '1'
    & go build -tags desktop -trimpath -buildvcs=false -ldflags "-H=windowsgui -s -w -extldflags=-static -X main.version=$Version -X main.commit=$commit" -o (Join-Path $stage 'proxy-gateway.exe') ./cmd/desktop
    if ($LASTEXITCODE -ne 0) { throw 'Fyne desktop build failed. A MinGW-w64 C compiler is required.' }
    & go run scripts/collect-go-licenses.go (Join-Path $stage 'licenses\Go-dependencies.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Dependency license collection failed.' }
    Copy-Item -LiteralPath (Join-Path $root 'assets\NotoSansSC-OFL.txt') -Destination (Join-Path $stage 'licenses\NotoSansSC-OFL.txt')
    Copy-Item -LiteralPath (Join-Path $root 'assets\MinGW-w64-runtime.txt') -Destination (Join-Path $stage 'licenses\MinGW-w64-runtime.txt')
    Copy-Item -LiteralPath (Join-Path $root 'assets\NSIS-zlib.txt') -Destination (Join-Path $stage 'licenses\NSIS-zlib.txt')

    $fileHashes = [ordered]@{}
    Get-ChildItem -LiteralPath $stage -File -Recurse | Sort-Object FullName | ForEach-Object {
        $relative = [IO.Path]::GetRelativePath($stage, $_.FullName).Replace('\', '/')
        $fileHashes[$relative] = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    $manifest = [ordered]@{
        project = 'https://github.com/AkparAby/proxy-gateway'
        version = $Version
        commit = $commit
        platform = 'windows/amd64/v1'
        builtAtUtc = [DateTime]::UtcNow.ToString('o')
        go = $goVersion
        installerCompiler = (& $MakeNSIS /VERSION | Out-String).Trim()
        mihomo = [ordered]@{
            version = $lock.version; commit = $lock.commit
            binaryUrl = $lock.binary.url; binaryArchiveSha256 = $lock.binary.sha256
            sourceUrl = "https://github.com/AkparAby/proxy-gateway/releases/download/$Version/$($lock.source.name)"
            upstreamSourceUrl = $lock.source.url; sourceArchiveSha256 = $lock.source.sha256
        }
        files = $fileHashes
    }
    [IO.File]::WriteAllText((Join-Path $stage 'release-manifest.json'), ($manifest | ConvertTo-Json -Depth 8) + "`n", $utf8)
    $package = Join-Path $out "$name.zip"
    [IO.Compression.ZipFile]::CreateFromDirectory($stage, $package, [IO.Compression.CompressionLevel]::Optimal, $true)

    $expected = @($payload) + @('proxy-gateway.exe', 'runtime/gateway-manager.exe', 'licenses/Go-dependencies.txt', 'licenses/NotoSansSC-OFL.txt', 'licenses/MinGW-w64-runtime.txt', 'licenses/NSIS-zlib.txt', 'runtime/mihomo.exe', 'licenses/Mihomo-GPL-3.0.txt', 'licenses/Go-BSD.txt', 'release-manifest.json')
    $zip = [IO.Compression.ZipFile]::OpenRead($package)
    try {
        $actual = @($zip.Entries | Where-Object { $_.Name } | ForEach-Object {
            $entryPath = $_.FullName.Replace('\', '/')
            if (-not $entryPath.StartsWith("$name/")) { throw "Unexpected ZIP root: $entryPath" }
            $entryPath.Substring($name.Length + 1)
        })
        if (Compare-Object ($expected | Sort-Object) ($actual | Sort-Object)) { throw 'Package allowlist validation failed.' }
    } finally { $zip.Dispose() }
    $installer = Join-Path $out "$name-setup.exe"
    & (Join-Path $PSScriptRoot 'package-windows-installer.ps1') -Stage $stage -Output $installer -MakeNSIS $MakeNSIS
    $sourceAsset = Join-Path $out $lock.source.name
    Copy-Item -LiteralPath $sourceZip -Destination $sourceAsset
    $checksums = foreach ($asset in @($package, $installer, $sourceAsset)) {
        '{0}  {1}' -f (Get-FileHash -LiteralPath $asset -Algorithm SHA256).Hash.ToLowerInvariant(), [IO.Path]::GetFileName($asset)
    }
    [IO.File]::WriteAllText((Join-Path $out 'SHA256SUMS.txt'), ($checksums -join "`n") + "`n", $utf8)
    Write-Output "Package: $package"
    Write-Output "Source:  $sourceAsset"
    Write-Output "Checksums: $out\SHA256SUMS.txt"
    Write-Output "Validated $($expected.Count) files; no local config, state, logs or backups included."
} finally {
    foreach ($key in $oldEnv.Keys) { [Environment]::SetEnvironmentVariable($key, $oldEnv[$key], 'Process') }
    Pop-Location
}

#Requires -Version 5.1
param([Parameter(Mandatory)][string]$Root)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
try {
    $installRoot = [IO.Path]::GetFullPath($Root).TrimEnd('\')
    if (-not (Test-Path -LiteralPath $installRoot)) { exit 0 }
    $ownedPaths = @('proxy-gateway.exe', 'runtime\gateway-manager.exe', 'runtime\mihomo.exe') |
        ForEach-Object { Join-Path $installRoot $_ }
    function Get-OwnedProcesses {
        @(Get-CimInstance Win32_Process -Filter "Name='proxy-gateway.exe' OR Name='gateway-manager.exe' OR Name='mihomo.exe'" |
            Where-Object { $_.ExecutablePath -and $ownedPaths -contains $_.ExecutablePath })
    }
    $owned = Get-OwnedProcesses
    $managerPath = Join-Path $installRoot 'runtime\gateway-manager.exe'
    $managerIDs = @($owned | Where-Object ExecutablePath -eq $managerPath | ForEach-Object ProcessId)
    $listener = @(Get-NetTCPConnection -LocalPort 17880 -State Listen -ErrorAction SilentlyContinue |
        Where-Object { $managerIDs -contains $_.OwningProcess })
    if ($listener.Count) {
        # Bypass inherited/system proxies, and only contact a listener owned by this install.
        Add-Type -AssemblyName System.Net.Http
        $handler = [Net.Http.HttpClientHandler]::new()
        $handler.UseProxy = $false
        $client = [Net.Http.HttpClient]::new($handler)
        $client.Timeout = [TimeSpan]::FromSeconds(150)
        try {
            $status = $client.GetStringAsync('http://127.0.0.1:17880/api/status').GetAwaiter().GetResult() | ConvertFrom-Json
            if (-not $status.token) { throw 'Gateway status has no token.' }
            $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Post, 'http://127.0.0.1:17880/api/stop')
            $request.Headers.Add('X-Gateway-Token', [string]$status.token)
            $request.Content = [Net.Http.StringContent]::new('{}', [Text.Encoding]::UTF8, 'application/json')
            $response = $client.SendAsync($request).GetAwaiter().GetResult()
            if (-not $response.IsSuccessStatusCode) { throw $response.Content.ReadAsStringAsync().GetAwaiter().GetResult() }
            Start-Sleep -Seconds 2
        } finally { $client.Dispose(); $handler.Dispose() }
    } else {
        if ($managerIDs.Count) { throw 'This installation is running but its management API is unavailable. Restore settings and exit it before continuing.' }
        $statePath = Join-Path $installRoot 'runtime\integrations-state.json'
        if (Test-Path -LiteralPath $statePath) {
            $state = Get-Content -LiteralPath $statePath -Raw -Encoding UTF8 | ConvertFrom-Json
            $active = @($state.PSObject.Properties | Where-Object { $_.Value.enabled })
            if ($active.Count) {
                $restore = Join-Path $installRoot 'scripts\integrations.ps1'
                if (-not (Test-Path -LiteralPath $restore)) { throw 'Restoration script is missing; retain this installation and restore its proxy settings first.' }
                & "$PSHOME\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $restore -Action restore -Root $installRoot
                if ($LASTEXITCODE -ne 0) { throw 'Could not restore managed proxy settings; installation files were retained.' }
            }
        }
    }
    # Re-query full executable paths immediately before stopping; never kill by name.
    foreach ($process in (Get-OwnedProcesses)) {
        Stop-Process -Id $process.ProcessId -Force -ErrorAction Stop
        Wait-Process -Id $process.ProcessId -Timeout 15 -ErrorAction SilentlyContinue
    }
    Write-Output 'Managed settings restored; installation processes stopped.'
    exit 0
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}

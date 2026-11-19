param([switch]$OpenDashboard)
$ErrorActionPreference = 'Stop'
$dashboard = 'http://127.0.0.1:17880'
$exe = Join-Path $PSScriptRoot 'proxy-gateway.exe'
$core = Join-Path $PSScriptRoot 'runtime\mihomo.exe'
$manager = Join-Path $PSScriptRoot 'runtime\gateway-manager.exe'

function Get-GatewayStatus {
    try {
        $status = Invoke-RestMethod "$dashboard/api/status" -TimeoutSec 1
        $port = 17890
        if ($null -ne $status.config.proxyPort) { $port = [int]$status.config.proxyPort }
        if ($status.proxy -eq "127.0.0.1:$port" -and $status.config -and $status.dashboard -eq $dashboard) { return $status }
    } catch { }
    return $null
}

try {
    $status = Get-GatewayStatus
    if (-not $status) {
        foreach ($file in @($exe, $core, $manager)) {
            if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
                throw "Missing file: $file. Extract the complete release ZIP first."
            }
        }
        $process = Start-Process -FilePath $exe -ArgumentList '--background' -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru
        for ($i = 0; $i -lt 30; $i++) {
            Start-Sleep -Milliseconds 500
            $status = Get-GatewayStatus
            if ($status) { break }
            if ($process.HasExited) { break }
        }
        if (-not $status) {
            throw "Gateway did not start. Check management ports 17880/17881, the configured proxy port, and runtime\gateway.log / runtime\mihomo.log."
        }
    } else {
        Write-Output 'A gateway is already running on this computer.'
    }
    Write-Output "Dashboard: $dashboard"
    Write-Output "HTTP / SOCKS5 proxy: $($status.proxy)"
    if ($OpenDashboard) { Start-Process -FilePath $exe -WorkingDirectory $PSScriptRoot }
    elseif ($status) { Start-Process -FilePath $exe -ArgumentList '--background' -WindowStyle Hidden }
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}

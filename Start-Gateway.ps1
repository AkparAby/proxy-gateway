param([switch]$OpenDashboard)
$ErrorActionPreference = 'Stop'
$dashboard = 'http://127.0.0.1:17880'
$exe = Join-Path $PSScriptRoot 'proxy-gateway.exe'
$core = Join-Path $PSScriptRoot 'runtime\mihomo.exe'

function Get-GatewayStatus {
    try {
        $status = Invoke-RestMethod "$dashboard/api/status" -TimeoutSec 1
        if ($status.proxy -eq '127.0.0.1:17890' -and $status.config) { return $status }
    } catch { }
    return $null
}

try {
    $status = Get-GatewayStatus
    if (-not $status) {
        foreach ($file in @($exe, $core)) {
            if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
                throw "Missing file: $file. Extract the complete release ZIP first."
            }
        }
        $process = Start-Process -FilePath $exe -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru
        for ($i = 0; $i -lt 30; $i++) {
            Start-Sleep -Milliseconds 500
            $status = Get-GatewayStatus
            if ($status) { break }
            if ($process.HasExited) { break }
        }
        if (-not $status) {
            throw "Gateway did not start. Check ports 17880/17881/17890 and runtime\gateway.log / runtime\mihomo.log."
        }
    } else {
        Write-Output 'A gateway is already running on this computer.'
    }
    Write-Output "Dashboard: $dashboard"
    Write-Output 'HTTP / SOCKS5 proxy: 127.0.0.1:17890'
    if ($OpenDashboard) { Start-Process $dashboard }
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}

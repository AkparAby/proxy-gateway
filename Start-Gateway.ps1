$ErrorActionPreference='Stop'
$exe=Join-Path $PSScriptRoot 'proxy-gateway.exe'
try { $r=Invoke-RestMethod 'http://127.0.0.1:17880/api/status' -TimeoutSec 1 } catch { Start-Process -FilePath $exe -WorkingDirectory $PSScriptRoot -WindowStyle Hidden }
Write-Output 'Dashboard: http://127.0.0.1:17880'
Write-Output 'HTTP / SOCKS5 proxy: 127.0.0.1:17890'

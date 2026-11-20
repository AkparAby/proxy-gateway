param(
 [ValidateSet('system','terminal','git','docker','wsl','autostart','restore','status','rebind')][string]$Action='status',
 [ValidateSet('enable','disable')][string]$Mode='enable',
 [string]$Root=(Split-Path $PSScriptRoot -Parent),
 [int]$ProxyPort=0
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
if($ProxyPort-eq0){
 $ProxyPort=17890
 $configPath=Join-Path $Root 'config.json'
 if(Test-Path -LiteralPath $configPath){$config=Get-Content -LiteralPath $configPath -Raw -Encoding UTF8|ConvertFrom-Json;if($null-ne$config.proxyPort){$ProxyPort=[int]$config.proxyPort}}
}
if($ProxyPort-lt1024-or$ProxyPort-gt65535){throw '网关端口无效'}
$proxyAddress="127.0.0.1:$ProxyPort"
$runtime=Join-Path $Root 'runtime'
$statePath=Join-Path $runtime 'integrations-state.json'
$statusPath=Join-Path $runtime 'integrations-status.json'
$state=@{}
if(Test-Path -LiteralPath $statePath){$obj=Get-Content -LiteralPath $statePath -Raw -Encoding UTF8 | ConvertFrom-Json;foreach($p in $obj.PSObject.Properties){$state[$p.Name]=$p.Value}}
function Save-State {
 $json=ConvertTo-Json -InputObject $state -Depth 30
 [IO.File]::WriteAllText($statePath,$json,[Text.UTF8Encoding]::new($false))
 $status=@{};foreach($k in $state.Keys){$status[$k]=[bool]$state[$k].enabled}
 [IO.File]::WriteAllText($statusPath,(ConvertTo-Json -InputObject $status),[Text.UTF8Encoding]::new($false))
}
function Notify-Proxy {
 if(-not ('GatewayNative' -as [type])) { Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class GatewayNative {
 [DllImport("wininet.dll", SetLastError=true)] public static extern bool InternetSetOption(IntPtr h, int o, IntPtr b, int l);
 [DllImport("user32.dll", CharSet=CharSet.Unicode, SetLastError=true)] public static extern IntPtr SendMessageTimeout(IntPtr h, uint m, IntPtr w, string l, uint f, uint t, out IntPtr r);
}
'@ }
 [void][GatewayNative]::InternetSetOption([IntPtr]::Zero,39,[IntPtr]::Zero,0)
 [void][GatewayNative]::InternetSetOption([IntPtr]::Zero,37,[IntPtr]::Zero,0)
 $result=[IntPtr]::Zero;[void][GatewayNative]::SendMessageTimeout([IntPtr]0xffff,0x1a,[IntPtr]::Zero,'Environment',2,50,[ref]$result)
}
function Remember($name,$original) {
 if(-not $state.ContainsKey($name) -or -not $state[$name].enabled){$copy=ConvertTo-Json -InputObject $original -Depth 30|ConvertFrom-Json;$state[$name]=[pscustomobject]@{enabled=$false;original=$copy};Save-State}
}
function Set-Flag($name,$value){$state[$name].enabled=$value;Save-State}
function Set-Property($object,$name,$value){$object | Add-Member -NotePropertyName $name -NotePropertyValue $value -Force}
function Read-JsonFile($path){if(Test-Path -LiteralPath $path){return Get-Content -LiteralPath $path -Raw -Encoding UTF8|ConvertFrom-Json};return [pscustomobject]@{}}
function Invoke-Action($name,$enable) {
 if(-not $enable -and (-not $state.ContainsKey($name) -or -not $state[$name].enabled)){return}
 switch($name){
 'system' {
  $key='HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
  if($enable){
   $p=Get-ItemProperty -LiteralPath $key
   if($Action-eq'rebind'){
    $previous=17890;if($state[$name].PSObject.Properties['appliedPort']){$previous=$state[$name].appliedPort}
    if($p.ProxyServer-ne"127.0.0.1:$previous"){throw '系统代理已被其他程序修改，请先停用系统代理接入或检查配置后重试。'}
   }
   $orig=@{};foreach($n in @('ProxyEnable','ProxyServer','ProxyOverride','AutoConfigURL')){$prop=$p.PSObject.Properties[$n];$orig[$n]=[pscustomobject]@{exists=($null-ne$prop);value=if($prop){$prop.Value}else{$null}}}
   Remember $name $orig
   Set-Flag $name $true
   Set-ItemProperty -LiteralPath $key -Name ProxyServer -Value $proxyAddress
   Set-Property $state[$name] 'appliedPort' $ProxyPort;Save-State
   Set-ItemProperty -LiteralPath $key -Name ProxyEnable -Type DWord -Value 1
   $bypass=$p.ProxyOverride;if(-not $bypass){$bypass='localhost;*.local;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*'}
   Set-ItemProperty -LiteralPath $key -Name ProxyOverride -Value $bypass
   Remove-ItemProperty -LiteralPath $key -Name AutoConfigURL -ErrorAction SilentlyContinue
  } else {
   $current=Get-ItemProperty -LiteralPath $key
   $previous=17890;if($state[$name].PSObject.Properties['appliedPort']){$previous=$state[$name].appliedPort}
   if($current.ProxyServer-ne"127.0.0.1:$previous"){throw '系统代理已被其他程序修改，未覆盖；请先检查当前代理设置。'}
   foreach($p in $state[$name].original.PSObject.Properties){if($p.Value.exists){Set-ItemProperty -LiteralPath $key -Name $p.Name -Value $p.Value.value}else{Remove-ItemProperty -LiteralPath $key -Name $p.Name -ErrorAction SilentlyContinue}}
  };Notify-Proxy;Set-Flag $name $enable
 }
 'terminal' {
  $names=@('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY');$values=@("http://$proxyAddress","http://$proxyAddress","socks5h://$proxyAddress",'localhost,127.0.0.1,::1,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10')
  if($enable){$orig=@{};foreach($n in $names){$orig[$n]=[Environment]::GetEnvironmentVariable($n,'User')};Remember $name $orig;Set-Flag $name $true;for($i=0;$i-lt$names.Count;$i++){New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name $names[$i] -PropertyType String -Value $values[$i] -Force | Out-Null}}
  else{foreach($p in $state[$name].original.PSObject.Properties){if($null-eq$p.Value){Remove-ItemProperty -LiteralPath 'HKCU:\Environment' -Name $p.Name -ErrorAction SilentlyContinue}else{New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name $p.Name -PropertyType String -Value $p.Value -Force | Out-Null}}}
  Notify-Proxy;Set-Flag $name $enable
 }
 'git' {
  $keys=@('http.proxy')
  if($enable){$orig=@{};foreach($k in $keys){$v=@(& git config --global --get-all $k);$orig[$k]=$v};Remember $name $orig
   & git config --global --replace-all http.proxy "http://$proxyAddress";if($LASTEXITCODE-ne0){throw 'Git 代理配置失败'}
   foreach($k in $keys|Select-Object -Skip 1){& git config --global --replace-all $k '';if($LASTEXITCODE-ne0){throw "Git 直连设置失败: $k"}}
  } else {foreach($p in $state[$name].original.PSObject.Properties){& git config --global --unset-all $p.Name 2>$null;foreach($v in @($p.Value)){if($PSVersionTable.PSVersion.Major-le5-and$v-eq''){& git config --global --add $p.Name '""'}else{& git config --global --add $p.Name $v}}}}
  Set-Flag $name $enable
 }
 'docker' {
  $file=Join-Path $env:USERPROFILE '.docker\config.json';$json=Read-JsonFile $file
  $desktopFile=Join-Path $env:APPDATA 'Docker\settings-store.json'
  $desktop=Read-JsonFile $desktopFile
  if($enable){$old=$json.PSObject.Properties['proxies'];Remember $name ([pscustomobject]@{exists=($null-ne$old);value=if($old){$old.Value}else{$null}})
   if(-not $state[$name].original.PSObject.Properties['desktop']){
    $modes=@{};foreach($key in @('ProxyHTTPMode','ContainersProxyHTTPMode')){$prop=$desktop.PSObject.Properties[$key];$modes[$key]=[pscustomobject]@{exists=($null-ne$prop);value=if($prop){$prop.Value}else{$null}}}
    Set-Property $state[$name].original 'desktop' ([pscustomobject]$modes);Save-State
   }
   Set-Property $desktop 'ProxyHTTPMode' 'system'
   Set-Property $desktop 'ContainersProxyHTTPMode' 'system'
   $proxies=$json.proxies;if(-not $proxies){$proxies=[pscustomobject]@{}}
   Set-Property $proxies 'default' ([pscustomobject]@{httpProxy="http://host.docker.internal:$ProxyPort";httpsProxy="http://host.docker.internal:$ProxyPort";noProxy='localhost,127.0.0.1,::1,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16'})
   Set-Property $json 'proxies' $proxies
  } else {
   if($state[$name].original.exists){Set-Property $json 'proxies' $state[$name].original.value}else{$json.PSObject.Properties.Remove('proxies')}
   if($state[$name].original.PSObject.Properties['desktop']){foreach($p in $state[$name].original.desktop.PSObject.Properties){if($p.Value.exists){Set-Property $desktop $p.Name $p.Value.value}else{$desktop.PSObject.Properties.Remove($p.Name)}}}
  }
  New-Item -ItemType Directory -Path (Split-Path $file -Parent) -Force | Out-Null
  [IO.File]::WriteAllText($file,(ConvertTo-Json -InputObject $json -Depth 40),[Text.UTF8Encoding]::new($false))
  [IO.File]::WriteAllText($desktopFile,(ConvertTo-Json -InputObject $desktop -Depth 40),[Text.UTF8Encoding]::new($false));Set-Flag $name $enable
 }
 'wsl' {
  $scriptPath=Join-Path $Root 'scripts\wsl-integration.sh'
  $linuxPath=(& wsl.exe -d Ubuntu -- wslpath -u $scriptPath.Replace('\','/') | Out-String).Trim();if($LASTEXITCODE-ne0){throw '无法访问 WSL Ubuntu'}
  if($enable){Remember $name ([pscustomobject]@{})}
  & wsl.exe -d Ubuntu -- sh $linuxPath $(if($enable){'enable'}else{'disable'}) $ProxyPort
  if($LASTEXITCODE-ne0){throw 'WSL 接入失败，未标记为已启用'};Set-Flag $name $enable
 }
 'autostart' {
  $key='HKCU:\Software\Microsoft\Windows\CurrentVersion\Run';$value='LocalProxyGateway';$entry='"'+(Join-Path $Root 'proxy-gateway.exe')+'" --background'
  if($enable){$old=Get-ItemProperty -LiteralPath $key -Name $value -ErrorAction SilentlyContinue;Remember $name ([pscustomobject]@{value=$old.$value});Set-ItemProperty -LiteralPath $key -Name $value -Value $entry}
  else {if($state[$name].original.value){Set-ItemProperty -LiteralPath $key -Name $value -Value $state[$name].original.value}else{Remove-ItemProperty -LiteralPath $key -Name $value -ErrorAction SilentlyContinue}};Set-Flag $name $enable
 }
 }
}
try {
 if($Action-eq'rebind'){
  foreach($n in @('system','terminal','git','docker','wsl')){if($state.ContainsKey($n)-and$state[$n].enabled){Invoke-Action $n $true}}
  Write-Output '已同步启用的接入端口。'
 }
 elseif($Action-eq'restore'){foreach($n in @('wsl','docker','git','terminal','system','autostart')){Invoke-Action $n $false};Write-Output '已恢复网关接入前的设置。'}
 elseif($Action-eq'status'){Save-State;Write-Output '已刷新接入状态。'}
 else{Invoke-Action $Action ($Mode-eq'enable');Write-Output "$Action 接入已$Mode。";if($Action-eq'terminal'){Write-Output '请重新打开终端或应用，使环境变量生效。'};if($Action-eq'docker'){Write-Output 'Docker 构建和新容器配置已保存。Desktop 镜像拉取代理模式也已保存；请重启 Docker Desktop 使其生效。'}}
 exit 0
}catch{[Console]::Error.WriteLine($_.Exception.Message);exit 1}

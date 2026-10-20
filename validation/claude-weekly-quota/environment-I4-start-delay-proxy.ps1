param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$e=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
if($e.synthetic_only -ne $true -or $e.url -notmatch '^http://127\.0\.0\.1:\d+$'){throw 'Exact synthetic candidate loopback receipt required'}
$health=Invoke-WebRequest -Uri ($e.url+'/health') -TimeoutSec 5
if($health.StatusCode -ne 200){throw 'Candidate is not reachable'}
$runtimeDir=Join-Path $env:ProgramData ('CodexTaskRuntime/claude-weekly-quota/I4-delay-proxy-'+[guid]::NewGuid().ToString('N'))
$null=New-Item -ItemType Directory -Path $runtimeDir
$exe=Join-Path $runtimeDir 'i4-delay-proxy.exe'
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$env:GOTOOLCHAIN='local'
& $go build -trimpath -o $exe (Join-Path $PSScriptRoot 'environment-I4-delay-proxy/main.go')
if($LASTEXITCODE -ne 0){throw 'Task delay proxy compile failed'}
$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0)
$listener.Start();$port=$listener.LocalEndpoint.Port;$listener.Stop()
$startup=New-CimInstance -CimClass (Get-CimClass -ClassName Win32_ProcessStartup) -ClientOnly -Property @{ShowWindow=[uint16]0}
$launched=Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=('"'+$exe+'" --target '+$e.url+' --port '+$port);CurrentDirectory=$runtimeDir;ProcessStartupInformation=$startup}
if($launched.ReturnValue -ne 0){throw "Task proxy WMI launch failed $($launched.ReturnValue)"}
$binding=$null
for($i=0;$i -lt 20;$i++){$binding=Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue|Where-Object OwningProcess -eq $launched.ProcessId;if($binding){break};Start-Sleep -Milliseconds 250}
if(-not $binding -or @($binding|Where-Object LocalAddress -ne '127.0.0.1').Count -gt 0){throw 'Delay proxy must bind exact loopback only'}
Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices; public static class CwqDelayJobProbe { [DllImport("kernel32.dll",SetLastError=true)] public static extern bool IsProcessInJob(IntPtr handle,IntPtr job,out bool result); }'
$inJob=$false;$process=Get-Process -Id $launched.ProcessId
if(-not [CwqDelayJobProbe]::IsProcessInJob($process.Handle,[IntPtr]::Zero,[ref]$inJob) -or $inJob){throw 'Task proxy did not detach from tool Job'}
$url='http://127.0.0.1:'+$port
$proxyHealth=Invoke-WebRequest -Uri ($url+'/health') -TimeoutSec 5
if($proxyHealth.StatusCode -ne 200){throw 'Proxy outer health failed'}
$result=[ordered]@{synthetic_only=$true;url=$url;target=$e.url;pid=$launched.ProcessId;in_job=$inJob;loopback_only=$true;binary=$exe;binary_sha256=(Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant();source_sha256=(Get-FileHash -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-delay-proxy/main.go') -Algorithm SHA256).Hash.ToLowerInvariant();environment=[IO.Path]::GetFileName($EnvironmentReceipt);control_path='/__cwq/control';payload_logging=$false;credentials_copied=$false;health_status=$proxyHealth.StatusCode;purpose='T47 late A preview success/error/finally and exact summary response hold, browser checks still required';created_utc=[DateTime]::UtcNow.ToString('o')}
$receipt=Join-Path $PSScriptRoot ('environment-I4-delay-proxy-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
$result|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $receipt -Encoding utf8
[pscustomobject]@{url=$url;pid=$launched.ProcessId;receipt=$receipt;target=$e.url}|ConvertTo-Json

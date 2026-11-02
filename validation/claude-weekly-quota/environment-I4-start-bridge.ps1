param([Parameter(Mandatory)][string]$ContainerID,[Parameter(Mandatory)][int]$Port,[Parameter(Mandatory)][string]$PrivateDir,[string]$BridgeExe='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if($ContainerID -cnotmatch '^[a-f0-9]{64}$' -or $Port -lt 1024 -or $Port -gt 65535){throw 'Invalid exact task bridge target'}
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$label=(& $docker --host npipe:////./pipe/docker_engine inspect --format '{{index .Config.Labels "com.codex.local-task"}}' $ContainerID).Trim()
$name=(& $docker --host npipe:////./pipe/docker_engine inspect --format '{{.Name}}' $ContainerID).Trim()
if($label -ne 'claude-weekly-quota' -or $name -cnotmatch '^/cwq-i4-[a-f0-9]+-candidate(?:-2)?$'){throw 'Bridge may only target the exact new I4 synthetic candidate'}
$null=New-Item -ItemType Directory -Path $PrivateDir -Force
if(-not $BridgeExe){
    $BridgeExe=Join-Path $PrivateDir 'i4-loopback-bridge.exe'
    $go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
    $env:GOTOOLCHAIN='local'
    & $go build -trimpath -o $BridgeExe (Join-Path $PSScriptRoot 'environment-I4-loopback-bridge/main.go')
    if($LASTEXITCODE -ne 0){throw 'Could not compile isolated byte bridge'}
}
# The tool-visible LocalAppData binary is not visible to this machine's WMI
# provider (CIM_DataFile misses it; direct Create returns 9). Only the public
# byte-bridge binary and payload-free metadata go in this task-specific runtime.
$runtimeDir=Join-Path $env:ProgramData ('CodexTaskRuntime/claude-weekly-quota/I4-bridge-'+[guid]::NewGuid().ToString('N'))
$null=New-Item -ItemType Directory -Path $runtimeDir
$runtimeExe=Join-Path $runtimeDir 'i4-loopback-bridge.exe'
Copy-Item -LiteralPath $BridgeExe -Destination $runtimeExe
$binaryHash=(Get-FileHash -LiteralPath $BridgeExe -Algorithm SHA256).Hash.ToLowerInvariant()
if((Get-FileHash -LiteralPath $runtimeExe -Algorithm SHA256).Hash.ToLowerInvariant() -cne $binaryHash){throw 'Detached bridge copy checksum mismatch'}
$cimName=$runtimeExe.Replace('\','\\')
if(-not (Get-CimInstance CIM_DataFile -Filter "Name='$cimName'")){throw 'Task runtime binary is not visible to WMI; no launch attempted'}
$startup=New-CimInstance -CimClass (Get-CimClass -ClassName Win32_ProcessStartup) -ClientOnly -Property @{ShowWindow=[uint16]0}
$logPath=Join-Path $runtimeDir 'loopback-bridge-metadata.log'
$command='"'+$runtimeExe+'" --container '+$ContainerID+' --port '+$Port+' --log "'+$logPath+'"'
$launched=Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=$command;CurrentDirectory=$runtimeDir;ProcessStartupInformation=$startup}
if($launched.ReturnValue -ne 0){throw "Detached bridge launch failed $($launched.ReturnValue)"}
Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices; public static class CwqBridgeJobProbe { [DllImport("kernel32.dll",SetLastError=true)] public static extern bool IsProcessInJob(IntPtr handle,IntPtr job,out bool result); }'
$binding=$null
for($attempt=0;$attempt -lt 20;$attempt++){
    $binding=Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue|Where-Object OwningProcess -eq $launched.ProcessId
    if($binding){break}
    Start-Sleep -Milliseconds 250
}
if(-not $binding -or @($binding|Where-Object LocalAddress -ne '127.0.0.1').Count -gt 0){throw 'Detached bridge did not acquire its exact loopback-only listener'}
$process=Get-Process -Id $launched.ProcessId -ErrorAction Stop
$inJob=$false
if(-not [CwqBridgeJobProbe]::IsProcessInJob($process.Handle,[IntPtr]::Zero,[ref]$inJob) -or $inJob){throw 'Detached bridge Job-isolation verification failed'}
[pscustomobject]@{pid=$launched.ProcessId;container=$ContainerID;listen='127.0.0.1';port=$Port;launch='WMI ShowWindow0, no permanent startup entry';in_job=$inJob;bridge_executable=$runtimeExe;bridge_sha256=$binaryHash;source_sha256=(Get-FileHash -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-loopback-bridge/main.go') -Algorithm SHA256).Hash.ToLowerInvariant();runtime_metadata_log=$logPath;credentials_copied=$false;external_network=$false;payload_logging=$false;created_utc=[DateTime]::UtcNow.ToString('o')}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $PrivateDir ("loopback-bridge-$Port-receipt.json")) -Encoding utf8
$launched.ProcessId

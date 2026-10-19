param([string]$Subnet = '10.247.190.0/24')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$docker = 'C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$endpoint = 'npipe:////./pipe/docker_engine'
$taskLabel = 'claude-weekly-quota'
$runID = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
$prefix = 'cwq-env-' + $runID.ToLowerInvariant()
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$manifestPath = Join-Path $PSScriptRoot 'environment-active.json'
$receiptPath = Join-Path $PSScriptRoot ('environment-receipt-' + $runID + '.json')
$moduleProxy = 'C:/go/pkg/mod/cache/download'

if (Test-Path -LiteralPath $manifestPath) { throw 'An environment manifest already exists; inspect it before starting another environment.' }
if (-not (Test-Path -LiteralPath $moduleProxy)) { throw 'The existing Go module download cache is absent.' }

function Invoke-Docker {
    param([Parameter(Mandatory)][AllowEmptyCollection()][AllowEmptyString()][string[]]$Arguments, [switch]$AllowFailure)
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $docker
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($item in @('--host', $endpoint) + $Arguments) { [void]$start.ArgumentList.Add($item) }
    $process = [Diagnostics.Process]::Start($start)
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit(60000)) { $process.Kill($true); throw "Docker operation timed out: $($Arguments[0])" }
    $result = [pscustomobject]@{exit_code=$process.ExitCode; stdout=$stdout.GetAwaiter().GetResult().Trim(); stderr=$stderr.GetAwaiter().GetResult().Trim()}
    if ($result.exit_code -ne 0 -and -not $AllowFailure) { throw "Docker $($Arguments[0]) failed ($($result.exit_code)): $($result.stderr)" }
    return $result
}

$state = [ordered]@{
    schema_version=1; task=$taskLabel; run_id=$runID; created_utc=[DateTime]::UtcNow.ToString('o')
    endpoint=$endpoint; repository=$repoRoot; synthetic_only=$true
    credential_policy='PostgreSQL trust authentication is confined to this new internal network; no real credentials or copied data are used.'
    existing_resources_policy='Docker Desktop startup automatically started existing containers according to their restart policies. No existing container was explicitly started, stopped, updated, or used by this task.'
    containers=@(); volumes=@(); network=$null; checks=[ordered]@{}; status='starting'
}
function Save-State { $state | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $manifestPath -Encoding utf8 }
function New-TaskVolume([string]$Suffix) {
    $name = "$prefix-$Suffix"
    $result = Invoke-Docker @('volume','create','--label',"com.codex.local-task=$taskLabel",'--label',"com.codex.run-id=$runID",$name)
    $state.volumes += [pscustomobject]@{name=$name; role=$Suffix}
    Save-State
    return $result.stdout
}
function Add-TaskContainer([string]$Role,[string[]]$Arguments) {
    $result = Invoke-Docker (@('run','--detach','--pull','never','--name',"$prefix-$Role",'--label',"com.codex.local-task=$taskLabel",'--label',"com.codex.run-id=$runID",'--label',"com.codex.test-role=$Role",'--restart','no') + $Arguments)
    $resource = [pscustomobject]@{id=$result.stdout; name="$prefix-$Role"; role=$Role}
    $state.containers += $resource
    Save-State
    return $resource
}

try {
    $server = Invoke-Docker @('info','--format','{{json .}}')
    $serverInfo = $server.stdout | ConvertFrom-Json
    if ($serverInfo.OSType -ne 'linux' -or $serverInfo.Name -ne 'docker-desktop') { throw 'Expected the local Docker Desktop Linux engine.' }
    $state.checks.engine = [pscustomobject]@{version=$serverInfo.ServerVersion; name=$serverInfo.Name; os=$serverInfo.OSType; architecture=$serverInfo.Architecture; cpus=$serverInfo.NCPU; memory_bytes=$serverInfo.MemTotal}
    foreach ($image in @('postgres:18.1-alpine3.23','redis:8.4-alpine','claude-weekly-quota-go:1.27-alpine-gcc')) {
        $null = Invoke-Docker @('image','inspect',$image)
    }
    Save-State
    # The existing Docker Desktop default pools are exhausted. Docker still
    # rejects overlap with another Docker network for this explicit test subnet.
    # Host IPv4 routes were checked before selecting the task-owned 10.247.190/24.
    $networkResult = Invoke-Docker @('network','create','--internal','--subnet',$Subnet,'--label',"com.codex.local-task=$taskLabel",'--label',"com.codex.run-id=$runID","$prefix-net")
    $state.network = [pscustomobject]@{id=$networkResult.stdout; name="$prefix-net"; internal=$true; subnet=$Subnet}
    Save-State
    $pgVolume = New-TaskVolume 'pgdata'
    $goCache = New-TaskVolume 'gocache'
    $goMod = New-TaskVolume 'gomodcache'
    $output = New-TaskVolume 'output'

    $pg = Add-TaskContainer 'postgres' @('--network',$state.network.id,'--network-alias','cwq-postgres','--cpus','1','--memory','512m','--pids-limit','128','--shm-size','64m','--security-opt','no-new-privileges','--mount',"type=volume,src=$pgVolume,dst=/var/lib/postgresql",'-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_DB=claude_weekly_synthetic','-e','TZ=UTC','postgres:18.1-alpine3.23')
    $redis = Add-TaskContainer 'redis' @('--network',$state.network.id,'--network-alias','cwq-redis','--cpus','0.5','--memory','128m','--pids-limit','64','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--tmpfs','/data:rw,noexec,nosuid,size=67108864','redis:8.4-alpine','redis-server','--save','','--appendonly','no')
    $runner = Add-TaskContainer 'runner' @('--network',$state.network.id,'--cpus','4','--memory','4g','--pids-limit','512','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--tmpfs','/tmp:rw,exec,nosuid,size=1073741824','--mount',"type=bind,src=$repoRoot,dst=/workspace,readonly",'--mount',"type=bind,src=$moduleProxy,dst=/module-proxy,readonly",'--mount',"type=volume,src=$goCache,dst=/root/.cache/go-build",'--mount',"type=volume,src=$goMod,dst=/go/pkg/mod",'--mount',"type=volume,src=$output,dst=/output",'-e','GOPROXY=file:///module-proxy','-e','GOSUMDB=off','claude-weekly-quota-go:1.27-alpine-gcc')

    $ready = $false
    for ($attempt=0; $attempt -lt 30; $attempt++) {
        $probe = Invoke-Docker @('exec',$pg.id,'pg_isready','-U','postgres','-d','claude_weekly_synthetic') -AllowFailure
        if ($probe.exit_code -eq 0) { $ready=$true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'Synthetic PostgreSQL did not become ready.' }
    $query = "SELECT json_build_object('server_version',current_setting('server_version'),'timezone',current_setting('TimeZone'),'public_tables',(SELECT count(*) FROM pg_tables WHERE schemaname='public'),'hours_168_seconds',extract(epoch FROM interval '168 hours'));"
    $pgProbe = Invoke-Docker @('exec',$pg.id,'psql','-X','-U','postgres','-d','claude_weekly_synthetic','-At','-v','ON_ERROR_STOP=1','-c',$query)
    $state.checks.postgres = $pgProbe.stdout | ConvertFrom-Json
    if (-not $state.checks.postgres.server_version.StartsWith('18.1') -or $state.checks.postgres.public_tables -ne 0) { throw 'Unexpected PostgreSQL version or pre-existing tables.' }
    $redisPing = Invoke-Docker @('exec',$redis.id,'redis-cli','PING')
    $redisDBSize = Invoke-Docker @('exec',$redis.id,'redis-cli','DBSIZE')
    $redisInfo = Invoke-Docker @('exec',$redis.id,'redis-cli','INFO','server')
    $versionLine = ($redisInfo.stdout -split "`n" | Where-Object { $_ -like 'redis_version:*' }).Trim()
    $state.checks.redis = [pscustomobject]@{ping=$redisPing.stdout; dbsize=[int]$redisDBSize.stdout; version=$versionLine.Split(':')[1]}
    if ($state.checks.redis.ping -ne 'PONG' -or $state.checks.redis.dbsize -ne 0) { throw 'Unexpected Redis state.' }
    $goProbe = Invoke-Docker @('exec',$runner.id,'go','version')
    $gccProbe = Invoke-Docker @('exec',$runner.id,'gcc','-dumpfullversion')
    $sourceProbe = Invoke-Docker @('exec',$runner.id,'test','-f','/workspace/backend/go.mod')
    $state.checks.linux = [pscustomobject]@{go_version=$goProbe.stdout; gcc_version=$gccProbe.stdout; source_readable=($sourceProbe.exit_code -eq 0); module_proxy='file:///module-proxy'; external_network=$false; docker_socket_mounted=$false}
    foreach ($container in $state.containers) {
        $inspection = (Invoke-Docker @('container','inspect',$container.id)).stdout | ConvertFrom-Json
        $details = @($inspection)[0]
        $container | Add-Member -NotePropertyName image_id -NotePropertyValue $details.Image
        $container | Add-Member -NotePropertyName host_ports -NotePropertyValue @($details.HostConfig.PortBindings.PSObject.Properties | Select-Object -ExpandProperty Name)
        $container | Add-Member -NotePropertyName docker_socket_mounted -NotePropertyValue ([bool]($details.Mounts | Where-Object { $_.Destination -match 'docker\.sock$' }))
        if ($container.host_ports.Count -ne 0 -or $container.docker_socket_mounted) { throw 'Unexpected port publication or Docker socket mount.' }
    }
    $state.status='ready'
} catch {
    $state.status='failed'
    $state.error=$_.Exception.Message
    throw
} finally {
    Save-State
    $state | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $receiptPath -Encoding utf8
}

[pscustomobject]@{status=$state.status; manifest=$manifestPath; receipt=$receiptPath; container_names=@($state.containers.name); network=$state.network.name; host_ports=@()} | ConvertTo-Json -Depth 5

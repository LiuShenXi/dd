param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$fixture=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$d='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
if($fixture.Application2){throw 'Dedicated WS instance already exists'}
$legacy=(& $d --host npipe:////./pipe/docker_engine exec $fixture.Redis redis-cli ZCARD ('concurrency:user:'+$fixture.A)).Trim()
if($legacy -ne '0'){throw 'All legacy GPT slots must be empty before starting another instance'}
$sql="UPDATE accounts SET extra=extra||'{`"openai_apikey_responses_websockets_v2_mode`":`"http_bridge`"}'::jsonb WHERE id=$($fixture.GPTAccount);"
$sql|& $d --host npipe:////./pipe/docker_engine exec -i $fixture.Postgres psql -U postgres -d $fixture.Database -X -q -v ON_ERROR_STOP=1
if($LASTEXITCODE -ne 0){throw 'Task account mode setup failed'}
$config=@((& $d --host npipe:////./pipe/docker_engine inspect --format '{{json .Config.Env}}' $fixture.Application)|ConvertFrom-Json)
$arguments=@('--host','npipe:////./pipe/docker_engine','run','--detach','--pull','never','--name',($fixture.RunID+'-candidate-2'),'--label','com.codex.local-task=claude-weekly-quota','--network',$fixture.Network,'--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','1536m','--cpus','2','--pids-limit','512','--tmpfs','/app/data:rw,size=128m,uid=1000,gid=1000','--tmpfs','/tmp:rw,size=64m,uid=1000,gid=1000')
foreach($entry in $config){if($entry -notlike 'GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=*'){$arguments+=@('--env',$entry)}}
$arguments+=@('--env','GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true',$fixture.Image)
$fixture.Application2=((& $d @arguments)-join '').Trim()
if($LASTEXITCODE -ne 0){throw 'Dedicated task WS app start failed'}
$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0);$listener.Start();$port=$listener.LocalEndpoint.Port;$listener.Stop()
$fixture.URL2='http://127.0.0.1:'+$port
$fixture.BridgePID2=& (Join-Path $PSScriptRoot 'environment-I4-start-bridge.ps1') -ContainerID $fixture.Application2 -Port $port -PrivateDir (Split-Path -Parent $public.private_fixture_path)
$deadline=[DateTime]::UtcNow.AddSeconds(70);$ok=$false
do{try{$ok=(Invoke-WebRequest -Uri ($fixture.URL2+'/health') -TimeoutSec 1).StatusCode -eq 200}catch{};if(-not $ok){Start-Sleep -Milliseconds 500}}while(-not $ok -and [DateTime]::UtcNow -lt $deadline)
if(-not $ok){throw 'WS candidate outer health not ready'}
$fixture|ConvertTo-Json -Depth 8|Set-Content -LiteralPath $public.private_fixture_path -Encoding utf8
$result=[ordered]@{stage='I4 dedicated synthetic GPT WS HTTP bridge';source_sha256=$public.source_sha256;image_id=$public.image_id;environment=[IO.Path]::GetFileName($EnvironmentReceipt);application=$fixture.Application2;url=$fixture.URL2;synthetic_only=$true;separate_PG_Redis_provider_from_main_and_browser=$true;configuration_difference=@('GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true','synthetic GPT account extra openai_apikey_responses_websockets_v2_mode=http_bridge');all_instances_started_before_capacity_traffic=$true;external_upstream_called=$false;production_connected=$false;created_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ('environment-I4-WS-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
$result|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{url=$fixture.URL2;receipt=$path;private_fixture=$public.private_fixture_path}|ConvertTo-Json

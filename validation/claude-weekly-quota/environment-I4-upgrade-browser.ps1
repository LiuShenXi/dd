param([Parameter(Mandatory)][string]$BuildReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$d='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$publicPath=Join-Path $PSScriptRoot 'environment-I4-20261019T180351Z-active.json'
$public=Get-Content -LiteralPath $publicPath -Raw|ConvertFrom-Json -AsHashtable
$privatePath=$public.private_fixture_path
$fixture=Get-Content -LiteralPath $privatePath -Raw|ConvertFrom-Json -AsHashtable
$build=Get-Content -LiteralPath $BuildReceipt -Raw|ConvertFrom-Json
if($build.status -cne 'pass' -or -not $build.source_stable -or $build.diagnostic -or $build.freeze -cne 'F4'){throw 'Actually built frozen F4 receipt required'}
if($fixture.URL -cne 'http://127.0.0.1:49441' -or $fixture.RunID -cnotmatch '^cwq-i4-[a-f0-9]+$' -or $fixture.C -ne 4 -or $fixture.Application2){throw 'Only the exact idle browser fixture may be replaced'}
$privateRoot=[IO.Path]::GetFullPath((Split-Path -Parent $privatePath))
$allowedPrivate=[IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA 'Codex/PrivateTests/claude-weekly-quota'))+[IO.Path]::DirectorySeparatorChar
if(-not ($privateRoot+[IO.Path]::DirectorySeparatorChar).StartsWith($allowedPrivate,[StringComparison]::OrdinalIgnoreCase)){throw 'Private fixture escaped task root'}
$oldApp=$fixture.Application;$oldBridge=[int]$fixture.BridgePID
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$receiptPath=Join-Path $PSScriptRoot ('environment-I4-browser-F4-cutover-'+$run+'.json')
$result=[ordered]@{stage='I4 isolated browser candidate F4 cutover';status='pending';build_receipt=[IO.Path]::GetFileName($BuildReceipt);url=$fixture.URL;old_application=$oldApp;old_image_id=$fixture.ImageID;new_image_id=$build.image_id;new_source_sha256=$build.source_sha256;normal_old_app_stop=$false;database_and_Redis_preserved=$true;ledger_reset=$false;external_upstream_called=$false;generation_API_called=$false;production_connected=$false;started_utc=[DateTime]::UtcNow.ToString('o')}
function Invoke-TaskDocker([string[]]$DockerArgs,[string]$Stdin='') {
 if($Stdin){$out=@($Stdin|& $d --host npipe:////./pipe/docker_engine @DockerArgs)}else{$out=@(& $d --host npipe:////./pipe/docker_engine @DockerArgs)}
 if($LASTEXITCODE -ne 0){throw ('Task Docker '+$DockerArgs[0]+' failed')}
 return (($out -join "`n").Trim())
}
function Read-TaskSQL([string]$SQL){return Invoke-TaskDocker @('exec','-i',$fixture.Postgres,'psql','-U','postgres','-d',$fixture.Database,'-X','-A','-t','-q','-v','ON_ERROR_STOP=1') $SQL}
function Financial-Fingerprint {
 $tables=@('users','user_subscriptions','redeem_codes','subscription_quota_terms','subscription_quota_requests','subscription_quota_ledger','subscription_quota_operations','accounts')
 $tables+=@((Read-TaskSQL "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND (table_name LIKE 'carpool\_%' ESCAPE '\' OR table_name LIKE 'payment\_%' ESCAPE '\') ORDER BY table_name;") -split "`n"|Where-Object {$_})
 $statements=@();foreach($table in $tables){if($table -cnotmatch '^[a-z0-9_]+$'){throw 'Unsafe task table identity'};$statements+="SELECT '$table',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM $table x;"}
 $statements+="SELECT 'key_money_limits_identity',md5(jsonb_agg(to_jsonb(k)-'last_used_at'-'updated_at' ORDER BY id)::text) FROM api_keys k;"
 return Read-TaskSQL ($statements -join "`n")
}
$newApp='';$newBridge=0;$bridgeReplaced=$false;$oldStopped=$false
try{
 $actual=Invoke-TaskDocker @('inspect','--format','{{.Image}}',$oldApp)
 if($actual -cne 'sha256:64aac420e42f38a4c4faa523d8b9b8cd02339dc7c0627d0444ad97966f3a019c' -or $actual -cne $fixture.ImageID){throw 'Unexpected original browser image'}
 if((Invoke-TaskDocker @('image','inspect','--format','{{.Id}}',$build.image_tag)) -cne $build.image_id){throw 'F4 tag/image mismatch'}
 if((Invoke-TaskDocker @('inspect','--format','{{index .Config.Labels "com.codex.local-task"}}',$oldApp)) -cne 'claude-weekly-quota'){throw 'Unexpected task container label'}
 $uiProxy=Invoke-RestMethod -Uri 'http://127.0.0.1:54551/__cwq/control' -TimeoutSec 5
 $oldProxy=Invoke-RestMethod -Uri 'http://127.0.0.1:59160/__cwq/control' -TimeoutSec 5
 if(@($uiProxy.rules|Where-Object {$_ -and ($_.held -or -not $_.consumed)}).Count -gt 0 -or $oldProxy.holding){throw 'All browser proxy gates/rules must be idle before cutover'}
 $providerBefore=Invoke-TaskDocker @('exec',$fixture.Provider,'wget','-q','-O','-','http://127.0.0.1:8081/__state')|ConvertFrom-Json
 if(@($providerBefore.active_by_model.PSObject.Properties|Where-Object Value -gt 0).Count -gt 0){throw 'Synthetic provider is not idle'}
 $userIDs=@((Read-TaskSQL 'SELECT id FROM users ORDER BY id;') -split "`n")
 foreach($id in $userIDs){$legacyWaiting=Invoke-TaskDocker @('exec',$fixture.Redis,'redis-cli','GET',('concurrency:wait:'+$id));if(($legacyWaiting -and $legacyWaiting -cne '0') -or (Invoke-TaskDocker @('exec',$fixture.Redis,'redis-cli','ZCARD',('concurrency:user:'+$id))) -cne '0' -or (Invoke-TaskDocker @('exec',$fixture.Redis,'redis-cli','ZCARD',('concurrency:scope:{claude_subscription:user:'+$id+'}:active'))) -cne '0' -or (Invoke-TaskDocker @('exec',$fixture.Redis,'redis-cli','ZCARD',('concurrency:scope:{claude_subscription:user:'+$id+'}:waiting'))) -cne '0'){throw 'All legacy and Claude capacity must be empty before startup'}}
 foreach($id in @((Read-TaskSQL 'SELECT id FROM accounts ORDER BY id;') -split "`n")){if((Invoke-TaskDocker @('exec',$fixture.Redis,'redis-cli','ZCARD',('concurrency:account:'+$id))) -cne '0'){throw 'Account capacity must be empty before startup'}}
 $before=Financial-Fingerprint
 $result.financial_fingerprint_before=$before
 $result.provider_calls_before=@($providerBefore.calls).Count
 $result.original_public_manifest_backup='environment-I4-browser-F2-before-F4-'+$run+'.json'
 Copy-Item -LiteralPath $publicPath -Destination (Join-Path $PSScriptRoot $result.original_public_manifest_backup)
 Copy-Item -LiteralPath $privatePath -Destination (Join-Path $privateRoot ('synthetic-fixture-before-F4-'+$run+'.json'))
 $environment=Invoke-TaskDocker @('inspect','--format','{{json .Config.Env}}',$oldApp)|ConvertFrom-Json
 $null=Invoke-TaskDocker @('stop','--time','30',$oldApp);$oldStopped=$true;$result.normal_old_app_stop=$true
 $arguments=@('run','--detach','--pull','never','--name',($fixture.RunID+'-candidate-2'),'--label','com.codex.local-task=claude-weekly-quota','--network',$fixture.Network,'--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','1536m','--cpus','2','--pids-limit','512','--tmpfs','/app/data:rw,size=128m,uid=1000,gid=1000','--tmpfs','/tmp:rw,size=64m,uid=1000,gid=1000')
 foreach($item in $environment){$arguments+=@('--env',[string]$item)};$arguments+=$build.image_tag
 $newApp=Invoke-TaskDocker $arguments
 $result.new_application=$newApp
 $deadline=[DateTime]::UtcNow.AddSeconds(70);$healthy=$false
 do{try{$health=Invoke-TaskDocker @('exec',$newApp,'wget','-q','-O','-','http://127.0.0.1:8080/health');$healthy=$true}catch{};if(-not $healthy){Start-Sleep -Milliseconds 500}}while(-not $healthy -and [DateTime]::UtcNow -lt $deadline)
 if(-not $healthy){throw 'New F4 internal health unavailable'}
 $process=Get-Process -Id $oldBridge -ErrorAction Stop
 $allowedRuntime=[IO.Path]::GetFullPath((Join-Path $env:ProgramData 'CodexTaskRuntime/claude-weekly-quota/I4-bridge-'))
 if(-not $process.Path.StartsWith($allowedRuntime,[StringComparison]::OrdinalIgnoreCase) -or -not $process.Path.EndsWith('i4-loopback-bridge.exe',[StringComparison]::OrdinalIgnoreCase)){throw 'Unexpected old bridge executable'}
 $binding=Get-NetTCPConnection -LocalPort 49441 -State Listen -ErrorAction Stop|Where-Object OwningProcess -eq $oldBridge
 if(-not $binding -or @($binding|Where-Object LocalAddress -ne '127.0.0.1').Count -gt 0){throw 'Old bridge exact loopback identity mismatch'}
 Stop-Process -Id $oldBridge -ErrorAction Stop;$bridgeReplaced=$true
 $newBridge=& (Join-Path $PSScriptRoot 'environment-I4-start-bridge.ps1') -ContainerID $newApp -Port 49441 -PrivateDir $privateRoot
 if((Invoke-WebRequest -Uri ($fixture.URL+'/health') -TimeoutSec 5).StatusCode -ne 200){throw 'F4 outer loopback health failed'}
 $projections=@();foreach($entry in @(@('AKey',2),@('AKey2',2),@('GPTKey',2),@('BKey',$null))){$reply=Invoke-RestMethod -Uri ($fixture.URL+'/v1/usage') -Headers @{Authorization=('Bearer '+$fixture[$entry[0]])} -TimeoutSec 8;$data=if($reply.PSObject.Properties['data']){$reply.data}else{$reply};$effective=if($null -eq $entry[1]){3}else{2};if($data.concurrency -ne 5 -or $data.default_claude_subscription_concurrency -ne 3 -or $data.claude_subscription_concurrency -ne $entry[1] -or $data.effective_claude_subscription_concurrency -ne $effective){throw ('F4 public projection failed for '+$entry[0])};$projections+=@{key_label=$entry[0];legacy=$data.concurrency;override=$data.claude_subscription_concurrency;effective=$data.effective_claude_subscription_concurrency;default=$data.default_claude_subscription_concurrency}}
 $after=Financial-Fingerprint
 if($after -cne $before){throw 'Financial/card/account/Key quota-limit identity changed during read-only cutover'}
 $providerAfter=Invoke-TaskDocker @('exec',$fixture.Provider,'wget','-q','-O','-','http://127.0.0.1:8081/__state')|ConvertFrom-Json
 if(@($providerAfter.calls).Count -ne @($providerBefore.calls).Count){throw 'Unexpected generation provider call during cutover'}
 $fixture.PreviousApplication=$oldApp;$fixture.PreviousImageID=$fixture.ImageID;$fixture.Application=$newApp;$fixture.BridgePID=[int]$newBridge;$fixture.Image=$build.image_tag;$fixture.ImageID=$build.image_id;$fixture.Source=$build.source_sha256
 $public.previous_application=$oldApp;$public.application=$newApp;$public.loopback_bridge_pid=[int]$newBridge;$public.image=$build.image_tag;$public.image_id=$build.image_id;$public.source_sha256=$build.source_sha256;$public.stage='I4 F4 synthetic browser fixture';$public.last_cutover_receipt=[IO.Path]::GetFileName($receiptPath)
 [IO.File]::WriteAllText($privatePath+'.next',($fixture|ConvertTo-Json -Depth 14),[Text.UTF8Encoding]::new($false));Move-Item -LiteralPath ($privatePath+'.next') -Destination $privatePath -Force
 [IO.File]::WriteAllText($publicPath+'.next',($public|ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false));Move-Item -LiteralPath ($publicPath+'.next') -Destination $publicPath -Force
 $result.status='pass';$result.new_bridge_pid=[int]$newBridge;$result.public_key_projections=$projections;$result.financial_fingerprint_after=$after;$result.financial_exactly_unchanged=$true;$result.provider_calls_after=@($providerAfter.calls).Count;$result.C_private_fields_preserved=$true;$result.old_container_retained_stopped=$true;$result.proxy_54551_target_unchanged=$true
}catch{
 $result.status='failed';$result.error=$_.Exception.Message
 if($newApp){try{$null=Invoke-TaskDocker @('stop','--time','30',$newApp)}catch{}}
 if($oldStopped){try{
  $null=Invoke-TaskDocker @('start',$oldApp)
  if($bridgeReplaced){
   $listener=Get-NetTCPConnection -LocalPort 49441 -State Listen -ErrorAction SilentlyContinue
   if($listener){foreach($owner in @($listener.OwningProcess|Select-Object -Unique)){$p=Get-Process -Id $owner -ErrorAction Stop;$prefix=[IO.Path]::GetFullPath((Join-Path $env:ProgramData 'CodexTaskRuntime/claude-weekly-quota/I4-bridge-'));if(-not $p.Path.StartsWith($prefix,[StringComparison]::OrdinalIgnoreCase) -or -not $p.Path.EndsWith('i4-loopback-bridge.exe',[StringComparison]::OrdinalIgnoreCase)){throw 'Rollback found an unexpected listener; will not stop it'};Stop-Process -Id $owner -ErrorAction Stop}}
   $restored=& (Join-Path $PSScriptRoot 'environment-I4-start-bridge.ps1') -ContainerID $oldApp -Port 49441 -PrivateDir $privateRoot
   $result.restored_bridge_pid=[int]$restored
   $restorePrivate=Get-Content -LiteralPath (Join-Path $privateRoot ('synthetic-fixture-before-F4-'+$run+'.json')) -Raw|ConvertFrom-Json -AsHashtable
   $restorePublic=Get-Content -LiteralPath (Join-Path $PSScriptRoot $result.original_public_manifest_backup) -Raw|ConvertFrom-Json -AsHashtable
   $restorePrivate.BridgePID=[int]$restored;$restorePublic.loopback_bridge_pid=[int]$restored
   $restorePrivate|ConvertTo-Json -Depth 14|Set-Content -LiteralPath $privatePath -Encoding utf8
   $restorePublic|ConvertTo-Json -Depth 12|Set-Content -LiteralPath $publicPath -Encoding utf8
  }
  $result.old_app_restored=$true
 }catch{$result.rollback_error=$_.Exception.Message}}
}finally{$result.finished_utc=[DateTime]::UtcNow.ToString('o');$result|ConvertTo-Json -Depth 12|Set-Content -LiteralPath $receiptPath -Encoding utf8}
[pscustomobject]@{receipt=$receiptPath;status=$result.status;url=$fixture.URL;image_id=$result.new_image_id}|ConvertTo-Json
if($result.status -cne 'pass'){throw ('Task browser cutover failed; see '+$receiptPath)}

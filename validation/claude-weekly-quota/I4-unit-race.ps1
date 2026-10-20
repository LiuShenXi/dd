param([Parameter(Mandatory)][string]$Freeze, [int]$Expected=0, [Parameter(Mandatory)][string]$ExpectedSource)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw | ConvertFrom-Json
$runner=($resources.containers | Where-Object role -eq 'runner').id
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I4-unit-race-$Freeze-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$listLog=Join-Path $PSScriptRoot ($prefix+'-list.log')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw | ConvertFrom-Json
if($before.source_sha256 -cne $ExpectedSource){throw 'Source changed before final Linux unit race'}
& $docker --host npipe:////./pipe/docker_engine exec $runner mkdir -p /output/tmp
if($LASTEXITCODE -ne 0){throw 'Could not prepare task-only disk-backed build directory.'}
$pattern='^TestSubscriptionQuota|^TestGetUserGroupVisibility|^TestAdminService_AdminUpdateAPIKeyGroupID|^TestAPIKey(Create|Update)|^Test(PeakMultiplierAt|CalculateCostUnified|Resolve_|GetIntervalPricing_|GetRequestTierPrice|TransformBedrockInvocationMetrics)|Carpool|UsageBilling|SubscriptionAppliesRateMultiplier|APIKeyAuthSnapshotPreservesPerUserBillingIsolation|DeleteGroup|Group.*Delete|Delete.*Group|UsageCleanup|DashboardAggregation|TestProvideCleanup|^TestClaudeSubscriptionConcurrency|^TestAdminService_(CreateUser|UpdateUser)|^TestAPIKeyAuth|^TestUserService_(GetProfile|GetByID)|^TestAuthHandlerGetCurrentUser|^TestUserRepo|^TestAPIKeyService_(GetByKey|InvalidateAuthCache)'
$pattern+='|^TestOpenAIResponsesWebSocket_Ingress|^TestOpenAIWSIngressEndedByClient|^TestAcquireOpenAIWSIngressLease|^TestOpenAIWSIngress|^TestIsOpenAIWSIngressPreviousResponseNotFound|^TestLiveLease|^TestGatewayCacheLiveCallIdentityAndController|^TestDeriveOpenAIForwardAttemptBody|^TestSanitizeOpenAICrossModeFailoverReasoning'
$packages=@('./internal/domain','./internal/service','./internal/handler','./internal/handler/dto','./internal/repository','./internal/server/routes','./internal/server/middleware')
$base=@('--host','npipe:////./pipe/docker_engine','exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','go','test','-p=2','-tags=unit')
$listArguments=$base+@('-list',$pattern)+$packages
& $docker @listArguments *> $listLog
if($LASTEXITCODE -ne 0){throw "Frozen test discovery failed; inspect $listLog"}
$discovered=@(Get-Content -LiteralPath $listLog | Where-Object {$_ -cmatch '^Test[A-Za-z0-9_]+$'})
if($Expected -eq 0){$Expected=$discovered.Count}
if($Expected -eq 0 -or $Expected -ne $discovered.Count){throw 'Frozen race test inventory does not match the expected top-level count.'}
$arguments=$base+@('-race','-json','-count=1','-timeout=10m','-run',$pattern)+$packages
$started=[DateTime]::UtcNow
& $docker @arguments *> $log
$code=$LASTEXITCODE
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log | ForEach-Object {try {$_ | ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notlike '*/*'})
$skips=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'skip'})
$failures=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'fail'})
$stable=$before.source_sha256 -ceq $after.source_sha256
$functional=($code -eq 0 -and $passes.Count -eq $Expected -and $skips.Count -eq 0 -and $failures.Count -eq 0)
$result=[ordered]@{
 freeze=$Freeze;started_utc=$started.ToString('o');finished_utc=[DateTime]::UtcNow.ToString('o')
 executable=$docker;arguments=$arguments;discovery_arguments=$listArguments;discovery_log=[IO.Path]::GetFileName($listLog);head=$before.head;runner_id=$runner
 source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable
 source_before_manifest=[IO.Path]::GetFileName($beforePath);source_after_manifest=[IO.Path]::GetFileName($afterPath)
 exit_code=$code;expected_top_level_tests=$Expected;top_level_passes=@($passes | ForEach-Object {$_.Package+'::'+$_.Test})
 skip_count=$skips.Count;failure_count=$failures.Count;functional_pass=$functional
 acceptance=($Freeze -ne 'debug' -and $functional -and $stable)
 synthetic_only=$true;production_connected=$false;external_upstream_called=$false;log=[IO.Path]::GetFileName($log)
 linux_go='go1.27.2 linux/amd64';race=$true;temporary_directory='/output/tmp'
 validation_kind='Linux critical unit concurrency race';integration_harness=$false;real_postgresql=$false
}
$result | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $PSScriptRoot ($prefix+'-receipt.json')) -Encoding utf8
[pscustomobject]@{freeze=$Freeze;source_stable=$stable;functional_pass=$functional;acceptance=$result.acceptance;passed=$passes.Count;expected=$Expected;skipped=$skips.Count;failed=$failures.Count;receipt=$prefix+'-receipt.json'} | ConvertTo-Json
if(-not $functional){Get-Content -LiteralPath $log -Tail 35;exit 1}

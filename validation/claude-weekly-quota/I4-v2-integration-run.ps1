param([string]$Freeze='debug',[switch]$Final)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I4-v2-redis-policy-$Freeze-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$env:PATH='C:/Program Files/Docker/Docker/resources/bin;C:/Program Files/Git/usr/bin;'+$env:PATH
$env:DOCKER_HOST='npipe:////./pipe/docker_engine'
Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI='true';$env:GOTOOLCHAIN='local';$env:GOMAXPROCS='4'
$env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$pattern='^TestSubscriptionQuotaConcurrencyV2(AtomicTwoClientsFivePlusTwo|WaitingTicketAtomicPromotion|RenewExpiryAndMaximumKeyTTL|UnknownCleanupBlocksLateCommands|UnavailableNeverFallsBackToGPT|PolicyDefaultOverrideAndUpdateLock|ServiceWaitingCancellationTimeoutAndPromotion|PolicyReductionKeepsExistingLeases|ProtectedBeforeUserScopeDurability|StatisticsPoolFaultIsBoundedUnknown|RenewalFaultCancelsBeforeLeaseExpiry|LegacyBulkKeepsClaudePolicy|ManualIssueRequiresConfiguredPolicy)$'
$arguments=@('test','-tags=integration','-json','-count=1','-timeout=5m','-run',$pattern,'./internal/repository')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw|ConvertFrom-Json
Push-Location (Join-Path $repoRoot 'backend')
try { & $go @arguments *> $log; $code=$LASTEXITCODE } finally { Pop-Location }
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw|ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log|ForEach-Object {try {$_|ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass' -and $_.Test -notlike '*/*'})
$subpasses=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass'})
$fails=@($events|Where-Object {$_.Action -eq 'fail'})
$skips=@($events|Where-Object {$_.Action -eq 'skip'})
$functional=$code -eq 0 -and $passes.Count -eq 13 -and $fails.Count -eq 0 -and $skips.Count -eq 0
$stable=$before.source_sha256 -ceq $after.source_sha256
$result=[ordered]@{stage='I4 v2 actual Redis dual-client and PostgreSQL policy integration';freeze=$Freeze;diagnostic=(-not [bool]$Final);executable=$go;arguments=$arguments;exit_code=$code;source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable;expected_top_level_tests=13;top_level_passes=@($passes|ForEach-Object {$_.Test});pass_count_including_subtests=$subpasses.Count;failure_count=$fails.Count;skip_count=$skips.Count;functional_pass=$functional;acceptance=([bool]$Final -and $functional -and $stable);log=[IO.Path]::GetFileName($log);original_TestMain=$true;real_redis=$true;real_postgresql=$true;independent_scope_clients_at_least=2;short_ttl_tests_replace_15minute_SSE=$false;actual_router_covered=$false;synthetic_only=$true;production_connected=$false;external_upstream_called=$false;finished_utc=[DateTime]::UtcNow.ToString('o')}
$receipt=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$result|ConvertTo-Json -Depth 6|Set-Content -LiteralPath $receipt -Encoding utf8
[pscustomobject]@{receipt=$receipt;functional_pass=$functional;acceptance=$result.acceptance;source_stable=$stable;passed=$passes.Count;failed=$fails.Count;skipped=$skips.Count}|ConvertTo-Json
if(-not $functional){Get-Content -LiteralPath $log -Tail 30;exit 1}
if($Final -and -not $stable){throw 'Source changed during final v2 integration validation'}

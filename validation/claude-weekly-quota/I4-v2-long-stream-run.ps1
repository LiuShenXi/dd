param([Parameter(Mandatory)][string]$EnvironmentReceipt,[int]$DurationMinutes=16,[string]$Freeze='debug',[switch]$Final,[string]$SourceBridgeReceipt='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if($DurationMinutes -lt 16){throw 'T68 requires an actual stream exceeding fifteen minutes'}
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$fixture=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
if($fixture.url -notlike 'http://127.0.0.1:*'){throw 'Only the synthetic loopback environment is supported'}
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I4-v2-T68-$Freeze-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$env:CWQ_I4_PRIVATE_DIR=Split-Path -Parent $fixture.private_fixture_path
$env:CWQ_I4_LONG_STREAM_MINUTES=$DurationMinutes.ToString()
$env:PATH='C:/Program Files/Docker/Docker/resources/bin;C:/Program Files/Git/usr/bin;'+$env:PATH
$env:DOCKER_HOST='npipe:////./pipe/docker_engine'
Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI='true';$env:GOTOOLCHAIN='local';$env:GOMAXPROCS='4'
$env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$arguments=@('test','-tags=integration','-json','-count=1','-timeout=20m','-run','^TestSubscriptionQuotaI4V2LongSSELease$','./internal/repository')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw|ConvertFrom-Json
. (Join-Path $PSScriptRoot 'environment-I4-source-compatibility.ps1')
$compatibility=Get-CwqI4SourceCompatibility -Driver $before -ImageSource $fixture.source_sha256 -ImageID $fixture.image_id -SourceBridgeReceipt $SourceBridgeReceipt
try {
    Push-Location (Join-Path $repoRoot 'backend')
    try { & $go @arguments *> $log; $code=$LASTEXITCODE } finally { Pop-Location }
} finally {
    Remove-Item Env:CWQ_I4_PRIVATE_DIR,Env:CWQ_I4_LONG_STREAM_MINUTES -ErrorAction SilentlyContinue
}
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw|ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log|ForEach-Object {try {$_|ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass' -and $_.Test -notlike '*/*'})
$fails=@($events|Where-Object {$_.Action -eq 'fail'})
$skips=@($events|Where-Object {$_.Action -eq 'skip'})
$functional=$code -eq 0 -and $passes.Count -eq 1 -and $fails.Count -eq 0 -and $skips.Count -eq 0
$terminal=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Test -eq 'TestSubscriptionQuotaI4V2LongSSELease' -and $_.Action -in @('pass','fail')}|Select-Object -Last 1)
$elapsed=if($terminal.Count -eq 1){$terminal[0].Elapsed}else{$null}
$outputs=(@($events|Where-Object {$_.PSObject.Properties['Output']}|ForEach-Object {$_.Output})) -join ''
$samples=[regex]::Matches($outputs,'T68 actual HTTP SSE elapsed=')
$renewed=[regex]::IsMatch($outputs,'minimum_expiry_extension_ms=[1-9][0-9]*')
$settled=$outputs.Contains('two_known_settled_ack=true other_wallets_unchanged=true')
$stable=$before.source_sha256 -ceq $after.source_sha256
$compatibilityAfter=Get-CwqI4SourceCompatibility -Driver $after -ImageSource $fixture.source_sha256 -ImageID $fixture.image_id -SourceBridgeReceipt $SourceBridgeReceipt
$accepted=[bool]$Final -and $functional -and $stable -and $elapsed -gt 900 -and $renewed -and $settled
$result=[ordered]@{stage='I4 v2 T68 actual Router HTTP SSE longer than fifteen minutes';freeze=$Freeze;diagnostic=(-not [bool]$Final);executable=$go;arguments=$arguments;planned_duration_minutes=$DurationMinutes;actual_test_elapsed_seconds=$elapsed;lease_sample_count=$samples.Count;exit_code=$code;driver_source_sha256=$before.source_sha256;driver_source_after_sha256=$after.source_sha256;driver_source_stable=$stable;candidate_source_sha256=$fixture.source_sha256;candidate_image_id=$fixture.image_id;candidate_url=$fixture.url;environment_receipt=[IO.Path]::GetFileName($EnvironmentReceipt);pass_count=$passes.Count;failure_count=$fails.Count;skip_count=$skips.Count;functional_pass=$functional;acceptance=$accepted;log=[IO.Path]::GetFileName($log);actual_candidate_configured=$true;actual_router_observed=($samples.Count -gt 0);actual_redis_renewal_observed=$renewed;actual_client_receive_metrics_observed=$outputs.Contains('client_bytes=');actual_postgresql_settlement_observed=$settled;synthetic_provider_only=$true;synthetic_provider_called=($samples.Count -gt 0);external_upstream_called=$false;production_connected=$false;finished_utc=[DateTime]::UtcNow.ToString('o')}
$result.source_compatibility=$compatibility
$result.source_compatibility_after=$compatibilityAfter
$receipt=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$result|ConvertTo-Json -Depth 6|Set-Content -LiteralPath $receipt -Encoding utf8
[pscustomobject]@{receipt=$receipt;functional_pass=$functional;acceptance=$accepted;passed=$passes.Count;failed=$fails.Count;skipped=$skips.Count}|ConvertTo-Json
if(-not $functional){Get-Content -LiteralPath $log -Tail 35;exit 1}
if($Final -and -not $accepted){throw 'Final long-stream evidence does not match the frozen image/source or did not reach the required observed assertions'}

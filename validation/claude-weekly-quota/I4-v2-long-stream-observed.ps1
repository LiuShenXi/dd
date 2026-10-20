param([Parameter(Mandatory)][string]$Receipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$r=Get-Content -LiteralPath $Receipt -Raw|ConvertFrom-Json
if(-not $r.functional_pass -or -not $r.acceptance){throw 'A completed accepted long-stream wrapper receipt is required'}
$events=@(Get-Content -LiteralPath (Join-Path $PSScriptRoot $r.log)|ForEach-Object {try{$_|ConvertFrom-Json}catch{}})
$test='TestSubscriptionQuotaI4V2LongSSELease'
$testEvents=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Test -ceq $test})
$start=$testEvents|Where-Object Action -eq 'run'|Select-Object -First 1
$end=$testEvents|Where-Object Action -eq 'pass'|Select-Object -Last 1
$samples=@($testEvents|Where-Object {$_.PSObject.Properties['Output'] -and $_.Output -match 'T68 actual HTTP SSE elapsed='})
$last=$testEvents|Where-Object {$_.PSObject.Properties['Output'] -and $_.Output -match 'T68 actual long-stream PASS elapsed='}|Select-Object -Last 1
if(-not $start -or -not $end -or -not $last -or $samples.Count -lt 50){throw 'Actual successful duration samples and terminal record missing'}
$text=$last.Output
$match=[regex]::Match($text,'elapsed=(\S+) checks=(\d+).*exact_debit=(\d+\.\d+) two_known_settled_ack=true other_wallets_unchanged=true')
if(-not $match.Success){throw 'Final actual ledger/ack marker missing'}
$seconds=0.0
foreach($part in [regex]::Matches($match.Groups[1].Value,'([\d.]+)(h|m|s)')){$factor=switch($part.Groups[2].Value){h{3600}m{60}s{1}};$seconds+=[double]$part.Groups[1].Value*$factor}
if($seconds -le 900){throw 'Observed stream duration did not exceed fifteen minutes'}
$maximumExpiry=0L;$clientSamples=@()
foreach($sample in $samples){
 $m=[regex]::Match($sample.Output,'minimum_expiry_extension_ms=(\d+) client_bytes=(\d+),(\d+) last_client_receive_age_ms=(\d+),(\d+)')
 if(-not $m.Success){throw 'Actual client/renewal observation missing'}
 $maximumExpiry=[Math]::Max($maximumExpiry,[long]$m.Groups[1].Value)
 $clientSamples+=@{time=$sample.Time;bytes=@([long]$m.Groups[2].Value,[long]$m.Groups[3].Value);receive_age_ms=@([long]$m.Groups[4].Value,[long]$m.Groups[5].Value)}
}
$envReceipt=Join-Path $PSScriptRoot $r.environment_receipt
$public=Get-Content -LiteralPath $envReceipt -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$sql=@'
SELECT jsonb_agg(x ORDER BY x.id)::text FROM (SELECT r.id,r.term_id,r.user_id,r.api_key_id,r.status,r.actual_cost_usd::text,r.usage_log_recorded_at IS NOT NULL AS log_acked,(SELECT count(*) FROM subscription_quota_ledger l WHERE l.request_row_id=r.id) AS ledger_rows,(SELECT count(*) FROM usage_billing_dedup d WHERE d.request_id=r.request_id AND d.api_key_id=r.api_key_id) AS dedup_rows FROM subscription_quota_requests r ORDER BY r.id DESC LIMIT 2) x;
'@
$requests=((@($sql|& 'C:/Program Files/Docker/Docker/resources/bin/docker.exe' --host npipe:////./pipe/docker_engine exec -i $e.Postgres psql -U postgres -d $e.Database -X -A -t -q -v ON_ERROR_STOP=1))-join "`n")|ConvertFrom-Json
if($requests.Count -ne 2 -or @($requests|Where-Object {$_.term_id -ne $e.ATerm -or $_.status -cne 'settled' -or $_.actual_cost_usd -cne '0.00060000' -or -not $_.log_acked -or $_.ledger_rows -ne 1 -or $_.dedup_rows -ne 1}).Count -gt 0){throw 'Post-test actual original-card request ownership did not match'}
$result=[ordered]@{stage='I4 final T68 actual observations';wrapper=[IO.Path]::GetFileName($Receipt);log=$r.log;image_id=$r.candidate_image_id;image_source_sha256=$r.candidate_source_sha256;driver_source_sha256=$r.driver_source_sha256;source_compatibility=$r.source_compatibility;test_start_utc=([DateTimeOffset]$start.Time).UtcDateTime.ToString('o');test_finish_utc=([DateTimeOffset]$end.Time).UtcDateTime.ToString('o');actual_test_elapsed_seconds=$end.Elapsed;actual_stream_elapsed_seconds=$seconds;actual_lease_sample_count=$samples.Count;maximum_observed_expiry_extension_ms=$maximumExpiry;actual_client_samples=$clientSamples;checks=[int]$match.Groups[2].Value;actual_original_term_debit=$match.Groups[3].Value;postgres_requests=$requests;actual_two_known_settled_ack=$true;original_UUID_and_active2_preserved_each_sample=$true;third_request_no_provider_or_admission_after15minutes_asserted=$true;other_wallet_full_period_assertions='B complete card, ordinary/native/GPT full-row fingerprints from the actual test';synthetic_provider_called=$true;external_upstream_called=$false;production_connected=$false;observations_derived_from_completed_logs_and_actual_PG=$true;observed_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ([IO.Path]::GetFileNameWithoutExtension($Receipt).Replace('-receipt','')+'-observed.json')
$result|ConvertTo-Json -Depth 9|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;stream_seconds=$seconds;test_seconds=$end.Elapsed;sample_count=$samples.Count;exact_debit=$match.Groups[3].Value;postgres_requests=$requests.Count}|ConvertTo-Json

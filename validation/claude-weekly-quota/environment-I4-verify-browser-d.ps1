param(
 [string]$StartReceipt='I4-browser-D-Start-20261020T000125Z.json',
 [string]$ObserveReceipt='I4-browser-D-Observe-20261020T000434Z.json'
)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$start=Get-Content -LiteralPath (Join-Path $PSScriptRoot $StartReceipt) -Raw|ConvertFrom-Json
$observe=Get-Content -LiteralPath (Join-Path $PSScriptRoot $ObserveReceipt) -Raw|ConvertFrom-Json
$cutover=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-browser-F4-cutover-20261019T234529Z.json') -Raw|ConvertFrom-Json
$public=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-20261019T180351Z-active.json') -Raw|ConvertFrom-Json
$private=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$d=Get-Content -LiteralPath $observe.private_fixture_path -Raw|ConvertFrom-Json
$id=[int64]$d.UserID
if($id -ne 5 -or -not $public.synthetic_only -or $private.URL -cne 'http://127.0.0.1:49441'){throw 'Only the exact synthetic D fixture is supported'}
$sql=@"
WITH qc AS MATERIALIZED (SELECT clock_timestamp() AS now)
SELECT jsonb_build_object('database_time',(SELECT now FROM qc),
 'current_term_id',(SELECT t.id FROM subscription_quota_terms t,qc WHERE t.user_id=$id AND t.cancelled_at IS NULL AND t.starts_at<=qc.now AND qc.now<t.expires_at),
 'current_quota_usd',(SELECT t.quota_usd::text FROM subscription_quota_terms t,qc WHERE t.user_id=$id AND t.cancelled_at IS NULL AND t.starts_at<=qc.now AND qc.now<t.expires_at),
 'parent_count',(SELECT count(*) FROM user_subscriptions WHERE user_id=$id),
 'parent_revision',(SELECT quota_revision FROM user_subscriptions WHERE user_id=$id),
 'operation_count',(SELECT count(*) FROM subscription_quota_operations WHERE idempotency_scope LIKE '%:user:$id' || ':group:%'),
 'dedup_count',(SELECT count(*) FROM usage_billing_dedup b JOIN subscription_quota_requests r ON r.request_id=b.request_id AND r.api_key_id=b.api_key_id WHERE r.user_id=$id),
 'usage_log_count',(SELECT count(*) FROM usage_logs WHERE user_id=$id),
 'balance',(SELECT balance::text FROM users WHERE id=$id))::text;
"@
$raw=(@($sql|& 'C:/Program Files/Docker/Docker/resources/bin/docker.exe' --host npipe:////./pipe/docker_engine exec -i $private.Postgres psql -U postgres -d $private.Database -X -A -t -q -v ON_ERROR_STOP=1))-join "`n"
if($LASTEXITCODE -ne 0){throw 'D final independent SQL failed'}
$database=$raw|ConvertFrom-Json
$foreignBefore=$start.foreign_after|ConvertTo-Json -Depth 20 -Compress
$foreignNow=$observe.foreign_observed|ConvertTo-Json -Depth 20 -Compress
$checks=[ordered]@{
 both_full168h=@($observe.facts.terms).Count -eq 2 -and @($observe.facts.terms|Where-Object duration_seconds -ne 604800).Count -eq 0
 distinct13and17=$observe.facts.terms[0].quota_usd -ceq '13.00000000' -and $observe.facts.terms[1].quota_usd -ceq '17.00000000'
 adjacent_windows=$observe.facts.terms[0].expires_at -ceq $observe.facts.terms[1].starts_at
 immutable_hash_unchanged=$start.facts.term_immutable_hash -ceq $observe.facts.term_immutable_hash
 no_used_delta=@($observe.facts.terms|Where-Object used_usd -ne '0.00000000').Count -eq 0
 no_requests_ledger=$observe.facts.request_count -eq 0 -and $observe.facts.ledger_count -eq 0
 no_dedup_log=$database.dedup_count -eq 0 -and $database.usage_log_count -eq 0
 no_fixture_operations=$database.operation_count -eq 0
 parent_still_revision2=$database.parent_count -eq 1 -and $database.parent_revision -eq 2
 server_now_selects_original_queued17=$database.current_term_id -eq $observe.facts.terms[1].id -and $database.current_quota_usd -ceq '17.00000000'
 D_balance0_legacy5_default3=[decimal]$database.balance -eq 0 -and $observe.facts.legacy_concurrency -eq 5 -and $null -eq $observe.facts.claude_override
 all_ABC_native_Keymoney_and20GPT_payment_tables_unchanged=$foreignBefore -ceq $foreignNow
 no_provider_call_delta=$cutover.provider_calls_after -eq $observe.provider_call_count
 provider_idle=@($observe.provider_active_by_model.PSObject.Properties|Where-Object Value -gt 0).Count -eq 0
}
$pass=@($checks.Values|Where-Object {-not $_}).Count -eq 0
$result=[ordered]@{
 stage='I4 T48 D independent final SQL/foreign/provider observations';pass=$pass;image_id=$public.image_id;source_sha256=$public.source_sha256
 checks=$checks;D_database=$database;original_start_receipt=$StartReceipt;final_observe_receipt=$ObserveReceipt
 provider_call_count_before=$cutover.provider_calls_after;provider_call_count_after=$observe.provider_call_count
 immutable_hash_before=$start.facts.term_immutable_hash;immutable_hash_after=$observe.facts.term_immutable_hash
 foreign_before=$start.foreign_after;foreign_after=$observe.foreign_observed
 UI_timeout_retry_branch='Root actual CUA: held response exceeded client timeout; unchanged13/queued17 awaiting server observed; release then manual Retry selected17. Original held delivery is not claimed to update DOM.'
 read_only=$true;new_model_API_called=$false;production_connected=$false;observed_utc=[DateTime]::UtcNow.ToString('o')
}
$result|ConvertTo-Json -Depth 20|Set-Content -LiteralPath (Join-Path $PSScriptRoot 'I4-browser-D-final-independent.json') -Encoding utf8
[pscustomobject]@{receipt='I4-browser-D-final-independent.json';pass=$pass;checks=$checks;current_term_id=$database.current_term_id}|ConvertTo-Json -Depth 6
if(-not $pass){throw 'D final independent invariant verification failed'}

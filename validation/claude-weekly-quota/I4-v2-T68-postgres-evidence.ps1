param([Parameter(Mandatory)][string]$Observation)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$o=Get-Content -LiteralPath $Observation -Raw|ConvertFrom-Json
$wrapper=Get-Content -LiteralPath (Join-Path $PSScriptRoot $o.wrapper) -Raw|ConvertFrom-Json
$public=Get-Content -LiteralPath (Join-Path $PSScriptRoot $wrapper.environment_receipt) -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$ids=@($o.postgres_requests|ForEach-Object {[int64]$_.id})
if($ids.Count -ne 2){throw 'Exactly two actually completed duration request IDs required'}
$idList=$ids -join ','
$sql=@"
SELECT jsonb_agg(jsonb_build_object(
 'request_row_id',r.id,'request_id',r.request_id,'user_id',r.user_id,'group_id',r.group_id,'api_key_id',r.api_key_id,'term_id',r.term_id,'status',r.status,'actual_cost_usd',r.actual_cost_usd::text,'admitted_at',r.admitted_at,'settled_at',r.settled_at,'usage_log_recorded_at',r.usage_log_recorded_at,
 'ledger',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.id) FROM subscription_quota_ledger l WHERE l.request_row_id=r.id),'[]'),
 'dedup',COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM usage_billing_dedup d WHERE d.request_id=r.request_id AND d.api_key_id=r.api_key_id),'[]'),
 'usage_logs',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',l.id,'request_id',l.request_id,'user_id',l.user_id,'api_key_id',l.api_key_id,'group_id',l.group_id,'account_id',l.account_id,'subscription_id',l.subscription_id,'subscription_term_id',l.subscription_term_id,'subscription_quota_request_id',l.subscription_quota_request_id,'model',l.model,'input_tokens',l.input_tokens,'output_tokens',l.output_tokens,'actual_cost',l.actual_cost::text) ORDER BY l.id) FROM usage_logs l WHERE l.subscription_quota_request_id=r.id),'[]')) ORDER BY r.id)::text
FROM subscription_quota_requests r WHERE r.id IN ($idList);
SELECT jsonb_agg(jsonb_build_object('term_id',id,'user_id',user_id,'quota_usd',quota_usd::text,'used_usd',used_usd::text) ORDER BY id)::text FROM subscription_quota_terms;
"@
$lines=@($sql|& 'C:/Program Files/Docker/Docker/resources/bin/docker.exe' --host npipe:////./pipe/docker_engine exec -i $e.Postgres psql -U postgres -d $e.Database -X -A -t -q -v ON_ERROR_STOP=1)
if($LASTEXITCODE -ne 0){throw 'Actual PG duration details query failed'}
$requests=$lines[0]|ConvertFrom-Json
$terms=$lines[1]|ConvertFrom-Json
foreach($r in $requests){
 if($r.term_id -ne $e.ATerm -or $r.status -cne 'settled' -or $r.actual_cost_usd -cne '0.00060000' -or -not $r.usage_log_recorded_at -or $r.ledger.Count -ne 1 -or $r.dedup.Count -ne 1 -or $r.usage_logs.Count -ne 1){throw 'Actual per-request financial cardinality/ownership mismatch'}
 $l=$r.ledger[0];$d=$r.dedup[0];$u=$r.usage_logs[0]
 if($l.term_id -ne $r.term_id -or $l.request_row_id -ne $r.request_row_id -or $l.entry_kind -cne 'usage_debit' -or [decimal]$l.delta_used_usd -ne [decimal]'0.00060000' -or $d.request_id -cne $r.request_id -or $d.api_key_id -ne $r.api_key_id -or $u.subscription_term_id -ne $r.term_id -or $u.user_id -ne $r.user_id -or $u.api_key_id -ne $r.api_key_id -or $u.group_id -ne $r.group_id -or $u.account_id -ne $e.Account -or $u.input_tokens -ne 100 -or $u.output_tokens -ne 20 -or [decimal]$u.actual_cost -ne [decimal]'0.00060000'){throw 'Actual ledger/dedup/log identity, tokens or exact amount mismatch'}
}
$mid=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'I4-v2-T68-F3-midrun-cards.json') -Raw|ConvertFrom-Json
$cardDeltas=@()
foreach($before in $mid.terms){$after=$terms|Where-Object term_id -eq $before.term_id;if(-not $after){throw 'Card disappeared'};$delta=[decimal]$after.used_usd-[decimal]$before.used_usd;if($before.term_id -eq $e.ATerm){if($delta -ne [decimal]'0.00120000'){throw 'Main exact debit differs'}}elseif($delta -ne 0){throw 'Foreign/next/secondary card used amount changed after mid-run observation'};$cardDeltas+=@{term_id=$before.term_id;before_used=$before.used_usd;after_used=$after.used_usd;delta_used=$delta.ToString('F8',[Globalization.CultureInfo]::InvariantCulture)}}
$result=[ordered]@{stage='I4 final T68 independent actual PostgreSQL request-ledger-dedup-log rows';observation=[IO.Path]::GetFileName($Observation);query=$sql;requests=$requests;per_request_identity_exact_amount_and_cardinality_pass=$true;additional_midrun_to_end_card_deltas=$cardDeltas;additional_card_delta_window='Explicit mid-run observation to end, not invented full-period before values';full_period_foreign_wallet_assertions_from_actual_test='B entire card; users/native/GPT/redeem/payment full-row fingerprints';candidate_image_id=$wrapper.candidate_image_id;candidate_source_sha256=$wrapper.candidate_source_sha256;driver_source_sha256=$wrapper.driver_source_sha256;source_compatibility=$wrapper.source_compatibility;synthetic_only=$true;production_connected=$false;read_only=$true;observed_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ([IO.Path]::GetFileNameWithoutExtension($Observation).Replace('-observed','')+'-postgres-details.json')
$result|ConvertTo-Json -Depth 12|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;requests=$requests.Count;ledger_rows_per_request=1;dedup_rows_per_request=1;log_rows_per_request=1;exact_cost_each='0.00060000';original_term=$e.ATerm;additional_cards_observed=$cardDeltas.Count}|ConvertTo-Json

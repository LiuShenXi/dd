param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$d='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$audit=(& $d --host npipe:////./pipe/docker_engine exec $e.Provider wget -q -O - http://127.0.0.1:8081/__audit)|ConvertFrom-Json
$last=$audit|Select-Object -Last 1
$sql="SELECT jsonb_build_object('request_row_id',r.id,'user_id',r.user_id,'term_id',r.term_id,'status',r.status,'actual_cost_usd',r.actual_cost_usd,'actual_cost_is_null',r.actual_cost_usd IS NULL,'last_error_category',r.last_error_category,'ledger_rows',(SELECT count(*) FROM subscription_quota_ledger l WHERE l.request_row_id=r.id),'dedup_rows',(SELECT count(*) FROM usage_billing_dedup d WHERE d.request_id=r.request_id AND d.api_key_id=r.api_key_id),'term_used_usd',(SELECT used_usd::text FROM subscription_quota_terms t WHERE t.id=r.term_id))::text FROM subscription_quota_requests r WHERE r.user_id=$($e.BoundaryUser) ORDER BY r.id DESC LIMIT 1;"
$row=((& $d --host npipe:////./pipe/docker_engine exec $e.Postgres psql -U postgres -d $e.Database -X -A -t -v ON_ERROR_STOP=1 -c $sql)-join "`n")|ConvertFrom-Json
$pass=$last.cancelled -eq $true -and $last.active -eq $false -and $row.status -eq 'reconcile_required' -and $row.actual_cost_is_null -eq $true -and $row.ledger_rows -eq 0 -and $row.dedup_rows -eq 0 -and $row.term_used_usd -ceq '0.00000000'
$result=[ordered]@{stage='I4 actual Redis-fault post-test read-only observation';synthetic_only=$true;production_connected=$false;environment=[IO.Path]::GetFileName($EnvironmentReceipt);image_id=$public.image_id;source_sha256=$public.source_sha256;provider_call_observed=$last;postgres_observed=$row;unknown_preserved_without_SQL_reset=$true;observation_pass=$pass;meaning='Actual provider request cancelled=true is independently observed, not inferred from active=0; unknown cost remains SQL NULL and no debit/dedup exists.';observed_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ('I4-v2-RedisFault-observed-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
$result|ConvertTo-Json -Depth 7|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;observation_pass=$pass;provider_cancelled=$last.cancelled;actual_cost_is_null=$row.actual_cost_is_null;ledger_rows=$row.ledger_rows;dedup_rows=$row.dedup_rows;term_used_usd=$row.term_used_usd}|ConvertTo-Json
if(-not $pass){throw 'Redis fault observed state did not match assertions'}

param([switch]$RequireFinal)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-20261019T180351Z-active.json') -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
if(-not $public.synthetic_only -or $e.C -ne 4 -or $e.URL -cne 'http://127.0.0.1:49441'){throw 'Only synthetic browser C4 is supported'}
$sql=@"
SELECT jsonb_build_object('user_id',u.id,'legacy_concurrency',u.concurrency,'claude_override',u.claude_subscription_concurrency,'balance',u.balance::text,
 'terms',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',t.id,'term_no',t.term_no,'subscription_id',t.subscription_id,'quota_usd',t.quota_usd::text,'used_usd',t.used_usd::text,'starts_at',t.starts_at,'expires_at',t.expires_at,'duration_seconds',extract(epoch FROM t.expires_at-t.starts_at),'cancelled_at',t.cancelled_at) ORDER BY t.term_no),'[]') FROM subscription_quota_terms t WHERE t.user_id=u.id),
 'parents',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',p.id,'starts_at',p.starts_at,'expires_at',p.expires_at,'quota_revision',p.quota_revision,'deleted_at',p.deleted_at) ORDER BY p.id),'[]') FROM user_subscriptions p WHERE p.user_id=u.id),
 'operations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',o.id,'kind',o.operation_kind,'idempotency_key_sha256',encode(sha256(convert_to(o.idempotency_key,'UTF8')),'hex'),'request_fingerprint',o.request_fingerprint) ORDER BY o.id),'[]') FROM subscription_quota_operations o WHERE o.idempotency_scope LIKE '%:user:'||u.id::text||':group:%'),
 'request_count',(SELECT count(*) FROM subscription_quota_requests WHERE user_id=u.id),
 'ledger_count',(SELECT count(*) FROM subscription_quota_ledger l JOIN subscription_quota_terms t ON t.id=l.term_id WHERE t.user_id=u.id))::text FROM users u WHERE u.id=4;
"@
$raw=(@($sql|& 'C:/Program Files/Docker/Docker/resources/bin/docker.exe' --host npipe:////./pipe/docker_engine exec -i $e.Postgres psql -U postgres -d $e.Database -X -A -t -q -v ON_ERROR_STOP=1))-join "`n"
if($LASTEXITCODE -ne 0){throw 'Read-only C final observation failed'}
$facts=$raw|ConvertFrom-Json
$checks=[ordered]@{legacy5=$facts.legacy_concurrency -eq 5;claude4=$facts.claude_override -eq 4;two_terms=@($facts.terms).Count -eq 2;two_operations=@($facts.operations).Count -eq 2;no_consumption=$facts.request_count -eq 0 -and $facts.ledger_count -eq 0}
if($checks.two_terms){$checks.amounts100then75=$facts.terms[0].quota_usd -ceq '100.00000000' -and $facts.terms[1].quota_usd -ceq '75.00000000';$checks.full168h=@($facts.terms|Where-Object {$_.duration_seconds -ne 604800}).Count -eq 0;$checks.contiguous=$facts.terms[0].expires_at -ceq $facts.terms[1].starts_at;$checks.no_cancel_or_debit=@($facts.terms|Where-Object {$_.cancelled_at -or $_.used_usd -cne '0.00000000'}).Count -eq 0;$checks.parent_summary=@($facts.parents).Count -eq 1 -and $facts.parents[0].starts_at -ceq $facts.terms[0].starts_at -and $facts.parents[0].expires_at -ceq $facts.terms[1].expires_at -and $facts.parents[0].quota_revision -eq 2 -and $null -eq $facts.parents[0].deleted_at}
if($checks.two_operations){$checks.one_open_one_renew=@($facts.operations|Where-Object kind -eq 'open_term').Count -eq 1 -and @($facts.operations|Where-Object kind -eq 'renew_term').Count -eq 1}
$finalPass=@($checks.Values|Where-Object {-not $_}).Count -eq 0
$path=Join-Path $PSScriptRoot ('I4-browser-C-final-observed-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
[ordered]@{stage='I4 C actual UI final SQL observations';image_id=$public.image_id;facts=$facts;checks=$checks;final_pass=$finalPass;require_final=[bool]$RequireFinal;read_only=$true;raw_idempotency_auth_payload_logged=$false;observed_utc=[DateTime]::UtcNow.ToString('o')}|ConvertTo-Json -Depth 14|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;terms=@($facts.terms).Count;operations=@($facts.operations).Count;final_pass=$finalPass}|ConvertTo-Json
if($RequireFinal -and -not $finalPass){throw 'C final SQL assertions not yet satisfied; no data was changed'}

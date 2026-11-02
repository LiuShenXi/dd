param(
 [Parameter(Mandatory)][ValidateSet('Prepare','Start','Observe','ArmRefresh','ReleaseRefresh')][string]$Action,
 [ValidateRange(45,300)][int]$Seconds=120,
 [ValidatePattern('^[a-zA-Z0-9_-]{1,64}$')][string]$RuleID='D_boundary_1'
)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$publicPath=Join-Path $PSScriptRoot 'environment-I4-20261019T180351Z-active.json'
$public=Get-Content -LiteralPath $publicPath -Raw|ConvertFrom-Json
$fixture=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json -AsHashtable
if(-not $public.synthetic_only -or $fixture.URL -cne 'http://127.0.0.1:49441' -or $fixture.Group -ne 2 -or $public.image_id -cne 'sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9'){throw 'Only the approved F4 synthetic browser fixture is supported'}
$privatePath=Join-Path (Split-Path -Parent $public.private_fixture_path) 'browser-expiry-d.json'
$receiptPath=Join-Path $PSScriptRoot ('I4-browser-D-'+$Action+'-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
function TaskSQL([string]$SQL){
 $out=@($SQL|& $docker --host npipe:////./pipe/docker_engine exec -i $fixture.Postgres psql -U postgres -d $fixture.Database -X -A -t -q -v ON_ERROR_STOP=1)
 if($LASTEXITCODE -ne 0){throw 'Synthetic browser SQL failed; no business trigger bypass is permitted'}
 return (($out -join "`n").Trim())
}
function SavePrivate($Value){[IO.File]::WriteAllText($privatePath,($Value|ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))}
function ForeignFacts {
 $ids=@($fixture.A,$fixture.B,$fixture.C)|ForEach-Object {[int64]$_}
 if($ids.Count -ne 3 -or @($ids|Where-Object {$_ -le 0}).Count){throw 'Expected existing A/B/C identities'}
 $list=$ids -join ','
 $legacyTables=(TaskSQL "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND (table_name LIKE 'carpool\_%' ESCAPE '\' OR table_name LIKE 'payment\_%' ESCAPE '\') ORDER BY table_name;") -split "`n"|Where-Object {$_}
 $legacyQueries=foreach($table in $legacyTables){if($table -cnotmatch '^[a-z0-9_]+$'){throw 'Unsafe synthetic table identifier'};"SELECT '$table' AS name,md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) AS hash FROM $table x"}
 $legacyUnion=$legacyQueries -join ' UNION ALL '
 return TaskSQL @"
SELECT jsonb_build_object(
 'users', (SELECT md5(jsonb_agg(to_jsonb(u)-'password_hash'-'email'-'username'-'notes' ORDER BY id)::text) FROM users u WHERE id IN ($list)),
 'parents',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(p) ORDER BY id)::text,'[]')) FROM user_subscriptions p WHERE user_id IN ($list)),
 'terms',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'[]')) FROM subscription_quota_terms t WHERE user_id IN ($list)),
 'requests',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id)::text,'[]')) FROM subscription_quota_requests r WHERE user_id IN ($list)),
 'ledger',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id)::text,'[]')) FROM subscription_quota_ledger l JOIN subscription_quota_terms t ON t.id=l.term_id WHERE t.user_id IN ($list)),
 'operations',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(o) ORDER BY id)::text,'[]')) FROM subscription_quota_operations o WHERE EXISTS(SELECT 1 FROM users u WHERE u.id IN ($list) AND o.idempotency_scope LIKE '%:user:'||u.id::text||':group:%')),
 'keys',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(k)-'last_used_at'-'updated_at' ORDER BY id)::text,'[]')) FROM api_keys k WHERE user_id IN ($list)),
 'all_gpt_and_payment_tables',(SELECT jsonb_object_agg(name,hash) FROM ($legacyUnion) preserved)
)::text;
"@
}
function ObserveD($D){
 $id=[int64]$D.UserID
 return (TaskSQL @"
SELECT jsonb_build_object('user_id',u.id,'legacy_concurrency',u.concurrency,'claude_override',u.claude_subscription_concurrency,'database_time',clock_timestamp(),
 'terms',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',t.id,'term_no',t.term_no,'quota_usd',t.quota_usd::text,'used_usd',t.used_usd::text,'starts_at',t.starts_at,'expires_at',t.expires_at,'duration_seconds',extract(epoch FROM t.expires_at-t.starts_at),'lifecycle_state',t.lifecycle_state,'cancelled_at',t.cancelled_at) ORDER BY t.term_no),'[]') FROM subscription_quota_terms t WHERE t.user_id=u.id),
 'term_immutable_hash',(SELECT md5(COALESCE(jsonb_agg(to_jsonb(t)-'lifecycle_state'-'closed_at'-'updated_at'-'revision' ORDER BY t.id)::text,'[]')) FROM subscription_quota_terms t WHERE t.user_id=u.id),
 'request_count',(SELECT count(*) FROM subscription_quota_requests WHERE user_id=u.id),
 'ledger_count',(SELECT count(*) FROM subscription_quota_ledger l JOIN subscription_quota_terms t ON t.id=l.term_id WHERE t.user_id=u.id))::text
FROM users u WHERE u.id=$id;
"@|ConvertFrom-Json)
}
$result=[ordered]@{stage='I4 T48 browser countdown fixture';action=$Action;image_id=$public.image_id;source_sha256=$public.source_sha256;environment=[IO.Path]::GetFileName($publicPath);private_fixture_path=$privatePath;status='pending';production_connected=$false;external_upstream_called=$false;browser_controlled=$false;generation_API_called=$false;raw_credentials_logged=$false;recorded_utc=[DateTime]::UtcNow.ToString('o')}
try{
 if($Action -eq 'Prepare'){
  $before=ForeignFacts
  if(Test-Path -LiteralPath $privatePath){$d=Get-Content -LiteralPath $privatePath -Raw|ConvertFrom-Json -AsHashtable}else{
   $login=Invoke-RestMethod -Method Post -Uri ($fixture.URL+'/api/v1/auth/login') -ContentType 'application/json' -Body (@{email=$fixture.AdminEmail;password=$fixture.Password}|ConvertTo-Json -Compress) -TimeoutSec 8
   $token=$login.data.access_token;if(-not $token){throw 'Synthetic admin login did not return a token'}
   $email='i4-ui-d-'+[guid]::NewGuid().ToString('N')+'@example.invalid'
   $body=@{email=$email;password=$fixture.Password;username='I4 synthetic T48 expiry D';role='user';balance=0;concurrency=5;allowed_groups=@(2);restrict_public_groups=$true}|ConvertTo-Json -Compress
   $created=Invoke-RestMethod -Method Post -Uri ($fixture.URL+'/api/v1/admin/users') -Headers @{Authorization=('Bearer '+$token)} -ContentType 'application/json' -Body $body -TimeoutSec 8
   $d=@{UserID=[int64]$created.data.id;Email=$email;Password=$fixture.Password;GroupID=2;URL='http://127.0.0.1:54551';DirectURL=$fixture.URL;Started=$false;CurrentQuota='13.00000000';NextQuota='17.00000000';DurationHours=168}
   if($d.UserID -le 4){throw 'Expected a new independent D identity'}
   SavePrivate $d
  }
  $facts=ObserveD $d
  if($facts.legacy_concurrency -ne 5 -or $null -ne $facts.claude_override -or @($facts.terms).Count -ne 0){throw 'Prepare requires independent default3/GPT5 D with no issued cards; countdown is not reset'}
  $after=ForeignFacts;if($after -cne $before){throw 'A/B/C or GPT fingerprint changed during preparation; preserve evidence and investigate concurrent UI activity'}
  $result.foreign_before=$before|ConvertFrom-Json;$result.foreign_after=$after|ConvertFrom-Json;$result.foreign_exactly_unchanged=$true;$result.countdown_started=$false;$result.facts=$facts
 }else{
  if(-not (Test-Path -LiteralPath $privatePath)){throw 'Prepare D first'}
  $d=Get-Content -LiteralPath $privatePath -Raw|ConvertFrom-Json -AsHashtable
  if($d.GroupID -ne 2 -or $d.UserID -le 4){throw 'Unexpected independent D identity'}
  if($Action -eq 'Start'){
   $facts=ObserveD $d
   if(@($facts.terms).Count -eq 0){
    $before=ForeignFacts;$id=[int64]$d.UserID;$admin=[int64]$fixture.Admin
    $raw=TaskSQL @"
BEGIN;
SET LOCAL lock_timeout='3s';
SET LOCAL statement_timeout='8s';
DO `$cwq`$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM users WHERE id=$id AND email LIKE 'i4-ui-d-%@example.invalid' AND deleted_at IS NULL) THEN RAISE EXCEPTION 'not independent synthetic D'; END IF;
 PERFORM 1 FROM users WHERE id=$id FOR UPDATE;
 PERFORM 1 FROM groups WHERE id=2 AND platform='anthropic' AND subscription_quota_mode='term' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'not synthetic term group2'; END IF;
 IF EXISTS(SELECT 1 FROM user_subscriptions WHERE user_id=$id) THEN RAISE EXCEPTION 'D already has history; never extend or reset it'; END IF;
END `$cwq`$;
CREATE TEMP TABLE cwq_browser_d_clock ON COMMIT DROP AS SELECT clock_timestamp()+make_interval(secs=>$Seconds) AS boundary;
CREATE TEMP TABLE cwq_browser_d_parent ON COMMIT DROP AS
 WITH p AS (INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by)
 SELECT $id,2,boundary-INTERVAL '168 hours',boundary+INTERVAL '168 hours','active',2,$admin FROM cwq_browser_d_clock RETURNING id) SELECT id FROM p;
INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot,created_by)
 SELECT p.id,$id,2,1,13,boundary-INTERVAL '168 hours',boundary,'active','admin','{"schema_version":1,"quota_usd":"13.00000000","duration_hours":168,"quota_source":"admin"}'::jsonb,$admin FROM cwq_browser_d_parent p CROSS JOIN cwq_browser_d_clock
 UNION ALL
 SELECT p.id,$id,2,2,17,boundary,boundary+INTERVAL '168 hours','scheduled','admin','{"schema_version":1,"quota_usd":"17.00000000","duration_hours":168,"quota_source":"admin"}'::jsonb,$admin FROM cwq_browser_d_parent p CROSS JOIN cwq_browser_d_clock;
COMMIT;
"@
    $after=ForeignFacts;if($before -cne $after){throw 'A/B/C or GPT changed during the independent D append; preserve facts, never reset cards'}
    $result.foreign_before=$before|ConvertFrom-Json;$result.foreign_after=$after|ConvertFrom-Json;$result.foreign_exactly_unchanged=$true
    $facts=ObserveD $d;$d.Started=$true;$d.StartedUTC=[DateTime]::UtcNow.ToString('o');$d.CurrentTermID=$facts.terms[0].id;$d.NextTermID=$facts.terms[1].id;$d.CurrentExpires=$facts.terms[0].expires_at;SavePrivate $d
    $result.synthetic_history_seed=$true;$result.existing_terms_updated=$false;$result.configured_seconds=$Seconds
   }else{$result.already_started=$true;$result.countdown_reset=$false}
   $facts=ObserveD $d
   if(@($facts.terms).Count -ne 2 -or @($facts.terms|Where-Object {$_.duration_seconds -ne 604800}).Count -or $facts.terms[0].quota_usd -cne '13.00000000' -or $facts.terms[1].quota_usd -cne '17.00000000' -or $facts.terms[0].expires_at -cne $facts.terms[1].starts_at){throw 'D must have two distinct adjacent full168h cards'}
   $result.countdown_started=$true;$result.facts=$facts
  }elseif($Action -eq 'Observe'){
   $result.facts=ObserveD $d;$result.countdown_started=@($result.facts.terms).Count -gt 0
   $result.foreign_observed=ForeignFacts|ConvertFrom-Json
   $providerRaw=@(& $docker --host npipe:////./pipe/docker_engine exec $fixture.Provider wget -q -O - http://127.0.0.1:8081/__state)
   if($LASTEXITCODE -ne 0){throw 'Synthetic provider read-only observation failed'}
   $provider=($providerRaw -join "`n")|ConvertFrom-Json
   $result.provider_call_count=@($provider.calls).Count
   $result.provider_active_by_model=$provider.active_by_model
  }
  else{
   $control=@{action=if($Action -eq 'ArmRefresh'){'arm'}else{'release'};rule_id=$RuleID}
   if($Action -eq 'ArmRefresh'){$control.path='/api/v1/subscription-quota';$control.method='GET';$control.phase='after_backend_response';$control.effect='hold'}
   $result.proxy_control=Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:54551/__cwq/control' -ContentType 'application/json' -Body ($control|ConvertTo-Json -Compress) -TimeoutSec 5
   $result.control=$control;$result.business_request_sent=$false
  }
 }
 $result.status='pass'
}catch{$result.status='failed';$result.error=$_.Exception.Message}
$result|ConvertTo-Json -Depth 14|Set-Content -LiteralPath $receiptPath -Encoding utf8
[pscustomobject]@{receipt=$receiptPath;status=$result.status;private_fixture_path=$privatePath;countdown_started=$result.Contains('countdown_started') -and $result.countdown_started}|ConvertTo-Json
if($result.status -cne 'pass'){throw ('T48 fixture action failed; see '+$receiptPath)}

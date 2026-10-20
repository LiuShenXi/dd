param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json -AsHashtable
if($public.synthetic_only -ne $true -or $e.URL -notmatch '^http://127\.0\.0\.1:\d+$'){throw 'Only the independent synthetic browser fixture is supported'}
$loginBody=@{email=$e.AdminEmail;password=$e.Password}|ConvertTo-Json -Compress
$login=Invoke-RestMethod -Method Post -Uri ($e.URL+'/api/v1/auth/login') -ContentType 'application/json' -Body $loginBody -TimeoutSec 8
$token=$login.data.access_token
if(-not $token){throw 'Synthetic admin login failed'}
$headers=@{Authorization=('Bearer '+$token)}
$compliance=Invoke-RestMethod -Uri ($e.URL+'/api/v1/admin/compliance') -Headers $headers -TimeoutSec 8
if($compliance.data.required){
 # Test-owned fictional admin in an isolated synthetic instance only. Use the
 # original API rather than bypassing its middleware or modifying settings SQL.
 $ackBody=@{phrase=$compliance.data.ack_phrase_en;language='en'}|ConvertTo-Json -Compress
 $ack=Invoke-RestMethod -Method Post -Uri ($e.URL+'/api/v1/admin/compliance/accept') -Headers $headers -ContentType 'application/json' -Body $ackBody -TimeoutSec 8
 if($ack.data.required){throw 'Synthetic local admin prerequisite acknowledgement failed'}
}
if(-not $e.ContainsKey('C')){
 $email='i4-ui-c-'+[guid]::NewGuid().ToString('N')+'@example.invalid'
 $body=@{email=$email;password=$e.Password;username='I4 synthetic UI real mutation C';role='user';balance=0;concurrency=5;allowed_groups=@($e.Group);restrict_public_groups=$true}|ConvertTo-Json -Compress
 $created=Invoke-RestMethod -Method Post -Uri ($e.URL+'/api/v1/admin/users') -Headers $headers -ContentType 'application/json' -Body $body -TimeoutSec 8
 $e.C=[int64]$created.data.id;$e.CEmail=$email;$e.CGroup=[int64]$e.Group;$e.CConcurrencyOriginal=5
 if($e.C -le 0){throw 'Actual admin CreateUser did not return an ID'}
}
$read=Invoke-RestMethod -Uri ($e.URL+'/api/v1/admin/users/'+$e.C) -Headers $headers -TimeoutSec 8
$user=$read.data
if($user.concurrency -ne 5 -or $null -ne $user.claude_subscription_concurrency -or $user.effective_claude_subscription_concurrency -ne 3){throw 'C must begin with legacy5, null Claude override and effective default3'}
$d='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$facts=(& $d --host npipe:////./pipe/docker_engine exec $e.Postgres psql -U postgres -d $e.Database -X -A -t -v ON_ERROR_STOP=1 -c ("SELECT count(*) FROM subscription_quota_terms WHERE user_id="+$e.C+';')).Trim()
if($facts -ne '0'){throw 'Browser C must have no issued cards before the real UI mutation'}
$e|ConvertTo-Json -Depth 12|Set-Content -LiteralPath $public.private_fixture_path -Encoding utf8
$result=[ordered]@{stage='I4 independent browser C actual admin API seed';synthetic_only=$true;production_connected=$false;environment=[IO.Path]::GetFileName($EnvironmentReceipt);candidate_image_id=$public.image_id;candidate_source_sha256=$public.source_sha256;user_id=$e.C;email=$e.CEmail;group_id=$e.CGroup;legacy_concurrency=5;claude_override=$null;claude_default_effective=3;issued_cards=0;credential_location='existing private fixture only';actual_create_and_read_admin_API=$true;created_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ('environment-I4-browser-C-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
$result|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;C=$e.C;CEmail=$e.CEmail;CGroup=$e.CGroup;private_fixture=$public.private_fixture_path}|ConvertTo-Json

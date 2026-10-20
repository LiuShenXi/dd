param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
function ReadKey {
 $raw=& $docker --host npipe:////./pipe/docker_engine exec $e.Postgres psql -U postgres -d $e.Database -X -A -t -v ON_ERROR_STOP=1 -c "SELECT to_jsonb(k)::text FROM api_keys k WHERE id=$($e.AKeyID);"
 if($LASTEXITCODE -ne 0){throw 'Task key read failed'}
 return (($raw -join "`n").Trim())
}
$before=ReadKey
$response=Invoke-WebRequest -Uri ($e.URL+'/v1/usage') -Headers @{Authorization=('Bearer '+$e.AKey)} -TimeoutSec 8
if($response.StatusCode -ne 200){throw 'Synthetic metadata authentication failed'}
$deadline=[DateTime]::UtcNow.AddSeconds(8)
do{$after=ReadKey;if($after -ne $before){break};Start-Sleep -Milliseconds 100}while([DateTime]::UtcNow -lt $deadline)
if($before -eq $after){throw 'No key metadata change observed; key may still be inside its debounce window'}
$privateRoot=Split-Path -Parent $public.private_fixture_path
$before|Set-Content -LiteralPath (Join-Path $privateRoot "metadata-auth-$run-before.json") -Encoding utf8
$after|Set-Content -LiteralPath (Join-Path $privateRoot "metadata-auth-$run-after.json") -Encoding utf8
$b=$before|ConvertFrom-Json -AsHashtable;$a=$after|ConvertFrom-Json -AsHashtable
$changed=@($b.Keys|Where-Object {($b[$_]|ConvertTo-Json -Depth 30 -Compress) -cne ($a[$_]|ConvertTo-Json -Depth 30 -Compress)}|Sort-Object)
$onlyMetadata=$changed.Count -eq 2 -and @($changed|Where-Object {$_ -notin @('last_used_at','updated_at')}).Count -eq 0
$result=[ordered]@{synthetic_only=$true;environment=[IO.Path]::GetFileName($EnvironmentReceipt);key_id=$e.AKeyID;http_status=$response.StatusCode;changed_fields=$changed;only_auth_last_use_metadata=$onlyMetadata;all_quota_rate_window_limits_identity_and_other_key_columns_exactly_unchanged=$onlyMetadata;private_snapshot_directory=$privateRoot;original_failure='I4-v2-MetadataAndUnsupportedRoutes-F2-20261019T180616Z-receipt.json';conclusion='Authenticated metadata can update Key last-use timestamps; this is not a generation or monetary charge. The subsequent full-row guard runs after this legitimate authentication update.';observed_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot "I4-metadata-auth-side-effect-$run.json"
$result|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $path -Encoding utf8
if(-not $onlyMetadata){throw 'Unexpected non-metadata key change; inspect private snapshots'}
[pscustomobject]@{receipt=$path;changed_fields=$changed;all_other_columns_unchanged=$onlyMetadata}|ConvertTo-Json

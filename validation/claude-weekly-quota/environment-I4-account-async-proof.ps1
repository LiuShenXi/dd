param([Parameter(Mandatory)][string]$EnvironmentReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$fixture=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$privateRoot=Split-Path -Parent $public.private_fixture_path
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
function ReadAccount {
    $raw=& $docker --host npipe:////./pipe/docker_engine exec $fixture.Postgres psql -U postgres -d $fixture.Database -X -A -t -v ON_ERROR_STOP=1 -c "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$($fixture.GPTAccount);"
    if($LASTEXITCODE -ne 0){throw 'Task PG read failed'}
    return (($raw -join "`n").Trim())
}
$before=ReadAccount
$before|Set-Content -LiteralPath (Join-Path $privateRoot "async-account-$run-before.json") -Encoding utf8
$requestStart=[DateTime]::UtcNow
$response=Invoke-WebRequest -Uri ($fixture.URL+'/v1/responses') -Method Post -Headers @{Authorization=('Bearer '+$fixture.GPTKey);'X-Request-ID'='synthetic-async-account-proof'} -ContentType 'application/json' -Body '{"model":"gpt-5.2","max_output_tokens":64,"stream":false,"input":"isolated asynchronous last-use proof"}' -TimeoutSec 20
if($response.StatusCode -ne 200){throw 'Synthetic GPT response failed'}
$after=''
$deadline=[DateTime]::UtcNow.AddSeconds(20)
do {
    $after=ReadAccount
    if($after -ne $before){break}
    Start-Sleep -Milliseconds 200
}while([DateTime]::UtcNow -lt $deadline)
if($after -eq $before){throw 'No asynchronous account change observed'}
$after|Set-Content -LiteralPath (Join-Path $privateRoot "async-account-$run-after.json") -Encoding utf8
$b=$before|ConvertFrom-Json -AsHashtable
$a=$after|ConvertFrom-Json -AsHashtable
$changes=@()
foreach($key in $b.Keys){
    if(($b[$key]|ConvertTo-Json -Depth 30 -Compress) -cne ($a[$key]|ConvertTo-Json -Depth 30 -Compress)){
        if($key -in @('last_used_at','updated_at')){$changes+=@{field=$key;before=$b[$key];after=$a[$key]}}
        else{$changes+=@{field=$key;values='private snapshots only'}}
    }
}
$proof=[ordered]@{stage='I4 foreign-account failure diagnosis';synthetic_only=$true;production_connected=$false;environment=[IO.Path]::GetFileName($EnvironmentReceipt);account_id=$fixture.GPTAccount;request_start_utc=$requestStart.ToString('o');observed_utc=[DateTime]::UtcNow.ToString('o');http_status=$response.StatusCode;changed_fields=$changes;all_other_columns_exactly_unchanged=($changes.Count -eq 2 -and @($changes.field|Where-Object {$_ -notin @('last_used_at','updated_at')}).Count -eq 0);private_snapshot_directory=$privateRoot;source_reference='DeferredService flushes original GPT last-use every 10s; BatchUpdateLastUsed changes last_used_at and updated_at only';original_failure_preserved='I4-pg-F1-20261019T175235Z-receipt.json'}
$path=Join-Path $PSScriptRoot "I4-foreign-account-async-$run.json"
$proof|ConvertTo-Json -Depth 8|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;changed_fields=$changes;all_other_columns_exactly_unchanged=$proof.all_other_columns_exactly_unchanged}|ConvertTo-Json -Depth 5

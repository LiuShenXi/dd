param([Parameter(Mandatory)][string]$Freeze, [int]$Expected=61)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw | ConvertFrom-Json
$runner=($resources.containers | Where-Object role -eq 'runner').id
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I2-domain-race-$Freeze-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw | ConvertFrom-Json
& $docker --host npipe:////./pipe/docker_engine exec $runner mkdir -p /output/tmp
if($LASTEXITCODE -ne 0){throw 'Could not prepare task-only disk-backed build directory.'}
$arguments=@('--host','npipe:////./pipe/docker_engine','exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','go','test','-p=2','-tags=unit','-race','-json','-count=1','-timeout=10m','-run','^TestSubscriptionQuota|^TestGetUserGroupVisibility|^TestAdminService_AdminUpdateAPIKeyGroupID|^TestAPIKey(Create|Update)','./internal/domain','./internal/service','./internal/handler','./internal/handler/dto','./internal/repository','./internal/server/routes')
$started=[DateTime]::UtcNow
& $docker @arguments *> $log
$code=$LASTEXITCODE
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log | ForEach-Object {try {$_ | ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notlike '*/*'})
$skips=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'skip'})
$failures=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'fail'})
$stable=$before.source_sha256 -ceq $after.source_sha256
$result=[ordered]@{
 freeze=$Freeze;started_utc=$started.ToString('o');finished_utc=[DateTime]::UtcNow.ToString('o')
 executable=$docker;arguments=$arguments;head=$before.head;runner_id=$runner
 source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable
 source_before_manifest=[IO.Path]::GetFileName($beforePath);source_after_manifest=[IO.Path]::GetFileName($afterPath)
 exit_code=$code;expected_top_level_tests=$Expected;top_level_passes=@($passes | ForEach-Object {$_.Package+'::'+$_.Test})
 skip_count=$skips.Count;failure_count=$failures.Count
 acceptance=($Freeze -ne 'debug' -and $code -eq 0 -and $passes.Count -eq $Expected -and $skips.Count -eq 0 -and $failures.Count -eq 0 -and $stable)
 synthetic_only=$true;production_connected=$false;upstream_called=$false;log=[IO.Path]::GetFileName($log)
}
$result | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $PSScriptRoot ($prefix+'-receipt.json')) -Encoding utf8
$result | ConvertTo-Json -Depth 6
if(-not $result.acceptance){Get-Content -LiteralPath $log -Tail 35;exit 1}

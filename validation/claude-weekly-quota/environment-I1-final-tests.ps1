param([Parameter(Mandatory)][ValidateSet('pg','race')][string]$Phase)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$active=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-I1-final-active.json') -Raw | ConvertFrom-Json
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw | ConvertFrom-Json
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$prefix="environment-I1-$($active.freeze)-$($active.run_id)-$Phase"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$receipt=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$started=[DateTime]::UtcNow
if($Phase -eq 'pg') {
    $env:PATH='C:/Program Files/Docker/Docker/resources/bin;C:/Program Files/Git/usr/bin;'+$env:PATH
    $env:DOCKER_HOST='npipe:////./pipe/docker_engine'
    Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
    $env:CI='true'; $env:GOTOOLCHAIN='local'; $env:GOMAXPROCS='4'
    $env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
    $pattern='^(TestSubscriptionQuotaMigrations_.*|TestSubscriptionQuotaProtected.*|TestSubscriptionQuotaClaudeKeysSurviveGPTOpenAndRenew|TestSubscriptionQuotaLegacyRepositorySinksCannotMutateTermParent|TestCarpoolBillingBindingPreservesGroupAndAdminThroughAuth|TestCarpoolStandardGroupOpenRenewAndExistingKeyAuth|TestCarpoolStandardOpeningRejectsConflictsWithoutMutation)$'
    $executable=$go
    $arguments=@('test','-tags=integration','-json','-count=1','-timeout=10m','-run',$pattern,'./internal/repository')
    $expected=18
} else {
    $runner=($resources.containers | Where-Object role -eq 'runner').id
    $executable=$docker
    & $docker --host npipe:////./pipe/docker_engine exec $runner mkdir -p /output/tmp
    if($LASTEXITCODE -ne 0){throw 'Could not prepare the task-only disk-backed temporary directory.'}
    $arguments=@('--host','npipe:////./pipe/docker_engine','exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','go','test','-p=2','-tags=unit','-race','-json','-count=1','-timeout=10m','-run','^TestSubscriptionQuota','./internal/service','./internal/handler','./internal/repository','./internal/server/routes')
    $expected=15
}
Push-Location (Join-Path $repoRoot 'backend')
try { & $executable @arguments *> $log; $code=$LASTEXITCODE } finally { Pop-Location }
$events=@(Get-Content -LiteralPath $log | ForEach-Object {try {$_ | ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notlike '*/*'})
$skips=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'skip'})
$failures=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'fail'})
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
$stable=$active.source_sha256 -ceq $after.source_sha256
$result=[ordered]@{
 freeze=$active.freeze;phase=$Phase;started_utc=$started.ToString('o');finished_utc=[DateTime]::UtcNow.ToString('o')
 executable=$executable;arguments=$arguments;source_sha256=$active.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable
 source_manifest=[IO.Path]::GetFileName($active.source_manifest);source_after_manifest=[IO.Path]::GetFileName($afterPath)
 frontend_dist_sha256=$active.frontend_dist_sha256;exit_code=$code;expected_top_level_tests=$expected
 top_level_passes=@($passes | ForEach-Object {$_.Package+'::'+$_.Test});skip_count=$skips.Count;failure_count=$failures.Count
 synthetic_only=$true;production_connected=$false;log=[IO.Path]::GetFileName($log)
 acceptance=($code -eq 0 -and $passes.Count -eq $expected -and $skips.Count -eq 0 -and $failures.Count -eq 0 -and $stable)
}
$result | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $receipt -Encoding utf8
$result | ConvertTo-Json -Depth 6
if(-not $result.acceptance){Get-Content -LiteralPath $log -Tail 20;exit 1}

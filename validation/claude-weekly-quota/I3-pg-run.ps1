param([string]$Freeze='debug', [string]$Pattern='^TestSubscriptionQuotaBilling_', [int]$Expected=37)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I3-pg-$Freeze-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$receipt=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw | ConvertFrom-Json
$env:PATH='C:/Program Files/Docker/Docker/resources/bin;C:/Program Files/Git/usr/bin;'+$env:PATH
$env:DOCKER_HOST='npipe:////./pipe/docker_engine'
Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI='true';$env:GOTOOLCHAIN='local';$env:GOMAXPROCS='4'
$env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
$arguments=@('test','-tags=integration','-json','-count=1','-timeout=10m','-run',$Pattern,'./internal/repository')
$started=[DateTime]::UtcNow
Push-Location (Join-Path $repoRoot 'backend')
try {& $go @arguments *> $log; $code=$LASTEXITCODE} finally {Pop-Location}
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log | ForEach-Object {try {$_ | ConvertFrom-Json -ErrorAction Stop} catch {}})
$passes=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notlike '*/*'})
$skips=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'skip'})
$failures=@($events | Where-Object {$_.PSObject.Properties['Action'] -and $_.Action -eq 'fail'})
$stable=$before.source_sha256 -ceq $after.source_sha256
$functional=($code -eq 0 -and $passes.Count -eq $Expected -and $skips.Count -eq 0 -and $failures.Count -eq 0)
$result=[ordered]@{
 freeze=$Freeze;started_utc=$started.ToString('o');finished_utc=[DateTime]::UtcNow.ToString('o')
 executable=$go;arguments=$arguments;head=$before.head;source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable
 source_before_manifest=[IO.Path]::GetFileName($beforePath);source_after_manifest=[IO.Path]::GetFileName($afterPath)
 exit_code=$code;expected_top_level_tests=$Expected;top_level_passes=@($passes | ForEach-Object {$_.Package+'::'+$_.Test})
 skip_count=$skips.Count;failure_count=$failures.Count;functional_pass=$functional
 acceptance=($Freeze -ne 'debug' -and $functional -and $stable)
 synthetic_only=$true;original_testmain=$true;postgres_image='postgres:18.1-alpine3.23';production_connected=$false;upstream_called=$false
 log=[IO.Path]::GetFileName($log)
}
$result | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $receipt -Encoding utf8
$result | ConvertTo-Json -Depth 6
if(-not $functional){Get-Content -LiteralPath $log -Tail 35;exit 1}


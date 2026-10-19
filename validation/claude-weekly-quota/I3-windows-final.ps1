param([Parameter(Mandatory)][string]$Freeze,[Parameter(Mandatory)][string]$ExpectedSource)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$env:PATH='C:/Program Files/Git/usr/bin;'+$env:PATH
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I3-windows-$Freeze-$run"
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content $beforePath -Raw | ConvertFrom-Json
if($before.source_sha256 -cne $ExpectedSource){throw 'Source changed before the final Windows run.'}
$pattern='^TestSubscriptionQuota|^TestGetUserGroupVisibility|^TestAdminService_AdminUpdateAPIKeyGroupID|^TestAPIKey(Create|Update)|^Test(PeakMultiplierAt|CalculateCostUnified|Resolve_|GetIntervalPricing_|GetRequestTierPrice|TransformBedrockInvocationMetrics)|Carpool|UsageBilling|SubscriptionAppliesRateMultiplier|APIKeyAuthSnapshotPreservesPerUserBillingIsolation|DeleteGroup|Group.*Delete|Delete.*Group|UsageCleanup|DashboardAggregation|TestProvideCleanup'
$packages=@('./internal/domain','./internal/service','./internal/repository','./internal/handler/...','./internal/server/...','./cmd/server')
$commands=@()
$started=[DateTime]::UtcNow
Push-Location (Join-Path $repoRoot 'backend')
try {
 $listLog=Join-Path $PSScriptRoot ($prefix+'-list.log')
 $arguments=@('test','-tags=unit','-list',$pattern)+$packages
 & $go @arguments *> $listLog
 if($LASTEXITCODE -ne 0){throw 'Final Windows inventory failed.'}
 $inventory=@(Get-Content $listLog | Where-Object {$_ -cmatch '^Test[A-Za-z0-9_]+$'})
 if($inventory.Count -eq 0){throw 'No functional tests selected.'}
 $unitLog=Join-Path $PSScriptRoot ($prefix+'-unit.jsonl')
 $arguments=@('test','-tags=unit','-json','-count=1','-timeout=10m','-run',$pattern)+$packages
 & $go @arguments *> $unitLog
 $commands+=@{arguments=$arguments;exit_code=$LASTEXITCODE;log=[IO.Path]::GetFileName($unitLog)}
 $compileLog=Join-Path $PSScriptRoot ($prefix+'-compile.log')
 $arguments=@('test','-tags=unit','-run','^$','./...')
 & $go @arguments *> $compileLog
 $commands+=@{arguments=$arguments;exit_code=$LASTEXITCODE;log=[IO.Path]::GetFileName($compileLog)}
 $vetLog=Join-Path $PSScriptRoot ($prefix+'-vet.log')
 $arguments=@('vet','-tags=unit')+$packages
 & $go @arguments *> $vetLog
 $commands+=@{arguments=$arguments;exit_code=$LASTEXITCODE;log=[IO.Path]::GetFileName($vetLog)}
} finally {Pop-Location}
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content $afterPath -Raw | ConvertFrom-Json
$events=@(Get-Content $unitLog | ForEach-Object {try{$_|ConvertFrom-Json -ErrorAction Stop}catch{}})
$passes=@($events|Where-Object{$_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notmatch '/'})
$individualSkips=@($events|Where-Object{$_.Action -eq 'skip' -and $_.PSObject.Properties['Test']})
$packageSkips=@($events|Where-Object{$_.Action -eq 'skip' -and -not $_.PSObject.Properties['Test']})
$failures=@($events|Where-Object{$_.Action -eq 'fail'})
$baseline=@(Get-Content (Join-Path $PSScriptRoot 'I0/gpt-unit-baseline.jsonl') | ForEach-Object{try{$_|ConvertFrom-Json -ErrorAction Stop}catch{}} | Where-Object{$_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notmatch '/'} | ForEach-Object{$_.Package+'::'+$_.Test})
$passedNames=@($passes|ForEach-Object{$_.Package+'::'+$_.Test})
$missingBaseline=@($baseline|Where-Object{$_ -cnotin $passedNames})
$stable=$before.source_sha256 -ceq $after.source_sha256
$passed=($commands.Where({$_.exit_code -ne 0}).Count -eq 0 -and $passes.Count -eq $inventory.Count -and $individualSkips.Count -eq 0 -and $failures.Count -eq 0 -and $baseline.Count -eq 84 -and $missingBaseline.Count -eq 0 -and $stable)
$receipt=[ordered]@{
 freeze=$Freeze;started_utc=$started.ToString('o');finished_utc=[DateTime]::UtcNow.ToString('o');executable=$go;commands=$commands
 source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;source_stable=$stable;head=$before.head
 source_before_manifest=[IO.Path]::GetFileName($beforePath);source_after_manifest=[IO.Path]::GetFileName($afterPath)
 inventory_log=[IO.Path]::GetFileName($listLog);expected_top_level_tests=$inventory.Count;top_level_pass_count=$passes.Count
 individual_test_skip_count=$individualSkips.Count;package_skip_count=$packageSkips.Count;failure_count=$failures.Count
 baseline_gpt_test_count=$baseline.Count;missing_baseline_gpt_tests=$missingBaseline;top_level_passes=$passedNames
 pass=$passed;synthetic_only=$true;production_connected=$false;upstream_called=$false
}
$receiptPath=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$receipt|ConvertTo-Json -Depth 7|Set-Content -LiteralPath $receiptPath -Encoding utf8
[pscustomobject]@{receipt=[IO.Path]::GetFileName($receiptPath);pass=$passed;source_stable=$stable;top_passes=$passes.Count;expected=$inventory.Count;individual_skips=$individualSkips.Count;package_skips=$packageSkips.Count;failures=$failures.Count;missing_gpt=$missingBaseline.Count}|ConvertTo-Json -Compress
if(-not $passed){exit 1}

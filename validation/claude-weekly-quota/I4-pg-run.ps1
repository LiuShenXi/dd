param([Parameter(Mandatory)][string]$BuildReceipt,[switch]$MatrixOnly,[string]$EnvironmentReceipt='',[switch]$PrepareOnly)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$build=Get-Content -LiteralPath $BuildReceipt -Raw|ConvertFrom-Json
if($build.status -notin @('diagnostic_built','pass')){throw 'An actually built I4 candidate and provider are required.'}
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="I4-pg-$($build.freeze)-$run"
$log=Join-Path $PSScriptRoot ($prefix+'.jsonl')
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
$publicPath=Join-Path $PSScriptRoot ("environment-I4-$run-active.json")
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw|ConvertFrom-Json
if($MatrixOnly){
    $existing=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
    if($existing.source_sha256 -ne $build.source_sha256 -or $existing.image_id -ne $build.image_id){throw 'The running persistent environment must match the selected build.'}
    $privateRoot=Split-Path -Parent $existing.private_fixture_path
    $publicPath=[IO.Path]::GetFullPath($EnvironmentReceipt)
    $pattern='^TestSubscriptionQuotaI4HTTPWalletMatrix$';$expected=1
}else{
    $privateRoot=Join-Path $env:LOCALAPPDATA ('Codex/PrivateTests/claude-weekly-quota/I4-'+$run)
    $null=New-Item -ItemType Directory -Path $privateRoot
    if($PrepareOnly){$pattern='^TestSubscriptionQuotaI4PrepareEnvironment$';$expected=1}else{$pattern='^TestSubscriptionQuotaI4(PrepareEnvironment|HTTPWalletMatrix)$';$expected=2}
}
$env:CWQ_I4_PRIVATE_DIR=$privateRoot
$env:CWQ_I4_PUBLIC_MANIFEST=$publicPath
$env:CWQ_I4_IMAGE=$build.image_tag
$env:CWQ_I4_SOURCE=$build.source_sha256
$env:CWQ_I4_PROVIDER_IMAGE=$build.provider_tag
$env:CWQ_I4_NETWORK=$resources.network.name
$env:CWQ_I4_PWSH=Join-Path $PSHOME 'pwsh.exe'
$env:CWQ_I4_BRIDGE_SCRIPT=Join-Path $PSScriptRoot 'environment-I4-start-bridge.ps1'
$env:PATH='C:/Program Files/Docker/Docker/resources/bin;C:/Program Files/Git/usr/bin;'+$env:PATH
$env:DOCKER_HOST='npipe:////./pipe/docker_engine';Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI='true';$env:GOTOOLCHAIN='local';$env:GOMAXPROCS='4';$env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
$go='C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$arguments=@('test','-tags=integration','-json','-count=1','-timeout=10m','-run',$pattern,'./internal/repository')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$before=Get-Content -LiteralPath $beforePath -Raw|ConvertFrom-Json
try{
    Push-Location (Join-Path $repoRoot 'backend')
    try{& $go @arguments *> $log;$code=$LASTEXITCODE}finally{Pop-Location}
}finally{
    foreach($name in @('CWQ_I4_PRIVATE_DIR','CWQ_I4_PUBLIC_MANIFEST','CWQ_I4_IMAGE','CWQ_I4_SOURCE','CWQ_I4_PROVIDER_IMAGE','CWQ_I4_NETWORK','CWQ_I4_PWSH','CWQ_I4_BRIDGE_SCRIPT')){Remove-Item "Env:$name" -ErrorAction SilentlyContinue}
}
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
$after=Get-Content -LiteralPath $afterPath -Raw|ConvertFrom-Json
$events=@(Get-Content -LiteralPath $log|ForEach-Object {try{$_|ConvertFrom-Json -ErrorAction Stop}catch{}})
$passes=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass' -and $_.Test -notlike '*/*'})
$subpasses=@($events|Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass'})
$fails=@($events|Where-Object {$_.Action -eq 'fail'})
$skips=@($events|Where-Object {$_.Action -eq 'skip'})
$functional=$code -eq 0 -and $passes.Count -eq $expected -and $fails.Count -eq 0 -and $skips.Count -eq 0
$stable=$before.source_sha256 -ceq $after.source_sha256 -and $before.source_sha256 -ceq $build.source_sha256
$result=[ordered]@{stage='I4 actual candidate HTTP and PG';freeze=$build.freeze;diagnostic=$build.diagnostic;executable=$go;arguments=$arguments;exit_code=$code;source_sha256=$before.source_sha256;source_after_sha256=$after.source_sha256;image_source_sha256=$build.source_sha256;source_stable=$stable;expected_top_level_tests=$expected;top_level_passes=@($passes|ForEach-Object {$_.Test});pass_count_including_subtests=$subpasses.Count;failure_count=$fails.Count;skip_count=$skips.Count;functional_pass=$functional;acceptance=(-not $build.diagnostic -and $functional -and $stable);build_receipt=[IO.Path]::GetFileName($BuildReceipt);log=[IO.Path]::GetFileName($log);persistent_environment=$publicPath;original_TestMain_seed_database=$true;new_PG18_persistent_clone=$true;application_loopback_only=$true;provider_external_egress=$false;synthetic_only=$true;production_connected=$false;external_upstream_called=$false;synthetic_provider_called=$true;finished_utc=[DateTime]::UtcNow.ToString('o')}
if(Test-Path -LiteralPath $publicPath){Copy-Item -LiteralPath $publicPath -Destination (Join-Path $PSScriptRoot 'environment-I4-active.json') -Force}
$receipt=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$result|ConvertTo-Json -Depth 6|Set-Content -LiteralPath $receipt -Encoding utf8
[pscustomobject]@{receipt=$receipt;functional_pass=$functional;acceptance=$result.acceptance;source_stable=$stable;passed=$passes.Count;failed=$fails.Count;skipped=$skips.Count;environment=$publicPath}|ConvertTo-Json
if(-not $functional){Get-Content -LiteralPath $log -Tail 25;exit 1}

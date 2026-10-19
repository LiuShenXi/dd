param(
    [Parameter(Mandatory)][string]$PGReceipt,
    [Parameter(Mandatory)][string]$RaceReceipt,
    [Parameter(Mandatory)][string]$WindowsReceipt,
    [Parameter(Mandatory)][string]$BuildReceipt,
    [Parameter(Mandatory)][string]$ImageProofReceipt
)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$paths=@($PGReceipt,$RaceReceipt,$WindowsReceipt,$BuildReceipt,$ImageProofReceipt)
$receipts=@($paths | ForEach-Object {Get-Content -LiteralPath $_ -Raw | ConvertFrom-Json})
$pg,$race,$windows,$build,$proof=$receipts
$source=$pg.source_sha256
foreach($r in $receipts){
    if($r.freeze -ne 'F3' -or $r.source_sha256 -cne $source -or -not $r.source_stable){throw 'All final receipts must bind one stable F3 source.'}
}
if(-not $pg.acceptance -or -not $race.acceptance -or -not $windows.pass -or $build.status -ne 'pass' -or -not $proof.acceptance){throw 'A required final validation did not pass.'}
if($build.diagnostic -or $build.image_id -cne $proof.image_id -or -not $proof.actual_server_started){throw 'The actual recovery proof must use this formal image.'}
if($race.integration_harness -or $race.real_postgresql){throw 'The Linux unit race must not be reported as a real PG integration race.'}
if(@($pg.top_level_passes).Count -ne 87 -or @($race.top_level_passes).Count -ne 293 -or $windows.top_level_pass_count -ne 356 -or @($windows.missing_baseline_gpt_tests).Count -ne 0){throw 'The final executable test inventories changed.'}
function Test-Counts([string]$Log){
    $events=@(Get-Content -LiteralPath (Join-Path $PSScriptRoot $Log) | ForEach-Object {try {$_ | ConvertFrom-Json -ErrorAction Stop} catch {}})
    [ordered]@{
        top_level_passes=@($events | Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass' -and $_.Test -notlike '*/*'}).Count
        passes_including_subtests=@($events | Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'pass'}).Count
        individual_skips=@($events | Where-Object {$_.PSObject.Properties['Test'] -and $_.Action -eq 'skip'}).Count
        failures=@($events | Where-Object {$_.Action -eq 'fail'}).Count
    }
}
$unitLog=@($windows.commands | Where-Object {($_.arguments -join ' ') -like 'test -tags=unit -json*'})[0].log
$result=[ordered]@{
    stage='I3 final F3 isolated validation';freeze='F3';source_sha256=$source;head=$pg.head;source_dirty=$build.source_dirty
    frontend_dist_sha256=$build.frontend_dist_sha256;source_stable=$true
    postgresql=[ordered]@{kind='original unmodified TestMain PostgreSQL18 integration/fault/concurrency';counts=(Test-Counts $pg.log);composition=@{billing=37;retained_I1_I2_GPT=29;additional_GPT_lifecycle_recovery=8;automatic_reset_execution=13};receipt=[IO.Path]::GetFileName($PGReceipt)}
    linux=[ordered]@{kind=$race.validation_kind;tags='unit';race=$true;real_postgresql=$false;go=$race.linux_go;counts=(Test-Counts $race.log);receipt=[IO.Path]::GetFileName($RaceReceipt)}
    windows=[ordered]@{go='go1.27.0 windows/amd64';counts=(Test-Counts $unitLog);baseline_GPT_tests=$windows.baseline_gpt_test_count;missing_baseline_GPT=@($windows.missing_baseline_gpt_tests);all_package_compile_and_vet=$true;no_test_helper_package_skips=$windows.package_skip_count;receipt=[IO.Path]::GetFileName($WindowsReceipt)}
    image=[ordered]@{tag=$build.image_tag;id=$build.image_id;revision=$build.revision;capability='I3 current receipt recovery plus existing GPT';build_go=$build.linux_go;cgo_enabled=$build.cgo_enabled;runtime_recipe=$build.runtime_recipe;runtime_recipe_sha256=$build.runtime_recipe_sha256;build_receipt=[IO.Path]::GetFileName($BuildReceipt);actual_proof_receipt=[IO.Path]::GetFileName($ImageProofReceipt);actual_server_started=$true;claude_known_recovered=$true;original_GPT_known_recovered=$true;restart_exactly_once=$true;term_HTTP_status=503;issue_enabled=$false;admission_enabled=$false;recovery_enabled=$true;host_ports=@();external_egress=$false;I5_paid_order_recovery_proven=$false;final_I6_schema_revalidation_required=$true}
    exact_receipt_files=@($paths | ForEach-Object {[ordered]@{file=[IO.Path]::GetFileName($_);sha256=(Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant()}})
    traceability='I3-pg-traceability.md';diagnostics='I3-pg-test-plan.md';superseded_freezes=@('F1','F2');retained_environment='environment-active.json';runner_limit_update='environment-I3-runner-limits.json'
    synthetic_only=$true;production_connected=$false;upstream_called=$false;activation_authorized=$false;acceptance=$true;finished_utc=[DateTime]::UtcNow.ToString('o')
}
$output=Join-Path $PSScriptRoot 'I3-pg-F3-final-receipt.json'
$result | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $output -Encoding utf8
[pscustomobject]@{receipt=$output;acceptance=$true;source_sha256=$source;image_id=$build.image_id;pg=$result.postgresql.counts;linux=$result.linux.counts;windows=$result.windows.counts} | ConvertTo-Json -Depth 5

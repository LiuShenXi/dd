param([Parameter(Mandatory)][string]$BuildReceipt)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$build=Get-Content -LiteralPath $BuildReceipt -Raw|ConvertFrom-Json
if($build.status -ne 'pass' -or $build.diagnostic -or -not $build.source_stable -or -not $build.image_built){throw 'The actual image proof requires a completed stable formal build, not a diagnostic image.'}
$env:CWQ_I3_R1_IMAGE=$build.image_tag
$env:CWQ_I3_R1_SOURCE_SHA256=$build.source_sha256
try {
    $proofText=(& (Join-Path $PSScriptRoot 'I3-pg-run.ps1') -Freeze $build.freeze -Pattern '^TestSubscriptionQuotaR1ImageRecovery$' -Expected 1) -join "`n"
    $proof=$proofText|ConvertFrom-Json
    if(-not $proof.acceptance -or $proof.source_sha256 -ne $build.source_sha256){throw 'Actual image recovery did not pass on the same frozen source.'}
    $combined=[ordered]@{
        freeze=$build.freeze;stage='I3 current-receipt recovery-compatible R1';source_sha256=$build.source_sha256
        image_tag=$build.image_tag;image_id=$build.image_id;revision=$build.revision;source_dirty=$build.source_dirty
        build_receipt=[IO.Path]::GetFileName($BuildReceipt);proof_log=$proof.log
        image_built=$true;actual_server_started=$true;original_testmain=$true;postgresql=18
        issue_enabled=$false;admission_enabled=$false;recovery_enabled=$true
        internal_network=$true;proof_subnet='10.247.191.0/28';host_ports=@();external_egress=$false;docker_socket_mounted=$false
        claude_known_cost='0.50000000';claude_account_cost='0.10000000';original_GPT_known_cost='0.25000000'
        restart_exactly_once=$true;actual_term_http_status=503;parent_native_and_wallet_rows_unchanged=$true
        top_level_passes=$proof.top_level_passes;skip_count=$proof.skip_count;failure_count=$proof.failure_count
        source_stable=$proof.source_stable;acceptance=$true;synthetic_only=$true;production_connected=$false;upstream_called=$false
        activation_authorized=$false;I5_paid_order_recovery_proven=$false;requires_I5_I6_final_schema_revalidation=$true
        ephemeral_proof_resources_auto_removed=$true;retained_environment='environment-active.json'
        finished_utc=[DateTime]::UtcNow.ToString('o')
    }
    $path=Join-Path $PSScriptRoot ("I3-pg-$($build.freeze)-R1-final-receipt.json")
    $combined|ConvertTo-Json -Depth 6|Set-Content -LiteralPath $path -Encoding utf8
    [pscustomobject]@{acceptance=$true;image_id=$build.image_id;source_sha256=$build.source_sha256;receipt=$path}|ConvertTo-Json
}finally{
    Remove-Item Env:CWQ_I3_R1_IMAGE -ErrorAction SilentlyContinue
    Remove-Item Env:CWQ_I3_R1_SOURCE_SHA256 -ErrorAction SilentlyContinue
}

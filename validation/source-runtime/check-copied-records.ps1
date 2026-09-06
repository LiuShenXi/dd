param([switch]$CaptureBaseline)
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
$baselinePath = Join-Path $runtimeDirectory 'copied-records-v2-baseline.stdout.log'
if ($CaptureBaseline -and (Test-Path -LiteralPath $baselinePath)) { throw 'Baseline exists; refusing to redefine copied-record conservation.' }
if (-not $CaptureBaseline -and -not (Test-Path -LiteralPath $baselinePath)) { throw 'Original copied-record baseline is missing.' }
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',(Join-Path $PSScriptRoot 'verify-copied-records.sql'),'carpool-v13-test-postgres:/tmp/verify-copied-records.sql') -Name 'copy-copied-record-verifier'
$label = if ($CaptureBaseline) { 'copied-records-v2-baseline' } else { 'copied-records-v2-after' }
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1','-U','carpool_test','-d','carpool_test','-f','/tmp/verify-copied-records.sql') -Name $label
if ($CaptureBaseline) {
    'COPIED_RECORD_BASELINE_CAPTURED: hashes and counts only; original records not printed.'
    exit 0
}
$before = Get-Content -LiteralPath $baselinePath
$after = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'copied-records-v2-after.stdout.log')
if (@(Compare-Object $before $after).Count -ne 0) { throw 'Copied-record conservation failed; see private hash-only diagnostics.' }
'COPIED_RECORD_CONSERVATION_PASS: original users, keys, usage, dedup and groups unchanged.'

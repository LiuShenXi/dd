param([switch]$CaptureBaseline)
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
$boundaryPath = Join-Path $runtimeDirectory 'copied-announcements-boundary.json'
$baselinePath = Join-Path $runtimeDirectory 'copied-announcements-baseline.stdout.log'
if ($CaptureBaseline) {
    if ((Test-Path -LiteralPath $boundaryPath) -or (Test-Path -LiteralPath $baselinePath)) { throw 'Announcement baseline already exists; refusing to redefine original records.' }
    if (Test-Path -LiteralPath (Join-Path $runtimeDirectory 'startup-state.json')) { throw 'Original announcement baseline must precede application startup.' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1','-U','carpool_test','-d','carpool_test','-c','SELECT COALESCE(max(id),0) FROM announcements') -Name 'copied-announcements-boundary'
    $boundary = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'copied-announcements-boundary.stdout.log') -Raw).Trim()
    if ($boundary -notmatch '^\d+$') { throw 'Invalid original announcement boundary.' }
    Write-PrivateRuntimeJSON -Path $boundaryPath -Value @{Task=$runtimeTask; MaxId=$boundary}
}
$boundary = Get-Content -LiteralPath $boundaryPath -Raw | ConvertFrom-Json
if ($boundary.Task -ne $runtimeTask -or $boundary.MaxId -notmatch '^\d+$') { throw 'Announcement boundary does not match this task.' }
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',(Join-Path $PSScriptRoot 'verify-copied-announcements.sql'),'carpool-v13-test-postgres:/tmp/verify-copied-announcements.sql') -Name 'copy-announcement-verifier'
$label = if ($CaptureBaseline) { 'copied-announcements-baseline' } else { 'copied-announcements-after' }
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1','-v',("announcement_max_id=" + $boundary.MaxId),'-U','carpool_test','-d','carpool_test','-f','/tmp/verify-copied-announcements.sql') -Name $label
if ($CaptureBaseline) { 'COPIED_ANNOUNCEMENT_BASELINE_CAPTURED: hashes and counts only.'; exit 0 }
if (-not (Test-Path -LiteralPath $baselinePath)) { throw 'Original announcement baseline is missing.' }
$before = Get-Content -LiteralPath $baselinePath
$after = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'copied-announcements-after.stdout.log')
if (@(Compare-Object $before $after).Count -ne 0) { throw 'Copied-announcement conservation failed; see private hash-only diagnostics.' }
'COPIED_ANNOUNCEMENT_CONSERVATION_PASS: original rows unchanged; migration source defaults remain empty.'

$ErrorActionPreference = 'Stop'

$root = $PSScriptRoot
$wrapperPath = Join-Path $root 'import-reset-marker-fixture.ps1'
$checkerPath = Join-Path $root 'check-preserved-synthetic.ps1'
$sqlPath = Join-Path $root 'verify-preserved-synthetic.sql'
$goPath = Join-Path $root 'reset-marker-fixture/main.go'
foreach ($path in @($wrapperPath,$checkerPath,$sqlPath,$goPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw 'Reset-marker guard source is missing.' }
}
foreach ($scriptPath in @($wrapperPath,$checkerPath)) {
    $tokens = $null
    $parseErrors = $null
    [void][Management.Automation.Language.Parser]::ParseFile($scriptPath,[ref]$tokens,[ref]$parseErrors)
    if ($parseErrors.Count -ne 0) { throw "PowerShell parse failure: $scriptPath" }
}

$wrapper = Get-Content -LiteralPath $wrapperPath -Raw
$checker = Get-Content -LiteralPath $checkerPath -Raw
$sql = Get-Content -LiteralPath $sqlPath -Raw
$go = Get-Content -LiteralPath $goPath -Raw
foreach ($required in @(
    '[switch]$Apply','Assert-LocalRuntimeIsolation','Assert-DirectorCodeReady','Write-PrivateRuntimeCreateOnlyJSON',
    "'check-copied-records.ps1'","'check-copied-announcements.ps1'","'check-preserved-synthetic.ps1'",
    "'carpool-v13-test-acceptance'",'ScopeId=2147480914',"ActualSchedulerExecution=`$false"
)) {
    if (-not $wrapper.Contains($required,[StringComparison]::Ordinal)) { throw "Importer wrapper lost required guard: $required" }
}
foreach ($required in @(
    '[switch]$CaptureBaseline','Write-PrivateRuntimeCreateOnlyJSON','refusing to redefine accepted state',
    "'preserved-synthetic-v1-baseline.stdout.log'","'reset-marker-fixture-import-attempt.json'"
)) {
    if (-not $checker.Contains($required,[StringComparison]::Ordinal)) { throw "Preservation checker lost required guard: $required" }
}
foreach ($required in @(
    'users WHERE id=122','carpool_terms WHERE id=78','carpool_cycles WHERE id=386',
    'users WHERE id=127','api_keys WHERE id=64','carpool_terms WHERE id=94',
    "carpool_reset_batches WHERE id=1 AND scope_id=1 AND status='scheduled'",
    'carpool_reset_qualifications WHERE scope_id=1 AND batch_id=1) <> 3',
    "announcements WHERE id=10 AND source_type='carpool_reset'"
)) {
    if (-not $sql.Contains($required,[StringComparison]::Ordinal)) { throw "Preservation SQL lost required contract: $required" }
}
foreach ($required in @(
    'sql.LevelSerializable','pg_advisory_xact_lock','fixtureScopeID','2_147_480_914',
    'historical-synthetic-reset-marker','actual_scheduler_execution','create_only_boundary',
    "code='three_seat'",'user_allowed_groups','generate_series(1,4)','163.00000000','0.00000000',
    'os.O_CREATE|os.O_EXCL','replay_refused'
)) {
    if (-not $go.Contains($required,[StringComparison]::Ordinal)) { throw "Importer lost required create-only contract: $required" }
}
foreach ($forbidden in @('UPDATE users','UPDATE groups','UPDATE carpool_','DELETE FROM users','DELETE FROM groups','DELETE FROM carpool_','scope_id=1')) {
    if ($go.Contains($forbidden,[StringComparison]::OrdinalIgnoreCase)) { throw "Importer contains forbidden existing-row mutation: $forbidden" }
}

'RESET_MARKER_FIXTURE_GUARD_TESTS_PASS'

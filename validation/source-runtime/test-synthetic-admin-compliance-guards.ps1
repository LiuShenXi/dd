$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'set-synthetic-admin-compliance.ps1'
$sqlPath = Join-Path $PSScriptRoot 'synthetic-admin-compliance.sql'
$scriptSource = Get-Content -LiteralPath $scriptPath -Raw
$sqlSource = Get-Content -LiteralPath $sqlPath -Raw

$tokens = $null
$parseErrors = $null
[void][Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) { throw 'Synthetic compliance fixture PowerShell does not parse.' }

$requiredSQL = @(
    'BEGIN ISOLATION LEVEL SERIALIZABLE;',
    'pg_advisory_xact_lock',
    'ON CONFLICT ("key") DO NOTHING;',
    "current_database() <> 'carpool_test'",
    "current_user <> 'carpool_test'",
    "fixture.user_agent <> 'synthetic-local-only test fixture; not operator consent'",
    "acknowledgement->>'user_agent' IS DISTINCT FROM fixture.user_agent",
    "jsonb_typeof(acknowledgement->'accepted_at') IS DISTINCT FROM 'string'",
    'stored_accepted_at IS NULL',
    'NOT isfinite(stored_accepted_at)',
    "stored_accepted_at > clock_timestamp() + INTERVAL '1 minute'",
    "SELECT 'SYNTHETIC_TEST_ACK_FIXTURE_READY';"
)
foreach ($required in $requiredSQL) {
    if (-not $sqlSource.Contains($required, [StringComparison]::Ordinal)) {
        throw "Synthetic compliance SQL lost required guard: $required"
    }
}

foreach ($forbidden in @('DO UPDATE', 'UPDATE settings', 'DELETE FROM settings', '/compliance/accept')) {
    if ($sqlSource.Contains($forbidden, [StringComparison]::OrdinalIgnoreCase) -or
        $scriptSource.Contains($forbidden, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Synthetic compliance fixture contains forbidden behavior: $forbidden"
    }
}

$requiredScript = @(
    'param([switch]$Apply)',
    'Assert-LocalRuntimeIsolation',
    "'carpool-v13-test-postgres'",
    '$network.Id -ne $binding.NetworkId',
    '$postgresTaskLabels.Count -eq 1 -and $postgresTaskLabels[0].Value -cne $runtimeTask',
    'sealed original PostgreSQL container predates task labels',
    "'carpool_test'",
    "'carpool-test-admin@example.invalid'",
    "'expected_user_agent=synthetic-local-only test fixture; not operator consent'",
    '-InputFile $sqlPath'
)
foreach ($required in $requiredScript) {
    if (-not $scriptSource.Contains($required, [StringComparison]::Ordinal)) {
        throw "Synthetic compliance setup lost required guard: $required"
    }
}

'SYNTHETIC_ADMIN_COMPLIANCE_GUARD_TESTS_PASS'

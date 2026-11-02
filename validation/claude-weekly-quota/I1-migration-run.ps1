param([switch]$IncludeConcurrentGPTPrepare)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$go = 'C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$runID = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$logPath = Join-Path $PSScriptRoot ("I1-migration-$runID.jsonl")
$receiptPath = Join-Path $PSScriptRoot ("I1-migration-$runID-receipt.json")
$testNames = @(
    'TestSubscriptionQuotaMigrations_EmptyDatabaseAndReplay',
    'TestSubscriptionQuotaMigrations_Legacy297UpgradePreservesAllRows',
    'TestSubscriptionQuotaMigrations_GroupModeAndCompositeOwnership',
    'TestSubscriptionQuotaMigrations_Exact168HoursAndNumericBoundaries',
    'TestSubscriptionQuotaMigrations_ImmutableTermsAndLedger',
    'TestSubscriptionQuotaMigrations_UnknownUsageAndUsageLogLinks',
    'TestSubscriptionQuotaMigrations_OnlineIndexFailureRestoresTimeouts',
    'TestSubscriptionQuotaMigrations_OnlineInvalidIndexRecovery'
)
if ($IncludeConcurrentGPTPrepare) { $testNames += 'TestSubscriptionQuotaProtectedConcurrentGPTPrepare' }
$pattern = '^(' + ($testNames -join '|') + ')$'
$env:PATH = 'C:/Program Files/Docker/Docker/resources/bin;' + $env:PATH
$env:DOCKER_HOST = 'npipe:////./pipe/docker_engine'
Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI = 'true'
$env:SUB2API_TEST_POSTGRES_IMAGE = 'postgres:18.1-alpine3.23'
$env:GOTOOLCHAIN = 'local'
$env:GOMAXPROCS = '4'
$started = [DateTime]::UtcNow
$sourceHash = (Get-FileHash -LiteralPath (Join-Path $repoRoot 'backend/internal/repository/subscription_quota_migration_integration_test.go') -Algorithm SHA256).Hash.ToLowerInvariant()
function Read-MigrationHashes {
    $hashes=[ordered]@{}
    Get-ChildItem -LiteralPath (Join-Path $repoRoot 'backend/migrations') -Filter '*.sql' | Where-Object { $_.Name -match '^24[5-9]_' } | Sort-Object Name | ForEach-Object {
        $hashes[$_.Name]=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    return $hashes
}
$migrationHashesBefore = Read-MigrationHashes
Push-Location (Join-Path $repoRoot 'backend')
try {
    & $go test -tags=integration -json -count=1 -timeout=10m -run $pattern ./internal/repository *> $logPath
    $exitCode = $LASTEXITCODE
} finally { Pop-Location }
$events = @(Get-Content -LiteralPath $logPath | ForEach-Object {
    try { $_ | ConvertFrom-Json -ErrorAction Stop } catch { }
})
$topLevelPasses = @($events | Where-Object { $_.Action -eq 'pass' -and $_.PSObject.Properties['Test'] -and $_.Test -notlike '*/*' })
$skips = @($events | Where-Object { $_.Action -eq 'skip' })
$failures = @($events | Where-Object { $_.Action -eq 'fail' })
$migrationHashesAfter = Read-MigrationHashes
$migrationsStable = ($migrationHashesBefore | ConvertTo-Json -Compress) -ceq ($migrationHashesAfter | ConvertTo-Json -Compress)
$result = [ordered]@{
    started_utc=$started.ToString('o'); finished_utc=[DateTime]::UtcNow.ToString('o')
    command='go test -tags=integration -json -count=1 -timeout=10m -run <recorded exact test names> ./internal/repository'
    requested_tests=$testNames
    toolchain='go1.27.0 windows/amd64'; postgres_image='postgres:18.1-alpine3.23'; redis_image='redis:8.4-alpine'
    docker_endpoint=$env:DOCKER_HOST; ci_fail_if_no_docker=$true; original_testmain=$true; synthetic_only=$true
    source_sha256=$sourceHash; migration_sha256=$migrationHashesAfter; migrations_stable_during_run=$migrationsStable
    exit_code=$exitCode; top_level_passes=@($topLevelPasses | ForEach-Object { $_.Test })
    skip_count=$skips.Count; failure_count=$failures.Count; log=[IO.Path]::GetFileName($logPath)
    acceptance=($exitCode -eq 0 -and $topLevelPasses.Count -eq $testNames.Count -and $skips.Count -eq 0 -and $failures.Count -eq 0 -and $migrationsStable)
}
$result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $receiptPath -Encoding utf8
$result | ConvertTo-Json -Depth 5
if (-not $result.acceptance) {
    Get-Content -LiteralPath $logPath -Tail 25
    exit 1
}

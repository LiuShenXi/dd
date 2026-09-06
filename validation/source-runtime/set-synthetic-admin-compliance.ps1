param([switch]$Apply)

. (Join-Path $PSScriptRoot 'runtime-common.ps1')

# This prepares acceptance-only test state. It does not assert that the user or
# operator accepted, acknowledged, or consented to any legal terms.
Assert-LocalRuntimeIsolation

$fixturePath = Join-Path $runtimeDirectory 'fixtures.json'
$startupPath = Join-Path $runtimeDirectory 'startup-state.json'
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
$sqlPath = Join-Path $PSScriptRoot 'synthetic-admin-compliance.sql'
foreach ($path in @($fixturePath, $startupPath, $bindingPath, $sqlPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw 'Synthetic compliance fixture prerequisite is missing.'
    }
}

$startup = Get-Content -LiteralPath $startupPath -Raw | ConvertFrom-Json
$binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json
if ($startup.Task -ne $runtimeTask -or -not $startup.Healthy -or
    $startup.FixtureSha256 -notmatch '^[0-9A-Fa-f]{64}$' -or
    (Get-FileHash -LiteralPath $fixturePath -Algorithm SHA256).Hash -ne $startup.FixtureSha256 -or
    $binding.Task -ne $runtimeTask -or $binding.NetworkId -ne $startup.NetworkId) {
    throw 'Synthetic compliance fixture is not bound to the healthy reviewed runtime.'
}

$postgresID = [string]$binding.Containers.'carpool-v13-test-postgres'
if ($postgresID -notmatch '^[0-9a-f]{64}$') {
    throw 'Synthetic compliance fixture has no exact bound PostgreSQL container.'
}
$network = @(& $runtimeDocker network inspect $runtimeNetwork | ConvertFrom-Json)[0]
if ($LASTEXITCODE -ne 0 -or $network.Id -ne $binding.NetworkId -or -not $network.Internal) {
    throw 'Synthetic compliance fixture network differs from the exact sealed binding.'
}
$postgres = @(& $runtimeDocker inspect $postgresID | ConvertFrom-Json)[0]
$postgresNetworks = @($postgres.NetworkSettings.Networks.PSObject.Properties)
$postgresPorts = if ($null -eq $postgres.HostConfig.PortBindings) { @() } else { @($postgres.HostConfig.PortBindings.PSObject.Properties) }
$postgresTaskLabels = if ($null -eq $postgres.Config.Labels) { @() } else { @($postgres.Config.Labels.PSObject.Properties | Where-Object { $_.Name -ceq $runtimeLabel }) }
# The sealed original PostgreSQL container predates task labels. Absence is
# accepted only because its exact ID is bound by the sanitization evidence.
$postgresTaskLabelMismatch = $postgresTaskLabels.Count -gt 1 -or ($postgresTaskLabels.Count -eq 1 -and $postgresTaskLabels[0].Value -cne $runtimeTask)
if ($LASTEXITCODE -ne 0 -or $postgres.Id -ne $postgresID -or $postgres.Name -ne '/carpool-v13-test-postgres' -or
    $postgresTaskLabelMismatch -or -not $postgres.State.Running -or
    $postgresNetworks.Count -ne 1 -or $postgresNetworks[0].Name -ne $runtimeNetwork -or
    $postgresNetworks[0].Value.Aliases -notcontains 'carpool-db' -or $postgresPorts.Count -ne 0) {
    throw 'Synthetic compliance fixture PostgreSQL role differs from the exact isolated binding.'
}

$postgresEnvironment = @{}
foreach ($entry in @($postgres.Config.Env)) {
    $parts = ([string]$entry).Split('=', 2)
    if ($parts.Count -eq 2) { $postgresEnvironment[$parts[0]] = $parts[1] }
}
if ($postgresEnvironment.POSTGRES_DB -ne 'carpool_test' -or
    $postgresEnvironment.POSTGRES_USER -ne 'carpool_test' -or
    [string]::IsNullOrEmpty($postgresEnvironment.POSTGRES_PASSWORD)) {
    throw 'Synthetic compliance fixture PostgreSQL database or user is not the isolated target.'
}
$postgresEnvironment = $null
$postgres = $null
$network = $null

$fixtures = Get-Content -LiteralPath $fixturePath -Raw | ConvertFrom-Json
$adminMatches = @($fixtures.users | Where-Object {
    $_.name -ceq 'admin' -or $_.email -ceq 'carpool-test-admin@example.invalid' -or $_.id -eq 14
})
if ($fixtures.version -ne 1 -or $fixtures.source -cne 'synthetic-local-only' -or
    $fixtures.base_url -cne 'http://carpool-app:8080' -or $fixtures.mock_url -cne 'http://carpool-mock:8090' -or
    $adminMatches.Count -ne 1 -or $adminMatches[0].name -cne 'admin' -or
    $adminMatches[0].email -cne 'carpool-test-admin@example.invalid' -or $adminMatches[0].id -ne 14 -or
    $adminMatches[0].password -cnotmatch '^[0-9a-f]{48}$') {
    throw 'Synthetic compliance fixture admin does not match the exact private bootstrap identity.'
}
$fixtures = $null
$adminMatches = $null

$complianceSource = Get-Content -LiteralPath (Join-Path $runtimeRepository 'backend/internal/service/admin_compliance.go') -Raw
$sourceContracts = @(
    'AdminComplianceVersion        = "v2026.06.10"',
    'AdminComplianceDocumentPathZH = "docs/legal/admin-compliance.zh.md"',
    'AdminComplianceDocumentPathEN = "docs/legal/admin-compliance.en.md"',
    'settingKeyAdminComplianceAcknowledgement = "admin_compliance_acknowledgement"'
)
foreach ($contract in $sourceContracts) {
    if (-not $complianceSource.Contains($contract, [StringComparison]::Ordinal)) {
        throw 'Synthetic compliance fixture no longer matches the application compliance contract.'
    }
}
foreach ($document in @('docs/legal/admin-compliance.zh.md', 'docs/legal/admin-compliance.en.md')) {
    if (-not (Test-Path -LiteralPath (Join-Path $runtimeRepository $document) -PathType Leaf)) {
        throw 'Synthetic compliance fixture references a missing application document.'
    }
}

if (-not $Apply) {
    'SYNTHETIC_TEST_ACK_FIXTURE_VALIDATED: acceptance-only fixture is ready; database unchanged.'
    exit 0
}

$arguments = @(
    'exec', '-i', $postgresID,
    'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
    '-v', 'expected_setting_key=admin_compliance_acknowledgement:14',
    '-v', 'expected_fixture_name=admin',
    '-v', 'expected_admin_id=14',
    '-v', 'expected_admin_email=carpool-test-admin@example.invalid',
    '-v', 'expected_admin_username=Carpool Test admin',
    '-v', 'expected_version=v2026.06.10',
    '-v', 'expected_document_zh=docs/legal/admin-compliance.zh.md',
    '-v', 'expected_document_en=docs/legal/admin-compliance.en.md',
    '-v', 'expected_ip_address=127.0.0.1',
    '-v', 'expected_user_agent=synthetic-local-only test fixture; not operator consent',
    '-U', 'carpool_test', '-d', 'carpool_test', '-f', '-'
)
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments $arguments -InputFile $sqlPath -Name 'synthetic-admin-compliance-fixture'
$result = @(Get-Content -LiteralPath (Join-Path $runtimeDirectory 'synthetic-admin-compliance-fixture.stdout.log') | Where-Object { $_ -ne '' })
if ($result.Count -ne 1 -or $result[0] -cne 'SYNTHETIC_TEST_ACK_FIXTURE_READY') {
    throw 'Synthetic compliance fixture did not return its exact non-consent test marker.'
}

'SYNTHETIC_TEST_ACK_FIXTURE_APPLIED: test state only; not operator or user legal consent.'

param(
    [Parameter(Mandatory=$true)][string]$CodeReadyEvidence,
    [switch]$Apply
)

. (Join-Path $PSScriptRoot 'runtime-common.ps1')

Assert-LocalRuntimeIsolation
$startupPath = Join-Path $runtimeDirectory 'startup-state.json'
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
$fixtureDirectory = Join-Path $PSScriptRoot 'reset-marker-fixture'
$sourcePath = Join-Path $fixtureDirectory 'main.go'
$testPath = Join-Path $fixtureDirectory 'main_test.go'
$attemptPath = Join-Path $runtimeDirectory 'reset-marker-fixture-import-attempt.json'
$resultPath = Join-Path $runtimeDirectory 'reset-marker-fixture.json'
foreach ($path in @($startupPath,$bindingPath,$sourcePath,$testPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw 'Reset-marker fixture importer prerequisite is missing.' }
}
$startup = Get-Content -LiteralPath $startupPath -Raw | ConvertFrom-Json
$binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json
$evidenceHash = Assert-DirectorCodeReady -EvidencePath $CodeReadyEvidence -RuntimeEvidenceHash $startup.CodeEvidenceHash
if (-not $startup.Healthy -or $startup.Task -ne $runtimeTask -or $binding.Task -ne $runtimeTask -or
    $binding.NetworkId -ne $startup.NetworkId -or $startup.ImageId -notmatch '^sha256:[0-9a-f]{64}$') {
    throw 'Reset-marker fixture importer is not bound to the healthy reviewed runtime.'
}
foreach ($name in @('carpool-v13-test-app','carpool-v13-test-mock')) {
    $container = @(& $runtimeDocker inspect $name | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0 -or -not $container.State.Running -or $container.Id -ne $startup.$name -or
        $container.Image -ne $startup.ImageId -or $container.Config.Labels.$runtimeLabel -ne $runtimeTask) {
        throw 'Application or mock differs from the reviewed local runtime.'
    }
}
$postgresID = [string]$binding.Containers.'carpool-v13-test-postgres'
$postgres = @(& $runtimeDocker inspect $postgresID | ConvertFrom-Json)[0]
$postgresNetworks = @($postgres.NetworkSettings.Networks.PSObject.Properties)
$postgresPorts = if ($null -eq $postgres.HostConfig.PortBindings) { @() } else { @($postgres.HostConfig.PortBindings.PSObject.Properties) }
if ($LASTEXITCODE -ne 0 -or $postgresID -notmatch '^[0-9a-f]{64}$' -or $postgres.Id -ne $postgresID -or
    $postgres.Name -ne '/carpool-v13-test-postgres' -or -not $postgres.State.Running -or
    $postgresNetworks.Count -ne 1 -or $postgresNetworks[0].Name -ne $runtimeNetwork -or
    $postgresNetworks[0].Value.Aliases -notcontains 'carpool-db' -or $postgresPorts.Count -ne 0) {
    throw 'Reset-marker fixture PostgreSQL role differs from the exact sealed local binding.'
}
if ((& $runtimeDocker ps -a --format '{{.Names}}') -contains 'carpool-v13-test-acceptance') {
    throw 'Acceptance runner exists; reset-marker fixture import is not allowed concurrently.'
}

$sourceHash = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash
$testHash = (Get-FileHash -LiteralPath $testPath -Algorithm SHA256).Hash
$schemaFiles = @('235_carpool_core.sql','237_carpool_resets.sql','238_carpool_reset_qualifications.sql','239_carpool_rules_v14.sql')
$schemaHashes = [ordered]@{}
foreach ($schemaFile in $schemaFiles) {
    $schemaPath = Join-Path $runtimeRepository "backend/migrations/$schemaFile"
    if (-not (Test-Path -LiteralPath $schemaPath -PathType Leaf)) { throw 'Reset-marker fixture schema source is missing.' }
    $schemaHashes[$schemaFile] = (Get-FileHash -LiteralPath $schemaPath -Algorithm SHA256).Hash
}
$runNonce = [Guid]::NewGuid().ToString('N')
$buildDirectory = (New-Item -ItemType Directory -Path (Join-Path $runtimeDirectory "reset-marker-fixture-build-$runNonce")).FullName
$binaryPath = Join-Path $buildDirectory 'reset-marker-fixture'
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('test','-count=1',$sourcePath,$testPath) -Name "reset-marker-$runNonce-tests" -WorkingDirectory (Join-Path $runtimeRepository 'backend')
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('vet',$sourcePath,$testPath) -Name "reset-marker-$runNonce-vet" -WorkingDirectory (Join-Path $runtimeRepository 'backend')
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',$binaryPath,$sourcePath) -Name "reset-marker-$runNonce-build" -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment @{GOOS='linux';GOARCH='amd64';CGO_ENABLED='0'}
if ((Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash -ne $sourceHash -or
    (Get-FileHash -LiteralPath $testPath -Algorithm SHA256).Hash -ne $testHash) {
    throw 'Reset-marker fixture source changed while testing or building.'
}
$binaryHash = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash

if (-not $Apply) {
    'RESET_MARKER_FIXTURE_VALIDATED: importer builds and guards match the reviewed runtime; database unchanged.'
    exit 0
}
if ((Test-Path -LiteralPath $attemptPath) -or (Test-Path -LiteralPath $resultPath)) {
    throw 'Reset-marker fixture import is create-only; an earlier attempt or result already exists.'
}
foreach ($schemaFile in $schemaFiles) {
    $schemaPath = Join-Path $runtimeRepository "backend/migrations/$schemaFile"
    if ((Get-FileHash -LiteralPath $schemaPath -Algorithm SHA256).Hash -ne $schemaHashes[$schemaFile]) {
        throw 'Reset-marker fixture schema source changed while testing or building.'
    }
}

& (Join-Path $PSScriptRoot 'check-copied-records.ps1')
& (Join-Path $PSScriptRoot 'check-copied-announcements.ps1')
& (Join-Path $PSScriptRoot 'check-preserved-synthetic.ps1') -CodeReadyEvidence $CodeReadyEvidence

Write-PrivateRuntimeCreateOnlyJSON -Path $attemptPath -Value ([ordered]@{
    Version=1; Task=$runtimeTask; Status='authorized-create-only-attempt'; RunNonce=$runNonce
    RuntimeImageId=$startup.ImageId; CodeEvidenceSha256=$evidenceHash; NetworkId=$startup.NetworkId; PostgresId=$postgresID
    ImporterSourceSha256=$sourceHash; ImporterTestSha256=$testHash; ImporterBinarySha256=$binaryHash
    SchemaSha256=$schemaHashes; ScopeId=2147480914; FixtureSource='historical-synthetic-reset-marker'
    ActualSchedulerExecution=$false; StartedAt=[DateTimeOffset]::UtcNow.ToString('o')
})

$containerBinaryPath = "/tmp/reset-marker-fixture-$runNonce"
$containerResultPath = "/tmp/reset-marker-fixture-$runNonce.json"
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',$binaryPath,"${postgresID}:$containerBinaryPath") -Name "reset-marker-$runNonce-copy-binary"
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$postgresID,'chmod','500',$containerBinaryPath) -Name "reset-marker-$runNonce-binary-mode"
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$postgresID,'/bin/busybox','sha256sum',$containerBinaryPath) -Name "reset-marker-$runNonce-installed-hash"
$installedHash = (Get-Content -LiteralPath (Join-Path $runtimeDirectory "reset-marker-$runNonce-installed-hash.stdout.log") -Raw).Trim()
if ($installedHash -notmatch '^([0-9a-fA-F]{64})\s+/tmp/reset-marker-fixture-[0-9a-f]{32}$' -or $Matches[1] -ne $binaryHash) {
    throw 'Installed reset-marker importer differs from the reviewed binary.'
}
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @(
    'exec',$postgresID,$containerBinaryPath,
    '-output',$containerResultPath,'-task',$runtimeTask,'-run-nonce',$runNonce,
    '-runtime-image-id',$startup.ImageId,'-code-evidence-sha256',$evidenceHash,'-importer-sha256',$binaryHash
) -Name "reset-marker-$runNonce-import"

$incomingPath = Join-Path $buildDirectory 'reset-marker-fixture.incoming.json'
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',"${postgresID}:$containerResultPath",$incomingPath) -Name "reset-marker-$runNonce-copy-result"
$result = Get-Content -LiteralPath $incomingPath -Raw | ConvertFrom-Json
$termStartsAt = [DateTimeOffset]::Parse($result.term_starts_at)
$termExpiresAt = [DateTimeOffset]::Parse($result.term_expires_at)
if ($result.version -ne 1 -or $result.source -cne 'historical-synthetic-reset-marker' -or
    $result.purpose -cne 'user-ui-reset-marker-projection-only' -or $result.historical_fixture -ne $true -or
    $result.actual_scheduler_execution -ne $false -or $result.scope_id -ne 2147480914 -or
    $result.binding.task -ne $runtimeTask -or $result.binding.run_nonce -ne $runNonce -or
    $result.binding.runtime_image_id -ne $startup.ImageId -or $result.binding.code_evidence_sha256 -ne $evidenceHash -or
    $result.binding.importer_sha256 -ne $binaryHash -or
    $result.user.email -cne 'carpool-reset-marker-v14@example.invalid' -or $result.user.password -cnotmatch '^[0-9a-f]{48}$' -or
    @($result.cycle_ids).Count -ne 4 -or @($result.qualification_ids).Count -ne 2 -or @($result.ledger_ids).Count -ne 3 -or
    @($result.reset_events).Count -ne 2 -or $result.reset_events[0].granted_usd -cne '163.00000000' -or
    $result.reset_events[1].granted_usd -cne '0.00000000' -or $result.available_usd -cne '700.00000000' -or
    ($termExpiresAt - $termStartsAt).TotalDays -ne 28) {
    throw 'Reset-marker fixture result manifest failed strict private validation.'
}
$input = [IO.FileStream]::new($incomingPath,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
$output = [IO.FileStream]::new($resultPath,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
try { $input.CopyTo($output); $output.Flush($true) } finally { $output.Dispose(); $input.Dispose() }
if ((Get-FileHash -LiteralPath $incomingPath -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $resultPath -Algorithm SHA256).Hash) {
    throw 'Private reset-marker fixture result copy is not exact.'
}
[IO.File]::Delete($incomingPath)
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$postgresID,'rm','-f',$containerBinaryPath,$containerResultPath) -Name "reset-marker-$runNonce-remove-container-temporaries"

& (Join-Path $PSScriptRoot 'check-copied-records.ps1')
& (Join-Path $PSScriptRoot 'check-copied-announcements.ps1')
& (Join-Path $PSScriptRoot 'check-preserved-synthetic.ps1') -CodeReadyEvidence $CodeReadyEvidence

'RESET_MARKER_FIXTURE_APPLIED: historical synthetic marker data only; this is not scheduler-execution evidence and is not eligible for scope-1 boosts.'

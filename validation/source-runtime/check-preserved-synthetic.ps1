param(
    [Parameter(Mandatory=$true)][string]$CodeReadyEvidence,
    [switch]$CaptureBaseline
)

. (Join-Path $PSScriptRoot 'runtime-common.ps1')

Assert-LocalRuntimeIsolation
$startupPath = Join-Path $runtimeDirectory 'startup-state.json'
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
$sqlPath = Join-Path $PSScriptRoot 'verify-preserved-synthetic.sql'
foreach ($path in @($startupPath,$bindingPath,$sqlPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw 'Preserved-synthetic checker prerequisite is missing.' }
}
$startup = Get-Content -LiteralPath $startupPath -Raw | ConvertFrom-Json
$binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json
$evidenceHash = Assert-DirectorCodeReady -EvidencePath $CodeReadyEvidence -RuntimeEvidenceHash $startup.CodeEvidenceHash
if (-not $startup.Healthy -or $startup.Task -ne $runtimeTask -or $binding.Task -ne $runtimeTask -or
    $binding.NetworkId -ne $startup.NetworkId -or $startup.ImageId -notmatch '^sha256:[0-9a-f]{64}$') {
    throw 'Preserved-synthetic checker is not bound to the healthy reviewed runtime.'
}
$postgresID = [string]$binding.Containers.'carpool-v13-test-postgres'
$postgres = @(& $runtimeDocker inspect $postgresID | ConvertFrom-Json)[0]
$postgresNetworks = @($postgres.NetworkSettings.Networks.PSObject.Properties)
$postgresPorts = if ($null -eq $postgres.HostConfig.PortBindings) { @() } else { @($postgres.HostConfig.PortBindings.PSObject.Properties) }
if ($LASTEXITCODE -ne 0 -or $postgresID -notmatch '^[0-9a-f]{64}$' -or $postgres.Id -ne $postgresID -or
    $postgres.Name -ne '/carpool-v13-test-postgres' -or -not $postgres.State.Running -or
    $postgresNetworks.Count -ne 1 -or $postgresNetworks[0].Name -ne $runtimeNetwork -or
    $postgresNetworks[0].Value.Aliases -notcontains 'carpool-db' -or $postgresPorts.Count -ne 0) {
    throw 'Preserved-synthetic checker PostgreSQL role differs from the sealed local binding.'
}

$baselinePath = Join-Path $runtimeDirectory 'preserved-synthetic-v1-baseline.stdout.log'
$baselineManifestPath = Join-Path $runtimeDirectory 'preserved-synthetic-v1-baseline.json'
$attemptPath = Join-Path $runtimeDirectory 'reset-marker-fixture-import-attempt.json'
$resultPath = Join-Path $runtimeDirectory 'reset-marker-fixture.json'
$sqlHash = (Get-FileHash -LiteralPath $sqlPath -Algorithm SHA256).Hash
if ($CaptureBaseline) {
    if ((Test-Path -LiteralPath $baselinePath) -or (Test-Path -LiteralPath $baselineManifestPath)) {
        throw 'Preserved-synthetic baseline already exists; refusing to redefine accepted state.'
    }
    if ((Test-Path -LiteralPath $attemptPath) -or (Test-Path -LiteralPath $resultPath)) {
        throw 'Preserved-synthetic baseline must be captured before any reset-marker import attempt.'
    }
} elseif (-not (Test-Path -LiteralPath $baselinePath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $baselineManifestPath -PathType Leaf)) {
    throw 'Director-approved preserved-synthetic baseline is missing.'
}

$containerSQLPath = "/tmp/verify-preserved-synthetic-$($sqlHash.ToLowerInvariant()).sql"
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',$sqlPath,"${postgresID}:$containerSQLPath") -Name 'copy-preserved-synthetic-verifier'
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$postgresID,'/bin/busybox','sha256sum',$containerSQLPath) -Name 'hash-preserved-synthetic-verifier'
$installedHash = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'hash-preserved-synthetic-verifier.stdout.log') -Raw).Trim()
if ($installedHash -notmatch '^([0-9a-fA-F]{64})\s+/tmp/verify-preserved-synthetic-[0-9a-f]{64}\.sql$' -or $Matches[1] -ne $sqlHash) {
    throw 'Installed preserved-synthetic verifier differs from reviewed source.'
}
$label = if ($CaptureBaseline) { 'preserved-synthetic-v1-baseline' } else { 'preserved-synthetic-v1-after' }
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$postgresID,'psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1','-U','carpool_test','-d','carpool_test','-f',$containerSQLPath) -Name $label
$outputPath = Join-Path $runtimeDirectory "$label.stdout.log"
$output = @(Get-Content -LiteralPath $outputPath | Where-Object { $_ -ne '' })
if ($output.Count -ne 21 -or @($output | Where-Object { $_ -notmatch '^[a-z0-9_]+\|\d+\|(?:[0-9a-f]{32}|empty)$' }).Count -ne 0) {
    throw 'Preserved-synthetic verifier returned an invalid hash-only envelope.'
}

if ($CaptureBaseline) {
    Write-PrivateRuntimeCreateOnlyJSON -Path $baselineManifestPath -Value ([ordered]@{
        Version=1; Task=$runtimeTask; RuntimeImageId=$startup.ImageId; CodeEvidenceSha256=$evidenceHash
        NetworkId=$startup.NetworkId; PostgresId=$postgresID; VerifierSha256=$sqlHash
        BaselineSha256=(Get-FileHash -LiteralPath $baselinePath -Algorithm SHA256).Hash
        CapturedAt=[DateTimeOffset]::UtcNow.ToString('o')
    })
    'PRESERVED_SYNTHETIC_BASELINE_CAPTURED: hashes only; accepted rows were not printed.'
    exit 0
}

$manifest = Get-Content -LiteralPath $baselineManifestPath -Raw | ConvertFrom-Json
if ($manifest.Version -ne 1 -or $manifest.Task -ne $runtimeTask -or $manifest.RuntimeImageId -ne $startup.ImageId -or
    $manifest.CodeEvidenceSha256 -ne $evidenceHash -or $manifest.NetworkId -ne $startup.NetworkId -or
    $manifest.PostgresId -ne $postgresID -or $manifest.VerifierSha256 -ne $sqlHash -or
    $manifest.BaselineSha256 -ne (Get-FileHash -LiteralPath $baselinePath -Algorithm SHA256).Hash) {
    throw 'Preserved-synthetic baseline manifest is stale or inconsistent.'
}
$before = @(Get-Content -LiteralPath $baselinePath)
$after = @(Get-Content -LiteralPath $outputPath)
if (@(Compare-Object -ReferenceObject $before -DifferenceObject $after -CaseSensitive).Count -ne 0) {
    throw 'Preserved synthetic reporter/UI/reset state changed; see private hash-only diagnostics.'
}
'PRESERVED_SYNTHETIC_CONSERVATION_PASS: users 122/127, terms 78/94, key 64, batch 1, announcement 10, and all three qualifications are unchanged.'

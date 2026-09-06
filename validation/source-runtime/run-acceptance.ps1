param([Parameter(Mandatory=$true)][string]$CodeReadyEvidence, [ValidateSet('http','websocket','reset')][string]$Suite = 'http')
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
$startup = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'startup-state.json') -Raw | ConvertFrom-Json
$evidenceHash = Assert-DirectorCodeReady -EvidencePath $CodeReadyEvidence -RuntimeEvidenceHash $startup.CodeEvidenceHash
if (-not $startup.Healthy -or $startup.Task -ne $runtimeTask -or $startup.CodeEvidenceHash -ne $evidenceHash) { throw 'The reviewed runtime is not healthy or its build evidence changed.' }
$runtimeConfigPath = Join-Path $runtimeDirectory 'config.yaml'
$runtimeEnvironmentPath = Join-Path $runtimeDirectory 'app.env'
if ((Get-FileHash -LiteralPath $runtimeConfigPath -Algorithm SHA256).Hash -ne $startup.ConfigHash -or
    (Assert-LocalRuntimeEnvironmentFile -Path $runtimeEnvironmentPath) -ne $startup.AppEnvironmentHash) {
    throw 'Runtime configuration or environment differs from the healthy startup snapshot.'
}
$fixturePath = Join-Path $runtimeDirectory 'fixtures.json'
if ((Get-FileHash -LiteralPath $fixturePath).Hash -ne $startup.FixtureSha256) { throw 'Synthetic fixtures differ from the committed bootstrap.' }
foreach ($name in @('carpool-v13-test-app','carpool-v13-test-mock')) {
    $container = (& $runtimeDocker inspect $name | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0 -or -not $container.State.Running -or $container.Id -ne $startup.$name -or $container.Image -ne $startup.ImageId -or $container.Config.Labels.$runtimeLabel -ne $runtimeTask) { throw 'Application or mock does not match the reviewed runtime.' }
    $networks = @($container.NetworkSettings.Networks.PSObject.Properties)
    $alias = if ($name -eq 'carpool-v13-test-app') { 'carpool-app' } else { 'carpool-mock' }
    if ($networks.Count -ne 1 -or $networks[0].Name -ne $runtimeNetwork -or $networks[0].Value.Aliases -notcontains $alias) { throw 'Application or mock is no longer isolated.' }
}
$runnerName = 'carpool-v13-test-acceptance'
if ((& $runtimeDocker ps -a --format '{{.Names}}') -contains $runnerName) { throw 'Acceptance runner already exists; refusing to replace it.' }
if ($Suite -eq 'reset') {
    $copiedTermQuery = "SELECT count(*) FROM carpool_terms t JOIN users u ON u.id=t.user_id WHERE u.email LIKE 'local-user-%@example.invalid';"
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1','-U','carpool_test','-d','carpool_test','-c',$copiedTermQuery) -Name 'reset-copied-term-preflight'
    $copiedTermCount = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'reset-copied-term-preflight.stdout.log') -Raw).Trim()
    if ($copiedTermCount -cne '0') { throw 'Reset acceptance requires zero copied-user carpool terms; only synthetic terms may be affected.' }
}
$runDirectory = Join-Path $runtimeDirectory ('acceptance-' + $Suite + '-' + [Guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path (Join-Path $runDirectory 'results'))
$binaryDirectory = (New-Item -ItemType Directory -Path (Join-Path $runDirectory 'bin')).FullName
$binaryPath = Join-Path $binaryDirectory 'carpool-acceptance'
$package = switch ($Suite) { 'http' { 'acceptance' }; 'websocket' { 'websocket' }; 'reset' { 'reset-http' } }
$sourcePath = Join-Path $PSScriptRoot "$package/main.go"
$testPath = Join-Path $PSScriptRoot "$package/main_test.go"
$driverSourceHash = (Get-FileHash -LiteralPath $sourcePath).Hash
$driverTestHash = (Get-FileHash -LiteralPath $testPath).Hash
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('test','-count=1',$sourcePath,$testPath) -Name 'acceptance-driver-unit-tests' -WorkingDirectory (Join-Path $runtimeRepository 'backend')
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',$binaryPath,$sourcePath) -Name 'build-acceptance-linux' -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment @{GOOS='linux';GOARCH='amd64';CGO_ENABLED='0'}
if ((Get-FileHash -LiteralPath $sourcePath).Hash -ne $driverSourceHash -or (Get-FileHash -LiteralPath $testPath).Hash -ne $driverTestHash) { throw 'Acceptance source changed while testing/building.' }
$driverBinaryHash = (Get-FileHash -LiteralPath $binaryPath).Hash
$runStatePath = Join-Path $runDirectory 'run-state.json'
$runNonce = [Guid]::NewGuid().ToString('N')
$runState = @{Task=$runtimeTask; Suite=$Suite; Status='running'; RunNonce=$runNonce; FixtureSha256=$startup.FixtureSha256; DriverBinarySha256=$driverBinaryHash; RunDirectory=$runDirectory; StartedAt=[DateTimeOffset]::UtcNow.ToString('o')}
Write-PrivateRuntimeJSON -Path $runStatePath -Value $runState
$privateEnvironmentPath = $null
$additionalEnvironment = @()
if ($Suite -in @('websocket','reset')) {
    $configPath = Join-Path $runtimeDirectory 'config.yaml'
    if ((Get-FileHash -LiteralPath $configPath).Hash -ne $startup.ConfigHash) { throw 'Private database configuration differs from the running application.' }
    $config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
    if ($config.database.host -ne 'carpool-db' -or $config.database.port -ne 5432 -or $config.database.dbname -ne 'carpool_test' -or $config.database.user -ne 'carpool_test' -or [string]::IsNullOrEmpty($config.database.password) -or $config.database.password -match '[\r\n]') { throw 'Receipt and source-metadata evidence requires the exact isolated database contract.' }
    $privateEnvironmentPath = Join-Path $runDirectory 'database-readonly.env'
    $databaseEnvironmentPrefix = if ($Suite -eq 'websocket') { 'CARPOOL_WS' } else { 'CARPOOL_RESET' }
    $environmentLines = @("${databaseEnvironmentPrefix}_DB_HOST=carpool-db","${databaseEnvironmentPrefix}_DB_PORT=5432","${databaseEnvironmentPrefix}_DB_NAME=carpool_test","${databaseEnvironmentPrefix}_DB_USER=carpool_test",("${databaseEnvironmentPrefix}_DB_PASSWORD=" + $config.database.password))
    [IO.File]::WriteAllLines($privateEnvironmentPath, $environmentLines, [Text.UTF8Encoding]::new($false))
    $environmentLines = $null
    $config = $null
    $additionalEnvironment = @('--env-file',$privateEnvironmentPath)
}
$create = @('create','--name',$runnerName,'--label',"$runtimeLabel=$runtimeTask",'--label',"$runtimeRunLabel=$runNonce",'--network',$runtimeNetwork,'--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--pids-limit','64','--memory','256m','--cpus','1',
    '--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,nodev,size=1m','--tmpfs','/runner:rw,exec,nosuid,nodev,size=64m','--tmpfs','/run-private:rw,noexec,nosuid,nodev,size=2m','--tmpfs','/results:rw,noexec,nosuid,nodev,size=4m',
    '--env','HTTP_PROXY=','--env','HTTPS_PROXY=','--env','ALL_PROXY=','--env','NO_PROXY=*') + $additionalEnvironment + @('--entrypoint','/bin/sleep',$startup.ImageId,'900')
$runError = $null
$runnerId = $null
try {
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments $create -Name "acceptance-$runNonce-create"
    if ($privateEnvironmentPath) { [IO.File]::Delete($privateEnvironmentPath) }
    $runnerId = (Get-Content -LiteralPath (Join-Path $runtimeDirectory "acceptance-$runNonce-create.stdout.log") -Raw).Trim()
    if ($runnerId -notmatch '^[0-9a-f]{64}$') { throw 'Acceptance runner returned an invalid container identity.' }
    $runState.ContainerId = $runnerId
    Write-PrivateRuntimeJSON -Path $runStatePath -Value $runState
    $runner = (& $runtimeDocker inspect $runnerId | ConvertFrom-Json)[0]
    $runnerNetworks = @($runner.NetworkSettings.Networks.PSObject.Properties)
    if ($LASTEXITCODE -ne 0 -or $runner.Id -ne $runnerId -or $runner.Config.Labels.$runtimeLabel -ne $runtimeTask -or $runner.Config.Labels.$runtimeRunLabel -ne $runNonce -or $runner.Image -ne $startup.ImageId -or $runnerNetworks.Count -ne 1 -or $runnerNetworks[0].Name -ne $runtimeNetwork -or -not $runner.HostConfig.ReadonlyRootfs -or @($runner.Mounts | Where-Object { $null -ne $_ }).Count -ne 0) { throw 'Acceptance runner does not match its private execution role.' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('start',$runnerId) -Name "acceptance-$runNonce-start"
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','-i',$runnerId,'/bin/sh','-c','umask 077; cat > /runner/carpool-acceptance && chmod 500 /runner/carpool-acceptance') -InputFile $binaryPath -Name "acceptance-$runNonce-install-binary"
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','-i',$runnerId,'/bin/sh','-c','umask 077; cat > /run-private/fixtures.json && chmod 400 /run-private/fixtures.json') -InputFile $fixturePath -Name "acceptance-$runNonce-install-fixtures"
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$runnerId,'sha256sum','/runner/carpool-acceptance','/run-private/fixtures.json') -Name "acceptance-$runNonce-installed-hashes"
    $installedHashes = @(Get-Content -LiteralPath (Join-Path $runtimeDirectory "acceptance-$runNonce-installed-hashes.stdout.log"))
    if ($installedHashes.Count -ne 2 -or $installedHashes[0] -notmatch ('(?i)^' + $driverBinaryHash + '\s+/runner/carpool-acceptance$') -or $installedHashes[1] -notmatch ('(?i)^' + $startup.FixtureSha256 + '\s+/run-private/fixtures.json$')) { throw 'Acceptance binary or fixture transfer did not match its expected hash.' }
    $batchFlag = if ($Suite -eq 'http') { '-batch-fixtures' } else { '-run-fixtures' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$runnerId,'/runner/carpool-acceptance','-fixtures','/run-private/fixtures.json',$batchFlag,'/results/batch-fixtures.json','-report','/results/report.json') -Name "acceptance-$runNonce-run" -ExpectedExitCodes @(0,1)
} catch { $runError = $_ }
finally {
    if ($privateEnvironmentPath) { [IO.File]::Delete($privateEnvironmentPath) }
    if ($runnerId -match '^[0-9a-f]{64}$') {
        $runner = (& $runtimeDocker inspect $runnerId | ConvertFrom-Json)[0]
        if ($LASTEXITCODE -ne 0 -or $runner.Id -ne $runnerId -or $runner.Config.Labels.$runtimeLabel -ne $runtimeTask -or $runner.Config.Labels.$runtimeRunLabel -ne $runNonce) { throw 'Refusing to collect or remove an unrecognized acceptance runner.' }
        try {
            if ($runner.State.Running) {
                foreach ($file in @('report.json','batch-fixtures.json')) {
                    $captureName = "acceptance-$runNonce-receive-$file"
                    try {
                        Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$runnerId,'/bin/cat',"/results/$file") -Name $captureName
                        $capturedPath = Join-Path $runtimeDirectory "$captureName.stdout.log"
                        $destination = Join-Path $runDirectory "results/$file"
                        [IO.File]::Move($capturedPath, $destination)
                    } catch { $runError = $_ }
                }
            }
        } finally {
            Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('rm','-f',$runnerId) -Name "acceptance-$runNonce-remove-owned-runner"
        }
    }
}
$reportPath = Join-Path $runDirectory 'results/report.json'
if (-not (Test-Path -LiteralPath $reportPath)) { throw 'Acceptance did not produce a report; restricted runner diagnostics retained.' }
$report = Get-Content -LiteralPath $reportPath -Raw | ConvertFrom-Json
if ($report.version -ne 1 -or $report.source -ne 'synthetic-local-only' -or $report.assertions.Count -eq 0 -or $report.passed -lt 0 -or $report.failed -lt 0 -or $report.assertions.Count -ne ($report.passed + $report.failed)) { throw 'Acceptance report envelope failed validation.' }
$scopeExclusions = @()
if ($Suite -eq 'reset') {
    $excludedChecks = @($report.excluded_checks)
    if ($excludedChecks.Count -ne 1 -or $excludedChecks[0].name -cne 'reset.actual_due_execution' -or $excludedChecks[0].coverage -cne 'postgresql_covered') {
        throw 'Reset acceptance must explicitly exclude actual due execution from its HTTP assertions.'
    }
    if ($report.scenario -cnotin @('first_publication','pending_merge') -and -not ([string]::IsNullOrEmpty($report.scenario) -and $report.failed -gt 0)) {
        throw 'Reset acceptance scenario is missing or unrecognized.'
    }
    $scopeExclusions = @([ordered]@{name='reset.actual_due_execution'; coverage='postgresql_covered'; evidence='source-runtime-pg-progress.md'})
}
$safeAssertions = @()
$assertionNames = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
foreach ($assertion in $report.assertions) {
    $httpStatus = if ($null -eq $assertion.http_status) { 0 } else { $assertion.http_status }
    if ($assertion.name -notmatch '^[a-zA-Z0-9_.]+$' -or $assertion.status -notin @('pass','fail') -or $httpStatus -lt 0 -or $httpStatus -gt 599) { throw 'Acceptance assertion failed redaction validation.' }
    if (-not $assertionNames.Add($assertion.name)) { throw 'Acceptance report has duplicate assertions.' }
    $safeAssertions += [ordered]@{name=$assertion.name; status=$assertion.status; http_status=$httpStatus}
}
if (@($safeAssertions | Where-Object { $_.status -eq 'pass' }).Count -ne $report.passed -or @($safeAssertions | Where-Object { $_.status -eq 'fail' }).Count -ne $report.failed) { throw 'Acceptance report counters contradict assertion outcomes.' }
$conservation = @{}
foreach ($check in @('copied-records','copied-announcements')) {
    try { & (Join-Path $PSScriptRoot ("check-$check.ps1")); $conservation[$check] = 'passed' } catch { $conservation[$check] = 'failed' }
}
$isolation = 'passed'
try {
    Assert-LocalRuntimeIsolation
    if ((Get-FileHash -LiteralPath $runtimeConfigPath -Algorithm SHA256).Hash -ne $startup.ConfigHash -or
        (Assert-LocalRuntimeEnvironmentFile -Path $runtimeEnvironmentPath) -ne $startup.AppEnvironmentHash) {
        throw 'Runtime configuration or environment changed during acceptance.'
    }
    foreach ($name in @('carpool-v13-test-app','carpool-v13-test-mock')) {
        $container = (& $runtimeDocker inspect $name | ConvertFrom-Json)[0]
        $networks = @($container.NetworkSettings.Networks.PSObject.Properties)
        if ($LASTEXITCODE -ne 0 -or $container.Id -ne $startup.$name -or $networks.Count -ne 1 -or $networks[0].Name -ne $runtimeNetwork) { throw 'Runtime resource changed.' }
    }
} catch { $isolation = 'failed' }
$overall = if (-not $runError -and $report.failed -eq 0 -and $isolation -eq 'passed' -and $conservation['copied-records'] -eq 'passed' -and $conservation['copied-announcements'] -eq 'passed') { 'passed' } else { 'failed' }
$safeReport = [ordered]@{task=$runtimeTask; suite=$Suite; source='synthetic-local-only'; overall_status=$overall; code_evidence_sha256=$evidenceHash; image_id=$startup.ImageId; config_sha256=$startup.ConfigHash; environment_sha256=$startup.AppEnvironmentHash; network_id=$startup.NetworkId; checkout_scope='immutable-reviewed-image-not-later-source-edits'; driver_source_sha256=$driverSourceHash; driver_test_sha256=$driverTestHash; driver_binary_sha256=$driverBinaryHash; completed_at=[DateTimeOffset]::UtcNow.ToString('o'); passed=$report.passed; failed=$report.failed; isolation=$isolation; conservation=$conservation; assertions=$safeAssertions}
if ($Suite -eq 'reset') {
    $safeReport.excluded_checks = $scopeExclusions
    $safeReport.scenario = if ([string]::IsNullOrEmpty($report.scenario)) { 'setup_failed' } else { $report.scenario }
    $safeReport.copied_user_term_preflight = 'zero'
}
$evidencePath = Join-Path $runtimeTaskDirectory ("evidence/source-runtime-$Suite-" + [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') + '.json')
Write-PrivateRuntimeJSON -Path $evidencePath -Value $safeReport
$runState.Status = $overall
$runState.Evidence = $evidencePath
$batchFixturePath = Join-Path $runDirectory 'results/batch-fixtures.json'
if (Test-Path -LiteralPath $batchFixturePath) { $runState.BatchFixtureSha256 = (Get-FileHash -LiteralPath $batchFixturePath).Hash }
Write-PrivateRuntimeJSON -Path $runStatePath -Value $runState
[pscustomobject]@{Passed=$report.passed; Failed=$report.failed; Evidence=$evidencePath; PrivateReport=$reportPath}
if ($overall -ne 'passed') { throw 'Runtime acceptance or its isolation/conservation postconditions failed; use the redacted evidence to route fixes.' }

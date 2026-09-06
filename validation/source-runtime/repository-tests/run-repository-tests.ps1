param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$RunPattern,

    [ValidateRange(1, 10)]
    [int]$Count = 1
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = 'C:/WORK-SPACE/sub2api-carpool-v1.3'
$backendRoot = Join-Path $repoRoot 'backend'
$sourceRoot = Join-Path $repoRoot 'validation/source-runtime/repository-tests'
$privateRoot = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905/repository-tests'
$goBinary = 'C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$dockerBinary = (Get-Command docker.exe).Source
$networkName = 'carpool-v13-test-net'
$postgresName = 'carpool-v13-integration-postgres'
$postgresAlias = 'carpool-integration-db'
$redisName = 'carpool-v13-integration-redis'
$redisAlias = 'carpool-integration-redis'
$runnerName = 'carpool-v13-integration-runner'
$volumeName = 'carpool-v13-integration-pgdata'
$databaseName = 'sub2api_carpool_integration_20260905'
$databaseUser = 'carpool_integration'
$taskLabel = '09-05-sub2api-carpool-v1-3'
$roleLabel = 'repository-tests'
$activeManifest = Join-Path $privateRoot 'active-run.json'

if ($RunPattern.Length -gt 512 -or $RunPattern.Contains("`r") -or $RunPattern.Contains("`n")) {
    throw 'RunPattern must be a single bounded Go test regular expression.'
}
if (-not (Test-Path -LiteralPath $goBinary -PathType Leaf)) {
    throw 'Pinned Go 1.27 binary is unavailable.'
}

function Invoke-Process {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][AllowEmptyString()][string[]]$Arguments,
        [string]$WorkingDirectory = $repoRoot,
        [hashtable]$Environment = @{},
        [string]$StandardInputFile = ''
    )
    if (-not [string]::IsNullOrWhiteSpace($StandardInputFile)) {
        $inputFile = Get-Item -LiteralPath $StandardInputFile
        if ($inputFile.PSIsContainer) { throw 'Process standard input must be a regular file.' }
        $StandardInputFile = $inputFile.FullName
    }
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $Executable
    $startInfo.WorkingDirectory = $WorkingDirectory
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.RedirectStandardInput = -not [string]::IsNullOrWhiteSpace($StandardInputFile)
    foreach ($argument in $Arguments) { $startInfo.ArgumentList.Add($argument) }
    foreach ($name in $Environment.Keys) { $startInfo.Environment[$name] = [string]$Environment[$name] }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    [void]$process.Start()
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    if ($startInfo.RedirectStandardInput) {
        $inputStream = [IO.File]::OpenRead($StandardInputFile)
        try {
            $inputStream.CopyTo($process.StandardInput.BaseStream)
        } finally {
            $inputStream.Dispose()
            $process.StandardInput.Close()
        }
    }
    $process.WaitForExit()
    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        StdOut = $stdoutTask.GetAwaiter().GetResult()
        StdErr = $stderrTask.GetAwaiter().GetResult()
    }
}

function Invoke-Docker {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string[]]$Arguments, [switch]$AllowFailure)
    $result = Invoke-Process -Executable $dockerBinary -Arguments $Arguments
    if (-not $AllowFailure -and $result.ExitCode -ne 0) {
        throw "Docker command failed at operation '$($Arguments[0])'."
    }
    return $result
}

function Write-PrivateText {
    param([string]$Path, [string]$Value)
    [IO.File]::WriteAllText($Path, $Value, [Text.UTF8Encoding]::new($false))
}

function New-RandomHex {
    param([int]$Bytes)
    $buffer = [byte[]]::new($Bytes)
    [Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return [Convert]::ToHexString($buffer).ToLowerInvariant()
}

function Assert-ResourceAbsent {
    foreach ($name in @($postgresName, $redisName, $runnerName)) {
        $inspection = Invoke-Docker -Arguments @('container', 'inspect', $name) -AllowFailure
        if ($inspection.ExitCode -eq 0) {
            throw "Existing container '$name' blocks a fresh run; refusing to overwrite it."
        }
    }
    $volumeInspection = Invoke-Docker -Arguments @('volume', 'inspect', $volumeName) -AllowFailure
    if ($volumeInspection.ExitCode -eq 0) {
        throw "Existing volume '$volumeName' blocks a fresh run; refusing to overwrite it."
    }
    if (Test-Path -LiteralPath $activeManifest) {
        throw 'An active private run manifest exists; use the bounded cleanup script after inspection.'
    }
}

function Assert-ImagePresent {
    param([string]$Image)
    $inspection = Invoke-Docker -Arguments @('image', 'inspect', $Image) -AllowFailure
    if ($inspection.ExitCode -ne 0) {
        throw "Required local image '$Image' is unavailable; this isolated run will not pull it."
    }
}

function Assert-ContainerIsolation {
    param([string]$Name, [string]$ExpectedID, [string]$ExpectedAlias, [string]$ExpectedRunID)
    $inspection = Invoke-Docker -Arguments @('container', 'inspect', $Name)
    $container = @($inspection.StdOut | ConvertFrom-Json)[0]
    if ($container.Id -ne $ExpectedID) { throw "Container identity changed for '$Name'." }
    if ($container.Config.Labels.'com.codex.local-task' -ne $taskLabel -or
        $container.Config.Labels.'com.codex.test-role' -ne $roleLabel -or
        $container.Config.Labels.'com.codex.run-id' -ne $ExpectedRunID) {
        throw "Container label mismatch for '$Name'."
    }
    $networks = @($container.NetworkSettings.Networks.PSObject.Properties)
    if ($networks.Count -ne 1 -or $networks[0].Name -ne $networkName) { throw "Container '$Name' is not isolated to the task network." }
    if ($networks[0].Value.Aliases -notcontains $ExpectedAlias) { throw "Container '$Name' lacks its exact synthetic alias." }
    if (@($container.HostConfig.PortBindings.PSObject.Properties).Count -ne 0) { throw "Container '$Name' unexpectedly publishes a host port." }
    if (($container.Mounts | ConvertTo-Json -Depth 8) -match 'docker\.sock') { throw "Container '$Name' unexpectedly mounts Docker control state." }
}

function Wait-ContainerProbe {
    param([string]$Name, [string[]]$ProbeArguments)
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds(60)
    do {
        $probe = Invoke-Docker -Arguments (@('exec', $Name) + $ProbeArguments) -AllowFailure
        if ($probe.ExitCode -eq 0) { return }
        Start-Sleep -Milliseconds 500
    } while ([DateTimeOffset]::UtcNow -lt $deadline)
    throw "Container '$Name' did not become ready within 60 seconds."
}

function Remove-OwnedContainer {
    param([string]$Name, [string]$ExpectedID, [string]$ExpectedRunID)
    if ([string]::IsNullOrWhiteSpace($ExpectedID)) { return }
    $inspection = Invoke-Docker -Arguments @('container', 'inspect', $Name) -AllowFailure
    if ($inspection.ExitCode -ne 0) { return }
    $container = @($inspection.StdOut | ConvertFrom-Json)[0]
    if ($container.Id -ne $ExpectedID -or
        $container.Config.Labels.'com.codex.local-task' -ne $taskLabel -or
        $container.Config.Labels.'com.codex.test-role' -ne $roleLabel -or
        $container.Config.Labels.'com.codex.run-id' -ne $ExpectedRunID) {
        throw "Refusing to remove changed container '$Name'."
    }
    $removal = Invoke-Docker -Arguments @('container', 'rm', '--force', $ExpectedID) -AllowFailure
    if ($removal.ExitCode -ne 0) { throw "Failed to remove owned container '$Name'." }
}

function Remove-OwnedVolume {
    param([string]$ExpectedRunID)
    if ([string]::IsNullOrWhiteSpace($ExpectedRunID)) { return }
    $inspection = Invoke-Docker -Arguments @('volume', 'inspect', $volumeName) -AllowFailure
    if ($inspection.ExitCode -ne 0) { return }
    $volume = @($inspection.StdOut | ConvertFrom-Json)[0]
    if ($volume.Name -ne $volumeName -or $volume.Labels.'com.codex.local-task' -ne $taskLabel -or $volume.Labels.'com.codex.test-role' -ne $roleLabel -or $volume.Labels.'com.codex.run-id' -ne $ExpectedRunID) {
        throw "Refusing to remove changed volume '$volumeName'."
    }
    $removal = Invoke-Docker -Arguments @('volume', 'rm', $volumeName) -AllowFailure
    if ($removal.ExitCode -ne 0) { throw "Failed to remove owned volume '$volumeName'." }
}

$postgresID = ''
$redisID = ''
$runnerID = ''
$runID = ''
$runDirectory = ''
$postgresEnv = ''
$runnerEnv = ''
$resultCode = 1
$manifestWritten = $false

try {
    $contextInspection = Invoke-Docker -Arguments @('context', 'inspect')
    $dockerContext = @($contextInspection.StdOut | ConvertFrom-Json)[0]
    if ($dockerContext.Endpoints.docker.Host -ne 'npipe:////./pipe/docker_engine') { throw 'Expected the verified local Docker engine.' }
    $networkInspection = Invoke-Docker -Arguments @('network', 'inspect', $networkName)
    $network = @($networkInspection.StdOut | ConvertFrom-Json)[0]
    if (-not $network.Internal -or $network.Name -ne $networkName) { throw 'Repository tests require the exact internal task network.' }
    Assert-ResourceAbsent
    Assert-ImagePresent 'postgres:18-alpine'
    Assert-ImagePresent 'redis:8-alpine'
    Assert-ImagePresent 'alpine:3.20'

    $runID = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + (New-RandomHex -Bytes 4)
    $runDirectory = Join-Path $privateRoot "runs/$runID"
    [void](New-Item -ItemType Directory -Path $runDirectory -Force)
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $aclResult = Invoke-Process -Executable (Get-Command icacls.exe).Source -Arguments @($runDirectory, '/inheritance:r', '/grant:r', "${identity}:(OI)(CI)F", '/grant:r', 'SYSTEM:(OI)(CI)F', '/Q')
    if ($aclResult.ExitCode -ne 0) { throw 'Failed to restrict the private repository-test run directory.' }

    $password = New-RandomHex -Bytes 32
    $postgresEnv = Join-Path $runDirectory 'postgres.synthetic.env'
    $runnerEnv = Join-Path $runDirectory 'runner.synthetic.env'
    Write-PrivateText -Path $postgresEnv -Value "POSTGRES_DB=$databaseName`nPOSTGRES_USER=$databaseUser`nPOSTGRES_PASSWORD=$password`n"
    Write-PrivateText -Path $runnerEnv -Value "CARPOOL_INTEGRATION_PG_HOST=$postgresAlias`nCARPOOL_INTEGRATION_PG_PORT=5432`nCARPOOL_INTEGRATION_PG_DATABASE=$databaseName`nCARPOOL_INTEGRATION_PG_USER=$databaseUser`nCARPOOL_INTEGRATION_PG_PASSWORD=$password`nCARPOOL_INTEGRATION_REDIS_HOST=$redisAlias`nCARPOOL_INTEGRATION_REDIS_PORT=6379`nCARPOOL_INTEGRATION_RUN_ID=$runID`n"

    $patchedHarness = Join-Path $runDirectory 'integration_harness_test.go'
    $overlay = Join-Path $runDirectory 'overlay.json'
    $binaryDirectory = Join-Path $runDirectory 'runner-root'
    [void](New-Item -ItemType Directory -Path $binaryDirectory)
    $testBinary = Join-Path $binaryDirectory 'repository-tests'
    $virtualHelper = Join-Path $backendRoot 'internal/repository/source_runtime_isolated_testmain_test.go'
    $generatorTest = Invoke-Process -Executable $goBinary -WorkingDirectory $backendRoot -Arguments @(
        'test', '../validation/source-runtime/repository-tests/prepare-overlay.go',
        '../validation/source-runtime/repository-tests/prepare_overlay_test.go'
    )
    Write-PrivateText -Path (Join-Path $runDirectory 'overlay-generator-test.stdout.log') -Value $generatorTest.StdOut
    Write-PrivateText -Path (Join-Path $runDirectory 'overlay-generator-test.stderr.log') -Value $generatorTest.StdErr
    if ($generatorTest.ExitCode -ne 0) { throw 'Overlay generator tests failed; see private logs.' }

    $generator = Invoke-Process -Executable $goBinary -WorkingDirectory $backendRoot -Arguments @(
        'run', '../validation/source-runtime/repository-tests/prepare-overlay.go',
        '-source', (Join-Path $backendRoot 'internal/repository/integration_harness_test.go'),
        '-output', $patchedHarness,
        '-overlay', $overlay,
        '-helper', (Join-Path $sourceRoot 'source_runtime_testmain_test.go'),
        '-virtual', $virtualHelper
    )
    Write-PrivateText -Path (Join-Path $runDirectory 'overlay-generator.stdout.log') -Value $generator.StdOut
    Write-PrivateText -Path (Join-Path $runDirectory 'overlay-generator.stderr.log') -Value $generator.StdErr
    if ($generator.ExitCode -ne 0) { throw 'Overlay generation failed; see private logs.' }

    $compiler = Invoke-Process -Executable $goBinary -WorkingDirectory $backendRoot -Environment @{
        GOOS = 'linux'; GOARCH = 'amd64'; CGO_ENABLED = '0'; GOTOOLCHAIN = 'local'
    } -Arguments @('test', '-c', '-tags=integration', "-overlay=$overlay", '-o', $testBinary, './internal/repository')
    Write-PrivateText -Path (Join-Path $runDirectory 'compile.stdout.log') -Value $compiler.StdOut
    Write-PrivateText -Path (Join-Path $runDirectory 'compile.stderr.log') -Value $compiler.StdErr
    if ($compiler.ExitCode -ne 0) { throw 'Repository integration test binary did not compile; see private logs.' }

    $manifest = @{
        task = $taskLabel; run_id = $runID; network = $networkName; run_directory = $runDirectory
        containers = @{ postgres = ''; redis = ''; runner = '' }; volume = $volumeName
    }
    Write-PrivateText -Path $activeManifest -Value ($manifest | ConvertTo-Json -Depth 8)
    $manifestWritten = $true

    $volumeCreate = Invoke-Docker -Arguments @('volume', 'create', '--label', "com.codex.local-task=$taskLabel", '--label', "com.codex.test-role=$roleLabel", '--label', "com.codex.run-id=$runID", $volumeName)
    if ($volumeCreate.ExitCode -ne 0) { throw 'Failed to create the fresh PostgreSQL volume.' }

    $postgresRun = Invoke-Docker -Arguments @(
        'run', '--detach', '--name', $postgresName,
        '--label', "com.codex.local-task=$taskLabel", '--label', "com.codex.test-role=$roleLabel", '--label', "com.codex.run-id=$runID",
        '--network', $networkName, '--network-alias', $postgresAlias,
        '--env-file', $postgresEnv,
        '--mount', "type=volume,source=$volumeName,target=/var/lib/postgresql",
        '--health-cmd', "pg_isready -U $databaseUser -d $databaseName", '--health-interval', '1s', '--health-timeout', '3s', '--health-retries', '60',
        'postgres:18-alpine'
    )
    $postgresID = $postgresRun.StdOut.Trim()
    $manifest.containers.postgres = $postgresID
    Write-PrivateText -Path $activeManifest -Value ($manifest | ConvertTo-Json -Depth 8)
    Assert-ContainerIsolation -Name $postgresName -ExpectedID $postgresID -ExpectedAlias $postgresAlias -ExpectedRunID $runID

    $redisRun = Invoke-Docker -Arguments @(
        'run', '--detach', '--name', $redisName,
        '--label', "com.codex.local-task=$taskLabel", '--label', "com.codex.test-role=$roleLabel", '--label', "com.codex.run-id=$runID",
        '--network', $networkName, '--network-alias', $redisAlias,
        '--tmpfs', '/data:rw,noexec,nosuid,size=64m',
        'redis:8-alpine', 'redis-server', '--save', '', '--appendonly', 'no'
    )
    $redisID = $redisRun.StdOut.Trim()
    $manifest.containers.redis = $redisID
    Write-PrivateText -Path $activeManifest -Value ($manifest | ConvertTo-Json -Depth 8)
    Assert-ContainerIsolation -Name $redisName -ExpectedID $redisID -ExpectedAlias $redisAlias -ExpectedRunID $runID

    Wait-ContainerProbe -Name $postgresName -ProbeArguments @('pg_isready', '-U', $databaseUser, '-d', $databaseName)
    Wait-ContainerProbe -Name $redisName -ProbeArguments @('redis-cli', 'ping')

    $runnerCreate = Invoke-Docker -Arguments @(
        'create', '--name', $runnerName,
        '--label', "com.codex.local-task=$taskLabel", '--label', "com.codex.test-role=$roleLabel", '--label', "com.codex.run-id=$runID",
        '--network', $networkName, '--network-alias', $runnerName,
        '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
        '--user', '65534:65534', '--tmpfs', '/tmp:rw,noexec,nosuid,nodev,size=64m',
        '--tmpfs', '/work:rw,exec,nosuid,nodev,size=256m',
        '--env-file', $runnerEnv,
        'alpine:3.20', '/bin/sleep', '3600'
    )
    $runnerID = $runnerCreate.StdOut.Trim()
    $manifest.containers.runner = $runnerID
    Write-PrivateText -Path $activeManifest -Value ($manifest | ConvertTo-Json -Depth 8)
    Assert-ContainerIsolation -Name $runnerName -ExpectedID $runnerID -ExpectedAlias $runnerName -ExpectedRunID $runID

    [void](Invoke-Docker -Arguments @('start', $runnerName))
    $binaryInstall = Invoke-Process -Executable $dockerBinary -StandardInputFile $testBinary -Arguments @(
        'exec', '--interactive', $runnerName, '/bin/sh', '-c',
        'umask 077; cat > /work/repository-tests; chmod 500 /work/repository-tests'
    )
    Write-PrivateText -Path (Join-Path $runDirectory 'runner-install.stdout.log') -Value $binaryInstall.StdOut
    Write-PrivateText -Path (Join-Path $runDirectory 'runner-install.stderr.log') -Value $binaryInstall.StdErr
    if ($binaryInstall.ExitCode -ne 0) { throw 'Failed to install the repository test binary into runner tmpfs; see private logs.' }

    $testRun = Invoke-Process -Executable $dockerBinary -Arguments @(
        'exec', $runnerName, '/work/repository-tests', '-test.v', '-test.run', $RunPattern, "-test.count=$Count", '-test.timeout=20m'
    )
    Write-PrivateText -Path (Join-Path $runDirectory 'repository-test.stdout.log') -Value $testRun.StdOut
    Write-PrivateText -Path (Join-Path $runDirectory 'repository-test.stderr.log') -Value $testRun.StdErr
    $resultCode = $testRun.ExitCode
    $runEvents = @(($testRun.StdOut + "`n" + $testRun.StdErr) -split "`r?`n" | Where-Object { $_ -match '^=== RUN\s+' })
    $zeroTestsMatched = $resultCode -eq 0 -and $runEvents.Count -eq 0
    if ($zeroTestsMatched) { $resultCode = 1 }
    $summary = @{
        task = $taskLabel; run_id = $runID; network = $networkName; database = $databaseName
        postgres_image = 'postgres:18-alpine'; redis_image = 'redis:8-alpine'; runner_image = 'alpine:3.20'
        run_pattern = $RunPattern; count = $Count; verbose_run_events = $runEvents.Count; exit_code = $resultCode; completed_at = [DateTimeOffset]::UtcNow.ToString('o')
    }
    Write-PrivateText -Path (Join-Path $runDirectory 'summary.json') -Value ($summary | ConvertTo-Json -Depth 8)
    if ($zeroTestsMatched) { throw 'Repository integration test pattern matched no runnable tests; see private logs.' }
    if ($resultCode -ne 0) { throw 'Repository integration tests failed; see private logs.' }
    Write-Output "Repository integration tests passed. Private evidence: $runDirectory"
}
finally {
    $cleanupErrors = [Collections.Generic.List[string]]::new()
    foreach ($resource in @(
        @{ Name = $runnerName; ID = $runnerID },
        @{ Name = $redisName; ID = $redisID },
        @{ Name = $postgresName; ID = $postgresID }
    )) {
        try { Remove-OwnedContainer -Name $resource.Name -ExpectedID $resource.ID -ExpectedRunID $runID } catch { $cleanupErrors.Add($_.Exception.Message) }
    }
    try { Remove-OwnedVolume -ExpectedRunID $runID } catch { $cleanupErrors.Add($_.Exception.Message) }
    foreach ($path in @($postgresEnv, $runnerEnv)) {
        if (-not [string]::IsNullOrWhiteSpace($path) -and (Test-Path -LiteralPath $path)) { Remove-Item -LiteralPath $path -Force }
    }
    if ($manifestWritten -and $cleanupErrors.Count -eq 0 -and (Test-Path -LiteralPath $activeManifest)) {
        try {
            $ownedManifest = Get-Content -LiteralPath $activeManifest -Raw | ConvertFrom-Json
            if ($ownedManifest.run_id -ne $runID) {
                $cleanupErrors.Add('Refusing to remove a changed active repository-test manifest.')
            } else {
                Remove-Item -LiteralPath $activeManifest -Force
            }
        } catch {
            $cleanupErrors.Add('Failed to verify or remove the active repository-test manifest.')
        }
    }
    if ($cleanupErrors.Count -gt 0) {
        throw ('Owned resource cleanup failed: ' + ($cleanupErrors -join '; '))
    }
}

$ErrorActionPreference = 'Stop'
$privateDirectory = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
$containerName = 'carpool-v13-test-postgres'
$networkName = 'carpool-v13-test-net'
$markerPath = Join-Path $privateDirectory 'sanitization-complete.json'
if (Test-Path -LiteralPath $markerPath) { throw 'Sanitization is already complete; refusing to mutate test fixtures.' }
$dockerPath = (Get-Command docker.exe).Source
$context = (& $dockerPath context inspect | ConvertFrom-Json)[0]
if ($context.Endpoints.docker.Host -ne 'npipe:////./pipe/docker_engine') { throw 'Docker endpoint is not the verified local engine.' }
$network = (& $dockerPath network inspect $networkName | ConvertFrom-Json)[0]
if (-not $network.Internal) { throw 'Test network has external routing.' }
$connectedNames = @($network.Containers.PSObject.Properties.Value.Name)
if (@($connectedNames | Where-Object { $_ -notin @($containerName, 'carpool-v13-test-redis') }).Count -ne 0) { throw 'Unexpected application attached before sanitization.' }
$container = (& $dockerPath inspect $containerName | ConvertFrom-Json)[0]
if (@($container.NetworkSettings.Networks.PSObject.Properties).Count -ne 1 -or $container.HostConfig.PortBindings.PSObject.Properties.Count -gt 0) { throw 'Database isolation or port check failed.' }

function Invoke-PrivateDockerStep {
    param([string[]]$StepArguments, [string]$LogName)
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $dockerPath
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    foreach ($argument in $StepArguments) { $startInfo.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    [void]$process.Start()
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    $process.WaitForExit()
    [IO.File]::WriteAllText((Join-Path $privateDirectory "$LogName.stdout.log"), $stdoutTask.GetAwaiter().GetResult(), [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $privateDirectory "$LogName.stderr.log"), $stderrTask.GetAwaiter().GetResult(), [Text.UTF8Encoding]::new($false))
    if ($process.ExitCode -ne 0) { throw "Local step $LogName failed; private diagnostics retained." }
}

foreach ($sqlName in @('verify-local-copy-integrity.sql', 'sanitize-local-copy.sql')) {
    Invoke-PrivateDockerStep -StepArguments @('cp', (Join-Path $PSScriptRoot $sqlName), "${containerName}:/tmp/$sqlName") -LogName "copy-$sqlName"
}
$psqlArguments = @('exec', $containerName, 'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-U', 'carpool_test', '-d', 'carpool_test', '-f')
Invoke-PrivateDockerStep -StepArguments ($psqlArguments + '/tmp/verify-local-copy-integrity.sql') -LogName 'integrity-before'
Invoke-PrivateDockerStep -StepArguments ($psqlArguments + '/tmp/sanitize-local-copy.sql') -LogName 'sanitization'
Invoke-PrivateDockerStep -StepArguments ($psqlArguments + '/tmp/verify-local-copy-integrity.sql') -LogName 'integrity-after'
$before = @(Get-Content -LiteralPath (Join-Path $privateDirectory 'integrity-before.stdout.log') | Where-Object { $_ })
$after = @(Get-Content -LiteralPath (Join-Path $privateDirectory 'integrity-after.stdout.log') | Where-Object { $_ })
if (@(Compare-Object $before $after).Count -ne 0) { throw 'Independent integrity comparison failed; application access remains blocked.' }
$evidence = [ordered]@{
    Status = 'SANITIZED_AND_INTEGRITY_VERIFIED'
    VerifiedAt = [DateTimeOffset]::UtcNow.ToString('o')
    Container = $containerName
    InternalNetwork = $networkName
    VerifiedMetrics = $before.Count
    ScriptSha256 = (Get-FileHash -LiteralPath (Join-Path $PSScriptRoot 'sanitize-local-copy.sql') -Algorithm SHA256).Hash
    RawArchive = 'production-snapshot.dump.dpapi'
    RegistrationTimestamps = 'unchanged'
}
[IO.File]::WriteAllText($markerPath, ($evidence | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
[pscustomobject]$evidence

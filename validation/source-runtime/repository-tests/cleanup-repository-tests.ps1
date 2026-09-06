Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$privateRoot = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905/repository-tests'
$activeManifest = Join-Path $privateRoot 'active-run.json'
$dockerBinary = (Get-Command docker.exe).Source
$networkName = 'carpool-v13-test-net'
$taskLabel = '09-05-sub2api-carpool-v1-3'
$roleLabel = 'repository-tests'
$volumeName = 'carpool-v13-integration-pgdata'
$containerNames = @{
    runner = 'carpool-v13-integration-runner'
    redis = 'carpool-v13-integration-redis'
    postgres = 'carpool-v13-integration-postgres'
}

if (-not (Test-Path -LiteralPath $activeManifest -PathType Leaf)) {
    throw 'No active repository-test manifest exists; nothing is authorized for cleanup.'
}
$manifest = Get-Content -LiteralPath $activeManifest -Raw | ConvertFrom-Json
if ($manifest.task -ne $taskLabel -or $manifest.network -ne $networkName -or $manifest.volume -ne $volumeName) {
    throw 'Active repository-test manifest does not match this task.'
}
$runID = [string]$manifest.run_id
if ([string]::IsNullOrWhiteSpace($runID)) { throw 'Active repository-test manifest has no run ID.' }
if ($runID -notmatch '^\d{8}T\d{6}Z-[0-9a-f]{8}$') { throw 'Active repository-test manifest has an invalid run ID.' }
$resolvedRunDirectory = [IO.Path]::GetFullPath([string]$manifest.run_directory)
$expectedRunDirectory = [IO.Path]::GetFullPath((Join-Path $privateRoot "runs/$runID"))
if (-not $resolvedRunDirectory.Equals($expectedRunDirectory, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Active run directory does not match the manifest run ID.'
}

function Invoke-Docker {
    param([string[]]$Arguments, [switch]$AllowFailure)
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $dockerBinary
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $startInfo.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    [void]$process.Start()
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    $process.WaitForExit()
    $result = [pscustomobject]@{ ExitCode = $process.ExitCode; StdOut = $stdout.GetAwaiter().GetResult(); StdErr = $stderr.GetAwaiter().GetResult() }
    if (-not $AllowFailure -and $result.ExitCode -ne 0) { throw "Docker cleanup operation '$($Arguments[0])' failed." }
    return $result
}

$networkInspection = Invoke-Docker -Arguments @('network', 'inspect', $networkName)
$network = @($networkInspection.StdOut | ConvertFrom-Json)[0]
if (-not $network.Internal -or $network.Name -ne $networkName) { throw 'Task network is not the expected internal network.' }

foreach ($key in @('runner', 'redis', 'postgres')) {
    $name = $containerNames[$key]
    $expectedID = [string]$manifest.containers.$key
    if ([string]::IsNullOrWhiteSpace($expectedID)) { continue }
    $inspection = Invoke-Docker -Arguments @('container', 'inspect', $name) -AllowFailure
    if ($inspection.ExitCode -ne 0) { continue }
    $container = @($inspection.StdOut | ConvertFrom-Json)[0]
    $networks = @($container.NetworkSettings.Networks.PSObject.Properties)
    if ($container.Id -ne $expectedID -or
        $container.Config.Labels.'com.codex.local-task' -ne $taskLabel -or
        $container.Config.Labels.'com.codex.test-role' -ne $roleLabel -or
        $container.Config.Labels.'com.codex.run-id' -ne $runID -or
        $networks.Count -ne 1 -or $networks[0].Name -ne $networkName -or
        @($container.HostConfig.PortBindings.PSObject.Properties).Count -ne 0 -or
        (($container.Mounts | ConvertTo-Json -Depth 8) -match 'docker\.sock')) {
        throw "Refusing to remove container '$name' because its binding changed."
    }
    $removal = Invoke-Docker -Arguments @('container', 'rm', '--force', $expectedID) -AllowFailure
    if ($removal.ExitCode -ne 0) { throw "Failed to remove owned container '$name'." }
}

$volumeInspection = Invoke-Docker -Arguments @('volume', 'inspect', $volumeName) -AllowFailure
if ($volumeInspection.ExitCode -eq 0) {
    $volume = @($volumeInspection.StdOut | ConvertFrom-Json)[0]
    if ($volume.Name -ne $volumeName -or
        $volume.Labels.'com.codex.local-task' -ne $taskLabel -or
        $volume.Labels.'com.codex.test-role' -ne $roleLabel -or
        $volume.Labels.'com.codex.run-id' -ne $runID) {
        throw "Refusing to remove volume '$volumeName' because its binding changed."
    }
    $removal = Invoke-Docker -Arguments @('volume', 'rm', $volumeName) -AllowFailure
    if ($removal.ExitCode -ne 0) { throw "Failed to remove owned volume '$volumeName'." }
}

foreach ($name in @('postgres.synthetic.env', 'runner.synthetic.env')) {
    $path = Join-Path $resolvedRunDirectory $name
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
}
Remove-Item -LiteralPath $activeManifest -Force
Write-Output 'Removed only the manifest-bound repository-test resources.'

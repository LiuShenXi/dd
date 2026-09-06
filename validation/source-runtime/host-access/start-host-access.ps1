param(
    [ValidateRange(1,128)][int]$MaxConnections = 32
)
$ErrorActionPreference = 'Stop'
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime-common.ps1')

Assert-LocalRuntimeIsolation
$statePath = (Resolve-Path -LiteralPath (Join-Path $runtimeDirectory 'startup-state.json')).Path
$allowedRoot = [IO.Path]::GetFullPath($runtimeDirectory) + [IO.Path]::DirectorySeparatorChar
if (-not $statePath.StartsWith($allowedRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Startup state must remain inside the private runtime directory.'
}
$state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
if ($state.Task -cne $runtimeTask -or $state.RunNonce -cnotmatch '^[0-9a-f]{32}$' -or
    $state.NetworkId -cnotmatch '^[0-9a-f]{64}$' -or $state.ImageId -cnotmatch '^sha256:[0-9a-f]{64}$' -or
    $state.'carpool-v13-test-app' -cnotmatch '^[0-9a-f]{64}$' -or -not $state.Healthy -or
    -not $state.BuildComplete -or -not $state.ConfigInstalled -or -not $state.FixturesCopied) {
    throw 'A healthy immutable startup state is required for host access.'
}
$resolvedDocker = (Resolve-Path -LiteralPath $runtimeDocker).Path
if (-not [IO.Path]::IsPathFullyQualified($resolvedDocker) -or
    -not [IO.Path]::GetFileName($resolvedDocker).Equals('docker.exe', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'The verified local Docker CLI path is invalid.'
}
if (Get-NetTCPConnection -State Listen -LocalPort 38088 -ErrorAction SilentlyContinue) {
    throw 'Loopback port 38088 is already in use.'
}

$binaryDirectory = Join-Path $runtimeDirectory 'host-access'
[void](New-Item -ItemType Directory -Path $binaryDirectory -Force)
$binaryPath = Join-Path $binaryDirectory 'carpool-host-access.exe'
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',$binaryPath,'.') `
    -Name 'build-host-access' -WorkingDirectory $PSScriptRoot
& $binaryPath -docker $resolvedDocker -state $statePath -max-connections $MaxConnections
if ($LASTEXITCODE -ne 0) { throw 'Host access bridge stopped with an error.' }

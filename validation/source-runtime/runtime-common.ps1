$ErrorActionPreference = 'Stop'
$runtimePrivateRoot = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
$runtimeDirectory = Join-Path $runtimePrivateRoot 'runtime'
$runtimeRepository = 'C:/WORK-SPACE/sub2api-carpool-v1.3'
$runtimeGo = 'C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe'
$runtimeDocker = (Get-Command docker.exe).Source
$runtimeNetwork = 'carpool-v13-test-net'
$runtimeImage = 'sub2api-carpool:v1.3-local-acceptance'
$runtimeTask = '09-05-sub2api-carpool-v1-3'
$runtimeTaskDirectory = Join-Path $runtimeRepository ".trellis/tasks/$runtimeTask"
$runtimeLabel = 'com.codex.local-task'
$runtimeRunLabel = 'com.codex.local-run'

function Get-LocalRuntimeEnvironmentLines {
    return [string[]]@(
        'DATA_DIR=/app/data', 'CONFIG_FILE=/app/data/config.yaml', 'RUN_MODE=standard', 'TZ=Asia/Shanghai',
        'SERVER_PORT=8080', 'AUTO_SETUP=false', 'TOKEN_REFRESH_ENABLED=false',
        'GATEWAY_CN_PROVIDERS_BALANCE_CHECK_ENABLED=false', 'CHANNEL_MONITOR_V2_DISABLE_AGGREGATOR=1',
        'OPS_ENABLED=false', 'USAGE_CLEANUP_ENABLED=false', 'DASHBOARD_AGGREGATION_ENABLED=false',
        'BATCH_IMAGE_ENABLED=false', 'BATCH_IMAGE_QUEUE_ENABLED=false', 'DATABASE_USER_PLATFORM_QUOTA_FLUSHER_ENABLED=false',
        'HTTP_PROXY=', 'HTTPS_PROXY=', 'ALL_PROXY=', 'http_proxy=', 'https_proxy=', 'all_proxy=', 'NO_PROXY=*', 'no_proxy=*'
    )
}

function Assert-LocalRuntimeEnvironmentFile {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw 'Private runtime environment file is missing.' }
    $expected = @(Get-LocalRuntimeEnvironmentLines)
    $actual = @(Get-Content -LiteralPath $Path)
    if ($actual.Count -ne $expected.Count) { throw 'Private runtime environment is not the canonical local-only configuration.' }
    for ($index = 0; $index -lt $expected.Count; $index++) {
        if ($actual[$index] -cne $expected[$index]) { throw 'Private runtime environment is not the canonical local-only configuration.' }
    }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
}

function Assert-LocalRuntimeIsolation {
    $context = (& $runtimeDocker context inspect | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0 -or $context.Endpoints.docker.Host -ne 'npipe:////./pipe/docker_engine') { throw 'Expected verified local Docker engine.' }
    $network = (& $runtimeDocker network inspect $runtimeNetwork | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0 -or -not $network.Internal) { throw 'Expected internal-only test network.' }
    $gate = Get-Content -LiteralPath (Join-Path $runtimePrivateRoot 'sanitization-complete.json') -Raw | ConvertFrom-Json
    if ($gate.Status -ne 'SANITIZED_AND_INTEGRITY_VERIFIED' -or $gate.Container -ne 'carpool-v13-test-postgres' -or $gate.InternalNetwork -ne $runtimeNetwork -or $gate.VerifiedMetrics -ne 109) { throw 'Invalid sanitization evidence.' }
    $auxiliaryPeers = @('carpool-v13-integration-postgres','carpool-v13-integration-redis','carpool-v13-integration-runner','carpool-v13-test-acceptance')
    foreach ($peer in @($network.Containers.PSObject.Properties.Value)) {
        if ($peer.Name -notin (@('carpool-v13-test-postgres','carpool-v13-test-redis','carpool-v13-test-app','carpool-v13-test-mock') + $auxiliaryPeers)) { throw 'Unexpected peer attached to private test network.' }
        if ($peer.Name -in $auxiliaryPeers) {
            $auxiliary = (& $runtimeDocker inspect $peer.Name | ConvertFrom-Json)[0]
            $attached = @($auxiliary.NetworkSettings.Networks.PSObject.Properties)
            $publishedPorts = if ($null -eq $auxiliary.HostConfig.PortBindings) { @() } else { @($auxiliary.HostConfig.PortBindings.PSObject.Properties) }
            if ($LASTEXITCODE -ne 0 -or $auxiliary.Config.Labels.$runtimeLabel -ne $runtimeTask -or $attached.Count -ne 1 -or $attached[0].Name -ne $runtimeNetwork -or $publishedPorts.Count -ne 0 -or @($auxiliary.Mounts | Where-Object { $_.Source -match 'docker\.sock|docker_engine' }).Count -ne 0) { throw 'Auxiliary test peer has an unexpected owner, network, port or socket.' }
        }
    }
    $bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
    $binding = if (Test-Path -LiteralPath $bindingPath) { Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json } else { $null }
    if ($binding -and ($binding.Task -ne $runtimeTask -or $binding.NetworkId -ne $network.Id)) { throw 'Runtime binding no longer matches this task/network.' }
    foreach ($name in @('carpool-v13-test-postgres', 'carpool-v13-test-redis')) {
        $container = (& $runtimeDocker inspect $name | ConvertFrom-Json)[0]
        $networks = @($container.NetworkSettings.Networks.PSObject.Properties)
        if ($networks.Count -ne 1 -or $networks[0].Name -ne $runtimeNetwork) { throw 'Database or Redis has unexpected network.' }
        $publishedPorts = if ($null -eq $container.HostConfig.PortBindings) { @() } else { @($container.HostConfig.PortBindings.PSObject.Properties) }
        if ($publishedPorts.Count -ne 0) { throw 'Database or Redis has a host port.' }
        $alias = if ($name -eq 'carpool-v13-test-postgres') { 'carpool-db' } else { 'carpool-redis' }
        if ($networks[0].Value.Aliases -notcontains $alias) { throw 'Database or Redis alias is missing.' }
        if ($binding -and $binding.Containers.$name -ne $container.Id) { throw 'Database or Redis container differs from the sanitized binding.' }
    }
}

function Initialize-LocalRuntimeBinding {
    Assert-LocalRuntimeIsolation
    $bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
    if (Test-Path -LiteralPath $bindingPath) { throw 'Runtime binding already exists; refusing to rebind data.' }
    $network = (& $runtimeDocker network inspect $runtimeNetwork | ConvertFrom-Json)[0]
    $containers = @{}
    foreach ($name in @('carpool-v13-test-postgres','carpool-v13-test-redis')) {
        $containers[$name] = ((& $runtimeDocker inspect $name | ConvertFrom-Json)[0]).Id
    }
    $binding = @{Task=$runtimeTask; NetworkId=$network.Id; Containers=$containers; CreatedAt=[DateTimeOffset]::UtcNow.ToString('o'); ConfigSha256=(Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'config.yaml')).Hash; AppEnvSha256=(Assert-LocalRuntimeEnvironmentFile -Path (Join-Path $runtimeDirectory 'app.env'))}
    Write-PrivateRuntimeJSON -Path $bindingPath -Value $binding
}

function Get-LocalRuntimeToolingIdentity {
    $paths = @(
        'backend/resources/model-pricing/model_prices_and_context_window.json',
        'validation/source-runtime/bootstrap/main.go',
        'validation/source-runtime/Dockerfile.runtime',
        'validation/source-runtime/Dockerignore.runtime',
        'validation/source-runtime/mock/main.go'
    ) | Sort-Object -CaseSensitive
    $hasher = [Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA256)
    try {
        foreach ($relativePath in $paths) {
            $absolutePath = Join-Path $runtimeRepository $relativePath
            if (-not (Test-Path -LiteralPath $absolutePath -PathType Leaf)) { throw "Runtime tooling input is missing: $relativePath" }
            $contentHash = (Get-FileHash -LiteralPath $absolutePath -Algorithm SHA256).Hash
            $hasher.AppendData([Text.Encoding]::UTF8.GetBytes($relativePath + "`0" + $contentHash + "`n"))
        }
        return [Convert]::ToHexString($hasher.GetHashAndReset())
    } finally { $hasher.Dispose() }
}

function Get-LocalRuntimeCodeIdentity {
    $sourcePaths = @(& git -C $runtimeRepository -c core.quotepath=false ls-files --cached --others --exclude-standard -- backend frontend) | Sort-Object -CaseSensitive -Unique
    if ($LASTEXITCODE -ne 0 -or $sourcePaths.Count -eq 0) { throw 'Cannot identify application source files.' }
    $distRoot = Join-Path $runtimeRepository 'backend/internal/web/dist'
    if (-not (Test-Path -LiteralPath (Join-Path $distRoot 'index.html'))) { throw 'Embedded frontend build is missing.' }
    $distPaths = @(Get-ChildItem -LiteralPath $distRoot -Recurse -File | ForEach-Object { [IO.Path]::GetRelativePath($runtimeRepository, $_.FullName).Replace('\','/') }) | Sort-Object -CaseSensitive
    $identity = @{}
    foreach ($kind in @('SourceSha256','EmbeddedFrontendSha256')) {
        $paths = if ($kind -eq 'SourceSha256') { $sourcePaths } else { $distPaths }
        $hasher = [Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA256)
        try {
            foreach ($relativePath in $paths) {
                $absolutePath = Join-Path $runtimeRepository $relativePath
                $contentHash = if (Test-Path -LiteralPath $absolutePath -PathType Leaf) { (Get-FileHash -LiteralPath $absolutePath -Algorithm SHA256).Hash } else { 'MISSING' }
                $hasher.AppendData([Text.Encoding]::UTF8.GetBytes($relativePath + "`0" + $contentHash + "`n"))
            }
            $identity[$kind] = [Convert]::ToHexString($hasher.GetHashAndReset())
        } finally { $hasher.Dispose() }
    }
    return $identity
}

function Assert-DirectorCodeReady {
    param([string]$EvidencePath, [string]$RuntimeEvidenceHash)
    $resolved = (Resolve-Path -LiteralPath $EvidencePath).Path
    $evidenceRoot = [IO.Path]::GetFullPath((Join-Path $runtimeTaskDirectory 'evidence')) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolved.StartsWith($evidenceRoot, [StringComparison]::OrdinalIgnoreCase)) { throw 'Code-ready evidence must belong to this task evidence directory.' }
    $gate = Get-Content -LiteralPath $resolved -Raw | ConvertFrom-Json
    $evidenceHash = (Get-FileHash -LiteralPath $resolved).Hash
    if ($gate.task -ne $runtimeTask -or $gate.git_head -notmatch '^[0-9a-f]{40}$' -or $gate.backend_build -ne 'passed' -or $gate.frontend_build -ne 'passed' -or $gate.approved_for_local_start -ne $true -or $gate.source_sha256 -notmatch '^[0-9a-fA-F]{64}$' -or $gate.embedded_frontend_sha256 -notmatch '^[0-9a-fA-F]{64}$') { throw 'Director code-ready evidence is incomplete.' }
    if ($RuntimeEvidenceHash) {
        if ($RuntimeEvidenceHash -notmatch '^[0-9a-fA-F]{64}$' -or $RuntimeEvidenceHash -ne $evidenceHash) { throw 'Evidence does not match the immutable runtime build.' }
        return $evidenceHash
    }
    $head = & git -C $runtimeRepository rev-parse HEAD
    if ($LASTEXITCODE -ne 0 -or $gate.task -ne $runtimeTask -or $gate.git_head -ne $head -or $gate.backend_build -ne 'passed' -or $gate.frontend_build -ne 'passed' -or $gate.approved_for_local_start -ne $true) { throw 'Director code-ready evidence is incomplete or stale.' }
    $identity = Get-LocalRuntimeCodeIdentity
    if ($gate.source_sha256 -ne $identity.SourceSha256 -or $gate.embedded_frontend_sha256 -ne $identity.EmbeddedFrontendSha256) { throw 'Source or embedded frontend changed after director approval.' }
    return $evidenceHash
}

function Invoke-PrivateRuntimeCommand {
    param([string]$Executable, [string[]]$Arguments, [string]$Name, [string]$WorkingDirectory = $runtimeRepository, [hashtable]$Environment = @{}, [int[]]$ExpectedExitCodes = @(0), [string]$InputFile = '')
    if ($InputFile -and -not (Test-Path -LiteralPath $InputFile -PathType Leaf)) { throw "Runtime input for step $Name is unavailable." }
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $Executable
    $start.WorkingDirectory = $WorkingDirectory
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.RedirectStandardInput = [bool]$InputFile
    foreach ($argument in $Arguments) { $start.ArgumentList.Add($argument) }
    foreach ($key in $Environment.Keys) { $start.Environment[$key] = $Environment[$key] }
    $input = if ($InputFile) { [IO.FileStream]::new($InputFile, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read) } else { $null }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $start
    $inputFailed = $false
    try {
        [void]$process.Start()
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if ($input) {
            try { [void]$input.CopyToAsync($process.StandardInput.BaseStream).GetAwaiter().GetResult() } catch { $inputFailed = $true }
            finally { $process.StandardInput.Dispose() }
        }
        $process.WaitForExit()
        $stdoutText = $stdout.GetAwaiter().GetResult()
        $stderrText = $stderr.GetAwaiter().GetResult()
        $exitCode = $process.ExitCode
    } finally {
        if ($input) { $input.Dispose() }
        $process.Dispose()
    }
    [IO.File]::WriteAllText((Join-Path $runtimeDirectory "$Name.stdout.log"), $stdoutText, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $runtimeDirectory "$Name.stderr.log"), $stderrText, [Text.UTF8Encoding]::new($false))
    if ($inputFailed) { throw "Runtime step $Name input transfer failed; restricted diagnostics retained." }
    if ($ExpectedExitCodes -notcontains $exitCode) { throw "Runtime step $Name failed; restricted diagnostics retained." }
}

function Write-PrivateRuntimeJSON {
    param([string]$Path, [object]$Value)
    $temporary = $Path + '.pending-' + [Guid]::NewGuid().ToString('N')
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes(($Value | ConvertTo-Json -Depth 20))
    $stream = [IO.FileStream]::new($temporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $stream.Write($bytes, 0, $bytes.Length); $stream.Flush($true) } finally { $stream.Dispose() }
    [IO.File]::Move($temporary, $Path, $true)
}

function Write-PrivateRuntimeCreateOnlyJSON {
    param([string]$Path, [object]$Value)
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes(($Value | ConvertTo-Json -Depth 20))
    $stream = [IO.FileStream]::new($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $stream.Write($bytes, 0, $bytes.Length); $stream.Flush($true) } finally { $stream.Dispose() }
}

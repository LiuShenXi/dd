param(
    [Parameter(Mandatory=$true)][string]$CodeReadyEvidence,
    [string]$ResumeTransition = ''
)
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
. (Join-Path $PSScriptRoot 'rebuild/rebuild-lib.ps1')
. (Join-Path $PSScriptRoot 'local-mock-http/transition-lib.ps1')

$appName = 'carpool-v13-test-app'
$mockName = 'carpool-v13-test-mock'
$acceptanceName = 'carpool-v13-test-acceptance'
$transferName = 'carpool-v13-config-transfer'
$appVolume = 'carpool-v13-test-appdata'
$configPath = Join-Path $runtimeDirectory 'config.yaml'
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
$statePath = Join-Path $runtimeDirectory 'startup-state.json'
$readyPath = Join-Path $runtimeDirectory 'runtime-ready.json'
$environmentPath = Join-Path $runtimeDirectory 'app.env'
$fixturePath = Join-Path $runtimeDirectory 'fixtures.json'
$transitionRoot = Join-Path $runtimeDirectory 'config-transitions'

function Get-LocalMockHTTPDockerObject {
    param([Parameter(Mandatory=$true)][string[]]$Arguments, [switch]$AllowMissing)
    $output = & $runtimeDocker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) {
        if ($AllowMissing) { return $null }
        throw 'A required local Docker resource is unavailable.'
    }
    return ConvertFrom-RebuildDockerInspect -Output $output
}

function Assert-LocalMockHTTPContainerRoles {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$State,
        [bool]$RequireApplicationRunning = $true
    )
    $image = Get-LocalMockHTTPDockerObject -Arguments @('image','inspect',$State.ImageId)
    $baseEnvironment = Merge-RebuildEnvironment -Base @($image.Config.Env)
    $applicationEnvironment = Merge-RebuildEnvironment -Base @($image.Config.Env) -Overlay @(Get-LocalRuntimeEnvironmentLines)
    $app = Get-LocalMockHTTPDockerObject -Arguments @('inspect',$appName)
    Assert-RebuildContainerRole -Container $app -Role application -ExpectedId $State[$appName] -ExpectedImageId $State.ImageId `
        -ExpectedRunNonce $State.RunNonce -TaskLabelName $runtimeLabel -RunLabelName $runtimeRunLabel `
        -ExpectedTask $runtimeTask -ExpectedNetwork $runtimeNetwork -ExpectedAlias 'carpool-app' `
        -ExpectedEnvironment $applicationEnvironment -AppVolume $appVolume -RequireRunning:$RequireApplicationRunning
    $mock = Get-LocalMockHTTPDockerObject -Arguments @('inspect',$mockName)
    Assert-RebuildContainerRole -Container $mock -Role mock -ExpectedId $State[$mockName] -ExpectedImageId $State.ImageId `
        -ExpectedRunNonce $State.RunNonce -TaskLabelName $runtimeLabel -RunLabelName $runtimeRunLabel `
        -ExpectedTask $runtimeTask -ExpectedNetwork $runtimeNetwork -ExpectedAlias 'carpool-mock' `
        -ExpectedEnvironment $baseEnvironment -AppVolume $appVolume
    return @{App=$app;Mock=$mock}
}

function Assert-LocalMockHTTPHealthyState {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$State,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Binding,
        [Parameter(Mandatory=$true)][string]$ConfigHash,
        [Parameter(Mandatory=$true)][string]$EnvironmentHash,
        [Parameter(Mandatory=$true)][string]$FixtureHash,
        [Parameter(Mandatory=$true)][string]$ToolingHash
    )
    if ($State.Task -cne $runtimeTask -or $Binding.Task -cne $runtimeTask -or $State.RunNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $State[$appName] -cnotmatch '^[0-9a-f]{64}$' -or $State[$mockName] -cnotmatch '^[0-9a-f]{64}$' -or
        $State.ImageId -cnotmatch '^sha256:[0-9a-f]{64}$' -or $State.NetworkId -cne $Binding.NetworkId -or
        $State.ConfigHash -cne $ConfigHash -or $Binding.ConfigSha256 -cne $ConfigHash -or
        $State.AppEnvironmentHash -cne $EnvironmentHash -or $Binding.AppEnvSha256 -cne $EnvironmentHash -or
        $State.FixtureSha256 -cne $FixtureHash -or $State.ToolingHash -cne $ToolingHash -or
        -not $State.BuildComplete -or -not $State.ConfigInstalled -or -not $State.FixturesCopied -or -not $State.Healthy) {
        throw 'Healthy startup resources do not match sealed configuration, environment, fixtures, or tooling.'
    }
}

function Assert-LocalMockHTTPHealthyReady {
    param([Parameter(Mandatory=$true)][Collections.IDictionary]$Ready)
    $properties = @($Ready.Keys | Sort-Object -CaseSensitive)
    $expected = @('Acceptance','App','Credentials','HostAccess','Mock','Network','StartedAt','Status','URL') | Sort-Object -CaseSensitive
    $startedAt = [DateTimeOffset]::MinValue
    if (@(Compare-Object -ReferenceObject $expected -DifferenceObject $properties -CaseSensitive).Count -ne 0 -or
        $Ready.Status -cne 'LOCAL_RUNTIME_HEALTHY' -or $Ready.URL -cne 'http://127.0.0.1:38088' -or
        $Ready.HostAccess -cne 'requires-separate-loopback-verification' -or $Ready.Network -cne $runtimeNetwork -or
        $Ready.App -cne $appName -or $Ready.Mock -cne $mockName -or $Ready.Credentials -cne 'private runtime/fixtures.json' -or
        $Ready.Acceptance -cne 'pending' -or -not [DateTimeOffset]::TryParse([string]$Ready.StartedAt, [ref]$startedAt)) {
        throw 'Runtime ready state is not the exact healthy local runtime projection.'
    }
}

function Save-LocalMockHTTPManifest {
    param([Parameter(Mandatory=$true)][string]$Path, [Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest)
    $Manifest.UpdatedAt = [DateTimeOffset]::UtcNow.ToString('o')
    Write-PrivateRuntimeJSON -Path $Path -Value $Manifest
}

function Assert-LocalMockHTTPManifest {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest,
        [Parameter(Mandatory=$true)][string]$ManifestPath,
        [Parameter(Mandatory=$true)][string]$EvidencePath,
        [Parameter(Mandatory=$true)][string]$EvidenceHash
    )
    $directory = [IO.Path]::GetDirectoryName($ManifestPath)
    $expectedPaths = [ordered]@{
        OriginalConfigPath = Join-Path $directory 'original-config.json'
        OriginalBindingPath = Join-Path $directory 'original-binding.json'
        OriginalStartupPath = Join-Path $directory 'original-startup.json'
        OriginalReadyPath = Join-Path $directory 'original-ready.json'
        IntendedConfigPath = Join-Path $directory 'intended-config.json'
        IntendedBindingPath = Join-Path $directory 'intended-binding.json'
        IntendedStartupPath = Join-Path $directory 'intended-startup.json'
        IntendedReadyPath = Join-Path $directory 'intended-ready.json'
    }
    foreach ($entry in $expectedPaths.GetEnumerator()) {
        $recorded = [string]$Manifest[$entry.Key]
        if ([string]::IsNullOrWhiteSpace($recorded) -or
            -not [IO.Path]::GetFullPath($recorded).Equals([IO.Path]::GetFullPath($entry.Value), [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Configuration transition archive paths changed.'
        }
        $hashName = $entry.Key.Replace('Path','Sha256')
        if (-not (Test-Path -LiteralPath $recorded -PathType Leaf) -or
            (Get-FileHash -LiteralPath $recorded -Algorithm SHA256).Hash -cne $Manifest[$hashName]) {
            throw 'Configuration transition archive is incomplete or changed.'
        }
    }
    if ($Manifest.Version -ne 1 -or $Manifest.Task -cne $runtimeTask -or $Manifest.ManifestPath -cne $ManifestPath -or
        $Manifest.TransitionNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $Manifest.Phase -notin @('Prepared','AppStopped','ContinuationCommitted','Completed') -or
        $Manifest.CodeReadyEvidence -cne $EvidencePath -or $Manifest.CodeEvidenceHash -cne $EvidenceHash -or
        $Manifest.CodeEvidenceHash -cnotmatch '^[0-9A-Fa-f]{64}$' -or $Manifest.ToolingHash -cnotmatch '^[0-9A-Fa-f]{64}$' -or
        $Manifest.AppEnvironmentSha256 -cnotmatch '^[0-9A-Fa-f]{64}$' -or $Manifest.FixtureSha256 -cnotmatch '^[0-9A-Fa-f]{64}$' -or
        $Manifest.AppId -cnotmatch '^[0-9a-f]{64}$' -or $Manifest.MockId -cnotmatch '^[0-9a-f]{64}$' -or
        $Manifest.ImageId -cnotmatch '^sha256:[0-9a-f]{64}$' -or $Manifest.RunNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $Manifest.NetworkId -cnotmatch '^[0-9a-f]{64}$' -or $Manifest.OriginalConfigSha256 -cnotmatch '^[0-9A-Fa-f]{64}$' -or
        $Manifest.IntendedConfigSha256 -cnotmatch '^[0-9A-Fa-f]{64}$' -or $Manifest.OriginalConfigSha256 -ceq $Manifest.IntendedConfigSha256) {
        throw 'Configuration transition manifest does not match the approved runtime.'
    }
    $originalConfiguration = Get-Content -LiteralPath $Manifest.OriginalConfigPath -Raw | ConvertFrom-Json -AsHashtable
    $intendedConfiguration = Get-Content -LiteralPath $Manifest.IntendedConfigPath -Raw | ConvertFrom-Json -AsHashtable
    $projectedConfiguration = New-LocalMockHTTPConfiguration -Configuration $originalConfiguration
    if (@(Get-LocalMockHTTPDifferencePaths -Before $projectedConfiguration -After $intendedConfiguration).Count -ne 0) {
        throw 'Archived intended configuration is not the single reviewed URL-policy mutation.'
    }
    $originalBinding = Get-Content -LiteralPath $Manifest.OriginalBindingPath -Raw | ConvertFrom-Json -AsHashtable
    $intendedBinding = Get-Content -LiteralPath $Manifest.IntendedBindingPath -Raw | ConvertFrom-Json -AsHashtable
    $projectedBinding = New-LocalMockHTTPBinding -Binding $originalBinding -ConfigHash $Manifest.IntendedConfigSha256
    if ($originalBinding.ConfigSha256 -cne $Manifest.OriginalConfigSha256 -or
        @(Get-LocalMockHTTPDifferencePaths -Before $projectedBinding -After $intendedBinding).Count -ne 0) {
        throw 'Archived intended binding is inconsistent with the configuration mutation.'
    }
    $originalState = Get-Content -LiteralPath $Manifest.OriginalStartupPath -Raw | ConvertFrom-Json -AsHashtable
    $intendedState = Get-Content -LiteralPath $Manifest.IntendedStartupPath -Raw | ConvertFrom-Json -AsHashtable
    $projectedState = New-LocalMockHTTPStartupState -State $originalState -ConfigHash $Manifest.IntendedConfigSha256
    if ($originalState.ConfigHash -cne $Manifest.OriginalConfigSha256 -or $originalState.CodeEvidenceHash -cne $Manifest.CodeEvidenceHash -or
        $originalState.AppEnvironmentHash -cne $Manifest.AppEnvironmentSha256 -or $originalState.FixtureSha256 -cne $Manifest.FixtureSha256 -or
        $originalState.ToolingHash -cne $Manifest.ToolingHash -or $originalState.NetworkId -cne $Manifest.NetworkId -or
        @(Get-LocalMockHTTPDifferencePaths -Before $projectedState -After $intendedState).Count -ne 0) {
        throw 'Archived intended startup state is inconsistent with the configuration mutation.'
    }
    Assert-LocalMockHTTPHealthyReady -Ready (Get-Content -LiteralPath $Manifest.OriginalReadyPath -Raw | ConvertFrom-Json -AsHashtable)
    $intendedReady = Get-Content -LiteralPath $Manifest.IntendedReadyPath -Raw | ConvertFrom-Json -AsHashtable
    $readyProperties = @($intendedReady.Keys | Sort-Object -CaseSensitive)
    $expectedReadyProperties = @('Status','Transition','UpdatedAt') | Sort-Object -CaseSensitive
    $readyUpdatedAt = [DateTimeOffset]::MinValue
    if (@(Compare-Object -ReferenceObject $expectedReadyProperties -DifferenceObject $readyProperties -CaseSensitive).Count -ne 0 -or
        $intendedReady.Status -cne 'LOCAL_RUNTIME_CONFIG_TRANSITION_PENDING' -or
        $intendedReady.Transition -cne $ManifestPath -or
        -not [DateTimeOffset]::TryParse([string]$intendedReady.UpdatedAt, [ref]$readyUpdatedAt)) {
        throw 'Archived intended ready state is invalid.'
    }
}

function Assert-LocalMockHTTPCurrentInputs {
    param([Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest)
    $environmentHash = Assert-LocalRuntimeEnvironmentFile -Path $environmentPath
    $fixtureHash = (Get-FileHash -LiteralPath $fixturePath -Algorithm SHA256).Hash
    if ($environmentHash -cne $Manifest.AppEnvironmentSha256 -or $fixtureHash -cne $Manifest.FixtureSha256 -or
        (Get-LocalRuntimeToolingIdentity) -cne $Manifest.ToolingHash -or
        (Get-LocalMockHTTPDockerObject -Arguments @('image','inspect',$runtimeImage)).Id -cne $Manifest.ImageId) {
        throw 'Runtime environment, fixtures, tooling, or immutable image changed during configuration transition.'
    }
}

Assert-LocalRuntimeIsolation
$containerNames = @(& $runtimeDocker ps -a --format '{{.Names}}')
if ($containerNames -contains $acceptanceName) { throw 'Acceptance runner is active or retained; configuration transition is forbidden.' }
$resolvedEvidence = (Resolve-Path -LiteralPath $CodeReadyEvidence).Path

if ($ResumeTransition) {
    $resolvedManifest = (Resolve-Path -LiteralPath $ResumeTransition).Path
    $allowedRoot = [IO.Path]::GetFullPath($transitionRoot) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedManifest.StartsWith($allowedRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Transition manifest must belong to the private runtime configuration-transition directory.'
    }
    $manifest = Get-Content -LiteralPath $resolvedManifest -Raw | ConvertFrom-Json -AsHashtable
    $evidenceHash = Assert-DirectorCodeReady -EvidencePath $resolvedEvidence -RuntimeEvidenceHash $manifest.CodeEvidenceHash
    Assert-LocalMockHTTPManifest -Manifest $manifest -ManifestPath $resolvedManifest -EvidencePath $resolvedEvidence -EvidenceHash $evidenceHash
    if ($manifest.Phase -eq 'Completed') { throw 'This configuration transition is complete; destructive replay is forbidden.' }
    if ($manifest.Phase -ne 'ContinuationCommitted' -and $containerNames -contains $transferName) {
        throw 'A configuration transfer container exists before the delegated startup recovery phase.'
    }
} else {
    if ($containerNames -contains $transferName) { throw 'A configuration transfer container is already retained.' }
    foreach ($requiredPath in @($configPath,$bindingPath,$statePath,$readyPath,$environmentPath,$fixturePath)) {
        if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) { throw 'A required healthy runtime artifact is missing.' }
    }
    $configuration = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json -AsHashtable
    $binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json -AsHashtable
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
    $ready = Get-Content -LiteralPath $readyPath -Raw | ConvertFrom-Json -AsHashtable
    $configHash = (Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash
    $environmentHash = Assert-LocalRuntimeEnvironmentFile -Path $environmentPath
    $fixtureHash = (Get-FileHash -LiteralPath $fixturePath -Algorithm SHA256).Hash
    $toolingHash = Get-LocalRuntimeToolingIdentity
    Assert-LocalMockHTTPHealthyState -State $state -Binding $binding -ConfigHash $configHash `
        -EnvironmentHash $environmentHash -FixtureHash $fixtureHash -ToolingHash $toolingHash
    Assert-LocalMockHTTPHealthyReady -Ready $ready
    $evidenceHash = Assert-DirectorCodeReady -EvidencePath $resolvedEvidence -RuntimeEvidenceHash $state.CodeEvidenceHash
    $roles = Assert-LocalMockHTTPContainerRoles -State $state
    if ((Get-LocalMockHTTPDockerObject -Arguments @('image','inspect',$runtimeImage)).Id -cne $state.ImageId) {
        throw 'Runtime image tag changed after the immutable application build.'
    }

    $intendedConfiguration = New-LocalMockHTTPConfiguration -Configuration $configuration
    $transitionNonce = [Guid]::NewGuid().ToString('N')
    [void](New-Item -ItemType Directory -Path $transitionRoot -Force)
    $archiveDirectory = Join-Path $transitionRoot ("mock-http-" + [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') + "-$transitionNonce")
    [void](New-Item -ItemType Directory -Path $archiveDirectory)
    $originalPaths = [ordered]@{
        Config = Join-Path $archiveDirectory 'original-config.json'
        Binding = Join-Path $archiveDirectory 'original-binding.json'
        Startup = Join-Path $archiveDirectory 'original-startup.json'
        Ready = Join-Path $archiveDirectory 'original-ready.json'
    }
    Copy-LocalMockHTTPCreateOnlyFile -Source $configPath -Destination $originalPaths.Config
    Copy-LocalMockHTTPCreateOnlyFile -Source $bindingPath -Destination $originalPaths.Binding
    Copy-LocalMockHTTPCreateOnlyFile -Source $statePath -Destination $originalPaths.Startup
    Copy-LocalMockHTTPCreateOnlyFile -Source $readyPath -Destination $originalPaths.Ready
    $intendedConfigPath = Join-Path $archiveDirectory 'intended-config.json'
    Write-LocalMockHTTPCreateOnlyJSON -Path $intendedConfigPath -Value $intendedConfiguration
    $intendedConfigHash = (Get-FileHash -LiteralPath $intendedConfigPath -Algorithm SHA256).Hash
    $intendedBinding = New-LocalMockHTTPBinding -Binding $binding -ConfigHash $intendedConfigHash
    $intendedState = New-LocalMockHTTPStartupState -State $state -ConfigHash $intendedConfigHash
    $intendedReady = [ordered]@{
        Status = 'LOCAL_RUNTIME_CONFIG_TRANSITION_PENDING'
        Transition = Join-Path $archiveDirectory 'transition-manifest.json'
        UpdatedAt = [DateTimeOffset]::UtcNow.ToString('o')
    }
    $intendedBindingPath = Join-Path $archiveDirectory 'intended-binding.json'
    $intendedStartupPath = Join-Path $archiveDirectory 'intended-startup.json'
    $intendedReadyPath = Join-Path $archiveDirectory 'intended-ready.json'
    Write-LocalMockHTTPCreateOnlyJSON -Path $intendedBindingPath -Value $intendedBinding
    Write-LocalMockHTTPCreateOnlyJSON -Path $intendedStartupPath -Value $intendedState
    Write-LocalMockHTTPCreateOnlyJSON -Path $intendedReadyPath -Value $intendedReady
    $manifestPath = Join-Path $archiveDirectory 'transition-manifest.json'
    $preparedAt = [DateTimeOffset]::UtcNow.ToString('o')
    $manifest = [ordered]@{
        Version = 1; Task = $runtimeTask; Phase = 'Prepared'; TransitionNonce = $transitionNonce
        ManifestPath = $manifestPath; CodeReadyEvidence = $resolvedEvidence; CodeEvidenceHash = $evidenceHash
        AppId = $state[$appName]; MockId = $state[$mockName]; ImageId = $state.ImageId
        RunNonce = $state.RunNonce; NetworkId = $state.NetworkId; AppEnvironmentSha256 = $environmentHash
        FixtureSha256 = $fixtureHash; ToolingHash = $toolingHash
        OriginalConfigPath = $originalPaths.Config; OriginalConfigSha256 = (Get-FileHash -LiteralPath $originalPaths.Config -Algorithm SHA256).Hash
        OriginalBindingPath = $originalPaths.Binding; OriginalBindingSha256 = (Get-FileHash -LiteralPath $originalPaths.Binding -Algorithm SHA256).Hash
        OriginalStartupPath = $originalPaths.Startup; OriginalStartupSha256 = (Get-FileHash -LiteralPath $originalPaths.Startup -Algorithm SHA256).Hash
        OriginalReadyPath = $originalPaths.Ready; OriginalReadySha256 = (Get-FileHash -LiteralPath $originalPaths.Ready -Algorithm SHA256).Hash
        IntendedConfigPath = $intendedConfigPath; IntendedConfigSha256 = $intendedConfigHash
        IntendedBindingPath = $intendedBindingPath; IntendedBindingSha256 = (Get-FileHash -LiteralPath $intendedBindingPath -Algorithm SHA256).Hash
        IntendedStartupPath = $intendedStartupPath; IntendedStartupSha256 = (Get-FileHash -LiteralPath $intendedStartupPath -Algorithm SHA256).Hash
        IntendedReadyPath = $intendedReadyPath; IntendedReadySha256 = (Get-FileHash -LiteralPath $intendedReadyPath -Algorithm SHA256).Hash
        PreparedAt = $preparedAt; UpdatedAt = $preparedAt
    }
    Write-LocalMockHTTPCreateOnlyJSON -Path $manifestPath -Value $manifest
    $resolvedManifest = $manifestPath
}

function Invoke-LocalMockHTTPTransition {
    Assert-LocalMockHTTPCurrentInputs -Manifest $manifest
    $originalState = Get-Content -LiteralPath $manifest.OriginalStartupPath -Raw | ConvertFrom-Json -AsHashtable
    if ($originalState.Task -cne $runtimeTask -or $originalState[$appName] -cne $manifest.AppId -or
        $originalState[$mockName] -cne $manifest.MockId -or $originalState.ImageId -cne $manifest.ImageId -or
        $originalState.RunNonce -cne $manifest.RunNonce -or $originalState.NetworkId -cne $manifest.NetworkId) {
        throw 'Archived startup identity does not match the transition manifest.'
    }
    $liveHashes = @{
        Config = (Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash
        Binding = (Get-FileHash -LiteralPath $bindingPath -Algorithm SHA256).Hash
        Startup = (Get-FileHash -LiteralPath $statePath -Algorithm SHA256).Hash
        Ready = (Get-FileHash -LiteralPath $readyPath -Algorithm SHA256).Hash
    }
    foreach ($kind in @('Config','Binding')) {
        if ($liveHashes[$kind] -cne $manifest["Original${kind}Sha256"] -and
            $liveHashes[$kind] -cne $manifest["Intended${kind}Sha256"]) {
            throw "Live $kind state is neither the archived original nor the intended transition state."
        }
    }

    if ($manifest.Phase -eq 'Prepared') {
        if ($liveHashes.Config -cne $manifest.OriginalConfigSha256 -or $liveHashes.Binding -cne $manifest.OriginalBindingSha256 -or
            $liveHashes.Startup -cne $manifest.OriginalStartupSha256 -or $liveHashes.Ready -cne $manifest.OriginalReadySha256) {
            throw 'Prepared transition cannot begin after runtime state changed.'
        }
        $roles = Assert-LocalMockHTTPContainerRoles -State $originalState -RequireApplicationRunning:$false
        if ($roles.App.State.Running) {
            Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('stop','--time','15',$manifest.AppId) `
                -Name "mock-http-$($manifest.TransitionNonce)-stop-app"
            $roles = Assert-LocalMockHTTPContainerRoles -State $originalState -RequireApplicationRunning:$false
        }
        if ($roles.App.State.Running) { throw 'Application did not stop before configuration transition.' }
        $manifest.Phase = 'AppStopped'
        Save-LocalMockHTTPManifest -Path $resolvedManifest -Manifest $manifest
    }

    if ($manifest.Phase -eq 'AppStopped') {
        if ($liveHashes.Startup -cne $manifest.OriginalStartupSha256 -and
            $liveHashes.Startup -cne $manifest.IntendedStartupSha256) {
            throw 'Live startup state is neither the archived original nor the intended transition state.'
        }
        if ($liveHashes.Ready -cne $manifest.OriginalReadySha256 -and $liveHashes.Ready -cne $manifest.IntendedReadySha256) {
            $expectedPendingReady = Get-Content -LiteralPath $manifest.IntendedReadyPath -Raw | ConvertFrom-Json -AsHashtable
            $actualPendingReady = Get-Content -LiteralPath $readyPath -Raw | ConvertFrom-Json -AsHashtable
            Assert-LocalMockHTTPPendingReadyEquivalent -Expected $expectedPendingReady -Actual $actualPendingReady
        }
        $roles = Assert-LocalMockHTTPContainerRoles -State $originalState -RequireApplicationRunning:$false
        if ($roles.App.State.Running) { throw 'Application restarted before configuration transition commit.' }
        $intendedConfiguration = Get-Content -LiteralPath $manifest.IntendedConfigPath -Raw | ConvertFrom-Json -AsHashtable
        $originalConfiguration = Get-Content -LiteralPath $manifest.OriginalConfigPath -Raw | ConvertFrom-Json -AsHashtable
        Assert-LocalMockHTTPDifferences -Before $originalConfiguration -After $intendedConfiguration `
            -ExpectedPaths @('security.url_allowlist.enabled')
        Copy-LocalMockHTTPAtomicFile -Source $manifest.IntendedConfigPath -Destination $configPath
        Copy-LocalMockHTTPAtomicFile -Source $manifest.IntendedBindingPath -Destination $bindingPath
        Copy-LocalMockHTTPAtomicFile -Source $manifest.IntendedStartupPath -Destination $statePath
        Copy-LocalMockHTTPAtomicFile -Source $manifest.IntendedReadyPath -Destination $readyPath
        foreach ($entry in @(
            @{Path=$configPath;Hash=$manifest.IntendedConfigSha256},
            @{Path=$bindingPath;Hash=$manifest.IntendedBindingSha256},
            @{Path=$statePath;Hash=$manifest.IntendedStartupSha256},
            @{Path=$readyPath;Hash=$manifest.IntendedReadySha256}
        )) {
            if ((Get-FileHash -LiteralPath $entry.Path -Algorithm SHA256).Hash -cne $entry.Hash) {
                throw 'Committed configuration continuation differs from the archived intent.'
            }
        }
        $manifest.Phase = 'ContinuationCommitted'
        Save-LocalMockHTTPManifest -Path $resolvedManifest -Manifest $manifest
    }

    if ($manifest.Phase -ne 'ContinuationCommitted') { throw 'Configuration transition phase cannot resume.' }
    if ((Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash -cne $manifest.IntendedConfigSha256 -or
        (Get-FileHash -LiteralPath $bindingPath -Algorithm SHA256).Hash -cne $manifest.IntendedBindingSha256) {
        throw 'Configuration or sealed binding changed after continuation commit.'
    }
    $continuation = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
    Assert-LocalMockHTTPResumeState -OriginalState $originalState -CurrentState $continuation -ConfigHash $manifest.IntendedConfigSha256
    $liveReadyHash = (Get-FileHash -LiteralPath $readyPath -Algorithm SHA256).Hash
    if ($liveReadyHash -cne $manifest.IntendedReadySha256) {
        Assert-LocalMockHTTPHealthyReady -Ready (Get-Content -LiteralPath $readyPath -Raw | ConvertFrom-Json -AsHashtable)
    }
    $roles = Assert-LocalMockHTTPContainerRoles -State $continuation -RequireApplicationRunning:$false
    $null = & (Join-Path $PSScriptRoot 'start-runtime.ps1') -CodeReadyEvidence $resolvedEvidence -Resume
    Assert-LocalMockHTTPCurrentInputs -Manifest $manifest
    if ((Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash -cne $manifest.IntendedConfigSha256) {
        throw 'Live configuration changed during delegated runtime resume.'
    }
    $finalState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
    $finalBinding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json -AsHashtable
    Assert-LocalMockHTTPHealthyState -State $finalState -Binding $finalBinding -ConfigHash $manifest.IntendedConfigSha256 `
        -EnvironmentHash $manifest.AppEnvironmentSha256 -FixtureHash $manifest.FixtureSha256 -ToolingHash $manifest.ToolingHash
    if ($finalState[$appName] -cne $manifest.AppId -or $finalState[$mockName] -cne $manifest.MockId -or
        $finalState.ImageId -cne $manifest.ImageId -or $finalState.RunNonce -cne $manifest.RunNonce) {
        throw 'Runtime resume changed immutable application resources.'
    }
    Assert-LocalMockHTTPHealthyReady -Ready (Get-Content -LiteralPath $readyPath -Raw | ConvertFrom-Json -AsHashtable)
    $null = Assert-LocalMockHTTPContainerRoles -State $finalState
    $manifest.Phase = 'Completed'
    $manifest.CompletedAt = [DateTimeOffset]::UtcNow.ToString('o')
    [void]$manifest.Remove('LastFailure')
    [void]$manifest.Remove('LastFailureAt')
    [void]$manifest.Remove('LastFailureDetail')
    [void]$manifest.Remove('ScriptStackTrace')
    Save-LocalMockHTTPManifest -Path $resolvedManifest -Manifest $manifest
    return [pscustomobject]@{Status='LOCAL_MOCK_HTTP_ENABLED';URL='http://127.0.0.1:38088';Manifest=$resolvedManifest}
}

try {
    Invoke-LocalMockHTTPTransition
} catch {
    if ($manifest -and $resolvedManifest -and (Test-Path -LiteralPath $resolvedManifest -PathType Leaf)) {
        $failureDetail = [string]$_.Exception.Message
        if ($failureDetail.Length -gt 2048) { $failureDetail = $failureDetail.Substring(0, 2048) }
        $failureStack = [string]$_.ScriptStackTrace
        if ($failureStack.Length -gt 4096) { $failureStack = $failureStack.Substring(0, 4096) }
        $manifest.LastFailure = switch ($manifest.Phase) {
            'Prepared' { 'application-stop-failed' }
            'AppStopped' { 'configuration-commit-failed' }
            'ContinuationCommitted' { 'runtime-resume-failed' }
            default { 'configuration-transition-incomplete' }
        }
        $manifest.LastFailureAt = [DateTimeOffset]::UtcNow.ToString('o')
        $manifest.LastFailureDetail = $failureDetail
        $manifest.ScriptStackTrace = $failureStack
        try { Save-LocalMockHTTPManifest -Path $resolvedManifest -Manifest $manifest } catch { }
        throw "Local mock HTTP transition is incomplete. Resume only with: pwsh -NoProfile -File validation/source-runtime/enable-local-mock-http.ps1 -CodeReadyEvidence '$resolvedEvidence' -ResumeTransition '$resolvedManifest'"
    }
    throw
}

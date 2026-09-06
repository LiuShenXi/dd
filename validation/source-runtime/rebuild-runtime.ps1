param(
    [Parameter(Mandatory=$true)][string]$CodeReadyEvidence,
    [string]$ResumeRebuild = ''
)
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
. (Join-Path $PSScriptRoot 'rebuild/rebuild-lib.ps1')

$appName = 'carpool-v13-test-app'
$mockName = 'carpool-v13-test-mock'
$acceptanceName = 'carpool-v13-test-acceptance'
$appVolume = 'carpool-v13-test-appdata'
$statePath = Join-Path $runtimeDirectory 'startup-state.json'
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
$fixturePath = Join-Path $runtimeDirectory 'fixtures.json'
$appEnvironmentPath = Join-Path $runtimeDirectory 'app.env'
$rebuildRoot = Join-Path $runtimeDirectory 'rebuilds'

function Get-RebuildDockerObject {
    param([Parameter(Mandatory=$true)][string[]]$Arguments, [switch]$AllowMissing)
    $output = & $runtimeDocker @Arguments 2>$null
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) {
        if ($AllowMissing) { return $null }
        throw 'A required local Docker resource is unavailable.'
    }
    return ConvertFrom-RebuildDockerInspect -Output $output
}

function Get-RebuildContainer {
    param([Parameter(Mandatory=$true)][string]$Name, [switch]$AllowMissing)
    return Get-RebuildDockerObject -Arguments @('inspect', $Name) -AllowMissing:$AllowMissing
}

function Get-RebuildImage {
    param([Parameter(Mandatory=$true)][string]$Reference)
    return Get-RebuildDockerObject -Arguments @('image', 'inspect', $Reference)
}

function Get-RebuildRoleDefinition {
    param([Parameter(Mandatory=$true)][ValidateSet('application','mock')][string]$Role)
    if ($Role -eq 'application') { return @{Name=$appName;Alias='carpool-app';Entrypoint='/app/sub2api'} }
    return @{Name=$mockName;Alias='carpool-mock';Entrypoint='/app/mock-upstream'}
}

function Assert-RebuildOwnedContainer {
    param(
        [Parameter(Mandatory=$true)][object]$Container,
        [Parameter(Mandatory=$true)][ValidateSet('application','mock')][string]$Role,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$State,
        [Parameter(Mandatory=$true)][string[]]$BaseEnvironment,
        [Parameter(Mandatory=$true)][string[]]$ApplicationEnvironment,
        [bool]$RequireRunning = $true
    )
    $definition = Get-RebuildRoleDefinition -Role $Role
    $expectedEnvironment = if ($Role -eq 'application') { $ApplicationEnvironment } else { $BaseEnvironment }
    Assert-RebuildContainerRole -Container $Container -Role $Role -ExpectedId $State[$definition.Name] `
        -ExpectedImageId $State.ImageId -ExpectedRunNonce $State.RunNonce -TaskLabelName $runtimeLabel `
        -RunLabelName $runtimeRunLabel -ExpectedTask $runtimeTask -ExpectedNetwork $runtimeNetwork `
        -ExpectedAlias $definition.Alias -ExpectedEnvironment $expectedEnvironment -AppVolume $appVolume `
        -RequireRunning:$RequireRunning
}

function Assert-RebuildStateCore {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$State,
        [Parameter(Mandatory=$true)][object]$Binding,
        [Parameter(Mandatory=$true)][string]$ConfigHash,
        [Parameter(Mandatory=$true)][string]$EnvironmentHash,
        [switch]$RequireHealthy
    )
    if ($State.Task -cne $runtimeTask -or $State.RunNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $State.NetworkId -cne $Binding.NetworkId -or $State.ConfigHash -cne $ConfigHash -or
        $State.AppEnvironmentHash -cne $EnvironmentHash -or $State.ConfigHash -cne $Binding.ConfigSha256 -or
        $State.AppEnvironmentHash -cne $Binding.AppEnvSha256 -or -not $State.ConfigInstalled -or
        -not $State.FixturesCopied -or -not $State.BuildComplete -or $State.FixtureSha256 -cnotmatch '^[0-9A-Fa-f]{64}$') {
        throw 'Startup state does not match the bound network, configuration, environment, or preserved fixtures.'
    }
    if ($RequireHealthy -and -not $State.Healthy) { throw 'Only a healthy reviewed runtime can begin a rebuild.' }
    if ((Get-FileHash -LiteralPath $fixturePath -Algorithm SHA256).Hash -cne $State.FixtureSha256) {
        throw 'Private synthetic fixtures changed after startup.'
    }
}

function Get-RebuildContainerMetadataHash {
    param([Parameter(Mandatory=$true)][object]$Container)
    $environmentHash = Get-RebuildObjectSha256 -Value @($Container.Config.Env | Sort-Object -CaseSensitive)
    $metadata = [ordered]@{
        Id = $Container.Id
        Image = $Container.Image
        Labels = $Container.Config.Labels
        EnvironmentSha256 = $environmentHash
        Entrypoint = @($Container.Config.Entrypoint)
        User = $Container.Config.User
        WorkingDirectory = $Container.Config.WorkingDir
        HostConfig = [ordered]@{
            NetworkMode = $Container.HostConfig.NetworkMode
            ReadonlyRootfs = $Container.HostConfig.ReadonlyRootfs
            Privileged = $Container.HostConfig.Privileged
            CapDrop = @($Container.HostConfig.CapDrop)
            CapAdd = @($Container.HostConfig.CapAdd)
            SecurityOpt = @($Container.HostConfig.SecurityOpt)
            PortBindings = $Container.HostConfig.PortBindings
            Tmpfs = $Container.HostConfig.Tmpfs
        }
        Mounts = @($Container.Mounts)
        Networks = $Container.NetworkSettings.Networks
    }
    return Get-RebuildObjectSha256 -Value $metadata
}

function Save-RebuildManifest {
    param([Parameter(Mandatory=$true)][string]$Path, [Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest)
    $Manifest.UpdatedAt = [DateTimeOffset]::UtcNow.ToString('o')
    Write-PrivateRuntimeJSON -Path $Path -Value $Manifest
}

function Assert-RebuildArchive {
    param([Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest)
    foreach ($entry in @(
        @{Path=$Manifest.PreviousStartupStatePath;Hash=$Manifest.PreviousStartupStateSha256},
        @{Path=$Manifest.ResourceMetadataPath;Hash=$Manifest.ResourceMetadataSha256},
        @{Path=$Manifest.IntendedContinuationPath;Hash=$Manifest.IntendedContinuationSha256},
        @{Path=$Manifest.BootstrapPath;Hash=$Manifest.BootstrapSha256}
    )) {
        if (-not (Test-Path -LiteralPath $entry.Path -PathType Leaf) -or
            (Get-FileHash -LiteralPath $entry.Path -Algorithm SHA256).Hash -cne $entry.Hash) {
            throw 'Private rebuild archive is incomplete or changed.'
        }
    }
}

function Assert-RebuildManifest {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest,
        [Parameter(Mandatory=$true)][string]$ManifestPath,
        [Parameter(Mandatory=$true)][string]$EvidencePath,
        [Parameter(Mandatory=$true)][string]$EvidenceHash,
        [Parameter(Mandatory=$true)][object]$Binding,
        [Parameter(Mandatory=$true)][string]$ConfigHash,
        [Parameter(Mandatory=$true)][string]$EnvironmentHash
    )
    $manifestDirectory = [IO.Path]::GetDirectoryName($ManifestPath)
    $expectedArchivePaths = [ordered]@{
        PreviousStartupStatePath = Join-Path $manifestDirectory 'previous-startup-state.json'
        ResourceMetadataPath = Join-Path $manifestDirectory 'previous-resource-metadata-sha256.json'
        IntendedContinuationPath = Join-Path $manifestDirectory 'intended-continuation-state.json'
        BootstrapPath = Join-Path $manifestDirectory 'bootstrap'
    }
    foreach ($archivePath in $expectedArchivePaths.GetEnumerator()) {
        $recordedPath = [string]$Manifest[$archivePath.Key]
        if ([string]::IsNullOrWhiteSpace($recordedPath) -or
            -not [IO.Path]::GetFullPath($recordedPath).Equals([IO.Path]::GetFullPath($archivePath.Value), [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Rebuild manifest archive paths must remain inside their original private rebuild directory.'
        }
    }
    if ($Manifest.Version -ne 1 -or $Manifest.RebuildNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $Manifest.NewImageTag -cne "sub2api-carpool:v1.3-rebuild-$($Manifest.RebuildNonce)" -or
        $Manifest.Task -cne $runtimeTask -or $Manifest.ManifestPath -cne $ManifestPath -or
        $Manifest.Phase -notin @('Prepared','OldContainersRemoved','ContinuationCommitted','Completed') -or
        $Manifest.CodeReadyEvidence -cne $EvidencePath -or $Manifest.CodeEvidenceHash -cne $EvidenceHash -or
        $Manifest.ConfigHash -cne $ConfigHash -or $Manifest.AppEnvironmentHash -cne $EnvironmentHash -or
        $Manifest.NetworkId -cne $Binding.NetworkId -or $Manifest.RunNonce -cnotmatch '^[0-9a-f]{32}$' -or
        $Manifest.OldAppId -cnotmatch '^[0-9a-f]{64}$' -or $Manifest.OldMockId -cnotmatch '^[0-9a-f]{64}$' -or
        $Manifest.OldImageId -cnotmatch '^sha256:[0-9a-f]{64}$' -or $Manifest.NewImageId -cnotmatch '^sha256:[0-9a-f]{64}$' -or
        $Manifest.Conservation.CopiedRecords -cne 'passed' -or $Manifest.Conservation.CopiedAnnouncements -cne 'passed' -or
        (Get-LocalRuntimeToolingIdentity) -cne $Manifest.ToolingHash) {
        throw 'Rebuild manifest does not match the current approved runtime inputs.'
    }
    Assert-RebuildArchive -Manifest $Manifest
    $newImage = Get-RebuildImage -Reference $Manifest.NewImageId
    $newTagImage = Get-RebuildImage -Reference $Manifest.NewImageTag
    if ($newImage.Id -cne $Manifest.NewImageId -or $newTagImage.Id -cne $Manifest.NewImageId) {
        throw 'Prepared rebuild image changed or is unavailable.'
    }
}

function Test-RebuildContinuationState {
    param([Collections.IDictionary]$State, [Collections.IDictionary]$Manifest)
    return $State.Task -ceq $runtimeTask -and $State.RunNonce -ceq $Manifest.RunNonce -and
        $State.NetworkId -ceq $Manifest.NetworkId -and $State.ConfigHash -ceq $Manifest.ConfigHash -and
        $State.AppEnvironmentHash -ceq $Manifest.AppEnvironmentHash -and $State.CodeEvidenceHash -ceq $Manifest.CodeEvidenceHash -and
        $State.ToolingHash -ceq $Manifest.ToolingHash -and $State.ImageId -ceq $Manifest.NewImageId -and
        $State.BootstrapSha256 -ceq $Manifest.BootstrapSha256 -and $State.BuildComplete -and
        $State.ConfigInstalled -and $State.FixturesCopied
}

function Remove-RebuildOldContainer {
    param(
        [Parameter(Mandatory=$true)][ValidateSet('application','mock')][string]$Role,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Manifest,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$OldState,
        [Parameter(Mandatory=$true)][string[]]$BaseEnvironment,
        [Parameter(Mandatory=$true)][string[]]$ApplicationEnvironment
    )
    $definition = Get-RebuildRoleDefinition -Role $Role
    $expectedId = if ($Role -eq 'application') { $Manifest.OldAppId } else { $Manifest.OldMockId }
    $container = Get-RebuildContainer -Name $definition.Name -AllowMissing
    if ($null -eq $container) { return }
    if ($container.Id -cne $expectedId) { throw "Refusing to replace changed $Role container." }
    Assert-RebuildOwnedContainer -Container $container -Role $Role -State $OldState -BaseEnvironment $BaseEnvironment `
        -ApplicationEnvironment $ApplicationEnvironment -RequireRunning:$false
    if ($container.State.Running) {
        Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('stop','--time','15',$expectedId) -Name "rebuild-$($Manifest.RebuildNonce)-stop-$Role"
        $container = Get-RebuildContainer -Name $definition.Name
    }
    if ($container.State.Running) {
        throw "Stopped $Role container changed before bounded removal."
    }
    Assert-RebuildOwnedContainer -Container $container -Role $Role -State $OldState -BaseEnvironment $BaseEnvironment `
        -ApplicationEnvironment $ApplicationEnvironment -RequireRunning:$false
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('rm',$expectedId) -Name "rebuild-$($Manifest.RebuildNonce)-remove-$Role"
}

Assert-LocalRuntimeIsolation
if ((& $runtimeDocker ps -a --format '{{.Names}}') -contains $acceptanceName) {
    throw 'Acceptance runner is active or retained; rebuild is forbidden.'
}
$resolvedEvidence = (Resolve-Path -LiteralPath $CodeReadyEvidence).Path
$codeEvidenceHash = Assert-DirectorCodeReady -EvidencePath $resolvedEvidence
$evidence = Get-Content -LiteralPath $resolvedEvidence -Raw | ConvertFrom-Json
$binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json
$configHash = (Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'config.yaml') -Algorithm SHA256).Hash
$environmentHash = Assert-LocalRuntimeEnvironmentFile -Path $appEnvironmentPath
$currentToolingHash = Get-LocalRuntimeToolingIdentity

if ($ResumeRebuild) {
    $resolvedManifest = (Resolve-Path -LiteralPath $ResumeRebuild).Path
    $allowedRoot = [IO.Path]::GetFullPath($rebuildRoot) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedManifest.StartsWith($allowedRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Rebuild manifest must belong to the private runtime rebuild directory.'
    }
    $manifest = Get-Content -LiteralPath $resolvedManifest -Raw | ConvertFrom-Json -AsHashtable
    Assert-RebuildManifest -Manifest $manifest -ManifestPath $resolvedManifest -EvidencePath $resolvedEvidence -EvidenceHash $codeEvidenceHash -Binding $binding -ConfigHash $configHash -EnvironmentHash $environmentHash
    if ($manifest.Phase -eq 'Completed') { throw 'This rebuild manifest is already complete; destructive replay is forbidden.' }
} else {
    if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) { throw 'A current startup state is required.' }
    $oldState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
    Assert-RebuildStateCore -State $oldState -Binding $binding -ConfigHash $configHash -EnvironmentHash $environmentHash -RequireHealthy
    $oldTaggedImage = Get-RebuildImage -Reference $runtimeImage
    if ($oldTaggedImage.Id -cne $oldState.ImageId) { throw 'The current runtime image tag no longer identifies the running image.' }
    $oldImage = Get-RebuildImage -Reference $oldState.ImageId
    $baseEnvironment = Merge-RebuildEnvironment -Base @($oldImage.Config.Env)
    $applicationEnvironment = Merge-RebuildEnvironment -Base @($oldImage.Config.Env) -Overlay @(Get-LocalRuntimeEnvironmentLines)
    $oldApp = Get-RebuildContainer -Name $appName
    $oldMock = Get-RebuildContainer -Name $mockName
    Assert-RebuildOwnedContainer -Container $oldApp -Role application -State $oldState -BaseEnvironment $baseEnvironment -ApplicationEnvironment $applicationEnvironment
    Assert-RebuildOwnedContainer -Container $oldMock -Role mock -State $oldState -BaseEnvironment $baseEnvironment -ApplicationEnvironment $applicationEnvironment

    & (Join-Path $PSScriptRoot 'check-copied-records.ps1')
    & (Join-Path $PSScriptRoot 'check-copied-announcements.ps1')

    [void](New-Item -ItemType Directory -Path $rebuildRoot -Force)
    $rebuildNonce = [Guid]::NewGuid().ToString('N')
    $archiveDirectory = Join-Path $rebuildRoot ("rebuild-" + [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') + "-$rebuildNonce")
    [void](New-Item -ItemType Directory -Path (Join-Path $archiveDirectory 'image/resources/model-pricing'))
    $imageDirectory = Join-Path $archiveDirectory 'image'
    $bootstrapPath = Join-Path $archiveDirectory 'bootstrap'
    $newImageTag = "sub2api-carpool:v1.3-rebuild-$rebuildNonce"
    $goEnvironment = @{GOOS='linux';GOARCH='amd64';CGO_ENABLED='0'}
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-tags','embed','-trimpath','-ldflags','-X main.Version=carpool-v1.3-local -X main.BuildType=development','-o',(Join-Path $imageDirectory 'sub2api'),'./cmd/server') -Name "rebuild-$rebuildNonce-build-app" -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',(Join-Path $imageDirectory 'mock-upstream'),(Join-Path $PSScriptRoot 'mock/main.go')) -Name "rebuild-$rebuildNonce-build-mock" -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',$bootstrapPath,(Join-Path $PSScriptRoot 'bootstrap/main.go')) -Name "rebuild-$rebuildNonce-build-bootstrap" -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Copy-Item -LiteralPath (Join-Path $runtimeRepository 'backend/resources/model-pricing/model_prices_and_context_window.json') -Destination (Join-Path $imageDirectory 'resources/model-pricing/model_prices_and_context_window.json')
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Dockerfile.runtime') -Destination (Join-Path $imageDirectory 'Dockerfile')
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Dockerignore.runtime') -Destination (Join-Path $imageDirectory '.dockerignore')
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('build','--network=none','--pull=false','-t',$newImageTag,$imageDirectory) -Name "rebuild-$rebuildNonce-build-image"
    if ((Assert-DirectorCodeReady -EvidencePath $resolvedEvidence) -cne $codeEvidenceHash -or (Get-LocalRuntimeToolingIdentity) -cne $currentToolingHash) {
        throw 'Approved code or runtime tooling changed during rebuild compilation.'
    }
    $newImage = Get-RebuildImage -Reference $newImageTag
    if ($newImage.Id -cnotmatch '^sha256:[0-9a-f]{64}$' -or $newImage.Id -ceq $oldState.ImageId) { throw 'Rebuild did not produce a distinct immutable image.' }
    $bootstrapHash = (Get-FileHash -LiteralPath $bootstrapPath -Algorithm SHA256).Hash

    $previousStatePath = Join-Path $archiveDirectory 'previous-startup-state.json'
    [IO.File]::Copy($statePath, $previousStatePath, $false)
    $previousStateHash = (Get-FileHash -LiteralPath $previousStatePath -Algorithm SHA256).Hash
    $resourceMetadataPath = Join-Path $archiveDirectory 'previous-resource-metadata-sha256.json'
    $resourceMetadata = [ordered]@{
        Task = $runtimeTask
        RunNonce = $oldState.RunNonce
        AppMetadataSha256 = Get-RebuildContainerMetadataHash -Container $oldApp
        MockMetadataSha256 = Get-RebuildContainerMetadataHash -Container $oldMock
        CapturedAt = [DateTimeOffset]::UtcNow.ToString('o')
    }
    Write-RebuildCreateOnlyJson -Path $resourceMetadataPath -Value $resourceMetadata
    $resourceMetadataHash = (Get-FileHash -LiteralPath $resourceMetadataPath -Algorithm SHA256).Hash
    $manifestPath = Join-Path $archiveDirectory 'rebuild-manifest.json'
    $intendedPath = Join-Path $archiveDirectory 'intended-continuation-state.json'
    $rebuiltAt = [DateTimeOffset]::UtcNow.ToString('o')
    $continuation = New-RebuildContinuationState -PreviousState $oldState -CodeEvidenceHash $codeEvidenceHash `
        -ToolingHash $currentToolingHash -ImageId $newImage.Id -ImageDirectory $imageDirectory `
        -BootstrapSha256 $bootstrapHash -ManifestPath $manifestPath -PreviousStateSha256 $previousStateHash -RebuiltAt $rebuiltAt
    Write-RebuildCreateOnlyJson -Path $intendedPath -Value $continuation
    $intendedHash = (Get-FileHash -LiteralPath $intendedPath -Algorithm SHA256).Hash
    $manifest = [ordered]@{
        Version = 1
        Task = $runtimeTask
        Phase = 'Prepared'
        RebuildNonce = $rebuildNonce
        ManifestPath = $manifestPath
        CodeReadyEvidence = $resolvedEvidence
        CodeEvidenceHash = $codeEvidenceHash
        SourceSha256 = $evidence.source_sha256
        EmbeddedFrontendSha256 = $evidence.embedded_frontend_sha256
        ToolingHash = $currentToolingHash
        ConfigHash = $configHash
        AppEnvironmentHash = $environmentHash
        NetworkId = $oldState.NetworkId
        RunNonce = $oldState.RunNonce
        OldAppId = $oldState[$appName]
        OldMockId = $oldState[$mockName]
        OldImageId = $oldState.ImageId
        NewImageId = $newImage.Id
        NewImageTag = $newImageTag
        BootstrapPath = $bootstrapPath
        BootstrapSha256 = $bootstrapHash
        PreviousStartupStatePath = $previousStatePath
        PreviousStartupStateSha256 = $previousStateHash
        ResourceMetadataPath = $resourceMetadataPath
        ResourceMetadataSha256 = $resourceMetadataHash
        IntendedContinuationPath = $intendedPath
        IntendedContinuationSha256 = $intendedHash
        Conservation = [ordered]@{CopiedRecords='passed';CopiedAnnouncements='passed'}
        PreparedAt = $rebuiltAt
        UpdatedAt = $rebuiltAt
    }
    Write-RebuildCreateOnlyJson -Path $manifestPath -Value $manifest
    $resolvedManifest = $manifestPath
}

function Invoke-RebuildTransition {
$oldState = Get-Content -LiteralPath $manifest.PreviousStartupStatePath -Raw | ConvertFrom-Json -AsHashtable
Assert-RebuildStateCore -State $oldState -Binding $binding -ConfigHash $configHash -EnvironmentHash $environmentHash -RequireHealthy
$oldImage = Get-RebuildImage -Reference $manifest.OldImageId
$baseEnvironment = Merge-RebuildEnvironment -Base @($oldImage.Config.Env)
$applicationEnvironment = Merge-RebuildEnvironment -Base @($oldImage.Config.Env) -Overlay @(Get-LocalRuntimeEnvironmentLines)

$liveState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
$liveStateHash = (Get-FileHash -LiteralPath $statePath -Algorithm SHA256).Hash
$isContinuation = Test-RebuildContinuationState -State $liveState -Manifest $manifest
if ($manifest.Phase -in @('Prepared','OldContainersRemoved') -and $liveStateHash -cne $manifest.PreviousStartupStateSha256 -and -not $isContinuation) {
    throw 'Startup state is neither the archived original nor the intended rebuild continuation.'
}

if ($manifest.Phase -eq 'Prepared') {
    Remove-RebuildOldContainer -Role application -Manifest $manifest -OldState $oldState -BaseEnvironment $baseEnvironment -ApplicationEnvironment $applicationEnvironment
    Remove-RebuildOldContainer -Role mock -Manifest $manifest -OldState $oldState -BaseEnvironment $baseEnvironment -ApplicationEnvironment $applicationEnvironment
    if ($null -ne (Get-RebuildContainer -Name $appName -AllowMissing) -or $null -ne (Get-RebuildContainer -Name $mockName -AllowMissing)) {
        throw 'An old application or mock container remains after bounded removal.'
    }
    $manifest.Phase = 'OldContainersRemoved'
    Save-RebuildManifest -Path $resolvedManifest -Manifest $manifest
}

if ($manifest.Phase -eq 'OldContainersRemoved') {
    if ($null -ne (Get-RebuildContainer -Name $appName -AllowMissing) -or $null -ne (Get-RebuildContainer -Name $mockName -AllowMissing)) {
        throw 'Container names were reused during the rebuild transition.'
    }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('tag',$manifest.NewImageId,$runtimeImage) -Name "rebuild-$($manifest.RebuildNonce)-promote-image"
    if ((Get-RebuildImage -Reference $runtimeImage).Id -cne $manifest.NewImageId) { throw 'Runtime image promotion did not select the prepared image.' }
    [IO.File]::Copy($manifest.BootstrapPath, (Join-Path $runtimeDirectory 'bootstrap'), $true)
    if ((Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'bootstrap') -Algorithm SHA256).Hash -cne $manifest.BootstrapSha256) {
        throw 'Promoted bootstrap binary differs from the prepared rebuild.'
    }
    $continuation = Get-Content -LiteralPath $manifest.IntendedContinuationPath -Raw | ConvertFrom-Json -AsHashtable
    if (-not (Test-RebuildContinuationState -State $continuation -Manifest $manifest)) { throw 'Prepared continuation state failed validation.' }
    Write-PrivateRuntimeJSON -Path $statePath -Value $continuation
    Write-PrivateRuntimeJSON -Path (Join-Path $runtimeDirectory 'runtime-ready.json') -Value ([ordered]@{
        Status='LOCAL_RUNTIME_REBUILD_PENDING';Manifest=$resolvedManifest;UpdatedAt=[DateTimeOffset]::UtcNow.ToString('o')
    })
    $manifest.Phase = 'ContinuationCommitted'
    Save-RebuildManifest -Path $resolvedManifest -Manifest $manifest
}

& (Join-Path $PSScriptRoot 'start-runtime.ps1') -CodeReadyEvidence $resolvedEvidence -Resume
$finalState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
if (-not $finalState.Healthy -or -not (Test-RebuildContinuationState -State $finalState -Manifest $manifest) -or
    $finalState[$appName] -cnotmatch '^[0-9a-f]{64}$' -or $finalState[$mockName] -cnotmatch '^[0-9a-f]{64}$') {
    throw 'Recreated runtime did not commit healthy exact container identities.'
}
$finalImage = Get-RebuildImage -Reference $manifest.NewImageId
$finalBaseEnvironment = Merge-RebuildEnvironment -Base @($finalImage.Config.Env)
$finalApplicationEnvironment = Merge-RebuildEnvironment -Base @($finalImage.Config.Env) -Overlay @(Get-LocalRuntimeEnvironmentLines)
Assert-RebuildOwnedContainer -Container (Get-RebuildContainer -Name $appName) -Role application -State $finalState `
    -BaseEnvironment $finalBaseEnvironment -ApplicationEnvironment $finalApplicationEnvironment
Assert-RebuildOwnedContainer -Container (Get-RebuildContainer -Name $mockName) -Role mock -State $finalState `
    -BaseEnvironment $finalBaseEnvironment -ApplicationEnvironment $finalApplicationEnvironment
$manifest.Phase = 'Completed'
$manifest.NewAppId = $finalState[$appName]
$manifest.NewMockId = $finalState[$mockName]
$manifest.CompletedAt = [DateTimeOffset]::UtcNow.ToString('o')
[void]$manifest.Remove('LastFailure')
[void]$manifest.Remove('LastFailureAt')
Save-RebuildManifest -Path $resolvedManifest -Manifest $manifest
return [pscustomobject]@{Status='LOCAL_RUNTIME_REBUILT';URL='http://127.0.0.1:38088';Manifest=$resolvedManifest;ImageId=$manifest.NewImageId}
}

try {
    Invoke-RebuildTransition
} catch {
    if ($manifest -and $resolvedManifest -and (Test-Path -LiteralPath $resolvedManifest -PathType Leaf)) {
        $manifest.LastFailure = switch ($manifest.Phase) {
            'Prepared' { 'old-container-removal-failed' }
            'OldContainersRemoved' { 'continuation-commit-failed' }
            'ContinuationCommitted' { 'runtime-recreation-failed' }
            default { 'rebuild-transition-incomplete' }
        }
        $manifest.LastFailureAt = [DateTimeOffset]::UtcNow.ToString('o')
        try { Save-RebuildManifest -Path $resolvedManifest -Manifest $manifest } catch { }
        throw "Rebuild transition is incomplete. Resume only with: pwsh -NoProfile -File validation/source-runtime/rebuild-runtime.ps1 -CodeReadyEvidence '$resolvedEvidence' -ResumeRebuild '$resolvedManifest'"
    }
    throw
}

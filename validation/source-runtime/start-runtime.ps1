param([Parameter(Mandatory=$true)][string]$CodeReadyEvidence, [switch]$Resume)
. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
$statePath = Join-Path $runtimeDirectory 'startup-state.json'
$resumeEvidenceHash = $null
if ($Resume) {
    $resumeState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($resumeState.BuildComplete) { $resumeEvidenceHash = $resumeState.CodeEvidenceHash }
}
$codeEvidenceHash = Assert-DirectorCodeReady -EvidencePath $CodeReadyEvidence -RuntimeEvidenceHash $resumeEvidenceHash
$binding = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'resource-binding.json') -Raw | ConvertFrom-Json
$configHash = (Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'config.yaml')).Hash
if ($binding.ConfigSha256 -ne $configHash) { throw 'Private configuration differs from the reviewed preparation.' }
$appEnvironmentPath = Join-Path $runtimeDirectory 'app.env'
$appEnvironmentHash = Assert-LocalRuntimeEnvironmentFile -Path $appEnvironmentPath
if (-not $binding.AppEnvSha256 -or $binding.AppEnvSha256 -ne $appEnvironmentHash) { throw 'Private runtime environment is unsealed or differs from the reviewed preparation.' }
if (-not (Test-Path -LiteralPath (Join-Path $runtimeRepository 'backend/internal/web/dist/index.html'))) { throw 'A completed embedded frontend build is required.' }
$appVolume = 'carpool-v13-test-appdata'
$configTransferName = 'carpool-v13-config-transfer'
$toolingHash = Get-LocalRuntimeToolingIdentity
$existingNames = @(& $runtimeDocker ps -a --format '{{.Names}}')
if ($Resume) {
    $startupState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json -AsHashtable
    if ($startupState.Task -ne $runtimeTask -or $startupState.CodeEvidenceHash -ne $codeEvidenceHash -or $startupState.ConfigHash -ne $configHash -or $startupState.AppEnvironmentHash -ne $appEnvironmentHash -or $startupState.ToolingHash -ne $toolingHash -or $startupState.NetworkId -ne $binding.NetworkId -or $startupState.RunNonce -notmatch '^[0-9a-f]{32}$') { throw 'Resume state does not match the reviewed resources/code/config.' }
} else {
    if (Test-Path -LiteralPath $statePath) { throw 'Startup state exists. Review the failure and use -Resume with the same evidence.' }
    if (Get-NetTCPConnection -State Listen -LocalPort 38088 -ErrorAction SilentlyContinue) { throw 'Port 38088 is already in use.' }
    if (@($existingNames | Where-Object { $_ -in @('carpool-v13-test-app','carpool-v13-test-mock',$configTransferName) }).Count -gt 0) { throw 'A runtime container exists without this startup state.' }
    if ((& $runtimeDocker volume ls --format '{{.Name}}') -contains $appVolume) { throw 'Application volume exists without this startup state.' }
    if (Test-Path -LiteralPath (Join-Path $runtimeDirectory 'fixtures.json')) { throw 'Fixture output already exists without this startup state.' }
    $startupState = @{Task=$runtimeTask; CodeEvidenceHash=$codeEvidenceHash; ConfigHash=$configHash; AppEnvironmentHash=$appEnvironmentHash; ToolingHash=$toolingHash; NetworkId=$binding.NetworkId; RunNonce=[Guid]::NewGuid().ToString('N'); CreatedAt=[DateTimeOffset]::UtcNow.ToString('o')}
}
function Save-LocalStartupState { Write-PrivateRuntimeJSON -Path $statePath -Value $startupState }
function Merge-LocalContainerEnvironment {
    param([string[]]$Base, [string[]]$Overlay = @())
    $values = [Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
    foreach ($entry in @($Base) + @($Overlay)) {
        $parts = $entry.Split('=', 2)
        if ($parts.Count -ne 2 -or -not $parts[0]) { throw 'Container image has an invalid environment entry.' }
        $values[$parts[0]] = $parts[1]
    }
    return [string[]]@($values.GetEnumerator() | ForEach-Object { $_.Key + '=' + $_.Value } | Sort-Object -CaseSensitive)
}
function Assert-LocalRuntimeContainerRole {
    param([object]$Container, [hashtable]$Definition, [string[]]$ExpectedEnvironment)
    $networks = @($Container.NetworkSettings.Networks.PSObject.Properties)
    $entrypoint = @($Container.Config.Entrypoint)
    $actualEnvironment = [string[]]@(@($Container.Config.Env) | Sort-Object -CaseSensitive)
    $environmentDifference = @(Compare-Object -ReferenceObject $ExpectedEnvironment -DifferenceObject $actualEnvironment -CaseSensitive)
    $capDrop = @($Container.HostConfig.CapDrop)
    $capAdd = @($Container.HostConfig.CapAdd | Where-Object { $null -ne $_ })
    $securityOptions = @($Container.HostConfig.SecurityOpt)
    $tmpfs = if ($null -eq $Container.HostConfig.Tmpfs) { @() } else { @($Container.HostConfig.Tmpfs.PSObject.Properties) }
    if ($Container.Config.Labels.$runtimeLabel -ne $runtimeTask -or $Container.Config.Labels.$runtimeRunLabel -ne $startupState.RunNonce -or
        $Container.Image -ne $startupState.ImageId -or $Container.Config.User -ne '1000:1000' -or $Container.Config.WorkingDir -ne '/app' -or
        $entrypoint.Count -ne 1 -or $entrypoint[0] -ne $Definition.Entrypoint -or @($Container.Config.Cmd | Where-Object { $null -ne $_ }).Count -ne 0 -or
        $environmentDifference.Count -ne 0 -or -not $Container.HostConfig.ReadonlyRootfs -or $Container.HostConfig.Privileged -or
        $capDrop.Count -ne 1 -or $capDrop[0] -ne 'ALL' -or $capAdd.Count -ne 0 -or
        $securityOptions.Count -ne 1 -or $securityOptions[0] -notmatch '^no-new-privileges(?::true)?$' -or
        $Container.HostConfig.NetworkMode -ne $runtimeNetwork -or $networks.Count -ne 1 -or $networks[0].Name -ne $runtimeNetwork -or
        $networks[0].Value.Aliases -notcontains $Definition.Alias -or $tmpfs.Count -ne $Definition.Tmpfs.Count) {
        throw "Existing $($Definition.Role) container does not match its immutable runtime role."
    }
    foreach ($expectedTmpfs in $Definition.Tmpfs.GetEnumerator()) {
        $actualTmpfs = @($tmpfs | Where-Object { $_.Name -eq $expectedTmpfs.Key })
        $options = if ($actualTmpfs.Count -eq 1) { @($actualTmpfs[0].Value -split ',') } else { @() }
        if ($actualTmpfs.Count -ne 1 -or $options.Count -ne 4 -or $options -notcontains $expectedTmpfs.Value.Mode -or
            $options -notcontains 'noexec' -or $options -notcontains 'nosuid' -or
            @($options | Where-Object { $_ -in $expectedTmpfs.Value.Sizes }).Count -ne 1) {
            throw "Existing $($Definition.Role) container has unexpected tmpfs mounts."
        }
    }
    $ports = if ($null -eq $Container.HostConfig.PortBindings) { @() } else { @($Container.HostConfig.PortBindings.PSObject.Properties) }
    $mounts = @($Container.Mounts | Where-Object { $null -ne $_ })
    if ($Definition.Role -eq 'application') {
        $bindings = if ($ports.Count -eq 1) { @($ports[0].Value) } else { @() }
        if ($ports.Count -ne 1 -or $ports[0].Name -ne '8080/tcp' -or $bindings.Count -ne 1 -or $bindings[0].HostIp -ne '127.0.0.1' -or $bindings[0].HostPort -ne '38088' -or
            $mounts.Count -ne 1 -or $mounts[0].Type -ne 'volume' -or $mounts[0].Name -ne $appVolume -or $mounts[0].Destination -ne '/app/data' -or -not $mounts[0].RW) {
            throw 'Existing application container has unexpected ports or mounts.'
        }
    } elseif ($ports.Count -ne 0 -or $mounts.Count -ne 0) {
        throw 'Existing mock container has unexpected ports or mounts.'
    }
}
function Assert-InstalledRuntimeConfig {
    $readOnlyMount = "type=volume,source=$appVolume,target=/data,readonly"
    $restricted = @('run','--rm','--network','none','--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=1m','--mount',$readOnlyMount,'--entrypoint','/bin/busybox',$startupState.ImageId)
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments ($restricted + @('sha256sum','/data/config.yaml')) -Name 'verify-installed-config-hash'
    $hashOutput = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'verify-installed-config-hash.stdout.log') -Raw).Trim()
    if ($hashOutput -notmatch '^([0-9a-fA-F]{64})\s+/data/config\.yaml$' -or $Matches[1] -ne $configHash) { throw 'Installed private configuration differs from the prepared file.' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments ($restricted + @('stat','-c','%u:%g %a %n','/data','/data/config.yaml')) -Name 'verify-installed-config-metadata'
    $metadata = @((Get-Content -LiteralPath (Join-Path $runtimeDirectory 'verify-installed-config-metadata.stdout.log')))
    if ($metadata.Count -ne 2 -or $metadata[0] -ne '1000:1000 700 /data' -or $metadata[1] -ne '1000:1000 600 /data/config.yaml') { throw 'Installed private configuration has unexpected ownership or mode.' }
}
function Assert-LocalConfigTransferRole {
    param([object]$Container, [string[]]$ExpectedEnvironment)
    $entrypoint = @($Container.Config.Entrypoint)
    $cmd = @($Container.Config.Cmd | Where-Object { $null -ne $_ })
    $actualEnvironment = [string[]]@(@($Container.Config.Env) | Sort-Object -CaseSensitive)
    $environmentDifference = @(Compare-Object -ReferenceObject $ExpectedEnvironment -DifferenceObject $actualEnvironment -CaseSensitive)
    $capDrop = @($Container.HostConfig.CapDrop)
    $capAdd = @($Container.HostConfig.CapAdd | Where-Object { $null -ne $_ })
    $securityOptions = @($Container.HostConfig.SecurityOpt)
    $ports = if ($null -eq $Container.HostConfig.PortBindings) { @() } else { @($Container.HostConfig.PortBindings.PSObject.Properties) }
    $networks = if ($null -eq $Container.NetworkSettings.Networks) { @() } else { @($Container.NetworkSettings.Networks.PSObject.Properties) }
    $tmpfs = if ($null -eq $Container.HostConfig.Tmpfs) { @() } else { @($Container.HostConfig.Tmpfs.PSObject.Properties) }
    $tmpfsOptions = if ($tmpfs.Count -eq 1) { @($tmpfs[0].Value -split ',') } else { @() }
    $mounts = @($Container.Mounts | Where-Object { $null -ne $_ })
    if ($Container.Config.Labels.$runtimeLabel -ne $runtimeTask -or $Container.Config.Labels.$runtimeRunLabel -ne $startupState.RunNonce -or
        $Container.Image -ne $startupState.ImageId -or $Container.Config.User -ne '0:0' -or $Container.Config.WorkingDir -ne '/app' -or
        $entrypoint.Count -ne 1 -or $entrypoint[0] -ne '/bin/true' -or $cmd.Count -ne 0 -or $environmentDifference.Count -ne 0 -or
        $Container.State.Running -or $Container.HostConfig.ReadonlyRootfs -or $Container.HostConfig.Privileged -or $Container.HostConfig.AutoRemove -or
        $capDrop.Count -ne 1 -or $capDrop[0] -ne 'ALL' -or $capAdd.Count -ne 0 -or $securityOptions.Count -ne 1 -or
        $securityOptions[0] -notmatch '^no-new-privileges(?::true)?$' -or $Container.HostConfig.NetworkMode -ne 'none' -or $networks.Count -ne 1 -or $networks[0].Name -ne 'none' -or $ports.Count -ne 0 -or
        $tmpfs.Count -ne 1 -or $tmpfs[0].Name -ne '/var/lib/postgresql' -or $tmpfsOptions.Count -ne 4 -or $tmpfsOptions -notcontains 'ro' -or
        $tmpfsOptions -notcontains 'noexec' -or $tmpfsOptions -notcontains 'nosuid' -or
        @($tmpfsOptions | Where-Object { $_ -in @('size=1m','size=1M','size=1024k','size=1024K','size=1048576') }).Count -ne 1 -or
        $mounts.Count -ne 1 -or $mounts[0].Type -ne 'volume' -or $mounts[0].Name -ne $appVolume -or $mounts[0].Destination -ne '/data' -or -not $mounts[0].RW) {
        throw 'Configuration transfer container does not match its bounded role.'
    }
}
function Assert-LocalSyntheticFixtures {
    param([object]$Fixtures)
    $fixtureNames = @('admin','four','three','two','fifth_four','fifth_three','fifth_two','ordinary','expired','renewal','termination','takeover')
    $envelopeProperties = @($Fixtures.PSObject.Properties.Name | Sort-Object -CaseSensitive)
    $expectedEnvelopeProperties = @('base_url','mock_url','source','users','version') | Sort-Object -CaseSensitive
    if ($Fixtures.version -ne 1 -or $Fixtures.source -ne 'synthetic-local-only' -or $Fixtures.base_url -ne 'http://carpool-app:8080' -or $Fixtures.mock_url -ne 'http://carpool-mock:8090' -or
        @(Compare-Object -ReferenceObject $expectedEnvelopeProperties -DifferenceObject $envelopeProperties -CaseSensitive).Count -ne 0 -or $Fixtures.users.Count -ne $fixtureNames.Count) {
        throw 'Synthetic fixture output failed validation; source retained for recovery.'
    }
    $fixtureIds = [Collections.Generic.HashSet[long]]::new()
    for ($index = 0; $index -lt $fixtureNames.Count; $index++) {
        $user = $Fixtures.users[$index]
        $name = $fixtureNames[$index]
        $properties = @($user.PSObject.Properties.Name | Sort-Object -CaseSensitive)
        $expectedProperties = @('email','id','name','password') | Sort-Object -CaseSensitive
        if (@(Compare-Object -ReferenceObject $expectedProperties -DifferenceObject $properties -CaseSensitive).Count -ne 0 -or
            $user.name -cne $name -or $user.email -cne "carpool-test-$name@example.invalid" -or $user.id -le 0 -or
            -not $fixtureIds.Add([long]$user.id) -or $user.password -cnotmatch '^[0-9a-f]{48}$') {
            throw 'Synthetic fixture output failed validation; source retained for recovery.'
        }
    }
}
Save-LocalStartupState

if (-not $startupState.BuildComplete) {
    $imageDirectory = Join-Path $runtimeDirectory ('image-' + [Guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path (Join-Path $imageDirectory 'resources/model-pricing'))
    $goEnvironment = @{GOOS='linux';GOARCH='amd64';CGO_ENABLED='0'}
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-tags','embed','-trimpath','-ldflags','-X main.Version=carpool-v1.3-local -X main.BuildType=development','-o',(Join-Path $imageDirectory 'sub2api'),'./cmd/server') -Name 'build-application-linux' -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',(Join-Path $imageDirectory 'mock-upstream'),(Join-Path $PSScriptRoot 'mock/main.go')) -Name 'build-mock-linux' -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',(Join-Path $runtimeDirectory 'bootstrap'),(Join-Path $PSScriptRoot 'bootstrap/main.go')) -Name 'build-bootstrap-current' -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment $goEnvironment
    Copy-Item -LiteralPath (Join-Path $runtimeRepository 'backend/resources/model-pricing/model_prices_and_context_window.json') -Destination (Join-Path $imageDirectory 'resources/model-pricing/model_prices_and_context_window.json')
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Dockerfile.runtime') -Destination (Join-Path $imageDirectory 'Dockerfile')
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Dockerignore.runtime') -Destination (Join-Path $imageDirectory '.dockerignore')
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('build','--network=none','--pull=false','-t',$runtimeImage,$imageDirectory) -Name 'build-runtime-image'
    if ((Assert-DirectorCodeReady -EvidencePath $CodeReadyEvidence) -ne $codeEvidenceHash) { throw 'Code-ready evidence changed during runtime build.' }
    if ((Get-LocalRuntimeToolingIdentity) -ne $toolingHash) { throw 'Runtime tooling inputs changed during the build.' }
    $startupState.ImageId = ((& $runtimeDocker image inspect $runtimeImage | ConvertFrom-Json)[0]).Id
    $startupState.ImageDirectory = $imageDirectory
    $startupState.BootstrapSha256 = (Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'bootstrap') -Algorithm SHA256).Hash
    $startupState.BuildComplete = $true
    Save-LocalStartupState
}
if (((& $runtimeDocker image inspect $runtimeImage | ConvertFrom-Json)[0]).Id -ne $startupState.ImageId) { throw 'Runtime image tag was changed; do not resume against another image.' }
$image = (& $runtimeDocker image inspect $startupState.ImageId | ConvertFrom-Json)[0]
$baseEnvironment = Merge-LocalContainerEnvironment -Base @($image.Config.Env)
$applicationEnvironment = Merge-LocalContainerEnvironment -Base @($image.Config.Env) -Overlay @(Get-LocalRuntimeEnvironmentLines)

if ((& $runtimeDocker volume ls --format '{{.Name}}') -notcontains $appVolume) {
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('volume','create','--label',"$runtimeLabel=$runtimeTask",'--label',"$runtimeRunLabel=$($startupState.RunNonce)",$appVolume) -Name 'create-app-volume'
}
$volume = (& $runtimeDocker volume inspect $appVolume | ConvertFrom-Json)[0]
$volumeOptions = if ($null -eq $volume.Options) { @() } else { @($volume.Options.PSObject.Properties) }
if ($volume.Driver -ne 'local' -or $volume.Scope -ne 'local' -or $volumeOptions.Count -ne 0 -or $volume.Labels.$runtimeLabel -ne $runtimeTask -or $volume.Labels.$runtimeRunLabel -ne $startupState.RunNonce) { throw 'Application volume is not bound to this runtime run.' }
$definitions = @(
    @{Name='carpool-v13-test-app'; Role='application'; Alias='carpool-app'; Entrypoint='/app/sub2api'; Environment=$applicationEnvironment; Tmpfs=@{'/tmp'=@{Mode='rw';Sizes=@('size=64m','size=64M','size=65536k','size=65536K','size=67108864')};'/var/lib/postgresql'=@{Mode='ro';Sizes=@('size=1m','size=1M','size=1024k','size=1024K','size=1048576')}}; Arguments=@('--publish','127.0.0.1:38088:8080','--env-file',$appEnvironmentPath,'--tmpfs','/tmp:rw,noexec,nosuid,size=64m','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=1m','--mount',"type=volume,source=$appVolume,target=/app/data")},
    @{Name='carpool-v13-test-mock'; Role='mock'; Alias='carpool-mock'; Entrypoint='/app/mock-upstream'; Environment=$baseEnvironment; Tmpfs=@{'/tmp'=@{Mode='rw';Sizes=@('size=16m','size=16M','size=16384k','size=16384K','size=16777216')};'/var/lib/postgresql'=@{Mode='ro';Sizes=@('size=1m','size=1M','size=1024k','size=1024K','size=1048576')}}; Arguments=@('--tmpfs','/tmp:rw,noexec,nosuid,size=16m','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=1m','--entrypoint','/app/mock-upstream')}
)
foreach ($definition in $definitions) {
    $name = $definition.Name
    if ((& $runtimeDocker ps -a --format '{{.Names}}') -notcontains $name) {
        if ($name -eq 'carpool-v13-test-app' -and (Get-NetTCPConnection -State Listen -LocalPort 38088 -ErrorAction SilentlyContinue)) { throw 'Another listener took port 38088.' }
        $create = @('create','--name',$name,'--label',"$runtimeLabel=$runtimeTask",'--label',"$runtimeRunLabel=$($startupState.RunNonce)",'--network',$runtimeNetwork,'--network-alias',$definition.Alias,'--read-only','--cap-drop','ALL','--security-opt','no-new-privileges') + $definition.Arguments + $startupState.ImageId
        Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments $create -Name "create-$name"
    }
    $container = (& $runtimeDocker inspect $name | ConvertFrom-Json)[0]
    if ($startupState[$name] -and $startupState[$name] -ne $container.Id) { throw 'A task container was replaced unexpectedly.' }
    Assert-LocalRuntimeContainerRole -Container $container -Definition $definition -ExpectedEnvironment $definition.Environment
    $startupState[$name] = $container.Id
    Save-LocalStartupState
}
if (-not $startupState.ConfigInstalled) {
    if ((& $runtimeDocker ps -a --format '{{.Names}}') -notcontains $configTransferName) {
        $transferCreate = @('create','--name',$configTransferName,'--label',"$runtimeLabel=$runtimeTask",'--label',"$runtimeRunLabel=$($startupState.RunNonce)",'--network','none','--user','0:0','--cap-drop','ALL','--security-opt','no-new-privileges',
            '--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=1m','--mount',"type=volume,source=$appVolume,target=/data",'--entrypoint','/bin/true',$startupState.ImageId)
        Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments $transferCreate -Name 'create-config-transfer'
    }
    $transfer = (& $runtimeDocker inspect $configTransferName | ConvertFrom-Json)[0]
    Assert-LocalConfigTransferRole -Container $transfer -ExpectedEnvironment $baseEnvironment
    $transferId = $transfer.Id
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',(Join-Path $runtimeDirectory 'config.yaml'),"${transferId}:/data/config.yaml") -Name 'copy-private-config'
    $transferAfterCopy = (& $runtimeDocker inspect $transferId | ConvertFrom-Json)[0]
    if ($transferAfterCopy.Id -ne $transferId) { throw 'Configuration transfer container changed during copy.' }
    Assert-LocalConfigTransferRole -Container $transferAfterCopy -ExpectedEnvironment $baseEnvironment
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('rm',$transferId) -Name 'remove-config-transfer'
    $writeMount = "type=volume,source=$appVolume,target=/data"
    $helper = @('run','--rm','--network','none','--read-only','--user','0:0','--cap-drop','ALL','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=1m')
    $helperTail = @('--security-opt','no-new-privileges','--mount',$writeMount,'--entrypoint','/bin/busybox',$startupState.ImageId)
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments ($helper + @('--cap-add','CHOWN','--cap-add','DAC_OVERRIDE') + $helperTail + @('chown','1000:1000','/data','/data/config.yaml')) -Name 'config-owner'
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments ($helper + @('--cap-add','FOWNER','--cap-add','DAC_OVERRIDE') + $helperTail + @('chmod','600','/data/config.yaml')) -Name 'config-mode'
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments ($helper + @('--cap-add','FOWNER','--cap-add','DAC_OVERRIDE') + $helperTail + @('chmod','700','/data')) -Name 'data-mode'
}
if ((& $runtimeDocker ps -a --format '{{.Names}}') -contains $configTransferName) { throw 'Configuration transfer container remains after installation.' }
Assert-InstalledRuntimeConfig
if (-not $startupState.ConfigInstalled) {
    $startupState.ConfigInstalled = $true
    Save-LocalStartupState
}

# A resume validates committed fixture credentials; it never renews or rewrites users.
if (-not $startupState.FixturesCopied) {
    $bootstrapPath = Join-Path $runtimeDirectory 'bootstrap'
    if (-not $startupState.BootstrapSha256 -or (Get-FileHash -LiteralPath $bootstrapPath -Algorithm SHA256).Hash -ne $startupState.BootstrapSha256) { throw 'Synthetic bootstrap binary differs from the completed runtime build.' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp',$bootstrapPath,'carpool-v13-test-postgres:/tmp/carpool-bootstrap') -Name 'copy-bootstrap'
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','chmod','700','/tmp/carpool-bootstrap') -Name 'bootstrap-mode'
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','/bin/busybox','sha256sum','/tmp/carpool-bootstrap') -Name 'verify-copied-bootstrap'
    $copiedBootstrapHash = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'verify-copied-bootstrap.stdout.log') -Raw).Trim()
    if ($copiedBootstrapHash -notmatch '^([0-9a-fA-F]{64})\s+/tmp/carpool-bootstrap$' -or $Matches[1] -ne $startupState.BootstrapSha256) { throw 'Copied synthetic bootstrap differs from the completed runtime build.' }
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','/tmp/carpool-bootstrap','-output','/tmp/carpool-fixtures.json') -Name 'bootstrap-synthetic-users'
    $incoming = Join-Path $runtimeDirectory ('fixtures-incoming-' + [Guid]::NewGuid().ToString('N') + '.json')
    Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('cp','carpool-v13-test-postgres:/tmp/carpool-fixtures.json',$incoming) -Name 'copy-private-fixtures'
    $fixtures = Get-Content -LiteralPath $incoming -Raw | ConvertFrom-Json
    Assert-LocalSyntheticFixtures -Fixtures $fixtures
    $fixtures = $null
    $fixturePath = Join-Path $runtimeDirectory 'fixtures.json'
    if (Test-Path -LiteralPath $fixturePath) {
        if ((Get-FileHash -LiteralPath $fixturePath).Hash -ne (Get-FileHash -LiteralPath $incoming).Hash) { throw 'Existing private fixture file differs; refusing to overwrite credentials.' }
        [IO.File]::Delete($incoming)
    } else {
        [IO.File]::Move($incoming, $fixturePath)
    }
    $startupState.FixtureSha256 = (Get-FileHash -LiteralPath $fixturePath).Hash
    $startupState.FixturesCopied = $true
    Save-LocalStartupState
}
if ((Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'fixtures.json')).Hash -ne $startupState.FixtureSha256) { throw 'Private fixtures no longer match the committed bootstrap.' }
# Delete only the verified duplicate created by this bootstrap, never a database or archive.
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-postgres','rm','-f','/tmp/carpool-fixtures.json') -Name 'remove-container-fixture-duplicate'
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('start','carpool-v13-test-mock','carpool-v13-test-app') -Name 'start-app-mock'
$healthy = $false
for ($attempt = 0; $attempt -lt 45; $attempt++) {
    try {
        Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec',$startupState['carpool-v13-test-app'],'wget','-q','-T','3','-O','-','http://127.0.0.1:8080/health') -Name 'application-internal-health'
        $response = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'application-internal-health.stdout.log') -Raw | ConvertFrom-Json
        if ($response.status -eq 'ok') { $healthy = $true; break }
    } catch { }
    Start-Sleep -Seconds 1
}
if (-not $healthy) { throw 'Application health did not pass; inspect restricted Docker logs before retry.' }
foreach ($definition in $definitions) {
    $container = (& $runtimeDocker inspect $definition.Name | ConvertFrom-Json)[0]
    if ($container.Id -ne $startupState[$definition.Name]) { throw 'Application or mock container was replaced during startup.' }
    Assert-LocalRuntimeContainerRole -Container $container -Definition $definition -ExpectedEnvironment $definition.Environment
}
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-app','sh','-c','command -v wget') -Name 'app-egress-probe-present'
Invoke-PrivateRuntimeCommand -Executable $runtimeDocker -Arguments @('exec','carpool-v13-test-app','wget','-q','-T','3','-O','/dev/null','http://1.1.1.1') -Name 'app-egress-denied' -ExpectedExitCodes @(1)
$egressError = Get-Content -LiteralPath (Join-Path $runtimeDirectory 'app-egress-denied.stderr.log') -Raw
if ($egressError -notmatch 'Network unreachable|No route to host|Operation not permitted') { throw 'Egress denial did not report a routing-level failure.' }
$startupState.Healthy = $true
Save-LocalStartupState
$ready = [ordered]@{Status='LOCAL_RUNTIME_HEALTHY'; URL='http://127.0.0.1:38088'; HostAccess='requires-separate-loopback-verification'; Network=$runtimeNetwork; App='carpool-v13-test-app'; Mock='carpool-v13-test-mock'; StartedAt=[DateTimeOffset]::UtcNow.ToString('o'); Credentials='private runtime/fixtures.json'; Acceptance='pending'}
Write-PrivateRuntimeJSON -Path (Join-Path $runtimeDirectory 'runtime-ready.json') -Value $ready
[pscustomobject]$ready

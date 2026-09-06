$ErrorActionPreference = 'Stop'

function ConvertFrom-RebuildDockerInspect {
    param([Parameter(Mandatory=$true)][string[]]$Output)
    try { $objects = @($Output | ConvertFrom-Json) }
    catch { throw 'A Docker inspection returned malformed JSON.' }
    if ($objects.Count -ne 1) { throw 'A Docker inspection returned an unexpected result count.' }
    return $objects[0]
}

function Merge-RebuildEnvironment {
    param([string[]]$Base, [string[]]$Overlay = @())
    $values = [Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
    foreach ($entry in @($Base) + @($Overlay)) {
        $parts = $entry.Split('=', 2)
        if ($parts.Count -ne 2 -or [string]::IsNullOrEmpty($parts[0])) { throw 'Container image has an invalid environment entry.' }
        $values[$parts[0]] = $parts[1]
    }
    return [string[]]@($values.GetEnumerator() | ForEach-Object { $_.Key + '=' + $_.Value } | Sort-Object -CaseSensitive)
}

function Get-RebuildTextSha256 {
    param([Parameter(Mandatory=$true)][string]$Value)
    $bytes = [Text.Encoding]::UTF8.GetBytes($Value)
    try { return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)) }
    finally { [Array]::Clear($bytes, 0, $bytes.Length) }
}

function Get-RebuildObjectSha256 {
    param([Parameter(Mandatory=$true)][object]$Value)
    return Get-RebuildTextSha256 -Value ($Value | ConvertTo-Json -Depth 30 -Compress)
}

function Write-RebuildCreateOnlyJson {
    param([Parameter(Mandatory=$true)][string]$Path, [Parameter(Mandatory=$true)][object]$Value)
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes(($Value | ConvertTo-Json -Depth 30))
    $stream = [IO.FileStream]::new($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $stream.Write($bytes, 0, $bytes.Length)
        $stream.Flush($true)
    } finally {
        $stream.Dispose()
        [Array]::Clear($bytes, 0, $bytes.Length)
    }
}

function New-RebuildContinuationState {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$PreviousState,
        [Parameter(Mandatory=$true)][string]$CodeEvidenceHash,
        [Parameter(Mandatory=$true)][string]$ToolingHash,
        [Parameter(Mandatory=$true)][string]$ImageId,
        [Parameter(Mandatory=$true)][string]$ImageDirectory,
        [Parameter(Mandatory=$true)][string]$BootstrapSha256,
        [Parameter(Mandatory=$true)][string]$ManifestPath,
        [Parameter(Mandatory=$true)][string]$PreviousStateSha256,
        [Parameter(Mandatory=$true)][string]$RebuiltAt
    )
    $continuation = [ordered]@{}
    foreach ($key in $PreviousState.Keys) { $continuation[$key] = $PreviousState[$key] }
    $continuation.CodeEvidenceHash = $CodeEvidenceHash
    $continuation.ToolingHash = $ToolingHash
    $continuation.ImageId = $ImageId
    $continuation.ImageDirectory = $ImageDirectory
    $continuation.BootstrapSha256 = $BootstrapSha256
    $continuation.BuildComplete = $true
    $continuation.Healthy = $false
    $continuation['carpool-v13-test-app'] = ''
    $continuation['carpool-v13-test-mock'] = ''
    $continuation.PreviousImageId = $PreviousState.ImageId
    $continuation.PreviousStartupStateSha256 = $PreviousStateSha256
    $continuation.RebuildManifest = $ManifestPath
    $continuation.RebuiltAt = $RebuiltAt
    return $continuation
}

function Assert-RebuildTmpfs {
    param([object]$Container, [hashtable]$Expected)
    $actual = if ($null -eq $Container.HostConfig.Tmpfs) { @() } else { @($Container.HostConfig.Tmpfs.PSObject.Properties) }
    if ($actual.Count -ne $Expected.Count) { throw 'Runtime container has unexpected tmpfs mounts.' }
    foreach ($item in $Expected.GetEnumerator()) {
        $match = @($actual | Where-Object { $_.Name -ceq $item.Key })
        $options = if ($match.Count -eq 1) { @($match[0].Value -split ',') } else { @() }
        if ($match.Count -ne 1 -or $options.Count -ne 4 -or $options -notcontains $item.Value.Mode -or
            $options -notcontains 'noexec' -or $options -notcontains 'nosuid' -or
            @($options | Where-Object { $_ -in $item.Value.Sizes }).Count -ne 1) {
            throw 'Runtime container has unexpected tmpfs options.'
        }
    }
}

function Assert-RebuildContainerRole {
    param(
        [Parameter(Mandatory=$true)][object]$Container,
        [Parameter(Mandatory=$true)][ValidateSet('application','mock')][string]$Role,
        [Parameter(Mandatory=$true)][string]$ExpectedId,
        [Parameter(Mandatory=$true)][string]$ExpectedImageId,
        [Parameter(Mandatory=$true)][string]$ExpectedRunNonce,
        [Parameter(Mandatory=$true)][string]$TaskLabelName,
        [Parameter(Mandatory=$true)][string]$RunLabelName,
        [Parameter(Mandatory=$true)][string]$ExpectedTask,
        [Parameter(Mandatory=$true)][string]$ExpectedNetwork,
        [Parameter(Mandatory=$true)][string]$ExpectedAlias,
        [Parameter(Mandatory=$true)][string[]]$ExpectedEnvironment,
        [Parameter(Mandatory=$true)][string]$AppVolume,
        [bool]$RequireRunning = $true
    )
    $taskLabel = $Container.Config.Labels.PSObject.Properties[$TaskLabelName].Value
    $runLabel = $Container.Config.Labels.PSObject.Properties[$RunLabelName].Value
    $networks = @($Container.NetworkSettings.Networks.PSObject.Properties)
    $actualEnvironment = [string[]]@(@($Container.Config.Env) | Sort-Object -CaseSensitive)
    $environmentDifference = @(Compare-Object -ReferenceObject $ExpectedEnvironment -DifferenceObject $actualEnvironment -CaseSensitive)
    $entrypoint = @($Container.Config.Entrypoint)
    $cmd = @($Container.Config.Cmd | Where-Object { $null -ne $_ })
    $capDrop = @($Container.HostConfig.CapDrop)
    $capAdd = @($Container.HostConfig.CapAdd | Where-Object { $null -ne $_ })
    $securityOptions = @($Container.HostConfig.SecurityOpt)
    if ($Container.Id -cne $ExpectedId -or ($RequireRunning -and -not $Container.State.Running) -or $Container.Image -cne $ExpectedImageId -or
        $taskLabel -cne $ExpectedTask -or $runLabel -cne $ExpectedRunNonce -or $Container.Config.User -cne '1000:1000' -or
        $Container.Config.WorkingDir -cne '/app' -or -not $Container.HostConfig.ReadonlyRootfs -or $Container.HostConfig.Privileged -or
        $capDrop.Count -ne 1 -or $capDrop[0] -cne 'ALL' -or $capAdd.Count -ne 0 -or
        $securityOptions.Count -ne 1 -or $securityOptions[0] -notmatch '^no-new-privileges(?::true)?$' -or
        $Container.HostConfig.NetworkMode -cne $ExpectedNetwork -or $networks.Count -ne 1 -or $networks[0].Name -cne $ExpectedNetwork -or
        $networks[0].Value.Aliases -notcontains $ExpectedAlias -or $environmentDifference.Count -ne 0 -or $entrypoint.Count -ne 1 -or $cmd.Count -ne 0) {
        throw "Existing $Role container does not match its reviewed runtime role."
    }
    $ports = if ($null -eq $Container.HostConfig.PortBindings) { @() } else { @($Container.HostConfig.PortBindings.PSObject.Properties) }
    $mounts = @($Container.Mounts | Where-Object { $null -ne $_ })
    if ($Role -eq 'application') {
        if ($entrypoint[0] -cne '/app/sub2api') { throw 'Application entrypoint changed.' }
        $bindings = if ($ports.Count -eq 1) { @($ports[0].Value) } else { @() }
        if ($ports.Count -ne 1 -or $ports[0].Name -cne '8080/tcp' -or $bindings.Count -ne 1 -or
            $bindings[0].HostIp -cne '127.0.0.1' -or $bindings[0].HostPort -cne '38088' -or
            $mounts.Count -ne 1 -or $mounts[0].Type -cne 'volume' -or $mounts[0].Name -cne $AppVolume -or
            $mounts[0].Destination -cne '/app/data' -or -not $mounts[0].RW) {
            throw 'Application ports or app-data mount changed.'
        }
        Assert-RebuildTmpfs -Container $Container -Expected @{
            '/tmp' = @{Mode='rw';Sizes=@('size=64m','size=64M','size=65536k','size=65536K','size=67108864')}
            '/var/lib/postgresql' = @{Mode='ro';Sizes=@('size=1m','size=1M','size=1024k','size=1024K','size=1048576')}
        }
    } else {
        if ($entrypoint[0] -cne '/app/mock-upstream' -or $ports.Count -ne 0 -or $mounts.Count -ne 0) { throw 'Mock role changed.' }
        Assert-RebuildTmpfs -Container $Container -Expected @{
            '/tmp' = @{Mode='rw';Sizes=@('size=16m','size=16M','size=16384k','size=16384K','size=16777216')}
            '/var/lib/postgresql' = @{Mode='ro';Sizes=@('size=1m','size=1M','size=1024k','size=1024K','size=1048576')}
        }
    }
}

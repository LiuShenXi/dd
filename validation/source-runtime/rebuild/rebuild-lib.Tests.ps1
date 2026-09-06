$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'rebuild-lib.ps1')

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-Throws {
    param([scriptblock]$Action, [string]$Message)
    $threw = $false
    try { & $Action } catch { $threw = $true }
    if (-not $threw) { throw $Message }
}

$dockerObject = ConvertFrom-RebuildDockerInspect -Output @('[','{"Id":"sha256:abc"}',']')
Assert-True -Condition ($dockerObject.Id -ceq 'sha256:abc') -Message 'Single-object Docker inspection JSON was not parsed correctly.'
Assert-Throws -Action { ConvertFrom-RebuildDockerInspect -Output '[{"Id":"one"},{"Id":"two"}]' } -Message 'Multiple Docker inspection objects were accepted.'
Assert-Throws -Action { ConvertFrom-RebuildDockerInspect -Output '{not-json}' } -Message 'Malformed Docker inspection JSON was accepted.'

$merged = Merge-RebuildEnvironment -Base @('B=old','A=1') -Overlay @('B=new','EMPTY=')
Assert-True -Condition (@(Compare-Object -ReferenceObject @('A=1','B=new','EMPTY=') -DifferenceObject $merged -CaseSensitive).Count -eq 0) -Message 'Environment overlay was not deterministic.'
Assert-Throws -Action { Merge-RebuildEnvironment -Base @('invalid') } -Message 'Invalid environment entry was accepted.'

$previous = [ordered]@{
    Task='09-05-sub2api-carpool-v1-3';RunNonce=('a' * 32);NetworkId='network';ConfigHash=('b' * 64)
    AppEnvironmentHash=('c' * 64);FixtureSha256=('d' * 64);ConfigInstalled=$true;FixturesCopied=$true
    BuildComplete=$true;Healthy=$true;ImageId=('sha256:' + ('e' * 64));ImageDirectory='old-image'
    BootstrapSha256=('f' * 64);'carpool-v13-test-app'=('1' * 64);'carpool-v13-test-mock'=('2' * 64)
}
$continuation = New-RebuildContinuationState -PreviousState $previous -CodeEvidenceHash ('3' * 64) -ToolingHash ('4' * 64) `
    -ImageId ('sha256:' + ('5' * 64)) -ImageDirectory 'new-image' -BootstrapSha256 ('6' * 64) `
    -ManifestPath 'private-manifest' -PreviousStateSha256 ('7' * 64) -RebuiltAt '2026-09-06T00:00:00Z'
Assert-True -Condition ($continuation.RunNonce -ceq $previous.RunNonce -and $continuation.NetworkId -ceq $previous.NetworkId) -Message 'Continuation changed the runtime identity.'
Assert-True -Condition ($continuation.ConfigHash -ceq $previous.ConfigHash -and $continuation.FixtureSha256 -ceq $previous.FixtureSha256 -and $continuation.ConfigInstalled -and $continuation.FixturesCopied) -Message 'Continuation did not preserve config or fixtures.'
Assert-True -Condition ($continuation.BuildComplete -and -not $continuation.Healthy -and -not $continuation['carpool-v13-test-app'] -and -not $continuation['carpool-v13-test-mock']) -Message 'Continuation did not clear only replaceable runtime identities.'
Assert-True -Condition ($continuation.PreviousImageId -ceq $previous.ImageId -and $previous.Healthy -and $previous['carpool-v13-test-app'] -eq ('1' * 64)) -Message 'Continuation construction mutated the archived state.'

$tempDirectory = Join-Path ([IO.Path]::GetTempPath()) ('carpool-rebuild-test-' + [Guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $tempDirectory)
try {
    $createOnlyPath = Join-Path $tempDirectory 'archive.json'
    Write-RebuildCreateOnlyJson -Path $createOnlyPath -Value @{value=1}
    Assert-Throws -Action { Write-RebuildCreateOnlyJson -Path $createOnlyPath -Value @{value=2} } -Message 'Create-only archive was overwritten.'
    Assert-True -Condition ((Get-Content -LiteralPath $createOnlyPath -Raw | ConvertFrom-Json).value -eq 1) -Message 'Create-only archive contents changed.'
} finally {
    Remove-Item -LiteralPath $tempDirectory -Recurse -Force
}

$app = @{
    Id=('1' * 64);Image=$previous.ImageId;State=@{Running=$true}
    Config=@{
        Labels=@{'com.codex.local-task'='09-05-sub2api-carpool-v1-3';'com.codex.local-run'=('a' * 32)}
        Env=@('A=1');Entrypoint=@('/app/sub2api');Cmd=$null;User='1000:1000';WorkingDir='/app'
    }
    HostConfig=@{
        ReadonlyRootfs=$true;Privileged=$false;CapDrop=@('ALL');CapAdd=$null;SecurityOpt=@('no-new-privileges')
        NetworkMode='carpool-v13-test-net';PortBindings=@{'8080/tcp'=@(@{HostIp='127.0.0.1';HostPort='38088'})}
        Tmpfs=@{'/tmp'='rw,noexec,nosuid,size=67108864';'/var/lib/postgresql'='ro,noexec,nosuid,size=1048576'}
    }
    NetworkSettings=@{Networks=@{'carpool-v13-test-net'=@{Aliases=@('carpool-app')}}}
    Mounts=@(@{Type='volume';Name='carpool-v13-test-appdata';Destination='/app/data';RW=$true})
} | ConvertTo-Json -Depth 20 | ConvertFrom-Json
Assert-RebuildContainerRole -Container $app -Role application -ExpectedId ('1' * 64) -ExpectedImageId $previous.ImageId `
    -ExpectedRunNonce ('a' * 32) -TaskLabelName 'com.codex.local-task' -RunLabelName 'com.codex.local-run' `
    -ExpectedTask '09-05-sub2api-carpool-v1-3' -ExpectedNetwork 'carpool-v13-test-net' -ExpectedAlias 'carpool-app' `
    -ExpectedEnvironment @('A=1') -AppVolume 'carpool-v13-test-appdata'
$app.Mounts[0].Name = 'another-volume'
Assert-Throws -Action {
    Assert-RebuildContainerRole -Container $app -Role application -ExpectedId ('1' * 64) -ExpectedImageId $previous.ImageId `
        -ExpectedRunNonce ('a' * 32) -TaskLabelName 'com.codex.local-task' -RunLabelName 'com.codex.local-run' `
        -ExpectedTask '09-05-sub2api-carpool-v1-3' -ExpectedNetwork 'carpool-v13-test-net' -ExpectedAlias 'carpool-app' `
        -ExpectedEnvironment @('A=1') -AppVolume 'carpool-v13-test-appdata'
} -Message 'Changed application volume was accepted.'
$app.Mounts[0].Name = 'carpool-v13-test-appdata'
$app.State.Running = $false
Assert-RebuildContainerRole -Container $app -Role application -ExpectedId ('1' * 64) -ExpectedImageId $previous.ImageId `
    -ExpectedRunNonce ('a' * 32) -TaskLabelName 'com.codex.local-task' -RunLabelName 'com.codex.local-run' `
    -ExpectedTask '09-05-sub2api-carpool-v1-3' -ExpectedNetwork 'carpool-v13-test-net' -ExpectedAlias 'carpool-app' `
    -ExpectedEnvironment @('A=1') -AppVolume 'carpool-v13-test-appdata' -RequireRunning:$false
Assert-Throws -Action {
    Assert-RebuildContainerRole -Container $app -Role application -ExpectedId ('1' * 64) -ExpectedImageId $previous.ImageId `
        -ExpectedRunNonce ('a' * 32) -TaskLabelName 'com.codex.local-task' -RunLabelName 'com.codex.local-run' `
        -ExpectedTask '09-05-sub2api-carpool-v1-3' -ExpectedNetwork 'carpool-v13-test-net' -ExpectedAlias 'carpool-app' `
        -ExpectedEnvironment @('A=1') -AppVolume 'carpool-v13-test-appdata'
} -Message 'Stopped application was accepted by a running-role check.'
Assert-Throws -Action {
    Assert-RebuildContainerRole -Container $app -Role application -ExpectedId ('9' * 64) -ExpectedImageId $previous.ImageId `
        -ExpectedRunNonce ('a' * 32) -TaskLabelName 'com.codex.local-task' -RunLabelName 'com.codex.local-run' `
        -ExpectedTask '09-05-sub2api-carpool-v1-3' -ExpectedNetwork 'carpool-v13-test-net' -ExpectedAlias 'carpool-app' `
        -ExpectedEnvironment @('A=1') -AppVolume 'carpool-v13-test-appdata' -RequireRunning:$false
} -Message 'Stopped replacement container was accepted for recovery removal.'

$mainPath = Join-Path (Split-Path $PSScriptRoot -Parent) 'rebuild-runtime.ps1'
$parseErrors = $null
[void][Management.Automation.Language.Parser]::ParseFile($mainPath, [ref]$null, [ref]$parseErrors)
Assert-True -Condition ($parseErrors.Count -eq 0) -Message 'Rebuild script has PowerShell parse errors.'
$source = Get-Content -LiteralPath $mainPath -Raw
$buildPosition = $source.IndexOf("@('build','--network=none','--pull=false','-t',`$newImageTag")
$manifestPosition = $source.IndexOf('Write-RebuildCreateOnlyJson -Path $manifestPath')
$removePosition = $source.IndexOf("Remove-RebuildOldContainer -Role application")
$commitPosition = $source.LastIndexOf("Write-PrivateRuntimeJSON -Path `$statePath")
$resumePosition = $source.LastIndexOf("'start-runtime.ps1'")
Assert-True -Condition ($buildPosition -ge 0 -and $buildPosition -lt $manifestPosition -and $manifestPosition -lt $removePosition -and $removePosition -lt $commitPosition -and $commitPosition -lt $resumePosition) -Message 'Rebuild safety phase order changed.'
Assert-True -Condition ($source.Contains('ResumeRebuild') -and $source.Contains("Phase = 'Prepared'") -and $source.Contains("Phase = 'ContinuationCommitted'")) -Message 'Durable rebuild recovery phases are missing.'
Assert-True -Condition ($source.Contains('Rebuild manifest archive paths must remain inside their original private rebuild directory.')) -Message 'Resume no longer anchors archived rebuild inputs to the manifest directory.'
Assert-True -Condition ($source.Contains("'runtime-recreation-failed'")) -Message 'Runtime recreation failures no longer retain a durable category.'
Assert-True -Condition ($source.Contains('if ($container.State.Running)') -and $source.Contains('-RequireRunning:$false')) -Message 'Stopped exact-ID container recovery path is missing.'
Assert-True -Condition (-not $source.Contains("@('volume','rm'") -and -not $source.Contains("@('rm','-f'")) -Message 'Rebuild script contains broad or forced deletion.'

Write-Output 'rebuild-runtime unit checks passed'

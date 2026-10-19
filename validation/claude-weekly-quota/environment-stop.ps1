param([switch]$RemoveVolumes, [switch]$ValidateOnly)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$docker = 'C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$endpoint = 'npipe:////./pipe/docker_engine'
$manifestPath = Join-Path $PSScriptRoot 'environment-active.json'
$state = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
if ($state.task -ne 'claude-weekly-quota' -or $state.endpoint -ne $endpoint -or -not $state.synthetic_only) { throw 'Unexpected task manifest.' }
if ($state.run_id -notmatch '^\d{8}T\d{6}Z-[a-f0-9]{8}$') { throw 'Invalid task run identity.' }

function Invoke-Docker([string[]]$Arguments) {
    $text = & $docker --host $endpoint @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) { throw "Docker $($Arguments[0]) failed: $text" }
    return ($text -join "`n")
}
function Assert-Labels($Resource) {
    if ($Resource.Labels.'com.codex.local-task' -ne $state.task -or $Resource.Labels.'com.codex.run-id' -ne $state.run_id) { throw 'Resource labels do not match the manifest.' }
}

# Validate the complete deletion set before the first removal. Unrecorded,
# relabeled, renamed, externally networked, or published resources are rejected.
foreach ($container in $state.containers) {
    $details = @(Invoke-Docker @('container','inspect',$container.id) | ConvertFrom-Json)[0]
    if ($details.Id -ne $container.id -or $details.Name -ne ('/' + $container.name)) { throw 'Container identity changed.' }
    Assert-Labels ([pscustomobject]@{Labels=$details.Config.Labels})
    $networks = @($details.NetworkSettings.Networks.PSObject.Properties)
    if ($networks.Count -ne 1 -or $networks[0].Value.NetworkID -ne $state.network.id) { throw 'Unexpected container network.' }
    if (@($details.HostConfig.PortBindings.PSObject.Properties).Count -ne 0) { throw 'Container publishes a host port.' }
    if ($details.Mounts | Where-Object { $_.Destination -match 'docker\.sock$' }) { throw 'Container has a Docker socket mount.' }
}
$network = @(Invoke-Docker @('network','inspect',$state.network.id) | ConvertFrom-Json)[0]
Assert-Labels $network
if ($network.Id -ne $state.network.id -or $network.Name -ne $state.network.name -or -not $network.Internal) { throw 'Network identity or isolation changed.' }
foreach ($member in $network.Containers.PSObject.Properties.Name) {
    if ($member -notin @($state.containers.id)) { throw 'Network contains an unmanifested container.' }
}
foreach ($volume in $state.volumes) {
    $details = @(Invoke-Docker @('volume','inspect',$volume.name) | ConvertFrom-Json)[0]
    Assert-Labels $details
    if ($details.Name -ne $volume.name) { throw 'Volume identity changed.' }
}
if ($ValidateOnly) {
    [pscustomobject]@{status='validated'; containers=$state.containers.Count; volumes=$state.volumes.Count; network=$state.network.id; removed=$false} | ConvertTo-Json
    exit 0
}

for ($index=$state.containers.Count-1; $index -ge 0; $index--) {
    $null = Invoke-Docker @('container','rm','--force',$state.containers[$index].id)
}
$null = Invoke-Docker @('network','rm',$state.network.id)
if ($RemoveVolumes) {
    foreach ($volume in $state.volumes) { $null = Invoke-Docker @('volume','rm',$volume.name) }
}
$state.status = 'stopped'
$state | Add-Member -NotePropertyName stopped_utc -NotePropertyValue ([DateTime]::UtcNow.ToString('o'))
$state | Add-Member -NotePropertyName volumes_removed -NotePropertyValue ([bool]$RemoveVolumes)
$archive = Join-Path $PSScriptRoot ('environment-stopped-' + $state.run_id + '.json')
$state | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $archive -Encoding utf8
Remove-Item -LiteralPath $manifestPath
[pscustomobject]@{status='stopped'; archive=$archive; volumes_removed=[bool]$RemoveVolumes} | ConvertTo-Json

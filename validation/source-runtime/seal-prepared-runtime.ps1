. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
$bindingPath = Join-Path $runtimeDirectory 'resource-binding.json'
if (-not (Test-Path -LiteralPath $bindingPath -PathType Leaf)) { throw 'Prepared resource binding is missing.' }
if (Test-Path -LiteralPath (Join-Path $runtimeDirectory 'startup-state.json')) { throw 'Runtime startup already began; refusing to alter its prepared binding.' }
$existingNames = @(& $runtimeDocker ps -a --format '{{.Names}}')
if (@($existingNames | Where-Object { $_ -in @('carpool-v13-test-app','carpool-v13-test-mock','carpool-v13-config-transfer') }).Count -gt 0) { throw 'A runtime container already exists; refusing to seal preparation.' }
$binding = Get-Content -LiteralPath $bindingPath -Raw | ConvertFrom-Json -AsHashtable
$configHash = (Get-FileHash -LiteralPath (Join-Path $runtimeDirectory 'config.yaml') -Algorithm SHA256).Hash
if ($binding.ConfigSha256 -ne $configHash) { throw 'Private configuration differs from the prepared binding.' }
$environmentHash = Assert-LocalRuntimeEnvironmentFile -Path (Join-Path $runtimeDirectory 'app.env')
if ($binding.ContainsKey('AppEnvSha256')) {
    if ($binding.AppEnvSha256 -ne $environmentHash) { throw 'Private runtime environment differs from the sealed binding.' }
    'PREPARED_RUNTIME_ALREADY_SEALED: canonical local environment hash already recorded.'
    return
}
$binding.AppEnvSha256 = $environmentHash
Write-PrivateRuntimeJSON -Path $bindingPath -Value $binding
'PREPARED_RUNTIME_SEALED: canonical local environment hash added without rebinding resources.'

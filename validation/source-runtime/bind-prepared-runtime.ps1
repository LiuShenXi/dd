. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
if (Test-Path -LiteralPath (Join-Path $runtimeDirectory 'resource-binding.json')) { throw 'Resources are already bound; do not rebind.' }
$configurationPath = Join-Path $runtimeDirectory 'config.yaml'
$configuration = Get-Content -LiteralPath $configurationPath -Raw | ConvertFrom-Json -AsHashtable
if ($configuration.database.host -ne 'carpool-db' -or $configuration.database.dbname -ne 'carpool_test' -or $configuration.redis.host -ne 'carpool-redis') { throw 'Prepared configuration is not local-only.' }
# Repair the first preparation's nesting without rotating any generated credential.
$configuration.gateway.Remove('cn_providers_balance_check_enabled') | Out-Null
$configuration.gateway.cn_providers = @{balance_check_enabled=$false}
Write-PrivateRuntimeJSON -Path $configurationPath -Value $configuration
$configuration = $null
Initialize-LocalRuntimeBinding
'PREPARED_RESOURCES_BOUND: exact sanitized database/cache IDs plus local config/environment hashes recorded.'

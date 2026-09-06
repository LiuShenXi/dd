. (Join-Path $PSScriptRoot 'runtime-common.ps1')
Assert-LocalRuntimeIsolation
[void](New-Item -ItemType Directory -Path $runtimeDirectory -Force)
$configPath = Join-Path $runtimeDirectory 'config.yaml'
if (Test-Path -LiteralPath $configPath) { throw 'Private runtime config exists; refusing to rotate secrets implicitly.' }
$postgresValues = @{}
foreach ($line in Get-Content -LiteralPath (Join-Path $runtimePrivateRoot 'postgres.env')) {
    $parts = $line.Split('=', 2)
    if ($parts.Count -eq 2) { $postgresValues[$parts[0]] = $parts[1] }
}
if ($postgresValues.POSTGRES_DB -ne 'carpool_test' -or $postgresValues.POSTGRES_USER -ne 'carpool_test' -or -not $postgresValues.POSTGRES_PASSWORD) { throw 'Unexpected local database environment.' }
$configuration = [ordered]@{
    server = @{ host='0.0.0.0'; port=8080; mode='release'; frontend_url='http://127.0.0.1:38088'; trusted_proxies=@() }
    database = @{ host='carpool-db'; port=5432; user='carpool_test'; dbname='carpool_test'; password=$postgresValues.POSTGRES_PASSWORD; sslmode='disable'; max_open_conns=30; max_idle_conns=5; user_platform_quota_flusher_enabled=$false }
    redis = @{ host='carpool-redis'; port=6379; db=0; password=''; enable_tls=$false }
    jwt = @{ secret=[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)); expire_hour=24 }
    totp = @{ encryption_key=[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)) }
    pricing = @{ remote_url=''; hash_url=''; data_dir='/app/data/pricing'; fallback_file='/app/resources/model-pricing/model_prices_and_context_window.json' }
    token_refresh = @{ enabled=$false }
    gateway = @{ cn_providers=@{balance_check_enabled=$false} }
    ops = @{ enabled=$false; cleanup=@{enabled=$false}; aggregation=@{enabled=$false} }
    usage_cleanup = @{ enabled=$false }
    dashboard_aggregation = @{ enabled=$false }
    batch_image = @{ enabled=$false; queue_enabled=$false }
    image_storage = @{ enabled=$false }
    security = @{ url_allowlist=@{enabled=$true; upstream_hosts=@('carpool-mock'); pricing_hosts=@('carpool-mock'); crs_hosts=@('carpool-mock'); allow_private_hosts=$true; allow_insecure_http=$true} }
    log = @{level='warn'; format='json'; env='local-acceptance'; output=@{to_stdout=$true; to_file=$false}}
    timezone = 'Asia/Shanghai'
}
# JSON is a structured YAML 1.2 subset and avoids ambiguous empty-env fallbacks.
Write-PrivateRuntimeJSON -Path $configPath -Value $configuration
$configuration = $null
$postgresValues = $null
$environmentLines = Get-LocalRuntimeEnvironmentLines
[IO.File]::WriteAllLines((Join-Path $runtimeDirectory 'app.env'), $environmentLines, [Text.UTF8Encoding]::new($false))
$bootstrapSource = Join-Path $PSScriptRoot 'bootstrap/main.go'
Invoke-PrivateRuntimeCommand -Executable $runtimeGo -Arguments @('build','-trimpath','-o',(Join-Path $runtimeDirectory 'bootstrap'),$bootstrapSource) -Name 'build-bootstrap' -WorkingDirectory (Join-Path $runtimeRepository 'backend') -Environment @{GOOS='linux';GOARCH='amd64';CGO_ENABLED='0'}
Initialize-LocalRuntimeBinding
'RUNTIME_PREPARED: config and synthetic bootstrap binary only; no application has started.'

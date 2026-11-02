$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$prefix = 'I4-ui-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
function SourceSnapshot {
    $files = @(& git -C $taskRoot ls-files --cached --others --exclude-standard -- frontend | Sort-Object -Unique)
    $items = @($files | ForEach-Object { [ordered]@{ path = $_; sha256 = (Get-FileHash -LiteralPath (Join-Path $taskRoot $_) -Algorithm SHA256).Hash.ToLowerInvariant() } })
    $bytes = [Text.Encoding]::UTF8.GetBytes(($items | ForEach-Object { $_.path + ' ' + $_.sha256 }) -join "`n")
    $hasher = [Security.Cryptography.SHA256]::Create()
    try { $hash = ([BitConverter]::ToString($hasher.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant() } finally { $hasher.Dispose() }
    return [ordered]@{ source_sha256 = $hash; files = $items }
}
Push-Location (Join-Path $taskRoot 'frontend')
try {
    $before = SourceSnapshot
    $before | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $PSScriptRoot ($prefix + '-source-before.json')) -Encoding utf8
    $results = @()
    $steps = @(
        @{ name = 'typecheck'; args = @('typecheck') },
        @{ name = 'lint'; args = @('lint:check') },
        @{ name = 'tests'; args = @('test:run', '--maxWorkers=4', '--minWorkers=1', '--reporter=default', '--reporter=json', ('--outputFile=' + (Join-Path $PSScriptRoot ($prefix + '-tests.json')))) },
        @{ name = 'build'; args = @('build') }
    )
    foreach ($step in $steps) {
        $log = Join-Path $PSScriptRoot ($prefix + '-' + $step.name + '.log')
        $arguments = $step.args
        & pnpm @arguments *> $log
        $results += [ordered]@{ name = $step.name; exit_code = $LASTEXITCODE; log = [IO.Path]::GetFileName($log) }
        [pscustomobject]$results[-1] | ConvertTo-Json -Compress
    }
    $after = SourceSnapshot
    $after | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $PSScriptRoot ($prefix + '-source-after.json')) -Encoding utf8
    [ordered]@{ source_sha256 = $before.source_sha256; source_stable = $before.source_sha256 -eq $after.source_sha256; steps = $results; finished_utc = [DateTime]::UtcNow.ToString('o'); browser_acceptance = $false } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $PSScriptRoot ($prefix + '-receipt.json')) -Encoding utf8
} finally { Pop-Location }

param([Parameter(Mandatory)][string]$OutputPath)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$paths = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
foreach ($path in @(& git -C $repoRoot -c core.quotepath=false ls-files --cached --others --exclude-standard -- backend frontend docs/legal Dockerfile .dockerignore deploy/docker-entrypoint.sh)) {
    [void]$paths.Add($path.Replace('\','/'))
}
foreach ($file in Get-ChildItem -LiteralPath (Join-Path $repoRoot 'backend/internal/web/dist') -Recurse -File) {
    [void]$paths.Add($file.FullName.Substring($repoRoot.Length+1).Replace('\','/'))
}
$sorted = [string[]]@($paths)
[Array]::Sort($sorted, [StringComparer]::Ordinal)
$entries = @()
$aggregate = [Text.StringBuilder]::new()
$frontendAggregate = [Text.StringBuilder]::new()
foreach ($path in $sorted) {
    $absolute = Join-Path $repoRoot $path
    $hash = 'MISSING'
    $size = 0
    if (Test-Path -LiteralPath $absolute -PathType Leaf) {
        $bytes = [IO.File]::ReadAllBytes($absolute)
        $size = $bytes.Length
        $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
    }
    $entries += [pscustomobject]@{path=$path; sha256=$hash; bytes=$size}
    [void]$aggregate.Append($path).Append([char]0).Append($hash).Append("`n")
    if ($path.StartsWith('backend/internal/web/dist/',[StringComparison]::Ordinal)) {
        [void]$frontendAggregate.Append($path).Append([char]0).Append($hash).Append("`n")
    }
}
$sourceHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($aggregate.ToString()))).ToLowerInvariant()
$frontendHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($frontendAggregate.ToString()))).ToLowerInvariant()
$result = [ordered]@{
    captured_utc=[DateTime]::UtcNow.ToString('o'); root=$repoRoot
    head=((& git -C $repoRoot rev-parse HEAD) -join '').Trim()
    branch=((& git -C $repoRoot branch --show-current) -join '').Trim()
    algorithm='SHA256(sorted ordinal relative path + NUL + file SHA256 + LF)'
    scope='Git tracked and nonignored backend/frontend/legal/build inputs, plus all ignored embedded frontend dist files; includes test sources; excludes logs and validation artifacts'
    source_sha256=$sourceHash; frontend_dist_sha256=$frontendHash; file_count=$entries.Count; files=$entries
}
$result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $OutputPath -Encoding utf8
[pscustomobject]@{source_sha256=$sourceHash; frontend_dist_sha256=$frontendHash; file_count=$entries.Count; manifest=$OutputPath} | ConvertTo-Json -Compress

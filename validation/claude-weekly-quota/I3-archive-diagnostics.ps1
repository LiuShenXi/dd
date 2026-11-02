Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archivePath = Join-Path $PSScriptRoot 'I3-diagnostics-archive.zip'
$manifestPath = Join-Path $PSScriptRoot 'I3-diagnostics-archive-manifest.json'
if ((Test-Path -LiteralPath $archivePath) -or (Test-Path -LiteralPath $manifestPath)) {
    throw 'Archive already exists. Do not overwrite immutable diagnostic evidence.'
}
$files = @(Get-ChildItem -LiteralPath $PSScriptRoot -File | Where-Object {
    $_.Name -like 'I3-*' -and $_.Extension -in @('.json', '.jsonl', '.log') -and
    $_.Name -ne 'I3-pg-F3-source.json' -and
    -not ($_.Name -match 'F3' -and $_.Name -notmatch 'source-(before|after)|final-source')
} | Sort-Object Name)
$entries = @($files | ForEach-Object {
    [ordered]@{ name = $_.Name; bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
})
Compress-Archive -LiteralPath $files.FullName -DestinationPath $archivePath -CompressionLevel Optimal
$zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
try {
    if ($zip.Entries.Count -ne $entries.Count) { throw 'Archive entry count mismatch.' }
    foreach ($expected in $entries) {
        $entry = $zip.GetEntry($expected.name)
        if ($null -eq $entry -or $entry.Length -ne $expected.bytes) { throw 'Archive entry mismatch.' }
        $stream = $entry.Open()
        $hasher = [Security.Cryptography.SHA256]::Create()
        try {
            $actual = [Convert]::ToHexString($hasher.ComputeHash($stream)).ToLowerInvariant()
            if ($actual -cne $expected.sha256) { throw 'Archive SHA256 mismatch.' }
        } finally { $hasher.Dispose(); $stream.Dispose() }
    }
} finally { $zip.Dispose() }
$manifest = [ordered]@{
    purpose = 'Superseded/debug raw evidence and duplicate source manifests; originals retained locally.'
    archive = [IO.Path]::GetFileName($archivePath)
    archive_sha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    archive_bytes = (Get-Item -LiteralPath $archivePath).Length
    entry_count = $entries.Count
    uncompressed_bytes = ($files | Measure-Object Length -Sum).Sum
    verified_every_entry = $true
    entries = $entries
}
$manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding utf8
$ignorePath = Join-Path $PSScriptRoot '.gitignore'
$ignore = @('# I3 raw originals are preserved in the verified diagnostic archive.') + @($entries | ForEach-Object { '/' + $_.name })
if (Test-Path -LiteralPath $ignorePath) { Add-Content -LiteralPath $ignorePath -Value $ignore -Encoding utf8 }
else { $ignore | Set-Content -LiteralPath $ignorePath -Encoding utf8 }
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
Push-Location $repoRoot
try {
    $paths = @($entries | ForEach-Object { 'validation/claude-weekly-quota/' + $_.name })
    & git restore --staged -- $paths
    if ($LASTEXITCODE -ne 0) { throw 'Unstaging raw originals failed.' }
} finally { Pop-Location }
[pscustomobject]$manifest | Select-Object archive, archive_sha256, archive_bytes, entry_count, uncompressed_bytes, verified_every_entry | ConvertTo-Json

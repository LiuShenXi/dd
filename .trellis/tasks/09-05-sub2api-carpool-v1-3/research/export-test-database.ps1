param(
    [string]$Destination = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
)

$ErrorActionPreference = 'Stop'
$expectedDestination = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
if ([IO.Path]::GetFullPath($Destination) -ne [IO.Path]::GetFullPath($expectedDestination)) {
    throw 'Unexpected private export directory.'
}
New-Item -ItemType Directory -Path $Destination -Force | Out-Null
$taskUserSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
& icacls.exe $Destination /inheritance:r /grant:r "*${taskUserSid}:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Private directory ACL setup failed.' }

$archivePath = Join-Path $Destination 'production-snapshot.dump'
$partialPath = Join-Path $Destination 'production-snapshot.dump.partial'
$errorPath = Join-Path $Destination 'export.stderr.log'
if ((Test-Path -LiteralPath $archivePath) -or (Test-Path -LiteralPath $partialPath)) {
    throw 'An export already exists; refusing to overwrite it.'
}

$startInfo = [Diagnostics.ProcessStartInfo]::new()
$startInfo.FileName = (Get-Command ssh.exe).Source
$startInfo.UseShellExecute = $false
$startInfo.CreateNoWindow = $true
$startInfo.RedirectStandardOutput = $true
$startInfo.RedirectStandardError = $true
foreach ($argument in @('-T', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'newapi-vultr', 'docker exec sub2api-postgres pg_dump -U sub2api -d sub2api --format=custom --compress=6 --no-owner --no-acl --lock-wait-timeout=5000')) {
    $startInfo.ArgumentList.Add($argument)
}
$exportProcess = [Diagnostics.Process]::Start($startInfo)
$archiveStream = [IO.File]::Open($partialPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
$errorStream = [IO.File]::Open($errorPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
try {
    $archiveCopy = $exportProcess.StandardOutput.BaseStream.CopyToAsync($archiveStream)
    $errorCopy = $exportProcess.StandardError.BaseStream.CopyToAsync($errorStream)
    $exportProcess.WaitForExit()
    $archiveCopy.GetAwaiter().GetResult() | Out-Null
    $errorCopy.GetAwaiter().GetResult() | Out-Null
    if ($exportProcess.ExitCode -ne 0) { throw 'Read-only export failed; private diagnostics retained without printing data.' }
} finally {
    $archiveStream.Dispose()
    $errorStream.Dispose()
    $exportProcess.Dispose()
}
Move-Item -LiteralPath $partialPath -Destination $archivePath
$archiveInfo = Get-Item -LiteralPath $archivePath
$archiveHash = Get-FileHash -LiteralPath $archivePath -Algorithm SHA256
[pscustomobject]@{
    Status = 'EXPORTED'
    Bytes = $archiveInfo.Length
    SHA256 = $archiveHash.Hash
    Path = $archivePath
}

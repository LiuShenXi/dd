$ErrorActionPreference = 'Stop'
$privateDirectory = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
$archivePath = Join-Path $privateDirectory 'production-snapshot.dump'
$protectedPath = Join-Path $privateDirectory 'production-snapshot.dump.dpapi'
if (Test-Path -LiteralPath $protectedPath) { throw 'Encrypted archive already exists; refusing to overwrite.' }
$rawBytes = [IO.File]::ReadAllBytes($archivePath)
try {
    $expectedHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($rawBytes))
    $encryptedBytes = [Security.Cryptography.ProtectedData]::Protect($rawBytes, $null, [Security.Cryptography.DataProtectionScope]::CurrentUser)
    [IO.File]::WriteAllBytes($protectedPath, $encryptedBytes)
    $verificationBytes = [Security.Cryptography.ProtectedData]::Unprotect([IO.File]::ReadAllBytes($protectedPath), $null, [Security.Cryptography.DataProtectionScope]::CurrentUser)
    try {
        $verifiedHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($verificationBytes))
        if ($verifiedHash -ne $expectedHash) { throw 'Encrypted archive round-trip verification failed.' }
    } finally {
        [Array]::Clear($verificationBytes)
    }
    Remove-Item -LiteralPath $archivePath
    [pscustomobject]@{ Status='ENCRYPTED_AND_VERIFIED'; Recovery='Windows DPAPI CurrentUser'; SHA256=$expectedHash; PlaintextArchivePresent=(Test-Path -LiteralPath $archivePath) }
} finally {
    [Array]::Clear($rawBytes)
}

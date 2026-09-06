$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'transition-lib.ps1')

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

$configuration = [ordered]@{
    jwt = [ordered]@{secret='unchanged-private-value'}
    security = [ordered]@{url_allowlist=[ordered]@{
        enabled=$true; upstream_hosts=@('carpool-mock'); pricing_hosts=@('carpool-mock');
        crs_hosts=@('carpool-mock'); allow_private_hosts=$true; allow_insecure_http=$true
    }}
}
$updatedConfiguration = New-LocalMockHTTPConfiguration -Configuration $configuration
Assert-True -Condition (-not $updatedConfiguration.security.url_allowlist.enabled) -Message 'Allowlist was not disabled.'
Assert-True -Condition ($updatedConfiguration.security.url_allowlist.allow_insecure_http -and $updatedConfiguration.security.url_allowlist.allow_private_hosts) -Message 'Required local HTTP/private-host flags changed.'
Assert-True -Condition ($updatedConfiguration.jwt.secret -ceq 'unchanged-private-value' -and $configuration.security.url_allowlist.enabled) -Message 'Configuration projection changed unrelated data or mutated its input.'
Assert-Throws -Action {
    $unsafe = Copy-LocalMockHTTPValue -Value $configuration
    $unsafe.security.url_allowlist.upstream_hosts = @('example.com')
    New-LocalMockHTTPConfiguration -Configuration $unsafe
} -Message 'Non-mock upstream allowlist was accepted.'

$binding = [ordered]@{Task='task';NetworkId=('1' * 64);ConfigSha256=('2' * 64);AppEnvSha256=('3' * 64)}
$updatedBinding = New-LocalMockHTTPBinding -Binding $binding -ConfigHash ('4' * 64)
Assert-LocalMockHTTPDifferences -Before $binding -After $updatedBinding -ExpectedPaths @('ConfigSha256')
$state = [ordered]@{Task='task';RunNonce=('5' * 32);ConfigHash=('2' * 64);ConfigInstalled=$true;Healthy=$true;FixturesCopied=$true}
$updatedState = New-LocalMockHTTPStartupState -State $state -ConfigHash ('4' * 64)
Assert-LocalMockHTTPDifferences -Before $state -After $updatedState -ExpectedPaths @('ConfigHash','ConfigInstalled','Healthy')
Assert-True -Condition (-not $updatedState.ConfigInstalled -and -not $updatedState.Healthy) -Message 'Startup continuation flags were not cleared.'
$installedContinuation = Copy-LocalMockHTTPValue -Value $updatedState
$installedContinuation.ConfigInstalled = $true
Assert-LocalMockHTTPResumeState -OriginalState $state -CurrentState $installedContinuation -ConfigHash ('4' * 64)
$healthyContinuation = Copy-LocalMockHTTPValue -Value $installedContinuation
$healthyContinuation.Healthy = $true
Assert-LocalMockHTTPResumeState -OriginalState $state -CurrentState $healthyContinuation -ConfigHash ('4' * 64)
Assert-Throws -Action {
    $changedContinuation = Copy-LocalMockHTTPValue -Value $installedContinuation
    $changedContinuation.RunNonce = ('9' * 32)
    Assert-LocalMockHTTPResumeState -OriginalState $state -CurrentState $changedContinuation -ConfigHash ('4' * 64)
} -Message 'Runtime resume accepted an unrelated startup-state change.'

$pendingReady = [ordered]@{Status='LOCAL_RUNTIME_CONFIG_TRANSITION_PENDING';Transition='private-manifest';UpdatedAt='2026-09-05T19:15:29.5540000+00:00'}
$roundTrippedReady = [ordered]@{Status='LOCAL_RUNTIME_CONFIG_TRANSITION_PENDING';Transition='private-manifest';UpdatedAt='2026-09-05T19:15:29.5540000Z'}
Assert-LocalMockHTTPPendingReadyEquivalent -Expected $pendingReady -Actual $roundTrippedReady
Assert-Throws -Action {
    $changedReady = Copy-LocalMockHTTPValue -Value $roundTrippedReady
    $changedReady.UpdatedAt = '2026-09-05T19:15:30.5540000Z'
    Assert-LocalMockHTTPPendingReadyEquivalent -Expected $pendingReady -Actual $changedReady
} -Message 'A changed pending-ready timestamp was accepted.'

$temporary = Join-Path ([IO.Path]::GetTempPath()) ('local-mock-http-test-' + [Guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $temporary)
try {
    $path = Join-Path $temporary 'archive.json'
    Write-LocalMockHTTPCreateOnlyJSON -Path $path -Value @{value=1}
    Assert-Throws -Action { Write-LocalMockHTTPCreateOnlyJSON -Path $path -Value @{value=2} } -Message 'Create-only archive was overwritten.'
    Assert-True -Condition ((Get-Content -LiteralPath $path -Raw | ConvertFrom-Json).value -eq 1) -Message 'Archived JSON changed.'
    $destination = Join-Path $temporary 'live.json'
    [IO.File]::WriteAllText($destination, 'old')
    Copy-LocalMockHTTPAtomicFile -Source $path -Destination $destination
    Assert-True -Condition ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ceq (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash) -Message 'Atomic publication changed archived intent bytes.'
} finally {
    Remove-Item -LiteralPath $temporary -Recurse -Force
}

$mainPath = Join-Path (Split-Path $PSScriptRoot -Parent) 'enable-local-mock-http.ps1'
$parseErrors = $null
[void][Management.Automation.Language.Parser]::ParseFile($mainPath, [ref]$null, [ref]$parseErrors)
Assert-True -Condition ($parseErrors.Count -eq 0) -Message 'Configuration transition script has parse errors.'
$source = Get-Content -LiteralPath $mainPath -Raw
$manifestPosition = $source.IndexOf('Write-LocalMockHTTPCreateOnlyJSON -Path $manifestPath')
$stopPosition = $source.IndexOf("@('stop','--time','15',`$manifest.AppId)")
$commitPosition = $source.IndexOf('Copy-LocalMockHTTPAtomicFile -Source $manifest.IntendedConfigPath')
$resumePosition = $source.LastIndexOf("'start-runtime.ps1'")
Assert-True -Condition ($manifestPosition -ge 0 -and $manifestPosition -lt $stopPosition -and $stopPosition -lt $commitPosition -and $commitPosition -lt $resumePosition) -Message 'Configuration transition safety order changed.'
Assert-True -Condition ($source.Contains("Phase = 'Prepared'") -and $source.Contains("Phase = 'AppStopped'") -and $source.Contains("Phase = 'ContinuationCommitted'")) -Message 'Durable recovery phases are missing.'
Assert-True -Condition ($source.Contains("-RuntimeEvidenceHash `$state.CodeEvidenceHash") -and $source.Contains("-RuntimeEvidenceHash `$manifest.CodeEvidenceHash")) -Message 'Immutable code-ready evidence checks are missing.'
Assert-True -Condition ($source.Contains("`$manifest.Phase -ne 'ContinuationCommitted' -and `$containerNames -contains `$transferName")) -Message 'Delegated startup transfer recovery is blocked.'
Assert-True -Condition ($source.Contains('LastFailureDetail') -and $source.Contains('ScriptStackTrace')) -Message 'Private transition diagnostics are not retained.'
Assert-True -Condition (-not $source.Contains("@('stop','--time','15',`$manifest.MockId)") -and -not $source.Contains("@('rm'") -and -not $source.Contains("@('volume'")) -Message 'Transition can stop/remove resources outside the exact application container.'

Write-Output 'local mock HTTP transition unit checks passed'

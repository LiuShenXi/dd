$ErrorActionPreference = 'Stop'

function Copy-LocalMockHTTPValue {
    param([Parameter(Mandatory=$true)][object]$Value)
    return ($Value | ConvertTo-Json -Depth 30 | ConvertFrom-Json -AsHashtable)
}

function Get-LocalMockHTTPDifferencePaths {
    param(
        [AllowNull()][object]$Before,
        [AllowNull()][object]$After,
        [string]$Path = ''
    )
    if ($null -eq $Before -or $null -eq $After) {
        if ($null -ne $Before -or $null -ne $After) { return ,$Path }
        return @()
    }
    if ($Before -is [Collections.IDictionary] -and $After -is [Collections.IDictionary]) {
        $keys = @($Before.Keys) + @($After.Keys) | Sort-Object -CaseSensitive -Unique
        $differences = @()
        foreach ($key in $keys) {
            $childPath = if ($Path) { "$Path.$key" } else { [string]$key }
            if (-not $Before.Contains($key) -or -not $After.Contains($key)) {
                $differences += $childPath
                continue
            }
            $differences += @(Get-LocalMockHTTPDifferencePaths -Before $Before[$key] -After $After[$key] -Path $childPath)
        }
        return $differences
    }
    $beforeList = $Before -is [Collections.IList] -and $Before -isnot [string]
    $afterList = $After -is [Collections.IList] -and $After -isnot [string]
    if ($beforeList -or $afterList) {
        if (-not $beforeList -or -not $afterList -or $Before.Count -ne $After.Count) { return ,$Path }
        $differences = @()
        for ($index = 0; $index -lt $Before.Count; $index++) {
            $differences += @(Get-LocalMockHTTPDifferencePaths -Before $Before[$index] -After $After[$index] -Path "$Path[$index]")
        }
        return $differences
    }
    if ($Before.GetType() -ne $After.GetType() -or -not [object]::Equals($Before, $After)) { return ,$Path }
    return @()
}

function Assert-LocalMockHTTPDifferences {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Before,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$After,
        [Parameter(Mandatory=$true)][string[]]$ExpectedPaths
    )
    $actual = @(Get-LocalMockHTTPDifferencePaths -Before $Before -After $After | Sort-Object -CaseSensitive)
    $expected = @($ExpectedPaths | Sort-Object -CaseSensitive)
    if (@(Compare-Object -ReferenceObject $expected -DifferenceObject $actual -CaseSensitive).Count -ne 0) {
        throw 'Structured transition changed an unexpected property.'
    }
}

function New-LocalMockHTTPConfiguration {
    param([Parameter(Mandatory=$true)][Collections.IDictionary]$Configuration)
    $allowlist = $Configuration.security.url_allowlist
    if ($allowlist -isnot [Collections.IDictionary] -or $allowlist.enabled -isnot [bool] -or $allowlist.enabled -ne $true -or
        $allowlist.allow_insecure_http -isnot [bool] -or $allowlist.allow_insecure_http -ne $true -or
        $allowlist.allow_private_hosts -isnot [bool] -or $allowlist.allow_private_hosts -ne $true) {
        throw 'Local URL policy is not the reviewed mock-only HTTP configuration.'
    }
    foreach ($hostList in @('upstream_hosts','pricing_hosts','crs_hosts')) {
        $hosts = @($allowlist[$hostList])
        if ($hosts.Count -ne 1 -or $hosts[0] -cne 'carpool-mock') {
            throw 'Local URL policy is not restricted to the synthetic mock host.'
        }
    }
    $updated = Copy-LocalMockHTTPValue -Value $Configuration
    $updated.security.url_allowlist.enabled = $false
    Assert-LocalMockHTTPDifferences -Before $Configuration -After $updated -ExpectedPaths @('security.url_allowlist.enabled')
    return $updated
}

function New-LocalMockHTTPBinding {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Binding,
        [Parameter(Mandatory=$true)][string]$ConfigHash
    )
    if ($ConfigHash -cnotmatch '^[0-9A-Fa-f]{64}$') { throw 'New configuration hash is invalid.' }
    $updated = Copy-LocalMockHTTPValue -Value $Binding
    $updated.ConfigSha256 = $ConfigHash
    Assert-LocalMockHTTPDifferences -Before $Binding -After $updated -ExpectedPaths @('ConfigSha256')
    return $updated
}

function New-LocalMockHTTPStartupState {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$State,
        [Parameter(Mandatory=$true)][string]$ConfigHash
    )
    if ($ConfigHash -cnotmatch '^[0-9A-Fa-f]{64}$' -or -not $State.ConfigInstalled -or -not $State.Healthy) {
        throw 'Healthy installed startup state is required for configuration transition.'
    }
    $updated = Copy-LocalMockHTTPValue -Value $State
    $updated.ConfigHash = $ConfigHash
    $updated.ConfigInstalled = $false
    $updated.Healthy = $false
    Assert-LocalMockHTTPDifferences -Before $State -After $updated -ExpectedPaths @('ConfigHash','ConfigInstalled','Healthy')
    return $updated
}

function Assert-LocalMockHTTPResumeState {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$OriginalState,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$CurrentState,
        [Parameter(Mandatory=$true)][string]$ConfigHash
    )
    if ($ConfigHash -cnotmatch '^[0-9A-Fa-f]{64}$' -or $CurrentState.ConfigHash -cne $ConfigHash -or
        $CurrentState.ConfigInstalled -isnot [bool] -or $CurrentState.Healthy -isnot [bool] -or
        ($CurrentState.Healthy -and -not $CurrentState.ConfigInstalled)) {
        throw 'Runtime resume state has invalid configuration flags.'
    }
    $normalized = Copy-LocalMockHTTPValue -Value $CurrentState
    $normalized.ConfigHash = $OriginalState.ConfigHash
    $normalized.ConfigInstalled = $OriginalState.ConfigInstalled
    $normalized.Healthy = $OriginalState.Healthy
    if (@(Get-LocalMockHTTPDifferencePaths -Before $OriginalState -After $normalized).Count -ne 0) {
        throw 'Runtime resume changed state outside the bounded configuration flags.'
    }
}

function Assert-LocalMockHTTPPendingReadyEquivalent {
    param(
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Expected,
        [Parameter(Mandatory=$true)][Collections.IDictionary]$Actual
    )
    $expectedKeys = @($Expected.Keys | Sort-Object -CaseSensitive)
    $actualKeys = @($Actual.Keys | Sort-Object -CaseSensitive)
    $requiredKeys = @('Status','Transition','UpdatedAt') | Sort-Object -CaseSensitive
    $expectedTime = [DateTimeOffset]::MinValue
    $actualTime = [DateTimeOffset]::MinValue
    if (@(Compare-Object -ReferenceObject $requiredKeys -DifferenceObject $expectedKeys -CaseSensitive).Count -ne 0 -or
        @(Compare-Object -ReferenceObject $requiredKeys -DifferenceObject $actualKeys -CaseSensitive).Count -ne 0 -or
        $Expected.Status -cne 'LOCAL_RUNTIME_CONFIG_TRANSITION_PENDING' -or $Actual.Status -cne $Expected.Status -or
        $Actual.Transition -cne $Expected.Transition -or
        -not [DateTimeOffset]::TryParse([string]$Expected.UpdatedAt, [ref]$expectedTime) -or
        -not [DateTimeOffset]::TryParse([string]$Actual.UpdatedAt, [ref]$actualTime) -or
        $actualTime.ToUniversalTime() -ne $expectedTime.ToUniversalTime()) {
        throw 'Pending ready state differs from the archived transition intent.'
    }
}

function Write-LocalMockHTTPCreateOnlyJSON {
    param([Parameter(Mandatory=$true)][string]$Path, [Parameter(Mandatory=$true)][object]$Value)
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes(($Value | ConvertTo-Json -Depth 30))
    $stream = [IO.FileStream]::new($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $stream.Write($bytes, 0, $bytes.Length)
        $stream.Flush($true)
    } finally {
        $stream.Dispose()
        [Array]::Clear($bytes, 0, $bytes.Length)
    }
}

function Copy-LocalMockHTTPCreateOnlyFile {
    param([Parameter(Mandatory=$true)][string]$Source, [Parameter(Mandatory=$true)][string]$Destination)
    $input = [IO.FileStream]::new($Source, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    $output = $null
    try {
        $output = [IO.FileStream]::new($Destination, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $input.CopyTo($output)
        $output.Flush($true)
    } finally {
        if ($output) { $output.Dispose() }
        $input.Dispose()
    }
}

function Copy-LocalMockHTTPAtomicFile {
    param([Parameter(Mandatory=$true)][string]$Source, [Parameter(Mandatory=$true)][string]$Destination)
    $temporary = $Destination + '.pending-' + [Guid]::NewGuid().ToString('N')
    $input = [IO.FileStream]::new($Source, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    $output = $null
    try {
        $output = [IO.FileStream]::new($temporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $input.CopyTo($output)
        $output.Flush($true)
    } finally {
        if ($output) { $output.Dispose() }
        $input.Dispose()
    }
    [IO.File]::Move($temporary, $Destination, $true)
}

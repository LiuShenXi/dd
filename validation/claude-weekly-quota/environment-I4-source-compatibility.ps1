function Get-CwqI4SourceCompatibility {
 param([Parameter(Mandatory)]$Driver,[Parameter(Mandatory)][string]$ImageSource,[Parameter(Mandatory)][string]$ImageID,[string]$SourceBridgeReceipt='')
 if($Driver.source_sha256 -ceq $ImageSource){return [pscustomobject]@{compatible=$true;mode='exact_source';receipt=$null}}
 if(-not $SourceBridgeReceipt){throw 'Driver/image source mismatch requires the explicit coordinator test-only bridge receipt'}
 if([IO.Path]::GetFileName($SourceBridgeReceipt) -cne 'coordinator-I4-F2-F3-test-only-bridge.json'){throw 'Unexpected source bridge receipt'}
 $bridge=Get-Content -LiteralPath $SourceBridgeReceipt -Raw|ConvertFrom-Json
 $expected='backend/internal/service/usage_cleanup_service_test.go'
 $provider=(Get-FileHash -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-provider/main.go') -Algorithm SHA256).Hash.ToLowerInvariant()
 if($bridge.approved -ne $true -or $bridge.unchanged_non_test_source -ne $true -or $bridge.image_source_sha256 -cne $ImageSource -or $bridge.driver_source_sha256 -cne $Driver.source_sha256 -or $bridge.candidate_image_id -cne $ImageID -or $bridge.frontend_dist_sha256 -cne $Driver.frontend_dist_sha256 -or $bridge.provider_source_sha256 -cne $provider -or @($bridge.changed_files).Count -ne 1 -or $bridge.changed_files[0] -cne $expected){throw 'Approved source bridge tuple does not match current driver/image/frontend/provider'}
 function Resolve-CwqManifest([string]$Path){if([IO.Path]::IsPathRooted($Path)){return $Path};return Join-Path $PSScriptRoot $Path}
 $before=Get-Content -LiteralPath (Resolve-CwqManifest $bridge.before_manifest) -Raw|ConvertFrom-Json
 $after=Get-Content -LiteralPath (Resolve-CwqManifest $bridge.after_manifest) -Raw|ConvertFrom-Json
 if($before.source_sha256 -cne $ImageSource -or $after.source_sha256 -cne $Driver.source_sha256 -or $before.file_count -ne $after.file_count -or $after.file_count -ne $Driver.file_count){throw 'Source bridge manifest identity/count mismatch'}
 $old=@{};foreach($file in $before.files){$old[$file.path]=$file.sha256}
 $current=@{};foreach($file in $Driver.files){$current[$file.path]=$file.sha256}
 $changed=@();foreach($file in $after.files){if(-not $old.ContainsKey($file.path) -or $old[$file.path] -cne $file.sha256){$changed+=$file.path};if(-not $current.ContainsKey($file.path) -or $current[$file.path] -cne $file.sha256){throw 'Current source differs from approved after manifest'}}
 if($changed.Count -ne 1 -or $changed[0] -cne $expected){throw 'Source bridge contains changes beyond the sole approved test stub synchronization'}
 return [pscustomobject]@{compatible=$true;mode='approved_test_only_bridge';receipt=[IO.Path]::GetFileName($SourceBridgeReceipt);image_source_sha256=$ImageSource;driver_source_sha256=$Driver.source_sha256;changed_files=$changed}
}

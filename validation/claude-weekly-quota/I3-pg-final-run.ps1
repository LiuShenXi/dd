param([Parameter(Mandatory)][string]$Freeze)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$pattern='^TestSubscriptionQuota(Billing_|Migrations_|Domain_|Protected)|^TestSubscriptionQuota(ClaudeKeysSurviveGPTOpenAndRenew|LegacyRepositorySinksCannotMutateTermParent)$|^TestCarpool(BillingBindingPreservesGroupAndAdminThroughAuth|StandardGroupOpenRenewAndExistingKeyAuth|StandardOpeningRejectsConflictsWithoutMutation|RollingLastShortNaturalPeriodGrantsFullQuotaAndExpiryWins|Lifecycle_FourConcurrentBoostClaimsGrantExactlyTwoAndRefreshQuota|OfficialReset_BypassesCardScheduleAndPreservesScopeState|PlanVersionAndRenewalUseFixedCurrentRules|RecoveryRepository_SkipsLockedStaleAdmissionAndPromotesKnownReceipt|Rolling_UnresolvedReceiptsHoldClosingUntilOriginalCycleSettles|AdmissionLockWait_RetriesIntoCurrentCycle)$|^TestCarpoolResetExecution_|^TestUsageBillingRepositoryApply_CarpoolConcurrentSettlementAppliesOnce$'
$inventory=@(& rg --no-filename --only-matching '^func Test\w+' (Join-Path $repoRoot 'backend/internal/repository') -g '*test.go' | ForEach-Object {$_ -replace '^func ',''} | Where-Object {$_ -match $pattern} | Sort-Object)
if($inventory.Count -ne 87){throw "Frozen PG inventory changed: expected87, found$($inventory.Count). Review scope before accepting new evidence."}
$plan=[ordered]@{freeze=$Freeze;validation_kind='original TestMain real PostgreSQL18 integration and fault/concurrency';pattern=$pattern;expected_top_level_tests=87;quota_billing=37;prior_I1_I2_and_GPT_baselines=29;additional_original_GPT_lifecycle_recovery=8;original_GPT_automatic_reset_execution=13;tests=$inventory;image_recovery_test_is_separate=$true;synthetic_only=$true}
$plan|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $PSScriptRoot ("I3-pg-$Freeze-inventory.json")) -Encoding utf8
& (Join-Path $PSScriptRoot 'I3-pg-run.ps1') -Freeze $Freeze -Pattern $pattern -Expected 87
exit $LASTEXITCODE

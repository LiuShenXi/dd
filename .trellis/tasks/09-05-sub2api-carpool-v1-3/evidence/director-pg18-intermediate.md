# Intermediate PG18 Review

This is not final acceptance or code-ready. The initial sections below predate application startup; the later local-start image is now internally healthy and source issued loopback HOSTREADY. That image predates subsequent fixes and is not final acceptance.

## Real Execution

The source coordinator's initial bounded run `20260905T175942Z-0a86f184` reported 10 top-level tests passed. Director independently inspected the subsequent two runs' summary and verbose test-name/assertion lines: source run `20260905T180311Z-968e24fa` and director run `20260905T180346Z-8ea412e8`. Each executed 17 top-level tests with exit 1: 13 passed and 4 failed. They completed at 18:03:28 UTC and 18:04:02 UTC respectively; actual resource lifetimes did not overlap.

The director independently hashed the latter Linux binary as `24ABA1E32032345F2D895381886FE27840DDB88266E268CE3080FE1276BBD007`. Private summaries and raw logs remain outside git under the run directories. No secret env file or copied identity is included here.

Verified passing groups in both 17-test runs:

- Eight `TestCarpoolBillingReconciliation_*` tests, including same-key concurrent byte-identical replay and forced final-operation failure after debit.
- Three `TestUsageBillingRepositoryApply_Carpool*` tests: concurrent one-time application, replay without second debit, and post-debit failure rollback.
- `TestCarpoolRepositoryMarkReconcileRequired_PreservesKnownUsageReceipt`.
- `TestSourceRuntimeHarnessIsolation`, with actual migrations and dedicated empty PG18/Redis.

Failures returned to core without weakening assertions:

- `TestCarpoolPreview_ValidatesTargetUserAndLatestEnabledPlan` and `TestCarpoolCreateTermRejectsSupersededPlan`: synthetic plan seed exceeds `varchar(32)` at `carpool_preview_integration_test.go:75`.
- `TestCarpoolScopeMembershipLock_SerializesTermCreation` and `TestCarpoolUserDetails_ResetCountIncludesSuccessfulZeroGrantTarget`: actual `CreateTerm` cycle INSERT fails with `pq: inconsistent types deduced for parameter $9`. The same parameter supplies `state` and a CASE comparison; production SQL needs explicit compatible typing or a separately bound activation timestamp.

## Execution Ownership

An ambiguous use of "main" caused both coordinators to invoke the runner. The director session has ended with exit 1 and its finally cleanup completed. Director confirmed no `carpool-v13-integration` containers remain; source independently confirmed exact-label container/volume absence and no active manifest. No copied-runtime resources were changed.

From this point only the SOURCE main task invokes or cleans E2. Director and workers submit exact test regexes and readiness. The director does not independently launch this shared resource set again.

## Other Evidence

`director-backend-unit-current.log` shows domain, handlers/admin/DTO/quotaview, middleware, repository and migrations passing. Service failed four changed group-message expectations, one existing content-moderation timeout, and four Windows plugin-package file-rename failures. The group assertions have been returned to gateway and reportedly corrected; the standalone moderation recheck still failed twice in three runs. Relevant moderation/plugin source and test files show no local diff. These facts do not constitute a baseline reproduction or a green full service suite.

`git diff --check` on backend/frontend and task contract paths exited 0, with only a line-ending warning for `backend/go.sum`. Reset/observer/outbox transactions, remaining time/lock boundaries, runtime HTTP/WS and desktop/narrow UI acceptance are still pending.

## Fixed Seventeen-Test Run And Full Build

Director independently inspected source run `20260905T181224Z-88be28ff` private verbose output: 17 top-level RUN, 17 PASS, zero FAIL. All four earlier failures passed after the SQL and fixture corrections. Source records exit 0 and binary SHA-256 `F655B6C36964CE51030F1BEF13EB4EF0E97E8239F84A359086176EC45A8439C9`; resource cleanup remains source-owned.

The director's second affected-unit run exited 1 only in the newly added two-turn WS test, where durable snapshot assertions received nil. Gateway identified a synthetic fixture missing `apiKey.UserID` despite a populated nested user ID and reports the corrected assertions pass. That report still requires final director regression review.

Core regenerated Wire after D added the missing audience and user-window repository methods. Director then independently ran `go build ./...` with the cached Go 1.27.0 toolchain from `backend`; exit 0, log `director-backend-full-build-second.log`. This is a real whole-backend compilation milestone, not a frozen-source startup approval. Core subsequently identified the still-missing `SetResetWindowReader` wiring, which remains to be corrected and rebuilt.

Source's next lock-boundary run stopped at compilation before creating resources because it captured a half-written balance-invariant test with an undefined local variable. Core corrected it and passed integration package compilation. All three owners acknowledged a short repository-only freeze for the source's combined five-test binary snapshot. This compile failure is not a test execution result.

## Boundary Execution And Baseline Reproduction

Director inspected actual verbose output for source run `20260905T182206Z-414de375`: four PASS, one FAIL, exit 1. Both key-lock admission and cycle-lock boost tests passed after real database waits across the boundary (2.03 seconds each); takeover closure and harness also passed. Adjustment closure failed at its expected base-balance assertion. Core pinned its plan premise and added per-step actual balance/ledger assertions without changing the expected -8 result; a fresh rerun is required. All repository writers were explicitly released after source compilation, and source owns completed cleanup.

Core completed reset-window reader wiring and reports focused wiring, Wire generation and full-build passes in `core-reset-wiring-test.log`, `core-wire-generate.log`, and `core-backend-full-build.log`. Director inspected the latter exit 0. D's remaining changes still invalidate any prospective final source hash.

The previously unresolved baseline classification is now supported by an independent exact-HEAD reproduction: four plugin installer failures in three of three repetitions, moderation timeout in one of three. See `director-baseline-service-review.md` and the actual `director-baseline-service-five-tests.log`. The current full service suite remains non-green; no unrelated fix was made.

## First Reset Transaction Run

Director independently inspected source run `20260905T190819Z-91d9f577` summary and actual verbose output: 16 RUN, 6 PASS, 10 FAIL, exit 1. Source confirmed compile capture before writers were released and owns cleanup. The empty dedicated PG18 database, not the copied runtime database, was used.

`TestCarpoolAdjustmentClosure_PreservesNetAndNeverCreatesNonBaseDebt` now passes, including explicit initial 550, per-step -10/-14/-8 and final closed -8 base balance with zero boost/manual debt and matching ledger net. The earlier failure's exact runtime value was not recorded, so the passing stricter fixture does not prove the previously proposed cross-test cause.

Also passing: the combined observation baseline/replacement/dedup/batch-merge test, all three scan-operation tests, and the isolation harness. The scan final-response failure test proves independently committed observation qualification is retained exactly once while a failed operation response is not persisted; retry does not duplicate qualification and only the successful retry response replays. This is not whole-scan transaction atomicity.

All seven reset execution and three announcement tests failed at shared fixture registration before their substantive assertions. The director verified the cause in source: the fixture allocator used `9_000_000_000 + sequence`, while `lockCarpoolScopeMembershipTx` rejects values above `2147483647` because its advisory-lock scope key is int32. Gateway owns correcting only the synthetic allocator and rerunning, with no widening of production scope or relaxed financial assertions. Execution and announcement cases are not accepted by this run.

## Final Reset And Recovery Transaction Run

Director independently reviewed the actual private summary and verbose output of source run `20260905T192309Z-87c0a6e5`: 20 top-level RUN, 20 PASS, zero FAIL, exit 0. Its 23 verbose RUN events include three audience subtests. The source-owned isolated PG18 harness, not the copied runtime database, ran the tests and cleaned its resources.

Coverage includes all seventeen reset execution/observation/announcement/scan tests, strict adjustment-closure balance/ledger equality, real SKIP LOCKED stale-admission recovery and known-cost promotion, and the isolation harness. New announcement cases verify SQL publication failure with durable retry exactly once, publication-relative dates, and suppression of obsolete promises after completion. Execution cases verify final term/cycle/minute rollback, same-minute cooldown seconds, all-or-nothing retry and zero-grant cycle five.

The intervening corrections were test-only: scope IDs now fit the existing int32 advisory-lock contract; ledger queries use `reset_batch_id`; the completion-suppression fixture covers the rescheduled execution with a real active cycle; unresolved recovery checks `KnownCostUSD` and nil resolved `ActualCostUSD`. No financial assertion or production boundary was relaxed. This evidence accepts these database regressions for the tested source, not final HTTP/WS/UI acceptance.

## Canonical Diagnostic Projection Regression

After the first rebuilt image exposed double-normalization in the administrator projection, core added exact self-mappings for the two existing missing-usage/persistence categories. Director inspected the all-alias/canonical unit table and actual repository list regression, then independently read source run `20260905T194859Z-4843a365` summary and full verbose output: two top-level tests and both canonical subtests passed, exit 0. `verbose_run_events: 4` includes the two subtests; it is not four independent top-level tests.

The test writes only an isolated synthetic fixture's diagnostic and reads through the real `ListBillingExceptions` repository query. Both `usage_receipt_missing` and `usage_persistence_or_settlement_failed` survive read-back. Source owns completed harness cleanup. The second immutable image and unchanged strict WS driver must still verify the actual HTTP projection end to end.

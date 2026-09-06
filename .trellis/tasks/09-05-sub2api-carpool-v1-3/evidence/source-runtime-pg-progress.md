# Source Runtime PG Verification

Status at 2026-09-06 Asia/Shanghai: expanded accounting, twenty-test reset/recovery, canonical-diagnostic projection and the final additive lifecycle run passed. Immutable V5 passed HTTP 67/67, strict WebSocket 11/11 and coherent pending-merge reset 22/22, recorded separately in `source-runtime-startup-progress.md`. Actual 22:00 reset execution is covered by PostgreSQL tests below, not counted as a live HTTP execution pass. The director owns final integrated acceptance.

## Passed Run

Run `20260905T175942Z-0a86f184` exited 0 with ten actual top-level `RUN` and `PASS` events. The source coordinator independently inspected the private summary and test-name outcomes.

- Six manual billing reconciliation cases: atomic actual-cost resolution, confirmed no-cost resolution, resolved/replay conflicts, transactional rollback, parallel resolution once, and delayed receipt preservation.
- Three gateway accounting cases: concurrent settlement once, replay without another debit, and failure rollback of all effects.
- One harness case verifies the distinct synthetic database, applied migrations and functioning isolated Redis.

Executed binary SHA-256: `24ABA1E32032345F2D895381886FE27840DDB88266E268CE3080FE1276BBD007`.

## Expanded Run

Source run `20260905T180311Z-968e24fa` exited 1 after seventeen actual top-level tests: thirteen passed and four failed. Two preview fixtures exceeded the existing 32-character plan-code limit. The scope-lock and zero-grant-reset-count tests exposed a PostgreSQL parameter-type conflict in term creation. These failures were returned to the core implementation owner without weakening assertions.

An accidental second dispatch by the director produced run `20260905T180346Z-8ea412e8` with the same outcomes. Timestamps show the first resource set had already completed before the second started. Both completed their bounded cleanup. Future E2 execution and cleanup belong exclusively to the source coordinator; the director and workers submit test patterns only.

## Verified Fixes

After the core owner fixed the plan-code fixtures and the actual term-insertion SQL parameter typing, source run `20260905T181224Z-88be28ff` passed the same expanded pattern. The source coordinator independently counted seventeen top-level `RUN`, seventeen `PASS`, zero `FAIL`, with process and summary exit 0. This includes latest-enabled-plan/user validation, superseded-plan rejection, scope-lock term-opening serialization and successful zero-grant reset counting, in addition to the accounting/reconciliation cases. Exact task/role-label filters found no E2 containers or test volume after cleanup.

Reset scheduling/observer transactions, live HTTP/WS and browser coverage are not implied by this result and remain separate gates.

Expanded-run binary SHA-256: `F655B6C36964CE51030F1BEF13EB4EF0E97E8239F84A359086176EC45A8439C9`.

## Lock Boundaries

Run `20260905T182206Z-414de375` executed five top-level tests, with four passes and one failure. Both admission and boost lock-wait tests passed after real PostgreSQL lock waits crossed a cycle boundary (about two seconds each). Takeover closure debt preservation and the harness also passed. The adjustment closure balance invariant failed at its exact value assertion and was returned to core for diagnosis; this run is not an overall pass.

## Reset Integration Run

Run `20260905T190819Z-91d9f577` executed sixteen top-level tests: six passed and ten failed. Adjustment closure now passed its exact balance invariant. Reset observation, all three reset-scan operation cases, and the harness also passed. All seven reset execution and three announcement cases failed at the shared qualification fixture (`carpool_reset_execution_integration_test.go:303`) with `CARPOOL_RELATIONSHIP_INVALID`, before their domain assertions. The director independently traced the fixture to scope IDs above the production advisory-lock int32 bound. The test owner will correct those synthetic IDs without widening production constraints or relaxing assertions. The coordinator released the compile-only freeze after binary capture and verified bounded E2 container cleanup.

Run `20260905T191725Z-3cd000d3` then executed twenty top-level tests: eleven passed and nine failed. The remaining failures were traced to the test ledger query using `batch_id` instead of `reset_batch_id`, a completion-suppression fixture whose cycle did not cover the rescheduled execution, and an unresolved-receipt assertion using `ActualCostUSD` instead of `KnownCostUSD`. Production contracts and accounting assertions were not widened.

After those test corrections, run `20260905T192309Z-87c0a6e5` passed twenty top-level tests, with twenty `PASS`, zero `FAIL`, and process exit 0. Three nested current/future/expired audience subtests also passed. This includes all seventeen reset execution, observation, announcement and scan-operation cases; adjustment closure; a real PostgreSQL `SKIP LOCKED` stale-admission/known-receipt recovery case; and the harness. The source coordinator independently counted top-level events, separating the three subtests from the twenty-test total.

## Canonical Diagnostic Projection

Run `20260905T194859Z-4843a365` executed the exact anchored pattern `^(TestSourceRuntimeHarnessIsolation|TestCarpoolBillingExceptionProjection_PreservesCanonicalDiagnostics)$` and exited 0. Independent output inspection counted two top-level tests and two canonical-diagnostic subtests, all passing. The real `ListBillingExceptions` projection preserves both `usage_receipt_missing` and `usage_persistence_or_settlement_failed` after repository sanitization. This is actual PostgreSQL evidence for the final WebSocket diagnostic correction, not an HTTP/WS image acceptance result.

The run used its own new empty synthetic database and Redis, not the copied runtime. Exact task/role-filtered checks found no remaining E2 containers or active manifest after bounded cleanup.

## Additive Lifecycle Evidence

The director's final acceptance audit identified missing direct lifecycle coverage, so the existing owner added only `carpool_lifecycle_integration_test.go`; application code and the V5 runtime remain unchanged. The source coordinator ran the exact four lifecycle tests plus the harness using an anchored pattern.

Run `20260906T001349Z-d4c3a8cd` exited 1: five top-level tests executed, three passed and two failed. Same-user cross-renewal net usage/history completeness/current-term reset count passed. Unresolved receipt closure passed for all four statuses (`admitted`, `usage_known`, `settling`, `reconcile_required`), including original-cycle settlement and final ledger/balance equality. Harness isolation passed. The two downtime/boost-replay tests failed in their shared test fixture before their business assertions: PostgreSQL reported inconsistent inferred types for parameter `$1` in the timestamp/CASE update. The owner is correcting only that test fixture; this failed run remains failed.

The coordinator verified that the failed run completed bounded cleanup with no E2 containers, test volume or active manifest remaining. The copied runtime was never connected to or modified by these tests. A passing rerun is required before the two remaining lifecycle criteria can be completed.

After the test owner separated the activation timestamp into its own parameter, run `20260906T001828Z-46e6ee52` passed the exact same anchored pattern. The coordinator independently counted five top-level `RUN` / five `PASS` events (four lifecycle tests and the harness), four nested unresolved-state passes, zero failures and process/summary exit 0. This now directly covers downtime/current-only grants with repeated maintenance and future-term nonactivation; boost replay across expiry and renewal with no extra slot/debit; expired new-claim refusal; cross-renewal net usage/history/current-term reset counts; and closing held until unresolved original-cycle receipts settle. Exact task/role checks again found no E2 containers, test volume or active manifest after cleanup.

Final lifecycle test SHA256 is `0F8F63C6E45DFAE8A8B2105CD0E556BA65CDA1D236D068A337BB02E70570109F`. `source-runtime-v5-final-equivalence-20260906T001844573Z.json` binds the final additive-test aggregate `05378BFC865B2E4AD5DF9ADC7DAEB369FFF9D66250247473CA6FE8FE72B65C53` to the unchanged V5 product: exact Linux embed compilation produced binary SHA256 `30BA65BC7633884C71C77F9DEDDAC7CCEF6E86C859759A3AACA4FD81A7946170`, identical to the preserved V5 image input, with unchanged embedded frontend. The original V5 approval and source hash were not rewritten, and no runtime rebuild or business mutation was needed for test-only additions.

Executed final integration binary SHA256: `BCFFA2F680DE0F2B742B91FA8661EF16B30EB129FC25F9B9BFF8DECFF73C5ED7` (118632357 bytes).

## Isolation And Tooling

Both copied database and copied Redis remain untouched by this runner. Every E2 run creates a distinct empty synthetic PostgreSQL 18 database and Redis with fresh local credentials and no published ports. The Go AST overlay replaces only Docker-starting TestMain, while retaining real ApplyMigrations, Ent, Redis and repository test helpers.

The first tooling attempts found PowerShell empty-argument binding and inaccessible ACL-private Docker bind mounts. Empty arguments are now preserved and tested. The proven runtime uses read-only rootfs, executable tmpfs and binary-safe stdin transfer; it has no host mount or Docker socket. Zero matching tests cannot count as success.

After the recorded runs, exact task/role-label checks found no E2 containers, PostgreSQL test volume or active manifest. Temporary E2 databases and synthetic environment files were removed; retained private logs and binary hashes provide evidence. No copied record, production database, original backup or unrelated local service was removed.

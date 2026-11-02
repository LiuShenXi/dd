> Historical02:08 draft, superseded for current results by coordinator-I5-I6-review.md and coordinator-acceptance.json. Original draft contents retained below.

# I5/I6 acceptance delta draft — 2026-10-20

Evidence review snapshot: **2026-10-20 02:08 UTC**. This is a proposal for the
coordinator's final merge, not a modification of `coordinator-acceptance.json`
or `coordinator-current-status.md`, not a release approval, and not a claim of
74 final-image passes. No compilation, test execution or production access was
performed while preparing this draft.

The current 74-case JSON still records **62 passed / 7 not_run / 5 partial** at
historical I4 scope. The 12 rows below are exactly the remaining T50–55,
T58–62 and T72 set. Historical passes retain their original source/image/layer;
they must not silently become passes of the current final image.

## Completed evidence and counting scope

| Evidence | Actual scope and result | Limits |
| --- | --- | --- |
| [I5 backend](I5-backend-receipt.md) and [I5 real PG/payment chain](I5-integration-receipt.md) | Final I5 selection: **15 top-level / 29 including subtests passed, 0 failed/skipped**. Separate shared manual-domain regression: **26 / 84 passed, 0 failed/skipped**. Real HTTP payment/webhook handlers, signed loopback EasyPay provider and original PG18.1/Redis harness. | The two selections overlap; do not add them as unique cases. Synthetic authentication does not validate login. This is not a provider-operated sandbox, a final-image/browser run or a race-detector pass. |
| [I6 controlled rollout](I6-controlled-rollout-receipt.md) | **11 top-level / 13 including subtests passed, 0 failed/skipped**; real maintenance/preview/convert/release handlers and fresh PG fixtures, including standard and subscription/windows groups. | Handler/DB evidence, not final container/browser proof. Failed Docker-port/disk attempts were excluded; the missing test-fixture starts_at was corrected and the complete selection rerun. |
| [Final selected backend unit receipt](I6-final-unit-receipt.json) | **895 top-level / 1891 including subtests passed** across the selected domain/config/service/handler/server/repository/server-main packages. | This is a regex-selected related suite, not every backend test. Its command is in the receipt. |
| [Final PG/Redis related selection](I6-final-pg-receipt.json) | **193 top-level / 402 including subtests passed, 0 failed**. | **2 top-level / 3 total skips**: two original GPT release-import suites require their separate source-runtime runner; one old concurrency batch-load case is TODO-skipped. All selected new quota/payment/conversion/migration suites ran. The later isolated nonfinancial metric-log fix has its separate test below. |
| [Final metric/lease unit receipt](I6-final-metrics-unit-receipt.json) | **20 tests passed**, exit0; includes actual keeper heartbeat/deadline and blocked-log-sink regression. | Overlaps other related selections; do not sum counts. It closes the specific synchronous logging finding in [root change review](I6-root-change-review.md), not final upstream cancellation evidence. |
| [History/performance receipt](I6-performance-receipt.md) | Initial experiment 1 top-level pass; indexed rerun **4 top-level passes, 0 failed/skipped**, including pending-count semantics, invalid-index recovery and timeout restoration. | Diagnostic local measurements, no supplied numeric SLO. At 300k history, indexed admission p95 4.23ms/backlog0.64ms, full ledger SUM16.421ms, Apply p95 50.45ms; mixed GPT throughput319.5/s versus456.3/s control (about30% lower). No deadlock/error/ledger disagreement; this does **not** prove negligible interference or production capacity. |
| [I5 frontend](I5-frontend-receipt.md), [I6 frontend](I6-frontend-receipt.md) | I5 full run **325 files / 2467 tests**; I6 full run **327 files / 2485 tests**. Later terminal-order warning had focused30 tests; the final duplicate-replica correction had the **18-test modal suite**, targeted lint and production build/type-check. | Runs overlap. The 2485 full-suite count precedes the later focused additions; do not label it a new full-suite rerun after those edits. DOM/unit/build success is not desktop or390px browser acceptance. |

## Row-by-row proposed evidence additions

Test names below are source locations to the actually executed tests identified
by the receipts; names of pending runtime tests describe required future evidence,
not completed passes. Backend payment tests live in
[`subscription_quota_payment_i5_integration_test.go`](../../backend/internal/repository/subscription_quota_payment_i5_integration_test.go),
rollout tests in
[`subscription_quota_rollout_i6_integration_test.go`](../../backend/internal/repository/subscription_quota_rollout_i6_integration_test.go).

| Case and current matrix state | Executed I5/I6 increment | Remaining final-image/browser evidence and merge boundary |
| --- | --- | --- |
| **T50 — not_run**: change/unpublish/delete plan after purchase | I5 `MutableDeletedPlanAndConcurrentSignedCallbacks`: real CreateOrder handler; provider observes the already-committed snapshot; actual UpdatePlan/DeletePlan; original125.12345678 quota and604800s survive later plan edits/deletion. `DatabaseRejectsEntitlementRewrite` covers snapshot/user/group/plan/type/duration immutability. Frontend displays returned snapshot rather than the current plan. | Backend contract is supported. Attach final image purchase/callback evidence from the successful upgrade/rollback proof and exact-image browser checkout/order views after plan change. Keep synthetic-provider scope explicit; no external merchant/payment acceptance is claimed. |
| **T51 — not_run**: callbacks/retries/lease handoff | I5 concurrent16 signed callbacks plus retry create one card, one payment operation, one SUBSCRIPTION_ASSIGNED. `LeaseTakeoverRejectsStaleOwner` and `ConcurrentLeaseHandoffCannotCompleteNewOwner` prove old workers cannot issue, complete or fail a newer lease. | Backend idempotency/lease contract is supported; do not turn a compiled image test into a runtime pass. The final image replay/restart must preserve one durable result; browser retry assertions remain part of final UI acceptance. |
| **T52 — not_run**: transaction failure then callback retry | I5 `RollbackBeforeIssuanceThenRetry` injects assignment-audit failure: parent/card/operation/outbox all roll back. `PostRightsFailureAndDisabledIssueRecovery` injects later completion failure: existing rights replay once and complete after new issuance is disabled. Provider creation remains one. I6 conversion outbox fault also rolls back mode/cards/receipt together. | Backend fault/recovery contract is supported. Actual frozen R2→R1 image recovery must pass with the same newest DB, paid callback and known usage, followed by another restart with unchanged fingerprints. The last recorded rollback attempt is failed, not accepted. |
| **T53 — not_run**: no-snapshot legacy order targets term group | I5 `LegacyAndMalformedSnapshotsFailClosed` retains payment evidence and creates no rights for missing/invalid snapshots. I6 `PaidPendingAndLateOrderDrift` distinguishes blocking pending/paid orders, stale payment facts and `legacy_terminal_orders`; both terminal orders can be reviewed without inventing allocations. | Backend fail-closed behavior is supported. Browser must show the terminal-order warning and still require explicit allocations. Preserve the distinction: CANCELLED may recover PAID then fail term fulfillment for manual review; EXPIRED only recovers within5min of updated_at; beyond grace it records PAYMENT_AFTER_EXPIRY without auto-PAID/rights. No new write-off/refund/entitlement inference protocol is accepted. |
| **T54 — partial**: legacy reset/extend/restore/bulk/redeem/worker paths | I5 `LegacyMutationsCannotChangePaidCard`, existing `LegacyRepositorySinksCannotMutateTermParent` and shared-domain suite run against real paid rights. Assign/extend/reset/revoke/restore/activate and maintenance sinks reject; bulk reports per-user failure; positive public and negative admin day-redemption remain unused; rights and financial fingerprints unchanged. I6 maintenance guard permits accounting drain while rejecting lifecycle changes and moves out of the maintained group. | Actual service/repository paths are covered. No standalone timer scheduling claim: the I5 receipt exercised real worker/service entry points, not each timer. Final image/manual UI must retain disabled legacy operations and separate term operations; no unsupported administrative mutation may be implied by visual hiding alone. |
| **T55 — partial**: old full/partial refunds/cancellation | I5 `FullPartialRefundsCannotAlterRightsOrWallet` and `RefundStateBlocksPaidRepository`: both deduction settings and force options reject before provider access; execution and pending-refund reconciliation reject; no parent-day subtraction, term rewrite or ordinary-wallet credit. Existing explicit card-cancellation scope remains separate from a cash refund. | Backend guard contract is supported. Final order UI/browser must show no legacy automatic term-refund action and describe separate handling. No real refund, processor sandbox refund or new automatic cash-refund workflow is accepted. |
| **T58 — partial**: stale auth cache crosses mode change | I6 real handler conversion commits the group mode and auth-cache outbox atomically; unchanged financial/native fingerprints and exact replay are covered. Related cache/auth unit and prior I4 cache-version evidence remain scoped evidence. | **Keep final runtime pending** until `TestSubscriptionQuotaI6RuntimeControlledConversion` proves authentic old windows cache against authoritative term DB is refused before any legacy user acquire/wait call, then the same Key succeeds on the term path after refresh. Require healthy/fenced Redis MONITOR evidence, actual completion, and exact image/source binding. New UI view alone does not prove this. |
| **T59 — partial**: active GPT work with Claude issuance/settlement | Existing I2 scope-lock evidence remains. I6 `GPTGuardDoesNotAcquireGroupRowLocks` holds a GPT group row lock while native GPT accounting succeeds; conversion touches only Anthropic. Actor/scope-wait and8-way confirmation tests pass. Dedicated performance run issues96 actual GPT Apply commands alone, then96 GPT +96 Claude concurrently per scale: no deadlocks, command errors or ledger mismatch. | **Keep final runtime pending** for active GPT HTTP and WS across actual conversion, both successful terminal responses and precise GPT settlement, with original user/WS-ingress memberships unchanged. PG coexistence supports no observed lock cycle; measured~30% local mixed-throughput reduction forbids a “no performance impact” statement. |
| **T60 — not_run**: disable new admission and roll R2 back to compatible R1 | I5 `UnissuedPaidRecoverySurvivesIssueDisable` and post-rights failure recovery prove new-sale policy does not revoke frozen paid entitlement. Related billing/recovery PG suites pass; final image proof is implemented and compiled. | **Keep not accepted** until `TestSubscriptionQuotaI6ImageUpgradeAndRollback` passes R0→R1→R2→compatibleR1 with startup migrations, real signed callback recovery, one known Claude/GPT debit/log, exact original money/native fingerprints, explicit HTTP503 refusal, and another R1 restart with no new effects. Latest recorded attempt exited1. Never substitute R0 rollback/down-migration/database restore. |
| **T61 — not_run**: an unupgraded/misconfigured replica still serves | I6 `RealRuntimeRosterRequiresBothProcesses` derives ads from two configured service instances; missing second member prevents token; actual default2 advertisement blocks confirmation; restoration permits it. Stale/duplicate/version/flags/roster/list mismatches and maintenance-exit refusal pass. Real Config.Load comma roster and repo policy units pass. | **Keep final runtime pending** until `TestSubscriptionQuotaI6RuntimeRejectsMixedReplica` starts an actual extra incompatible app, observes its real advertisement, blocks preview/exit, then waits real45s freshness expiry before release. Current recorded attempt is failed. The operator must enumerate real deployed replicas: self-registration cannot detect an unlisted legacy process. |
| **T62 — partial**: old wallet/window requests remain in flight at conversion | I6 `ProtectedQueueAndAsyncUsageDrain`: admitted pre-slot marker + durable usage_pending both block preview; maintenance prevents new protected admission; proved no-send drains one; native accounting is still writable; known completion drains the other; only then real conversion succeeds. `AttemptedUnknownCannotExpireIntoConversion` remains blocked after synthetic1year and cannot be relabeled no_charge. No test SQL stands in for conversion. | **Keep final runtime pending** for actual queued/streaming old-source requests, real completed billing and no-send cancellation before API conversion, exact source/money separation after it, and no unknown-to-zero transition. Test a successful message_stop/response.completed, not only HTTP200 or EOF. |
| **T72 — not_run**: activation/rollback with old-pool work | Combines the controlled PG gate above, real configuration roster checks, independent GPT row-lock proof and final metric/keeper units (including nonblocking log-sink regression). These support the algorithm but do not exercise live process replacement. | **Keep final runtime pending** for actual controlled conversion with active GPT HTTP+WS, unchanged original user members and WS ingress, successful terminal events and exact two GPT settlements; pre-slot old queue must prevent conversion and verified no-send must permit drain. Mixed runtime rejection and compatible rollback must also pass. All app instances must start before injecting synthetic GPT traffic; the real single-instance deployment uses full drain before replacement. Never clear shared Redis keys or pretend unchanged original startup cleanup is safe during active GPT. |

## Final runtime/browser collection still required

At this review snapshot, `office-I6-build-receipt.json` is
`built_source_stable_pending_runtime_acceptance`, source
`2ceb119eb44d2d8d9ba68e37e0cfe4646b51cc067628cc34175734607daaa55d` and R2 image
`sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483`.
Preparation and the second-instance setup each have one pass. The latest observed
`office-I6-rollback-receipt.json` (02:04:00 UTC) and
`office-I6-mixed-receipt.json` (02:04:33 UTC) each record exit1 / zero passes.
Their raw-log hashes are respectively
`09234173f534e5199b30e0a694d25bc69228fcd3656abdb83533297163f28e58` and
`2e672730fd6b186cdea73b4cb6018ae094e5625af10e24eacc3de7cd46c57608`.
The coordinator is still diagnosing/running final gates; later successful
receipts must supersede these attempts explicitly. Neither image build nor
setup successes establish upgrade, mixed-version, long-stream or browser passes.

The existing I4 acceptance remains historical. Revalidate the combined final
image's HTTP/JSON/SSE/WS routing, >15minute SSE heartbeat/lease ownership,
client cancellation and lost-lease actual upstream cancellation, account limits,
independent synthetic5+2 pools, metrics and precise receipt/log accounting.
Tie runtime results to stable source/image/driver hashes; account for test-only
driver corrections instead of labeling earlier failures as passes. The isolated
PG18.1 harness and retained production PG18.6 image archive do not by themselves
prove realistic production migration timing.

On the exact rebuilt candidate, desktop and390px browser evidence must cover
plan default and override, frozen order rights/price separation, signed synthetic
purchase/callback, changed/deleted plan with an old order, separate maintenance
→ explicit user/quota preview → confirm → explicit release, zero-card approval,
terminal-order warning, protected/order/replica blockers, stale confirmation,
held response across close/reopen and changed identity, keyboard flow and no
horizontal overflow. GPT must not receive the conversion action. Frontend
unit snapshots and old I4 screenshots do not waive these new I5/I6 paths.

## Production input and scope handoff

The verified recorded topology is one Compose application service `sub2api`
(container `sub2api2`) in project `sub2api2`, on main-server through
`/opt/sub2api2/docker-compose.migrated.yml` and127.0.0.1:29080. The R0 is the
provenance-verified `.13` image, not historical BWH blue/green or a different
local classic-Docker ID. [Production handoff](I6-production-handoff.md) now
records exact feature flags, process-wide release controls, per-group rollout
routes, runtime roster and late-payment behavior. Its inventory is a dated
read-only snapshot and must be rechecked before any approved mutation.

Still required from the business/operator: the exact target Claude group(s);
complete explicit recipient user IDs and strict quota strings (or an explicitly
approved empty set); treatment/owner for pending or late-paid legacy orders;
approved maintenance window and acceptable operational latency/backlog budget;
approved plan default quota, sale price/currency and listing choice if new sales
are intended; and a specifically authorized synthetic/test identity with an explicit paid
upstream smoke-test allowance if such a smoke test is desired. Independently,
recheck actual replica inventory, admin authentication, readable backups and
frozen compatible rollback artifacts. Existing customer Keys, balances, plan
values and the user's request to finish development supply none of those
recipient/amount/consumption decisions.

Do not sum overlapping test counts, count skips as passes, mutate the official
matrix from this draft, or infer production conversion/issuance/consumption
from local synthetic acceptance. The root coordinator owns the final evidence
merge and completion decision.

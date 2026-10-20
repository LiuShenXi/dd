# I6 plan duration correction and queue promotion driver

Recorded 2026-10-20, after the browser exposed the `days`/`day` contract mismatch.
This receipt separates executed service/PG checks from a compiled runtime test.
No production operation, external payment, commit or push was performed.

## Product correction

The existing UI submits `validity_unit: "days"`; the newly introduced term-plan
checks accepted only `day`. Consequently an actual admin save returned
`TERM_PLAN_DURATION_INVALID`, and even a separately inserted `days` plan failed
checkout preflight. The fix changes exactly these two production files:

- `backend/internal/service/payment_config_plans.go`: add shared
  `validateTermPlanDuration`; require `validity_days == 7` and unit exactly
  `days` or the existing `day` spelling. `validatePlanQuota` calls it, covering
  create/update, sale listing and the locked order snapshot validation.
- `backend/internal/service/payment_order.go`: checkout preflight calls that
  same validator. Missing default quota still returns `QUOTA_REQUIRED`; invalid
  term duration now returns the specific `TERM_PLAN_DURATION_INVALID` error.

The frozen entitlement remains 604800 seconds. Month/year units and other day
counts remain invalid. Native windows validation, paid snapshot recovery,
concurrency, lease algorithms, pricing and financial amounts are unchanged by
this correction. The backend spec now records canonical `days` and compatible
`day`; the frontend was not edited by this worker.

Frozen file SHA-256 values:

| Production file | SHA-256 |
| --- | --- |
| `payment_config_plans.go` | `7aa93eb2840693fc4ddd6124b98334c8a939fa920608a686712c7c74c446c093` |
| `payment_order.go` | `b06f18f1a0f635ca7dbf4ffd678e244013a8e20cc383d0e4bf8af5dd0bee0646` |

## Executed checks

- Service selection: **32 top-level / 32 total PASS, 0 FAIL, 0 SKIP**. It covers
  strict term amount/duration validation, existing paid dispatch and plan
  required/patch checks. Unit test now exercises both accepted spellings and
  rejects month/year variants, six/eight days, empty and whitespace-prefixed
  units.
- Real PostgreSQL I5 payment selection: **16 top-level / 32 including subtests
  PASS, 0 FAIL, 0 SKIP**. This reruns all existing I5 payment guards/recovery
  cases and adds
  `TestSubscriptionQuotaPaymentI5_PlanDayUnitsThroughAdminAndCheckout/{days,day}`.
  The new case calls actual admin POST/PUT handlers with JSON, confirms invalid
  duration updates are rejected and valid plans remain listed, then calls the
  real user checkout handler, signed loopback EasyPay provider and callback.
  Each accepted spelling produces exactly one committed frozen entitlement,
  one 604800-second card, quota125.12345678, operation and assignment audit;
  ordinary/GPT/native money guards remain unchanged. Authentication is synthetic
  in this test router; production/browser authentication is not claimed here.
- `gofmt` and `git diff --check`: PASS.

Executed commands from `backend/`:

```sh
GOMAXPROCS=3 go test -p 1 -ldflags='-s -w' -tags=unit ./internal/service -run '^(TestTermPlanDefaultAndDurationValidation|TestPaidTermDispatchDoesNotDependOnSalesOrPlan|TestValidatePlanRequired|TestValidatePlanPatch)' -count=1 -json
GOMAXPROCS=3 go test -p 1 -ldflags='-s -w' -tags=integration -c -o /Users/shenxi/.codex/private/claude-weekly-quota/office/repository-promotion-final.test ./internal/repository
CI=true GOMAXPROCS=3 /Users/shenxi/.codex/private/claude-weekly-quota/office/repository-promotion-final.test -test.run '^TestSubscriptionQuotaPaymentI5_' -test.v -test.timeout=6m
```

Private full logs: `office/plan-days-unit.jsonl`, `office/plan-days-i5-pg.log`
under `/Users/shenxi/.codex/private/claude-weekly-quota/`.
The initial unstripped service link failed with `no space left on device`
before any test ran. After coordinator-scoped cache cleanup, the above stripped
debug link and checks succeeded. No source or assertion was disabled for this.

## Compiled queue promotion test — not executed by this worker

New isolated source:
`backend/internal/repository/subscription_quota_promotion_http_i6_integration_test.go`.
Test name: `TestSubscriptionQuotaI6RuntimeClaudeQueuePromotion`.

Its assertions require the explicit I6 synthetic fixture's B user to use the
configured default concurrency3 and a300 card. Three held real Claude streams
occupy the pool; the fourth must own one waiting ticket without an upstream
call. `release_sequence` ends exactly one actual upstream normally while the
global provider hold stays enabled. A fourth upstream call, waiting0, active3,
two retained original owner UUIDs and one new active owner jointly prove real
promotion. The waiting ticket UUID and active owner UUID are intentionally
different in the product protocol.

After release, all four clients must receive `message_stop`, all four receipts
must be settled and log-acknowledged, four actual usage logs and ledger entries
must exist, and the B card's exact increment must be0.00240000. Other Claude
cards and all ordinary/native/GPT/payment fingerprints must match. Final
provider activity, user/account active slots and waiting tickets must be empty.

Safety review: the test reads Redis but does not create artificial slots or
delete ownership. All four clients use a bounded90-second context, a buffered
result channel and unconditional cancellation cleanup; the provider hold is
also unconditionally released. Execute serially after long SSE, because this
test owns the fixture's global synthetic provider gate. It makes no external
provider call and does not edit the main fixture or product concurrency code.

- Compiled driver: `/Users/shenxi/.codex/private/claude-weekly-quota/office/repository-promotion-final.test`
- Driver SHA-256: `3570a0be972bf709822d766e5124120bbd7444a09d5b3e258121309d61969e9b`
- Driver size:101196818 bytes; debug symbols stripped, test semantics unchanged.
- Promotion source SHA-256: `18da5a6af93b2b5b799e7be831c39bfc70e5d5c4c5bf057c7a50cfef66181c0a`

The coordinator must execute this runtime test and collect scope metrics;
compilation supplies no T67 runtime PASS. The new plan correction also requires
the coordinator's rebuilt final image and actual browser/paid/rollback checks.
Earlier image evidence remains attributed to its original source/image hash;
this receipt does not relabel it or alter the official acceptance matrix.

## Promotion driver corrections after the coordinator's first runs

The two initial runtime attempts remain **FAIL**, with their original driver
hashes and receipts retained. Neither failure is relabeled as acceptance:

1. `office-I6-promotion-nonempty-account-failed.json` records the initial
   precondition failure before any new traffic. An earlier fault case left an
   unrelated legacy account member. The actual fixture loads the30-minute
   account lease default, so an empty global account pool is not a valid
   ownership precondition. The final2 driver snapshots the baseline membership,
   requires every newly added member to be released, and preserves baseline
   members that remain unexpired. It permits the existing Acquire algorithm to
   prune expired members. The B user's active/waiting sets and actual provider
   activity must still start and finish empty. No Redis member is fabricated or
   deleted by this test.
2. The final2 attempt on F1 reached real queue promotion and passed the four
   completed streams, settled/log-acknowledged receipts, four ledger entries
   and exact B debit0.00240000 before failing the foreign-card hash assertion.
   Recovery retries of unrelated closing terms update `updated_at`. The final3
   test excludes **only** that retry timestamp from `to_jsonb(t)` in this hash;
   every financial, identity, interval, lifecycle and revision field remains
   guarded. Ordinary/native/GPT/payment fingerprints are unchanged. This is a
   test correction, not a product or fixture-data mutation.

The final2 driver SHA-256 is
`59febca0f6e21a8d1115c1580d6cd103ea77e46736f8356d2b2ec5803341fdbe`.
Its coordinator receipt at the time of this note records exit1, one failed
top-level test and zero skips; passing earlier assertions do not make that run
PASS.

After the coordinator froze the successful F3 product build, this worker
compiled the final3 driver with the same stripped integration command and a
new output path:

- Driver: `/Users/shenxi/.codex/private/claude-weekly-quota/office/repository-promotion-final3.test`
- Driver SHA-256: `c228980169044eff0a381975577a2e717bd541cf197fb1baa84241e6cf3b8969`
- Driver size:101196850 bytes.
- Promotion source SHA-256: `6ac5f55e0e3d071a7dfe48b149b31f3d87af358533f56d5adf2b0dd1da6a0115`
- Compile and scoped `git diff --check`: PASS. Runtime: **not executed by this worker**.

No product source, Redis or PostgreSQL data was modified by these driver
corrections. The coordinator owns the final runtime result and metrics evidence.

## Coordinator execution — completed 2026-10-20 02:39 UTC

The final3 driver was subsequently executed against the unchanged F1 runtime.
`office-I6-promotion-receipt.json` records one PASS, zero failures/skips in6.49s,
with stable driver SHA-256 `c228980169044eff0a381975577a2e717bd541cf197fb1baa84241e6cf3b8969`.
Three actual held streams filled default3; a fourth remained queued without
upstream traffic, then acquired after one normal release. All four completed,
settled and log-acknowledged with four ledger entries and exact0.00240000 debit.
Only newly owned account slots were required to clear; preexisting unexpired
account membership and every other monetary/identity/interval field remained
protected. The prior failed attempts remain failures in their own receipts.

`office-I6-metrics-receipt.json` then passed, including observed
`promote.active.acquired` in `office-I6-metrics.json`.
`office-I6-long-sse-receipt.json` independently passed a real974.033-second
stream with63 ownership checks, lease renewal, queued cancellation and exact
0.00120000 final settlement. These are F1 executions, not relabeled F4 tests.
F2 and F3 actual payment/rollback proofs are recorded separately; final source
attribution is in `office-I6-final-source-audit.json`.

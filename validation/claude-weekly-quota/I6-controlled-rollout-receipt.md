# I6 controlled conversion backend receipt

Scope: local synthetic acceptance only; no production conversion, real customer allocation, payment-provider call, commit, push or deployment was performed.

## Implemented contract

The real admin routes under `/api/v1/admin/groups/:id/subscription-quota/rollout` expose GET state and POST `maintenance`, `preview`, `convert`. The maintenance body requires an explicit boolean, reason, revision and Idempotency-Key. Preview takes an explicit user/quota array; omitted/null arrays fail, while an explicitly reviewed empty array performs zero issuance. Quotas remain strict positive decimal strings with eight-place canonical storage. The preview token binds the administrator, complete allocation set, group/key/user/native-parent/order identities and state, revision, pending counts and replica capabilities. Changed state requires a fresh review.

Conversion takes the group-operation advisory lock, sorted user/group scopes, all existing parents, all terms, sorted user locks, the final group lock, and key locks. It rechecks the administrator after waiting and rereads all blockers and the preview fingerprint. A short SHARE table lock freezes replica advertisement updates and new registrations during final verification. Mode change, explicitly allocated 168-hour cards, parent summaries, durable operation receipt and auth-cache outbox commit atomically. Eight concurrent retries of an identical operation are tested to create one set of rights. The group remains in `quota_maintenance` until the separate release operation; release restores its previous active/inactive status. A converted group cannot return to windows through these APIs.

All unfinished protected markers block conversion, including queue-waiting, known asynchronous usage and reconciliation states. New authoritative protected admission is denied during maintenance; previously known accounting may drain. Migration253 blocks native entitlement/lifecycle changes (including moves out of the maintained group), generic group edits and new subscription orders. Accounting counters/window maintenance are allowed to settle. Only Anthropic groups take the new group row-lock path; the GPT lock-behavior test holds a GPT group lock while updating its subscription accounting.

Pending/paid/unfulfilled/refunding orders block conversion. Historical null-snapshot `CANCELLED`/`EXPIRED` orders are separately exposed as `legacy_terminal_orders`, with an operator warning rather than a new entitlement inference. A valid late payment after conversion retains its payment evidence and follows the existing T53 manual-review path; it does not mint rights from the latest plan. This does not create a new settlement or write-off protocol.

## Runtime capability boundary

`SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID` names the process (e.g. `app-1`). `SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS` is the complete operator-managed deployment roster; comma-separated values such as `app-1,app-2` are covered through the real config Load path. YAML may use an ordinary string array. Each running process registers a random instance identity from its actual configuration, after checking migrated billing readiness. There is no HTTP registration or request-body capability override.

Heartbeat interval is 10 seconds; freshness is 45 seconds. Duplicate live instances using the same replica ID block activation; after replacement the expired old instance stops blocking after the freshness window. Every expected replica must be present, with capability/receipt/lease versions 1, Claude default concurrency 3, issuance/admission/recovery enabled, identical enabled/protected group lists containing the target, and the same complete roster. Unknown extra replicas, missing/stale/duplicate processes and mixed configuration block conversion and release. Entering maintenance remains available to stop traffic even when replicas are unhealthy. Outside controlled operations the roster does not change existing group admission. The roster must reflect actual deployed replicas: heartbeat self-discovery alone cannot prove the absence of an unlisted legacy process.

Migration253 uses bounded transaction lock/statement timeouts. Billing readiness now includes immutable-order migration252, rollout253, history-index254 checksums and ready/valid history indexes. Existing migrations and original GPT startup cleanup are preserved.

## Validation status

The scoped backend gates below passed. Final-image HTTP/browser/performance/rollback acceptance remains the coordinator's separate gate.

- Wire generation succeeded; initial service/repository/handler/routes compile succeeded. The initial cmd/server test fixture needed the new cleanup dependency; its missing nil was fixed.
- First real PostgreSQL attempt stopped before tests: Docker Desktop published ports refused localhost connections. A gateway-IP trial reached TCP but PostgreSQL/Reaper protocols returned EOF, also before tests. These are environment failures, not accepted runs or skips.
- Docker Desktop was restarted with the coordinator's authorization, affecting only disposable test containers. No image or volume was deleted. Database checks are rerun after recovery.
- Final unit/config, PostgreSQL acceptance and clean-diff results will be recorded below after execution.

### Real PostgreSQL acceptance

Command (from `backend`, using the unchanged original TestMain/PG18+Redis harness):

```sh
GOMAXPROCS=3 go test -p 1 -tags=integration -c -o /tmp/claude-i6-repository.test ./internal/repository
CI=true GOMAXPROCS=3 /tmp/claude-i6-repository.test -test.run='^TestSubscriptionQuotaRolloutI6_' -test.count=1 -test.timeout=6m -test.v
```

Final result: **11 top-level / 13 including subtests PASS, 0 failure, 0 skip**, about 11 seconds of test bodies after harness startup. Log: `/tmp/claude-i6-rollout-tests.log`. No HOST_OVERRIDE was needed after Docker Desktop restart. One initial executed test fixture omitted a required `starts_at`; it was corrected and the whole conversion selection rerun. The intentional outbox exception is an asserted rollback failure and the test passes.

Covered: real GET/maintenance/preview/convert/release handlers for standard and windows groups; unchanged unrelated group/native/financial fingerprints; two users receiving exactly 100/300 and 168 hours; multiple Keys sharing one user's card; explicit empty allocation; strict request schema and money; stale user/key/payment previews; atomic outbox failure and successful same-key retry; queue and known async drain; attempted unknown reconciliation retained even after a synthetic year; eight concurrent identical confirmations issuing once; legacy pending/paid and terminal-order distinctions; native/group/order mutation guards; GPT row-lock independence; actor revocation while waiting for scope; runtime-originated two-process roster, mismatch, stale and duplicate rejection.

Config Load's comma roster test and repository replica-policy unit test passed. A simultaneous multi-package service/server link attempt ran out of local disk; no test assertion failed, and the coordinator reclaimed only stale reproducible build archives. Follow-up builds use one package at a time with `-p 1`. A one-time cache-file ENOENT during that coordinated cleanup was retried successfully before the accepted PG run.

### Final scoped build checks

- `GOMAXPROCS=3 go test -p 1 -tags=unit ./internal/service -run '^TestSubscriptionQuotaRollout' -count=1` — PASS (1.561s).
- `GOMAXPROCS=3 go test -p 1 -tags=unit ./cmd/server -run 'Cleanup' -count=1` — PASS (0.485s); generated Wire and its cleanup fixture compile together.
- Config Load roster and repository replica-policy unit selection — PASS (0.920s / 0.852s).
- `GOMAXPROCS=3 go test -p 1 -tags=integration -c -o /Users/shenxi/.codex/private/claude-weekly-quota/office/repository.test ./internal/repository` — PASS; the coordinator continued adding test-only runtime assertions after this handoff build and will perform the final synchronized `-c` before image acceptance.
- `git diff --check` — PASS.

The tested conversion business source is frozen at handoff. These scoped checks do not claim that every backend test, final image gate, browser interaction or production rollout has passed. No production state was read or changed by this subtask.

- `GOMAXPROCS=3 go test -p 1 -tags=unit ./internal/service -run '^TestClaudeConcurrencyMetrics' -count=1` — all three tests PASS (16.415s), including queue promotion/cleanup, lost lease/renew failure deadline, and failed acquire without legacy reads.

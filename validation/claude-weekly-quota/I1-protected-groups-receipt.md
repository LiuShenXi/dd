# I1 protected groups implementation receipt

Base: `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`. This submodule is part of the isolated `codex/claude-weekly-quota` worktree. No commit, production request, production migration, account/model call or Codex connection change was performed by this work.

## Change boundary

The old Claude standard/windows request can pass cached authorization, wait for an account and then reach upstream after the group has entered maintenance or changed mode. Its HTTP handler may also return before the old asynchronous billing task is complete. A process-local HTTP counter therefore cannot prove that the old source is drained.

This change adds an explicit, default-empty protected-group list, a final authoritative SQL check and durable request markers for those groups only. It does not implement term admission, quota debit, pricing snapshots or term billing recovery (I3). Unconfigured groups return before touching this repository; GPT's existing admission and ledger are unchanged.

Owned files:

- `backend/migrations/249_subscription_quota_protected_groups.sql`: bounded migration; durable `subscription_quota_protected_requests` and unfinished partial index.
- `backend/internal/service/subscription_quota_protected_groups.go`: shared endpoint allowlist, protected group admission, durable lifecycle, conservative unknown-usage handling and atomic old-billing dependency check.
- `backend/internal/service/billing_cache_subscription_quota_protected.go`: startup setter/getter and missing-dependency/simple-mode rejection.
- `backend/internal/repository/subscription_quota_protected_group_repo.go`: SQL admission/usage/outcome/count implementation.
- `backend/internal/handler/subscription_quota_protected_group.go`: shared handler helpers and mandatory usage scheduling.
- `backend/internal/handler/gateway_handler.go`, `gateway_handler_chat_completions.go`, `gateway_handler_responses.go`: actual final Forward guards, usage completion callbacks and synchronous CountTokens completion. Channel usage metadata is captured before the async task to avoid reading a reused Gin context after HTTP completion.
- Corresponding service, handler and repository tests.

Config fields, normal auth mode guards, BillingCacheService's field, Ent and Wire are owned/wired by the main developer. Normal auth calls the shared endpoint allowlist and rejects protected simple-mode, Google, ForcePlatform, unsupported platforms and unsupported charging paths.

## Durable boundary

Admission locks **user -> group -> API Key** in separate statements, then reads a fresh database clock after all locks. It verifies current user/Key/group status, Key identity and group, Key expiry, total/rate-window limits, anthropic platform, original standard/windows subscription type and applicable group/subscription permission. A mode change, missing dependency or unknown database state returns unavailable before upstream send.

The group lock and marker insertion are one short transaction. Maintenance must acquire the group lock, then check persisted unfinished rows across all instances. No lock is held during upstream work. The exact operational conversion/release API remains an I6 responsibility.

States are admitted -> usage_pending -> completed, or no_charge for the existing CountTokens path. Unknown, error and ambiguous-zero results remain reconcile_required; a crashed admitted or dropped/incomplete worker remains unfinished indefinitely. No TTL, HTTP defer, shutdown callback or process restart removes it from the count. Usage payload is a non-secret metering projection; raw bodies, upstream headers and credentials are excluded. Changed payload/outcome replays cannot overwrite original facts.

Protected usage is submitted through the existing mandatory queue path, so explicit drop/sample overflow cannot silently discard it. Marker completion happens only after the old atomic billing method returns success. A missing atomic billing repository refuses its legacy multi-write fallback. Client cancellation does not cancel the bounded persistence context. Already completed/no-charge outcomes cannot move backwards.

## Actual validation

1. Executed on Windows Go 1.27.0:

   `go test -tags=unit -run '^TestSubscriptionQuotaProtected' -count=1 ./internal/service ./internal/handler`

   Result: service `ok 2.936s`, handler `ok 1.706s`; six service top-level tests and one handler top-level test passed. Covers default-off no-Repo calls for GPT, missing dependencies, cross-group/platform refusal, usage after disconnect, pending asynchronous billing after HTTP return, failure/unknown/crash holds, supported route allowlist and CountTokens no-charge plus deferred exit.

2. Executed by the dedicated environment owner using the original repository TestMain and fresh synthetic PostgreSQL 18/Redis containers:

   `go test -tags=integration -json -count=1 -timeout=10m -run '^TestSubscriptionQuotaProtected' ./internal/repository`

   Four top-level tests passed, zero skip/failure, in 9.233s. Evidence: [receipt](I1-migration-protected-20261019T132428Z-receipt.json) and [JSONL](I1-migration-protected-20261019T132428Z.jsonl). Tests: PersistentDrain, FinalDatabaseState, UserLockOrderAndModeWait, KeyWaitRefreshesDatabaseTime. They execute real row locks, wait until `pg_stat_activity` observes the lock, switch the group under the held user lock, and pass the database expiry boundary while waiting for the Key lock. They also prove marker persistence across repository reconstruction, immutable usage replay, unchanged ordinary balance and no age-based drain.

3. `git diff --check` passed for the three edited handler files. The main developer owns full repository compilation, regressions, image construction and release evidence.

4. The additional `TestSubscriptionQuotaProtectedConcurrentGPTPrepare` passed in a subsequent fresh harness. It invokes the actual existing GPT preparation function while stale standard Claude admission waits for the same user, proves GPT preparation completes, and proves the stale Claude request is rejected without a marker. Evidence: [combined receipt](I1-migration-20261019T132802Z-receipt.json) and [JSONL](I1-migration-20261019T132802Z.jsonl); the combined run had 9/9 passing top-level tests, zero skips/failures (eight migration cases and this concurrency case).

Migration 249 was tightened after the first four-case run so `usage_pending` also requires a non-NULL usage payload. The later combined harness applied that latest migration, but did not repeat the first four protected cases. The main developer's final combined regression must use the frozen latest source/migration hashes; the earlier four-case receipt is not relabeled as an exact final-source run.

## Limits of this evidence

The handler test uses a real Gin HTTP lifecycle and the actual shared queue helper, with a fake marker repository/billing completion. It does not constitute a complete real Gateway Router/upstream or final-image acceptance. PostgreSQL tests exercise the actual SQL repository and original GPT preparation helper when the additional run completes. No Linux race, browser, image, production activation or full I3 recovery claim is made here.

I1 deliberately provides no automatic reconciliation release. Unfinished facts block a cutover until operators can verify and resolve them through the later controlled conversion/recovery tooling; they must never be manually deleted to manufacture a zero count. The legacy ForwardResult cannot distinguish missing usage from a genuinely known all-zero token result, so all-zero remains held conservatively in this compatibility gate.

The current I1 preflight guards on old entitlement/payment mutations do not themselves implement I6's full maintenance-and-conversion serialization. I6 must gate those writes against maintenance and final group locks before enabling or switching mode; the first R1 candidate is not the final activation/rollback image.

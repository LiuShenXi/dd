# I5 PostgreSQL and controllable payment-chain receipt

Executed on **2026-10-20, Asia/Shanghai** in the local Claude weekly-quota development checkout. These are completed tests, not a proposed test plan. No production database, user session, API Key, payment account, or real payment was used.

## Environment and boundary

- Go `go1.27.0 darwin/arm64`; `CI=true` makes missing Docker an error rather than a skipped pass.
- The existing repository `TestMain` starts PostgreSQL `postgres:18.1-alpine3.23` and Redis `redis:8.4-alpine`. Every new I5 fixture uses a fresh database created on that disposable PostgreSQL container and applies the repository migrations.
- PostgreSQL image digest: `sha256:aa6eb304ddb6dd26df23d05db4e5cb05af8951cda3e0dc57731b771e0ef4ab29`.
- Redis image digest: `sha256:0514fa59e3d84e2f3be66731c5401fc2a85b8ea71b49c0116c850f6a558076f4`.
- The payment chain uses the real `PaymentHandler`, `PaymentService`, provider selection/configuration, EasyPay HTTP client, EasyPay signature verification, `PaymentWebhookHandler`, fulfillment lease/state machine, and PostgreSQL repositories. The provider endpoint is an `httptest` loopback server containing only synthetic merchant data. Authentication is a synthetic `AuthSubject`; these tests do not claim to test login or JWT verification.
- The fake provider checks the outbound signature and reads the already-committed order snapshot before returning its synthetic payment response. Callbacks use the real webhook handler with signed synthetic forms. No callback initiates another provider creation request.
- Faults are injected with temporary PostgreSQL triggers in the disposable database. The actual rights, assignment audit, operation, parent summary and outbox transactions remain real.

## Executed results

From the `backend` directory:

```sh
CI=true GOMAXPROCS=3 go test -p 2 -tags=integration ./internal/repository \
  -run '^(TestSubscriptionQuotaPaymentI5_|TestSubscriptionQuotaDomain_|TestSubscriptionQuotaLegacyRepositorySinksCannotMutateTermParent$)' \
  -count=1 -timeout=15m -json
```

**PASS at 09:21:47 +08:00:** 26 top-level tests, 84 tests including subtests, zero failures and zero skips; package elapsed time 56.452 seconds. This reran the existing manual domain suite after manual and paid issuance began sharing the term-creation helper.

After adding explicit overlapping-worker lease handoff, redemption/bulk guards and the final migration version:

```sh
CI=true GOMAXPROCS=3 go test -p 2 -tags=integration ./internal/repository \
  -run '^TestSubscriptionQuotaPaymentI5_' -count=1 -timeout=15m -json
```

**PASS at 09:22:40 +08:00:** all 15 I5 top-level tests, 29 tests including subtests, zero failures and zero skips; package elapsed time 28.043 seconds. `git diff --check` also passed.

| Contract | Executed evidence |
| --- | --- |
| T50 immutable purchased entitlement | Create through the real HTTP handler; the provider sees the committed snapshot; modify price/default/name and unpublish through `UpdatePlan`, then delete through `DeletePlan` while the frozen order is pending. Fulfillment issues exactly `125.12345678` USD for `604800` seconds from the original snapshot. |
| T51 concurrent callbacks | Sixteen signed callbacks race for one order, followed by a normal retry. Exactly one card, one payment operation and one `SUBSCRIPTION_ASSIGNED` audit exist. Transient callbacks may return the existing lease conflict; the durable order converges to `COMPLETED`. |
| T51 lease ownership | A stale lease cannot invoke the paid repository, even after rights exist. A separate two-worker test pauses both workers after real rights commit/replay, expires the first lease in the fixture, and proves the old worker can change neither completion nor failure state while the new worker owns the order. Only the new owner completes. |
| T52 rollback before rights commit | PostgreSQL raises an error during assignment-audit insertion. Card, operation, new parent and outbox all roll back. Removing the fault and replaying the signed callback issues one card; provider creation count remains one. |
| T52 failure after rights commit | PostgreSQL raises an error on the later `COMPLETED` update. The committed card and assignment audit remain; order becomes recoverable `FAILED`. Retry returns the same card and completes. |
| Stopped sales / recovery | Existing paid rights recover with issuance disabled and the default Claude concurrency set to zero. An unissued paid order also fulfills after the issuance allowlist is removed. New purchases are rejected when issuance is disabled. |
| T53 legacy and invalid snapshots | Historical fixture orders are inserted with missing snapshot, unsupported version, wrong group, numeric allowance, or missing allowance. All retain payment evidence, become `FAILED` for explicit manual review, and create no rights/audit/operation/outbox. The immutable trigger remains enabled. |
| Database snapshot contract | Attempts to clear/change a frozen snapshot or change its user, group, plan, type or duration fail with the immutable-entitlement constraint. |
| T54 legacy mutation boundaries | Against a real paid card, legacy assign, extend, reset, revoke, restore, activation and maintenance entry points reject; bulk assignment reports both users failed; positive-day public redemption and negative-day admin redemption reject without consuming the codes. Full financial-row fingerprints remain unchanged. Existing repository maintenance sinks also passed. |
| T55 refund boundaries | Full and partial refund preparation reject with both deduction settings and force enabled. Direct execution and pending-refund reconciliation reject before provider access. Refund-related order states cannot invoke paid issuance. No parent-day deduction, card rewrite or ordinary-wallet credit occurs. |
| Legacy windows compatibility | A null-snapshot windows purchase still uses native subscription fulfillment. Duplicate callbacks extend its original expiry by exactly seven days once, produce one assignment audit and create no term card. |
| Payment validation | Invalid signature leaves the order unpaid; a correctly signed wrong amount also leaves it unpaid and creates no card. A subsequent valid callback succeeds. |
| R02 explicit manual override | An unsold plan with null default accepts explicit `100` USD and records `admin_override` without an invented plan default. Omitting both allowance and default returns `QUOTA_REQUIRED`. |
| Financial isolation | Before/after fingerprints protect ordinary users/balances, all existing `carpool_*` financial records, redeem codes, usage logs and native windows where applicable. Payment issuance does not charge or credit those unrelated stores. |

## Reproducibility

- Test source: `backend/internal/repository/subscription_quota_payment_i5_integration_test.go`.
- Test source SHA-256: `34fc7290d1dc0c26951f6af2a9436094836306000fb707927a1be5bce9e916a0`.
- Final migration `252_subscription_quota_order_snapshot_immutable.sql` SHA-256: `87fa4c0c8d25988344df8a34d6f40b36647e0c314e49f0a088564d5bd486ea39`.
- Final local JSON log: `/tmp/claude-i5-final-integration.jsonl`; SHA-256 `0537efba4a830974d4b179490e33db6469ca17647501d8a615ff648f4df4aa4e`.
- Shared-domain regression log: `/tmp/claude-i5-domain-integration.jsonl`; SHA-256 `44010025640bf91c5adf8abd0fe1ddb085e8ec9d30193cc79739703dc84e9fd4`.

## Limits and earlier interruption

An initial parallel compilation attempt exited without results during local disk exhaustion; another run was interrupted for disk recovery. Neither is counted as passing. The completed runs above used bounded compilation concurrency after disk space was recovered.

This receipt establishes PostgreSQL transactions and a controllable provider HTTP/webhook chain. It does not establish a provider-operated payment sandbox, browser checkout, final container-image behavior, production activation, or a Go race-detector run. Those must not be inferred from these passes. The standalone timer scheduling of maintenance workers is not exercised here; their real service entry points and repository write barriers are exercised. Production PostgreSQL was not contacted by these tests.

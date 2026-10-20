# I5 backend implementation receipt — 2026-10-20

Baseline: `203047eda`, branch `codex/claude-weekly-quota`. This receipt covers
backend implementation and checks, not deployment or production activation.

Implemented:

- Plan create/update validates a strict positive decimal string separately from
  sale price; term duration is exactly seven days. For-sale term plans require
  an active Anthropic term group and a default. All public/admin list, checkout,
  create and update projections expose `quota_mode` and fixed-eight
  `default_quota_usd`. Invalid legacy term products are omitted from sale lists.
- Manual plan selection copies its default once. An explicit amount overrides
  it, including an unsold plan with no default; omission without a default fails.
- Order creation locks and rereads the plan, user and group in its existing Ent
  transaction, checks sales/allowlist and independent concurrency policy, then
  commits the versioned exact entitlement before calling the provider. A racing
  sale-price change rejects checkout. Provider subject uses frozen display names.
- Paid fulfillment dispatches before the native assignment path into a separate
  SQL transaction: order row and persisted lease version, then scope, operation,
  parent, sorted terms, and final entities. It uses the same term creation helper
  as manual issuance. Card, nullable-actor operation, original
  `SUBSCRIPTION_ASSIGNED` audit and auth invalidation outbox commit atomically.
  Affiliate handling and lease-conditional completion remain in the existing
  recoverable state machine.
- Paid recovery does not read current plans, sale flags, allowlists or current
  concurrency defaults. Snapshot replay preserves the original card. Malformed
  snapshots and missing legacy snapshots whose group became term fail closed for
  manual review. Existing full/partial legacy refund and redemption guards remain.
- User/admin order history and order-create responses expose only a validated,
  typed snapshot; unknown fields are never reflected into those DTOs.
- New migration `252_subscription_quota_order_snapshot_immutable.sql` prevents
  changes to a frozen snapshot and its user/order/plan/group/duration identity.
  It uses local 5-second lock and 60-second statement timeouts. Earlier migrations
  were not modified. No new entitlement tables or Ent schema fields were needed.
- Wire sources and generated wiring connect the dedicated payment repository and
  runtime configuration. `go generate ./cmd/server` completed successfully.

Executed checks (Go 1.27.0 darwin/arm64; bounded concurrency after a temporary
local disk-full compilation failure):

```
GOMAXPROCS=3 go test -p 2 ./internal/domain/...
GOMAXPROCS=3 go test -p 2 -tags=unit ./internal/service -run 'Test(TermPlan|PaidTerm|.*Payment|.*Plan|.*SubscriptionQuota)' -count=1
GOMAXPROCS=3 go test -p 2 -tags=unit ./internal/handler/... ./internal/repository -run 'Test(.*Payment|.*Plan|.*SubscriptionQuota)' -count=1
GOMAXPROCS=3 go test -p 2 -tags=unit -run '^$' ./cmd/server
GOMAXPROCS=3 go test -p 2 -tags=unit ./internal/service -run 'Test(.*Fulfillment|.*Processed|.*Notification|.*Redeem)' -count=1
GOMAXPROCS=3 go generate ./cmd/server
git diff --check
```

All commands above passed. Scope includes new strict snapshot/default tests,
DTO precision/sanitization checks, existing subscription quota tests, and legacy
payment fulfillment/lease/redeem/notification behavior. These scoped unit checks
are not a substitute for PostgreSQL or final-image acceptance. The independent
I5 integration receipt records actual PG18, signed local fake-provider callback,
lease/crash recovery, immutable snapshot, plan mutation/deletion, refund guards,
and manual missing-default/explicit-override evidence.

No commit, push, deployment, real payment-provider call or real customer billing
was performed by this backend worker. Remaining I6 release gates are tracked by
the coordinator; sales/admission are not enabled by this code change.

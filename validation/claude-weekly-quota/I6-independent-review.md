# Independent I5/I6 cross-layer review

> Historical source-review checkpoint. The terminal-order warning, final runtime/browser tests and later fixes are now recorded in coordinator-I5-I6-review.md; follow-up wording below describes this review time.

2026-10-20, branch `codex/claude-weekly-quota`, baseline `203047eda`.
Role: read-only reviewer following `trellis-check`; this agent authored the UI,
so this pass concentrated on backend sources and database contracts. No Go
commands or implementation edits were performed in this review.

**Result: no confirmed blocking implementation defect found in the reviewed
scope. One historical-payment limitation was surfaced and explicitly retained
under the coordinator's T53 decision; its operator warning is follow-up work.**

Reviewed paths and conclusions:

- Plan create/update and transactional order creation: exact positive allowance
  normalization, fixed seven days, current plan/group/user locks and stale-price
  rejection. Sale price remains separate from quota. The created order contains
  its own versioned entitlement; fulfillment does not reread the current plan.
- Migration252 and paid repository: frozen entitlement/user/group/type/plan/days
  cannot be rewritten; payment order, lease version, paid/refund state, scope,
  parent and terms are checked before issuance. Card, operation, assignment audit
  and outbox share one transaction. Recovery replays committed rights; stopped
  sales/default-concurrency changes do not revoke an already-paid obligation.
  Completion/failure updates remain conditional on the same fulfillment lease.
- Legacy refund/redemption/assignment guards and financial separation: the new
  snapshot path does not credit ordinary balance or call native day extension.
  Null-snapshot windows orders keep the existing path; malformed snapshots and
  old orders targeting term mode fail closed for manual handling.
- Migration253 and rollout repository: conversion acquires ordered scopes,
  parent and term locks before final user/group/key locks; allocated deleted or
  inactive users are excluded by the final validity query. The administrator is
  rechecked after lock waits. Revision, actor-bound preview, complete allocation
  fingerprint, pending requests/orders, replica roster/capabilities and config
  are rechecked before the atomic mode/card/outbox/operation commit.
- Maintenance guards block entitlement changes and new orders while permitting
  existing windows accounting counters to drain. Both old and new group IDs
  are checked when a parent is moved. GPT groups avoid the Anthropic group-lock
  trigger path. Conversion remains maintained until explicit release.
- Protected queue marker is persisted before user concurrency acquisition and
  reused at the final upstream gate. Proven no-send cleanup can drain it;
  attempted unknown usage remains held. Trusted usage persists before accounting,
  and accounting failure does not mark the protected request completed.
- HTTP routes, strict maintenance boolean/required arrays, versioned DTO fields,
  response projections and the frontend operation/preview contracts align.

Historical-payment boundary raised during review:

`subscription_quota_rollout_repo.go` pending-order classification excludes
historical `CANCELLED` and elapsed-grace `EXPIRED` rows, whereas
`payment_fulfillment.go` permits a valid cancelled-order callback to recover
payment. Such a callback after conversion preserves payment evidence but cannot
invent a term allowance from a new plan; it requires manual handling. The
coordinator accepted this existing T53 boundary instead of broadening conversion
into settlement/cancellation of historical orders. Backend/UI owners are adding
`legacy_terminal_orders` and an explicit warning; that addition was not assumed
complete by this review.

Evidence consulted: I5 integration receipt (15 top-level / 29 incl. subtests),
payment integration tests for concurrent signed callbacks, immutable/deleted
plans, transaction rollback, stale leases, disabled issuance and refund guards;
rollout tests for strict empty allocations, changed previews, atomic failure,
pending queue/late usage, replicas, concurrent confirmation, order drift,
revoked actor after scope wait and unchanged GPT row locking. Tests were read,
not rerun by this reviewer; their execution remains the integration owner's gate.

Limits: static inspection cannot prove every SQL interleaving, production
replica configuration, provider delivery behavior, or the final-image browser
flow. Existing/native payment and GPT code outside the touched paths was not
exhaustively re-audited. Final combined source hashes, real PostgreSQL/Redis
execution, desktop/390px interaction and rebuilt-image rollback evidence remain
separate required checks. No production users, amounts, database or credentials
were accessed.

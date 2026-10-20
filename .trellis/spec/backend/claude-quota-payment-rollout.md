# Claude quota payment and controlled rollout

## 1. Scope / Trigger

Read when changing Claude `subscription_quota_mode=term` plan sales, paid
fulfillment, maintenance/conversion, replica configuration or recovery. Original
GPT carpool and native windows contracts remain separate. Sources are
`subscription_quota_payment.go`, `subscription_quota_rollout*.go`, migrations
252–254 and their real PostgreSQL / final-image tests.

## 2. Signatures

Admin group rollout base: `/api/v1/admin/groups/:id/subscription-quota/rollout`.
GET returns authoritative state. POST `/maintenance` takes explicit `enabled`,
`expected_revision`, reason and Idempotency-Key. POST `/preview` takes explicit
`allocations:[{user_id,quota_usd}]`; POST `/convert` additionally takes
`expected_revision`, `preview_token` and Idempotency-Key.

A term payment order carries immutable schema1 subscription entitlement with
mode, group/platform, exact quota string, 604800-second duration and display
names. Migration252 rejects changes to its snapshot and entitlement identity.
`FulfillPaidTerm` verifies the current payment order's fulfillment lease version
before invoking the shared transaction-local card creation helper.

`POST /api/v1/payment/public/orders/resolve` takes `{resume_token}` and, after
existing signature/order-identity validation, returns `PublicOrderResult` with
`subscription_quota_snapshot: SubscriptionQuotaOrderSnapshot | null`. The
legacy anonymous `POST .../public/orders/verify` takes `{out_trade_no}` and
returns only its existing `PublicOrderVerifyResult` status/timestamps; it never
includes the entitlement, price or plan/user identifiers.

## 3. Contracts

Sale price and quota are different values. A saleable term plan requires a
strict positive default quota string with at most eight fractional digits
(canonical storage has eight) and seven days. The UI unit is `days`; existing
`day` plans remain accepted. Plan CRUD, sale listing, checkout preflight and the
locked snapshot validation use one fixed-seven-day duration validator; month,
year and other day counts are rejected. Manual issuance may
use an explicit positive override when a valid selected plan has no default;
that plan need not be for sale.
Never use today's plan as the source of a previously paid order's entitlement.

Signed resume-token results and authenticated order results project the same
frozen entitlement through `service.PaymentOrderQuotaSnapshot(order)`. Do not
copy the raw JSON map: strict decoding rejects malformed, future-version and
unknown-field snapshots, returning null for display without exposing extra data.
The result page must distinguish purchase price from the frozen quota; adding
the field only to authenticated order DTOs misses the actual checkout redirect.

Order lease validation, unique payment identity, card, operation, assignment
audit and auth-cache outbox share one transaction. Existing payment completion
and affiliate behavior occurs afterward. Disabling new issuance/admission does
not revoke already-paid frozen entitlements or known-usage recovery. An old
null snapshot targeting term mode requires manual review; never infer a quota.

The rollout preview binds the actor, allocation set, revision, group/key/user/
parent/order facts and replica capabilities. Conversion locks ordered scopes,
parents and terms before final users/group/keys, rechecks the actor after lock
waits, and atomically commits mode, explicitly allocated cards and outbox.
An explicit empty allocation list creates zero cards. Converted groups remain
in `quota_maintenance` until a separate release. Generic entitlement/lifecycle
changes and new orders cannot bypass maintenance; trusted old-source accounting
may drain. Only Anthropic group rows use the new trigger-lock path.

`SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID` is a deployment identity; expected IDs are
an operator-provided complete roster via comma-separated
`SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS` or a YAML array. Each process
advertises a random instance token after database readiness. Heartbeats are 10s,
freshness45s. The exact flags are `SUBSCRIPTION_QUOTA_ISSUE_ENABLED`,
`SUBSCRIPTION_QUOTA_ADMISSION_ENABLED`, `SUBSCRIPTION_QUOTA_RECOVERY_ENABLED`;
the independent default is `DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY` and lists
are `SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS` / `SUBSCRIPTION_QUOTA_PROTECTED_GROUP_IDS`.
Both image variants default to issue/admission false and recovery true; an R2
tag alone never activates a group. Expected live instances require default Claude concurrency3,
capability/receipt/lease versions1, all three flags enabled, identical enabled/
protected lists containing the group, and an identical roster. Self-registration
cannot discover an unlisted legacy process; verify the actual deployment roster.

`legacy_terminal_orders` counts historical null-snapshot CANCELLED/EXPIRED rows.
It is an operator warning, not an additional blocker; an EXPIRED row still in
the five-minute grace period may also count toward blocking pending orders.
A valid CANCELLED-order callback may recover PAID; after conversion a missing
snapshot makes fulfillment fail for manual handling without a guessed card.
EXPIRED recovers only within its existing five-minute `updated_at` grace;
a callback beyond grace records `PAYMENT_AFTER_EXPIRY` audit evidence and does
not automatically restore PAID. Preserve this distinction in operator copy.

## 4. Validation & Error Matrix

| Condition | Required outcome |
| --- | --- |
| Invalid/zero/exponent quota | Reject before rights writes |
| Concurrent duplicate callback or confirmation | One card/operation result |
| Payment lease changes before fulfillment | No rights from stale worker |
| Outbox/audit failure | Roll back card and all related changes |
| Changed actor/preview facts | Conflict or denied actor, refresh/review |
| Missing/null allocations | Invalid; explicit `[]` remains valid zero issuance |
| Queue marker, async known or unknown usage unfinished | Conversion blocked |
| Missing/stale/duplicate/extra/mixed replica | Conversion and maintenance exit blocked |
| Unhealthy replicas during maintenance entry | Stop-flow operation remains available |
| Stale windows auth cache against DB term | Refuse before any old user slot/wait operation |
| Concurrent history index build is invalid | Drop/rebuild only the named invalid index |
| Valid signed resume token and valid frozen snapshot | Typed immutable entitlement in result DTO |
| Legacy/malformed/future/unknown-field result snapshot | Snapshot null; no raw fields or invented rights |
| Anonymous out_trade_no lookup | Existing status/timestamps only; no snapshot |

## 5. Good/Base/Bad Cases

Good: paid quota100 survives a later plan change to300 and fulfillment retries.
Base: explicitly convert one user to75.12345678, keep other keys without invented
rights, and replay the same confirmation safely. Bad: reset native days for a
term refund, replace a positive ledger SUM check with guessed totals, or mark an
unknown protected request no-charge merely because it is old.
For checkout redirects, good is a signed result showing price12.34 and frozen
quota125.12345678 separately; bad is treating the purchase amount as a balance
credit or exposing the snapshot through anonymous order-number lookup.

## 6. Tests Required

Use real PostgreSQL for signed fake-provider callbacks, frozen/deleted plans,
concurrent retries, lease handover, transaction rollback, actor revocation after
lock wait, preview drift, paid-order blockers and GPT row-lock independence.
Final images must prove R0→R1→R2→compatibleR1, known usage and already-paid
fulfillment exactly once with sales/admission off, and no old-balance rewrite.
Exercise actual resume-token handlers with signed tokens and database reads:
valid entitlement, legacy null, malformed/future/unknown-field null, mismatched
identity rejection and unchanged anonymous DTO allowlist. Browser verification
must follow the real successful checkout redirect as well as order history.

Runtime conversion tests need real pre-slot waiting, actual completed legacy
billing, explicit conversion, old auth-cache injection, unchanged active GPT
HTTP/WS/user/ingress leases and successful GPT settlement. Redis MONITOR command
names are case-insensitive: calibrate with a no-write script, drain a final fence,
and verify reader health before accepting zero old-scope attempts. An HTTP200
or clean SSE EOF alone is not a successful upstream completion.

Keep migration254's online-index timeout restoration and invalid-index recovery
checks. Performance receipts must include substantial ledger/request histories,
scope latency and GPT interference. Exact ledger SUM remains linear; partial
indexes improve admission/backlog without removing monetary invariants.

Lease operation observation must not perform synchronous logging or other I/O:
an unavailable log sink must not delay the real cancellation deadline. Update
bounded counters under a short mutex and publish a copied snapshot from the
observer worker. Keep the blocked-log-writer regression alongside lease timing
tests; moving only the final cancel before one log call misses earlier renew errors.

## 7. Wrong vs Correct

Wrong: reread plan quota after callback, return success after only appending a
card, or roll back an activated database to R0 because its image still starts.

Correct: consume the frozen entitlement under the original order lease,
commit rights/audit/outbox together, and use the same final compatible executable
with new sales/admission disabled and recovery enabled. Retain the newest
ledger and database; never down-migrate or change term back to wallet mode.

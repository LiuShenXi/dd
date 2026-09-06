# Carpool v1.4 HTTP and Gateway Contract

All paths below are relative to `/api/v1`. Existing response envelopes and authentication middleware remain unchanged. USD and CNY amounts are JSON decimal strings. Times are RFC3339 instants. All state-changing endpoints require `Idempotency-Key`; replay with the same normalized payload returns the original response, while the same key with a different payload returns `409`. The read-only POST preview is explicitly exempt and creates no operation or financial records.

## Gateway contract

Core types live in `internal/domain/carpool.go`.

```go
type CarpoolBillingSnapshot struct {
    BillingRequestID int64
    RequestID        string
    UserID           int64
    APIKeyID         int64
    GroupID          int64
    TermID           int64
    CycleID          int64
    AdmittedAt       time.Time
}
```

`CarpoolBillingService.Admit` is called at final upstream admission, after concurrency waiting, and once per WebSocket turn. It verifies the user/key/group relationship, activates the current fixed cycle if necessary, and persists the immutable billing intent. Ordinary retries and failover reuse the returned snapshot.

Known actual usage is persisted before applying the debit. Recovery returns durable, unsettled receipts without holding a receipt row lock while calling `UsageBillingRepository.Apply`. The gateway command carries the snapshot and a quantized `CarpoolCost`; core's repository helper locks the original cycle, debits base then boost then manual, retains overuse as base debt, appends ledger rows, and marks the billing request settled in the existing usage-dedup transaction. Carpool never falls back to ordinary balance.

## User API

User identity comes only from the authenticated subject. These endpoints accept no user, term, or cycle selector.

### `GET /user/carpool/details`

```json
{
  "server_now": "2026-09-05T13:00:00+08:00",
  "timezone": "Asia/Shanghai",
  "quota": { "available_usd": "576.55000000" },
  "usage": {
    "total_used_usd": "123.45000000",
    "history_complete": true,
    "statistics_since": "2026-08-01T00:00:00+08:00"
  },
  "term": {
    "status": "active",
    "starts_at": "2026-09-05T12:00:00+08:00",
    "expires_at": "2026-10-03T12:00:00+08:00",
    "reset_count": 0,
    "reset_count_basis": "current_term",
    "current_cycle_no": 1,
    "cycles": [
      {"cycle_no": 1, "starts_at": "2026-09-05T12:00:00+08:00", "ends_at": "2026-09-12T12:00:00+08:00", "status": "active"}
    ],
    "reset_events": []
  },
  "reset_window": {
    "status": "none",
    "scheduled_at": null,
    "schedule_revision": 0,
    "eligible_for_me": false,
    "ineligible_reason": null
  }
}
```

`term` is null when there is no current or nearest relevant term. The example abbreviates cycles; new terms return four contiguous7-day entries and expiry28days after start. Legacy terms return their actual persisted cycles/dates.

`quota` is null for no active covered term/current cycle, including pending/expired. An active term exposes only `available_usd`, including string zero when exhausted. Compute max(0,base+boost+manual) on the server after activation; never add ordinary balance. Dashboard/header share this projection and refetch after claims, boundaries and visibility. Query failure is not a successful null response.

`reset_count_basis` is `current_term`. `term.reset_events` is an array (never null), ordered by actual execution time with stable tie order. Each item is `{ "cycle_no": 1, "occurred_at": "2026-09-06T22:00:20+08:00", "target_quota_usd": "700.00000000" }`. Read only succeeded targets joined to completed batches and the selected user's matching term/cycle. Time is target.executed_at, not scheduled/detected time; target is immutable cycle.base_quota_usd, not granted delta/current balance. Include zero-grant successes. Reset count agrees with successful participation; pending/failed/foreign records are excluded.

`reset_window.status` remains `none|scheduled|executing|delayed|completed`. No internal IDs, plan configuration, bucket breakdown, payments, actor notes, upstream evidence or raw ledgers are exposed. The two approved amount projections above replace the prior blanket time-only restriction. Reset annotations never alter time progress.

### `GET /user/carpool/boosts`

```json
{
  "eligible": true,
  "remaining": 3,
  "total": 3,
  "amount_usd": "55.00000000",
  "help_text": "This 28-day term includes 3 boosts. Boost quota expires with the current cycle.",
  "unavailable_reason": null
}
```

### `POST /user/carpool/boosts`

Request body is `{}`. Successful data:

```json
{
  "eligible": true,
  "remaining": 2,
  "total": 3,
  "amount_usd": "55.00000000",
  "help_text": "This 28-day term includes 3 boosts. Boost quota expires with the current cycle.",
  "unavailable_reason": null
}
```

The response omits term/cycle IDs and expiry. A successful replay returns the original result even across boundaries. Amount/count/help derive from the frozen term, including legacy duration/count. Successful claims refresh details quota; never increase users.balance or optimistically add the nominal amount.

## Admin write API

### `POST /admin/users/:id/carpool/preview`

```json
{
  "plan_id": 1,
  "starts_at": null,
  "mode": "new",
  "takeover": null
}
```

`starts_at: null` is evaluated from database time. For a deterministic preview, the response returns `calculated_at` and the resolved `starts_at`. Historical takeover requires `takeover` with `current_base_balance_usd`, `current_boost_balance_usd`, `current_manual_balance_usd`, `boost_used`, `history_complete`, and optional `ordinary_balance_transfer_usd`.

Preview validates the positive path user ID and the existence of that nondeleted user (invalid ID: 400; unknown user: 404), and rejects disabled or superseded plans. It is a read-only calculation; no `Idempotency-Key` is required or persisted. Each invocation recalculates database time, and a later open with null `starts_at` resolves its own transaction time rather than inheriting the preview time.

```json
{
  "calculated_at": "2026-09-05T12:00:00+08:00",
  "mode": "new",
  "plan": {"plan_id": 1, "name": "four_seat", "list_price_cny": "330.00", "weekly_quota_usd": "550.00000000", "duration_days": 28, "cycle_days": 7, "boost_amount_usd": "55.00000000", "boost_count": 2},
  "starts_at": "2026-09-05T12:00:00+08:00",
  "expires_at": "2026-10-03T12:00:00+08:00",
  "cycles": [{"cycle_no": 1, "starts_at": "2026-09-05T12:00:00+08:00", "ends_at": "2026-09-12T12:00:00+08:00", "base_quota_usd": "550.00000000", "initial_action": "grant"}],
  "warnings": []
}
```

### `POST /admin/users/:id/carpool/terms`

Uses the preview body plus `group_id`, `notes`, and optional payment. The backend assigns the global carpool `scope_id` of `1`; clients cannot split cooldown/reset scope by choosing a group ID.

```json
{"payment":{"amount_cny":"330.00","payment_kind":"payment","paid_at":"2026-09-05T12:00:00+08:00","channel":"manual","external_order_no":null,"notes":null}}
```

Returns the admin term with all four weekly cycles and optional payment. Snapshot price, weekly quota, boost ratio/count/amount and duration/cycle rules. No fifth cycle is created for new28-day terms. The legacy cycle_5_quota_usd field may remain for compatibility, but is not a new-term grant or displayed offering. Overlap remains transactional; old snapshots/ledger are unchanged.

When `history_complete` is true, takeover also requires `historical_used_usd` and `statistics_since`. `ordinary_balance_transfer_usd` is funding attribution for the supplied final current bucket balances, must not exceed their positive net total, and never adds quota a second time.

### `POST /admin/carpool/plans/:id/versions`

```json
{"name":"Four-seat","list_price_cny":"330.00","weekly_quota_usd":"550.00000000","boost_ratio":"0.10000000","boost_count":2,"enabled":true}
```

Creates a new immutable version with fixed28-day duration,7-day cycles and0.10 ratio. boost_count is an administrator-editable integer parameter accepting2 or3, default2; existing snapshots retain their original count. Superseded versions cannot open new terms. Upgrade appends current-rule versions preserving latest custom name/price/weekly/enabled; never overwrite old snapshots or revive disabled tiers.

Only the latest version of a plan code can be versioned. A disabled latest version remains manageable and may be replaced by an enabled version; superseded versions cannot be revived directly. The response uses the admin plan projection with `plan_id`, snapshot fields, `enabled`, and `is_latest`.

### `POST /admin/carpool/terms/:id/renew`

```json
{"plan_id":1,"notes":null,"payment":null}
```

Creates a non-overlapping28-day future term at current expiry, without early quota or boost restoration. Omitted plan_id resolves latest same-tier version, not a superseded old ID or guessed other tier. An explicit administrator-selected plan_id may change the next term's tier, retaining the existing upgrade/downgrade contract; it must still be a latest enabled plan. Disabled latest tiers remain unavailable.

### `POST /admin/carpool/terms/:id/terminate`

```json
{"reason":"operator requested termination","effective_at":null}
```

Stops new admission at the database transaction time when `effective_at` is null. It does not delete terms, cycles, payments, or ledger rows.

### `POST /admin/carpool/cycles/:id/adjustments`

```json
{"bucket":"manual","delta_usd":"25.00000000","reason":"documented correction","reverses_ledger_id":null}
```

`delta_usd` may be positive or negative but not zero. Reversal is another immutable entry and must name the original entry. Positive grants first offset base debt through paired transfer entries.

### `GET/POST /admin/carpool/terms/:id/payments`

POST body:

```json
{"amount_cny":"330.00","payment_kind":"payment","paid_at":"2026-09-05T12:00:00+08:00","channel":"manual","external_order_no":null,"notes":null}
```

Refunds use `payment_kind: "refund"` and a positive magnitude; net receipts are payments minus refunds. Payment records never alter USD quota.

## Admin read API

`GET /admin/carpool/plans` returns an array of the latest versions, one per plan code, including disabled latest versions. Every admin plan includes `plan_id`, snapshot fields, `enabled`, and `is_latest:true`. Opening and renewal choices must exclude disabled plans; the management view keeps them visible for re-enabling through a new version.

`GET /admin/carpool/terms`, `/admin/carpool/cycles`, and `/admin/carpool/ledger` use `page`, `page_size`, and documented field-specific filters. These list responses use `{items, total, page, page_size}`. Admin projections may include entity IDs, snapshots, balances, payment totals, actors and reasons, but never raw upstream credentials or reset-card IDs.

The reset batch endpoints in the architecture are reserved for the reset package and must preserve these identity, decimal, time, idempotency and admin-auth rules.

## Billing Exception Reconciliation

`GET /admin/carpool/billing-exceptions` returns paginated sanitized exception records, including frozen request/user/key/term/cycle references, admission time, state, retry count and a static diagnostic category. It never returns billing payloads, prompts, credentials or upstream response bodies.

### `POST /admin/carpool/billing-exceptions/:id/reconcile`

Administrator authentication and `Idempotency-Key` are required. This is the explicit manual resolution required by architecture sections 5.2 and 6.4, not an automatic assumption about missing usage.

```json
{"confirmed":true,"resolution":"actual_cost","actual_cost_usd":"1.25000000","reason":"verified accounting correction"}
```

`resolution` is `no_cost` or `actual_cost`. `confirmed` must be true and `reason` nonblank. `actual_cost` requires a positive amount after eight-place quantization. `no_cost` permits no amount or zero only. New resolution is restricted to unresolved `reconcile_required` records. A fresh operation on an already resolved record conflicts; an exact successful idempotency replay returns its original response even after settlement. The same key with another normalized payload returns 409.

Resolution uses the original frozen cycle and the common usage-dedup/debit implementation. The administrator operation, original response, audit record, debit/dedup and settled state commit atomically. Lock ordering agrees with normal billing; manual resolution must not hold a receipt lock while acquiring its cycle or invoke an independently committed Apply before saving its operation. Concurrent or late durable usage cannot be overwritten by a manual amount.

Unknown token/account/key auxiliary usage is not guessed, and no fabricated usage-log entry is created. The sanitized response records `resolution`, `resolved_at`, `resolved_by`, `reason`, `actual_cost_usd`, and `auxiliary_usage_reconstructed:false` alongside the exception projection. An explicit no-cost resolution may unblock cycle closure, but absence of a receipt alone never may.

## Reset Admin Contract (Director Integration Decision)

All writes require `Idempotency-Key` and payload-consistent transactional replay. Default `scope_id` is 1 (the shared carpool business), not the group ID. The observation service is read-only toward upstream providers and never consumes reset cards.

### `GET /admin/carpool/reset-batches`

Accepts `page`, `page_size`, optional `scope_id` and `status`. Returns `{items,total,page,page_size}`. Each item contains `id`, `scope_id`, `status` (`qualified|scheduled|running|completed|cancelled|needs_review`), `detected_at`, `qualified_at`, `slot_at`, `scheduled_at`, `schedule_revision`, `effective_at`, `completed_at`, `delay_reason`, `announcement_state`, `target_count`, and `granted_usd` (decimal string). Nullable timestamps are explicit null. `granted_usd` and targets are actual successful results, never promised amounts. Internal raw card IDs and credentials are excluded even from admin DTOs.

### `POST /admin/carpool/reset-batches`

This is explicit administrator registration of a locally confirmed qualification, including the source architecture's direct upstream reset with no card event. It is not an immediate quota reset.

```json
{"scope_id":1,"confirmed":true,"source_event_key":"operator-event-reference","reason":"documented qualification evidence"}
```

`confirmed` must be true. `source_event_key` and nonblank `reason` are required; event identity is unique within scope, so different retry keys cannot register the same external event twice. The frontend asks for the reference and reason, not an upstream credential or raw card ID. This endpoint joins the scope's existing pending window without delaying it, or creates/schedules one qualification and a publication event. Returns the admin batch projection above. Automatic complete card observations use the same scheduler, not this HTTP handler.

### `POST /admin/carpool/reset-batches/:id/schedule`

```json
{"reason":"documented schedule review"}
```

No client-provided arbitrary date or skip-cooldown option. Scheduled future batches retain their promise unless it is invalid; overdue or invalid slots move to the earliest qualified precise 22:00 slot and increment schedule revision with a correction event. Needs-review qualification requires administrator confirmation through this action with a nonblank reason. Completed/cancelled batches cannot be reissued. Returns the updated admin batch.

### `POST /admin/carpool/reset-batches/:id/execute`

Body `{}`. This is due-only idempotent execution/retry, never an override. Returns the admin batch when completed or rescheduled. Before due time or without confirmed qualification, return a structured conflict/unavailable error without granting. Execution outside its one-minute slot reschedules and corrects the promise; it never grants immediately at an arbitrary hour.

### `GET /admin/carpool/reset-observations`

Returns paginated sanitized health records: `upstream_identity_hash`, `baseline_complete`, `last_complete_at`, `health_status`, `known_credit_count`, `unassigned_credit_count`. The UI must not collect or display raw provider secrets/card IDs.

### `POST /admin/carpool/reset-observations/scan`

Body `{}` plus write idempotency key. Requests a bounded read-only scan of managed OpenAI parent accounts using the existing query abstraction. Returns `{scanned,complete,incomplete,new_candidates}` counts only. Synthetic adapters validate the path locally; the sealed production-data copy cannot access real providers. The endpoint must not call card consumption or alter provider balances.

## User Announcement Version Contract

### `GET /announcements/version`

Uses the existing authenticated announcement route group and returns `{ "version": "opaque-stable-version", "unread_count": 0 }`. Identity comes only from auth; no user/scope selectors. Compute the version from this user's currently visible announcement IDs, update revisions/timestamps and read state, including changes to current/future-term audience eligibility. An unauthorized audience must not learn global batch activity through a shared version. No title, content, source evidence, term IDs or raw upstream data is included.

The existing full list and mark-read endpoints remain `/announcements` and `/announcements/:id/read`. Carpool member polling checks this light projection every 30-60 seconds while visible, refreshes the full list on version change, and rechecks on route entry/window visibility. Logout/account switching clears timers, versions, cached announcements and stale in-flight responses. Backend list/read-status/mark-read/version use the same audience predicate, including current or future nonterminated terms in the carpool scope.

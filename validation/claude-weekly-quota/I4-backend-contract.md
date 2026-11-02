# I4 backend boundary and frontend contract

Base: accepted I3 commit `9005acffad8f9b30975ea3dd27e82095eb9e5520`.
This file freezes interfaces for parallel implementation, not acceptance.

The gap is presentation and audited administration: old subscription surfaces
still expose parent windows, usage metadata has no term allowance, and the I3
resolution domain has no administrator HTTP adapter. Canonical term reads
already exist. Main owns backend projection/DTO/handlers/middleware/request
query/wire and their tests; the frontend owner consumes these fields without
editing backend. Synthetic candidate infrastructure is independently owned.

- Existing user/admin subscription rows, progress and summary items add
  `quota_mode: "windows" | "term"` and
  `quota_summary: SubscriptionQuotaSummary | null`. The summary is the existing
  I2 public DTO with eight-decimal strings, server_time, current_term, next_terms
  and last_term. Null for term means unavailable, never windows/balance fallback.
  Term serialization omits daily/weekly/monthly parent-window values. Legacy
  windows values retain their existing shape and behavior. Active term rows
  require canonical current_term, not future parent expires_at.
- `/v1/usage` term is selected before the Key-limit branch and returns
  `billing_scope: "claude_subscription"`, `quota_mode: "term"`,
  `quota_summary`, `remaining` (current card remaining string or `"0.00000000"`),
  `unit: "USD"`, `expires_at` (current-card timestamp or null), `planName`,
  `status`, and `isValid`. Existing mode is still quota_limited/unrestricted
  according to Key limits. Existing `quota` and `rate_limits` remain independent
  Key constraints and never overwrite card remaining. Existing usage,
  daily_usage and model_stats remain optional Key-scoped statistics.
- Administrator request query: GET
  `/api/v1/admin/subscription-quota/requests`, optional cursor, limit 1..100
  default20, status enum, positive user_id/group_id. Response uses OpenAPI
  RequestPage `{items,next_cursor,has_more}`. Request is the sanitized I3
  projection; pricing/command/log/evidence are not returned.
- Resolve: POST
  `/api/v1/admin/subscription-quota/requests/:request_id/resolve`;
  Idempotency-Key required, expected_request_revision required (zero valid),
  resolution/evidence_reference/reason and optional verified_usage strictly
  follow OpenAPI. All five required token counters use presence pointers at
  the HTTP boundary; missing/null differs from explicit zero. Return202 for a
  newly accepted resolution, 200 for replay, never imply completed settlement.
- Manual UI issuance uses required explicit quota and existing preview/open/
  cancel APIs. No reliable term-plan enumeration exists yet; plan/payment
  settings and order recovery remain I5. Existing explicit plan_id on the I2
  service API remains supported.

Implementation will touch subscription service/read model and pure DTO mapper,
user/admin subscription handlers, Gateway usage and its metadata guard,
independent request query and strict resolution adapters, and Wire providers.
The initial presentation slice does not change ledger, price calculation,
GPT admission or paid orders. The subsequently authorized v2 supplement below
adds one compatible migration. Existing windows paths are proven without a term
reader call, and actual HTTP/isolated candidate evidence must complement
frontend component tests.

## Authorized v2 concurrency supplement

User-selected production default is independently **3**. The per-user nullable
override is `claude_subscription_concurrency`; HTTP omission preserves the old
value, while explicit null, noninteger, zero, negative and values greater than
PostgreSQL int32 max 2147483647 are rejected. Migration251 adds only this nullable
positive field. Existing `concurrency` values and old bulk interfaces stay scoped
to the original pool. Explicit changes invalidate all user Key auth snapshots
and log a separate actor/target/old/new audit. Auth snapshot version is27.

The User/Admin/self and `/v1/usage` projections add these flat fields:

| Field | Meaning |
| --- | --- |
| `claude_subscription_concurrency` | nullable user override |
| `default_claude_subscription_concurrency` | independently configured default |
| `effective_claude_subscription_concurrency` | nullable positive effective limit |
| `claude_current_concurrency`, `claude_waiting_count` | nullable observed active/waiting counts |
| `claude_concurrency_status` | available, unavailable, or not_configured |
| `legacy_current_concurrency` | nullable observed count using original legacy/live key semantics |
| `legacy_concurrency_status` | available or unavailable |

Old admin `current_concurrency` retains its existing representation. New UI uses
the explicit legacy nullable/status pair. Each pool's added read is independent
and bounded using a separate Redis client with context deadlines and retries
disabled. A failed read never becomes a reported zero, and neither pool's
admission reads the other pool's usage as a shared upper bound.

New Claude scope Lua handles active/waiting UUIDs atomically with Redis TIME,
per-member expiry, atomic promotion, idempotent release, and nonresurrecting renew.
A same-slot released-ID tombstone prevents an unknown operation's late retry from
reentering after cleanup. Lease60s/heartbeat15s/operation2s are engineering values.
Local cancellation deadlines derive from Redis remaining duration and monotonic
elapsed time, allowing host wall-clock skew. Request cancellation cancels the
actual upstream; release waits for the actual local forwarding task to terminate.
Known monetary recovery remains independent and does not hold concurrency slots.

Acquire/Promote read the current policy under a short user FOR SHARE transaction
around one bounded Redis operation. No PostgreSQL lock spans queue waiting or
account/term admission. Failure, unknown response or failed commit cleans both
preallocated request IDs with an independent bounded context. New issuance and
final term admission also validate positive effective concurrency. Successful
issuance replay precedes the current policy check; paid-order recovery remains
an I5 obligation and must not lose already purchased rights.

For listed protected legacy groups, a durable DB-validated marker is established
before any legacy user-slot action and reused/revalidated at final forward. This
expands the protected drain interval to include queue waiting. A never-attempted
request may finish with explicit no-send evidence; an attempted unknown retains
reconciliation. Complete maintenance conversion, all-replica/config/protocol
compatibility and final-image rollback remain I6 requirements, not claims made
by the I4 local marker tests.

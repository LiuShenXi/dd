# v2 concurrency acceptance supplement — pending

This is a review checklist, not evidence of passed tests. It supplements the
historical I0-I3 v1 acceptance and the in-progress I4 work. No final I4, I5,
I6 or production acceptance is implied.

## Backend and gateway boundaries

- Only authenticated anthropic subscription term identities select the new
  claude_subscription scope. Clients cannot select a scope. One user shares
  it across all Keys, term groups and card periods. GPT keeps legacy_user keys,
  user limits, queues, HTTP/WS/live behavior and failure policy.
- The nullable positive user override and independent positive default are
  fully mapped through migrations, Ent, user APIs, cache and display. Omitted
  admin updates leave policy unchanged. Explicit null/zero/negative updates
  fail. Legacy bulk concurrency updates do not write the new field.
- Policy changes serialize against the final Acquire/Promote policy read.
  The proposed short user FOR SHARE transaction may encompass one bounded
  Redis Lua call; it ends before waiting, account acquisition or term admission.
  Redis grants whose response or following PG commit is uncertain must be
  cleaned by their pre-generated server ID, with an independent bounded context.
- Lua active/waiting members use Redis TIME and the same per-user hash tag.
  Repeated IDs do not consume capacity twice; renewal never recreates a missing
  lease. Key expiry cannot shorten another member's valid deadline. Promotion
  atomically verifies and removes the original waiting ticket and adds a lease.
- Every error path, cancellation, panic and same-group account retry preserves
  user-slot ownership until forwarding ends and then releases it once. The
  account slot remains separately enforced. No PostgreSQL transaction waits
  in a concurrency queue.
- v2 replaces the v1 term-only detached upstream context on client cancellation.
  Actual forwarding is cancelled, known complete usage still gets independent
  durable persistence, and incomplete usage remains SQL NULL/reconciliation.
  GPT/legacy cancellation behavior remains unchanged. Historical v1 disconnect
  transport evidence must be rerun under this updated contract.
- Heartbeat loss has a local conservative cancellation deadline and bounded
  calls. Redis failure cannot switch pools or forgive usage. Queue pings must
  retain the existing streaming error envelope after headers are sent.

## Required execution evidence

- T63-T71 use real isolated Redis. Two application instances demonstrate a
  shared atomic user limit and queue. A synthetic GPT 5 / Claude 2 identity
  sustains 5+2 without a combined user cap, while account capacity is sufficient.
- T68 includes an actual stream longer than the old fifteen-minute TTL, with
  renewal, no early new admission, and final cleanup. Short-TTL tests supplement
  rather than replace that duration proof.
- T69/T74 use the actual router and controllable provider across all generating
  aliases and stream modes. Explicitly check the final embedded UI/static
  middleware does not swallow root API aliases, as the I4 diagnostic discovered.
- T73 includes old-cache, lowered-limit while queued, legacy bulk operation,
  both UI values and null/unavailable statistics; never turn read errors into 0.
- T72 uses the final images and latest database: old Claude in-flight drains,
  mismatched replica capability/default/protocol configuration refuses enable,
  and rollback rejects new Claude while keeping receipts/orders recoverable.
  GPT active keys, waiting counters and WS ingress are never emptied to pass.

## Production preparation observations

The read-only recovery probe found PostgreSQL 18.6 and usage_logs estimated at
403,183 rows / 536,117,248 bytes. Historical integration tests used PostgreSQL
18.1. Final I6 must record the version comparison and exercise migration 250
against realistic synthetic legacy data, including trigger/precision behavior,
lock limits and elapsed time. This does not authorize copying real user data.

The user explicitly selected production default Claude concurrency 3, with
administrator per-user overrides and unchanged GPT settings. Verify the
actual final release configuration. The 5/2 synthetic values are separate.

## In-progress coordinator findings

1. Redis socket timeout alone was insufficient: the installed go-redis client
   default disabled context deadlines. Main inspected its actual library and
   introduced a separate Claude-only client with context deadlines, no retries
   and bounded dial/pool/read/write settings. Real fault validation is pending.
2. A late uncertain Redis grant after cleanup needs a protocol boundary. Main
   added per-scope released-ID tombstones, isolated from GPT. Validate delayed
   command ordering and cleanup; do not claim infinite-partition guarantees.
3. The local lease deadline now derives Redis remaining duration minus observed
   operation time and safety margin, rather than comparing unrelated wall clocks.
   Fault/timing tests remain pending.
4. Client cancellation previously released account slots before the actual term
   forward returned. Reviewer now uses once/deferred cleanup for Claude term
   account/user/serialization ownership and retains legacy wrappers. Actual
   cancellation/panic/account-switch evidence is still required.
5. A stale protected standard-group auth snapshot can choose the old user pool
   before the final send-time DB rejection. Main/reviewer must close this scope
   selection boundary and its conversion race without giving GPT a new DB
   dependency. Pending implementation and T58/T72 evidence.
6. UserEditModal's new form-identity watch cleared submitting on every edit,
   permitting overlapping writes to the same user. Requested a per-target
   mutation mutex separate from response identity, plus delayed-update tests.
   Reviewer implemented the per-authentication/per-target mutex and reported
   focused deferred-request tests passing. Final frozen-source/browser evidence
   remains required.
7. The new policy repository needs explicit nil-repository/nil-DB rejection
   instead of a panic; requested alongside missing-user/default/override and
   row-lock race coverage. Pending verification.
8. The initial self DTO exposed only Claude statistics, so the new Key page's
   legacy current count could never be available. Main added independent nullable
   legacy_current_concurrency and legacy_concurrency_status projections without
   changing old admin wrappers or GPT admission. Frontend wiring and final real
   Redis/UI verification remain in progress.
9. The additive legacy projection initially called the original Redis client,
   which does not honor short context deadlines. Requested a bounded read-only
   statistics path on the separate client, so a stalled legacy read cannot hold
   the new self/Key/admin view or suppress an otherwise available Claude reading.
   The original GPT admission client and its failure behavior remain unchanged.

## Diagnostic evidence checked by the coordinator

`I4-v2-redis-policy-debug-20261019T171631Z-receipt.json` records seven real Redis /
PostgreSQL top-level tests, zero failures and zero skips. They exercise dual-client
atomic limits, waiting promotion, Redis time/expiry, no resurrection after cleanup,
unavailability, policy defaults/overrides/row locking, and service queue cleanup.
The source changed during execution; the receipt explicitly has acceptance=false.
This is useful diagnostic evidence, not the final T63-T74 acceptance and not a
replacement for the real fifteen-minute-plus stream or actual router coverage.

The production R0 image provenance is independently verified in
`coordinator-r0-provenance.json`. Its OCI index digest and the local classic Docker
config ID differ by representation, not application contents: the validated OCI
graph, twenty RootFS diff IDs, revision and executable SHA-256 match. No application
was started and the local hash-only container had networking disabled.

Coordinator code follow-up: findings 5, 7, 8 and 9 now have implementation
changes and focused unit/actual-local-router tests. The protected pre-user
marker is persisted before entering the old scope and reused at final send;
unknown attempted requests remain held. Nil policy/database fails closed.
Both statistics projections use independently nullable values, with a separate
bounded Redis client for the legacy read and unchanged old admission client.
Full real Redis/PG and final-image acceptance is still pending. A tenth review
finding (user-create nil guard order) was corrected and is included in the
required final-source regression, not silently ignored as an old failure.

Two further coordinator/owner findings were found before final acceptance:

11. The scope Redis Options shallow copy shared mutable push/maintenance state
    with the original client, and its existing default dialer captured the old
    options. Main rebuilt only the new client's mutable configuration and bounded
    dialer, preserving network/auth/TLS and the original GPT client. An actual
    local TCP blackhole test now returns both operations in about two seconds;
    final candidate and real Redis evidence must include this fix.
12. The real Key page reads `auth.user` from `/api/v1/auth/me`. That handler used
    GetByID without the new statistics, while the tested `/user/profile` used
    GetProfile with them. This would leave real Key-page statistics unavailable.
    Main was asked to fix the specific current-user HTTP projection, not add Redis
    reads to global GetByID/JWT authentication, and cover the actual registered
    `/auth/me` route. Frontend first-load refresh timing is also under review.

The final read-only cross-review found a remaining instance of finding 9 in
the admin list handler: its original synchronous GetUsersLoadBatch ran before
the bounded projection. The developer replaced only this display read with the
same-key/same-expiry bounded legacy count. The actual registered admin route
returned in 2.0002425 seconds against an accepting but nonresponding TCP server,
with nullable unknown statistics and the legacy wrapper's compatible zero.
The old admission client's timeout/retry/context settings were asserted unchanged.
Evidence: I4-v2-admin-list-blackhole-debug.jsonl. The previous F1 candidate is
superseded; this fix must be in the next frozen candidate, not inferred from F1.

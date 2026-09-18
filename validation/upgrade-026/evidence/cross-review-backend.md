# Independent backend compatibility cross-review

Reviewed integration HEAD `7ac4f2bc5f2f00f11d0ea859b2eff22fe7a7372b` plus the current uncommitted image-cache validation and regression tests. This was a read-only source review; no shared source files, test runner or production resources were modified.

## Result

No additional release-blocking defect was identified in the selected merge surfaces. This conclusion is limited to source review of the image-cache accounting change, carpool HTTP/WS admission and cancellation behavior, authentication-cache v25, and lifecycle wiring. PostgreSQL/runtime acceptance remains the coordinator's separate gate.

## Baseline and source boundary

Read `production-baseline.md`: the BWH runtime image is `sha256:133537d6b33be5578a6e825fa74e7254de655ddf14b07ab7221790922477e4d0`, labeled `dfb612ae3+image-main-gpt55`. That report establishes the recovered build-source relationship through binary, all 291 migration checksums and 313 embedded frontend files; it explicitly does not claim reproducible build equivalence. Current compatibility review retains the deployed `gpt-5.5` default while accepting upstream's new `SUB2API_IMAGES_MAIN_MODEL` override. The separate image-generation tool model remains independent.

## Findings checked against implementation

| Surface | Review result |
| --- | --- |
| New image-cache usage | `openAIForwardResultHasKnownBillableUsage` now rejects negative `ImageCacheReadTokens` even when another counter is positive and recognizes positive cache-only usage. The new field reaches `UsageTokens`, discounted pricing and the cloned `ImageSizeBreakdown` projection. |
| Durable carpool accounting | `RecordUsage` requires the admission snapshot; malformed/missing known usage returns before receipt/billing/log writes. `applyUsageBilling` persists known usage before repository Apply, attaches exact snapshot/request ID and cost, and cannot fall back to ordinary balance. Cache updates and ordinary platform-quota accounting remain exempt for carpool. |
| Pricing time | `openAIUsagePricingAtWithContext` gives the snapshot's DB admission timestamp precedence over a later input timestamp. Both user billing and the newly extended account-stat cost call receive that `pricingAt`. WS also freezes the canonical snapshot time after admission lock waits. |
| New passthrough BeforeTurn | Passthrough now invokes BeforeRequest then BeforeTurn for response.create frames. Both handler hooks converge on `prepareTurnForForward`; the mutex + prepared-turn map prevents a second slot acquisition or a second durable admission for the same turn. The first turn is prepared before forwarding. |
| Model allowlist + carpool | First-frame allowlist validation precedes connection admission. Follow-up candidate validation precedes preparation, including effective session model and duplicate/nested candidates. The new regression checks rejected second turns produce no second admission or reconciliation request in dedicated and passthrough modes. |
| Retry/failover | Admission store is outside account-attempt scope. Existing same-turn snapshots are retained; current-turn failover remaps the surviving snapshot to retry turn 1. Same-account retry explicitly reacquires released user/account slots before retrying. Unknown/unsettled snapshots are reconciled by deferred drain. |
| Client cancellation | `usageRecordContext` copies billing snapshot and request identities to a fresh bounded worker context. Both regular and mandatory-media paths select synchronous carpool processing before the queue. `markCarpoolUsageUnknown` uses WithoutCancel plus timeout. Responses partial results and disconnect branches submit known usage; preemption returns through the deferred snapshot drain. |
| Auth cache v25 | Serialization round-trip preserves both per-user projected carpool billing and enforcing ModelAllowlist. `applyAuthCacheEntry` rejects mismatched snapshot versions, including both incompatible historic v24 forms. Per-request Group objects are reconstructed, preserving ordinary/carpool separation. |
| Wiring / shutdown / release gate | Wire and generated Wire include CarpoolService, CarpoolResetService, usage billing repository dependency, reset observation/audience hooks and API-key invalidation. Cleanup retains both carpool workers and adds ticket-harvester stop. Router retains usage-pool PendingTasks, ReleaseDrain and release routes. |

## Regression adequacy checked

The new RecordUsage image-cache test parses a representative Codex direct-image response, checks exact `0.00517000` customer cost through receipt, repository command and usage row, confirms image-cache projection, and asserts zero ordinary-balance fallback. Its malformed variant asserts no durable receipt or debit.

The canceled-context task test covers both text and mandatory-image submission and verifies one synchronous call, original snapshot and a live bounded context with no queue submission. Existing two-turn WS assertions require distinct admission IDs and settlement in both dedicated and passthrough modes; the new allowlist combination exercises the pre-admission reject case. The auth-cache regression verifies JSON round-trip and explicit v24 rejection.

`backend-compatibility-review.md` records the implementation worker's focused Go test passes. This independent reviewer read those results and the test source but did not rerun or claim independent execution while the coordinator owns the running PostgreSQL suite.

## Non-blocking observations / remaining evidence

- The inherited Messages disconnect logging branch checks nonnil result inside a nil-result alternative, making that specific log unreachable. The actual nonnil partial-result path still reaches common usage submission. This is not evidence of dropped carpool billing and does not justify an unrelated refactor during this merge.
- Full model-policy ordering, provider transport and transaction acceptance must use the coordinator's final source/image. Source inspection and repository doubles do not establish end-to-end production success or authorize real ticket harvesting.
- The separate migration bridge and exact-291 upgrade tests are subject to the coordinator's live PostgreSQL results. Schema rollback compatibility does not mean restoring an old database snapshot is safe after new usage occurs.

## Reviewed source fingerprints

Raw SHA256 at review time:

- `backend/internal/service/openai_gateway_usage.go`: `d975d8c96d210440d85b0e449f3a5dbef84f0f2c8bf5126bbaf1872faa2aab08`
- `backend/internal/handler/openai_gateway_handler.go`: `fe71deb16720a34d744fe259e1723a7631b7c0362ad1ce1ab8c831de4daec981`
- `backend/internal/handler/carpool_gateway.go`: `66f3066a5affc24a76ee520aa7a62009087aff03ce18b98faa167774c2362a2a`
- `backend/internal/service/api_key_auth_cache_impl.go`: `0c4d41943fe58394115131e2d6a957a425e7673a7a2e78e2c4966d53782fd911`

# I2 API and entitlement implementation boundary (read-only study)

Read against the architecture package and current isolated I1 source on 2026-10-19. No I2 business code was implemented. Authoritative sources: `api-contracts.md`, `contracts/openapi.json`, `billing-protocol.md`, `data-model.md`, `integration-contracts.md`, `ui-contracts.md` in `.trellis/tasks/10-19-claude-weekly-quota-architecture/`.

## Minimal domain boundary

Use one SQL-owned `SubscriptionQuotaService` with shared input normalization and schedule calculation for Preview and Open, plus Cancel, Summary and stable history queries. Keep the existing Ent `UserSubscriptionRepository` windows methods unchanged; they are deliberately guarded against term operations by I1. OpenTerm must create/update its compatibility parent through the new SQL transaction, not by calling the old Ent assign/extend service from inside that transaction.

Suggested small ports (names are implementation choices, semantics are contractual):

- `PreviewTerm(ctx, actorID, targetUserID, PreviewInput) -> Preview` (strictly read-only).
- `OpenTerm(ctx, actorID, targetUserID, idempotencyKey, OpenInput) -> MutationResult`.
- `CancelTerm(ctx, actorID, termID, idempotencyKey, CancelInput) -> MutationResult`.
- `GetSummary(ctx, userID, groupID)`, `ListSummaries(ctx, userID)`, `ListTerms(ctx, userID, groupID, cursor, limit)`.
- A read-only entitlement port `HasCurrentTerm(ctx, userID, groupID)` and batched `ListCurrentTermGroupIDs(ctx, userID)` shared by Key binding/list paths. It must not grant wallet permission or invoke a term mutation.

Keep admission disabled in I2. Manual issuance becomes possible only after its domain implementation is wired; paid snapshots/fulfillment remain I5 and generation/settlement remain I3.

## Exact new HTTP contracts

All paths below are under `/api/v1`, with the existing `{code:0,message:"success",data:...}` envelope and normal login/admin authorization. Register through the real user/admin route functions, handlers aggregate and Wire; a handler method alone is not an exposed API.

| I2 path | Inputs and response |
| --- | --- |
| `GET /admin/users/:user_id/subscription-quota` | Required `group_id > 0`; Summary even when no parent/card exists. |
| `GET /admin/users/:user_id/subscription-quota/terms` | Required group_id; cursor history page. |
| `POST /admin/users/:user_id/subscription-quota/terms/preview` | PreviewInput; 200 Preview. No Idempotency-Key required and no parent, operation, outbox or term writes. |
| `POST /admin/users/:user_id/subscription-quota/terms` | OpenInput plus Idempotency-Key length 16-128; 201 new operation, 200 replay. |
| `POST /admin/subscription-quota/terms/:term_id/cancel` | CancelInput plus Idempotency-Key; 200 MutationResult. Identity is resolved from the persisted term, never payload. |
| `GET /subscription-quota` | Own Summary array, never accept target user_id from client. |
| `GET /subscription-quota/groups/:group_id` | Own Summary; may expose future schedule while granting no current usage. |
| `GET /subscription-quota/groups/:group_id/terms` | Own term history page. |

The full package also defines own term usage, administrator request list and request resolve. Those depend on I3 request/ledger facts; do not implement a successful fake resolution in I2. Existing subscriptions list/progress/UI compatibility is part of the complete product and must subsequently consume the same summary projection.

### Input DTOs

`PreviewInput`: required `group_id` positive int64 and `operation` enum open/renew; optional `quota_usd` string, `plan_id` positive int64, notes length <=500; at least quota or plan must be provided. `OpenInput` adds required `expected_revision >= 0`. `CancelInput` has required `expected_revision >= 0` and reason length 1-500 (whitespace-only is not meaningful).

Use pointer/presence fields for expected_revision and quota_usd: a missing revision differs from valid zero, and omitted quota differs from an explicitly invalid empty string. Gin's `binding:"required"` on a numeric value rejects zero, while default JSON binding accepts unknown fields; neither matches this schema without explicit handling. `additionalProperties:false` forbids user_id/platform/validity_days and arbitrary wallet fields rather than silently trusting them.

Quota lexical input is exactly `^(0|[1-9][0-9]{0,11})(\.[0-9]{1,8})?$` excluding numeric zero. Reject exponents, plus sign, NaN/Infinity, negatives, extra decimals and overflow instead of rounding them into an accepted allowance. Persist/emit exact decimal strings with eight fraction digits. Explicit quota wins over plan default and is recorded as an administrator override. I1 already maps `SubscriptionPlan.TermQuotaUsd` as `*string`, not float (`backend/ent/schema/subscription_plan.go:36`).

### Output DTOs

`Term`: id, group_id, subscription_id, term_no; quota_usd/used_usd/pending_usd/remaining_usd as Money strings; status enum scheduled/active/exhausted/expired/cancelled/reconciling; lifecycle_state enum scheduled/active/closing/closed; starts_at/expires_at; nullable cancelled_at; term revision; unsettled_request_count. All schema fields are required, including nullable cancelled_at.

`Summary`: group_id, quota_mode="term", parent revision, server_time, nullable current_term, nullable last_term and next_terms array. Empty arrays must encode `[]`. `Preview`: group_id, effective_quota_usd, proposed start/expiry, server_time, current_revision, warnings array, nullable current_term and next_terms array. `MutationResult`: term, summary, replayed.

Use new term DTOs instead of filling legacy float fields or replacing group.weekly_limit_usd. Existing user DTO deliberately excludes administrator assignment metadata/notes (`dto/types.go:757-791`, `dto/mappers.go:866-908`); preserve that privacy boundary. Admin-only internal overage is not a user field.

History pages use `{items:[],next_cursor:null,has_more:false}`, default limit20, range1-100. This is a stable cursor contract, not legacy page/page_size pagination. Scope every SQL query by the authenticated/authorized user and group before applying its cursor. A scoped `(term_no,id)` ordering is stable because cancellation does not reorder terms.

## Preview, scheduling and transaction rules

Preview and Open share pure quota resolution/schedule rules. A preview does not reserve time and must not create a parent row or acquire a lasting claim. Preview returns revision0 if no parent exists. New creation recalculates database time after waits; it does not copy the preview timestamp into SQL.

Open conflicts if any uncancelled card has not ended (including a future card). Renew appends at `max(database_time, latest_uncancelled_expiry)`. Every expiry is start +168 absolute hours, intervals are `[starts_at,expires_at)`, no carrying remainder. Cancel leaves the card's quota, usage, start, expiry and future-card schedule unchanged. It increments the parent schedule revision; normal usage does not.

Write order: optional payment order lock only for future payment integration, user/group advisory scope lock, operation idempotency row, parent, sorted term rows, then user/group/Key counter rows if needed. A cancel can first perform a non-locking identity lookup to determine scope, then acquire the scope lock before locking parent/term; do not lock the target term first. Use `clock_timestamp()` after blocking locks.

Successful replay must be resolved before today's revision, expiry, plan availability or default amount is checked. Same scope/key with changed normalized intent returns409. The stored operation result, card, parent summary and durable cache invalidation outbox commit atomically. Return the original result/time on replay (with replayed=true), not a newly synthesized card.

Summary used/pending/remaining comes from one SQL/CTE snapshot. Pending sums only usage_known and never estimates unknown requests; after settlement the cost moves into used and must not also remain pending. For current usability select the term covering database now and not cancelled, never the farthest parent expiry. Parent starts/expires are only a compatibility schedule projection; parent is never soft-deleted and restore never grants term entitlement.

## Current Key-binding bug and all real callers

Current `UserSubscriptionRepository.GetActiveByUserIDAndGroupID` checks only parent status active and expires_at > application time (`backend/internal/repository/user_subscription_repo.go:117-131`). `ListActiveByUserID` has the same parent-only predicate at200-214. A parent spanning a gap or only future cards therefore passes them.

| Actual path | I2 required change |
| --- | --- |
| `service/api_key_service.go:450` `canUserBindGroup` | Term branch uses authoritative current-term existence; windows retains old active-parent check. Used by Create:501 and cross-group Update:825. |
| `api_key_service.go:1034/1071` `GetAvailableGroups` / `canUserBindGroupInternal` | Batch current-term group IDs. Do not put term parent IDs from ListActiveByUserID into the authorization set. Future schedules belong in quota summaries, not this bindable-group set. |
| `api_key_service.go:1096` `GetUserGroupVisibility` | This is a display/visibility function, not generation permission. It currently mixes allowed_groups and active parent subscriptions; add term schedule visibility separately if needed without making it a wallet grant. |
| `service/admin_group.go:1330+` `AdminUpdateAPIKeyGroupID` subscription check at1375-1385 | Admin rebinding also uses the active parent repo; switch its term branch to the same current-term predicate. Do not auto-add a term group to user_allowed_groups. |
| `api_key_service.go:812-831` Update with same group | Existing identity can remain when entitlement expires; a benign name/limit edit does not need to delete/rebind the Key. Final gateway admission still independently checks actual current term. |

The narrow current-term predicate is `term.user_id/group_id match AND cancelled_at IS NULL AND starts_at <= database_now AND expires_at > database_now`, plus valid group mode/platform. This grants Key identity/binding, not spending: the existing windows binding check does not evaluate remaining quota either. Current exhausted/reconciling cards can retain identity while I3 generation separately refuses quota exhaustion/reconciliation. Future-only or gap-only schedules fail current binding/authorization and must never fall back to user_allowed_groups, ordinary balance or GPT.

Recommended I2 tests add future-only, gap after cancelling a future middle card, exact start/expiry boundaries, same user multiple Keys, admin/self-service rebinding and a stale parent expiry far beyond current entitlement. Current-term batched visibility and direct binding must agree; generation remains unavailable in all I2 cases.

## Contract details to settle explicitly during implementation

1. The package says operation fingerprints include resolved quota and a plan snapshot version, while also requiring replay before reading a modified/deleted current plan. `subscription_plans` has no version column. Do not resolve today's default before replay. Either reconstruct comparison from the original term's immutable source_snapshot or store stable normalized intent/default-selection provenance with the operation; keep a versioned source snapshot. Record the exact choice so omitted default vs explicit override and plan edits cannot invalidate an already successful replay.
2. Manual opening from a selected plan is needed before I5 online selling. The source model has `ForSale`, not an independent enabled/version field. Treat sale eligibility and manual quota-default validity as separate concerns; define plan/group match, positive exact default and fixed-duration checks without accidentally requiring a product to be sold in I2.
3. API Open says any unfinished uncancelled future card conflicts. The data-model statement that a newly opened card can start immediately after cancelling the current card is therefore conditional on no other unfinished card remaining. Do not silently implement gap filling in front of an existing future queue; raise a product-contract decision if that behavior is desired.

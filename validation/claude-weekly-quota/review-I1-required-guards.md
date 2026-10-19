# I1 required guards: independent source review

Reviewed source: `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d` (2026-10-19). Line numbers below refer to this baseline, before the I1 changes. This is a static implementation checklist, not a test pass. No production API, DB, model or configuration was accessed.

Read: README, sol-handoff, implement, test-plan, billing-protocol, integration-contracts, migration-rollout, design/API/data-model sections, trellis-check, backend index, carpool contracts and release safety.

## 1. Legacy subscription mutation and maintenance

| Baseline path and function | Real effect / required boundary |
| --- | --- |
| `backend/internal/service/subscription_service.go:221` `assignOrExtendSubscription` | Checks only subscription type at 223-229; then renews existing parent or creates one. Reject term before dispatch into old parent locking/renewal. Covers payment/public redemption, but not admin assignment's separate helper. |
| `subscription_service.go:506` `assignSubscriptionWithReuse` | Separate admin assignment/idempotent reuse path, called by AssignSubscription:203 and BulkAssignSubscription:471. Guard before exists/reuse/create so term cannot be reported as successfully assigned through an old API. |
| `subscription_service.go:290` `updateExistingSubscriptionTerm` | Enters `withSubscriptionUpdateTx` and parent `GetByIDForUpdate` at 297-298; expired renewal resets old counters through `renewedSubscriptionTerm` and Update:326-329. Dispatch term before this legacy transaction; a guard inside the already-held parent lock must never call a new scope-locking term service. |
| `subscription_service.go:600` `RevokeSubscription` | Soft-deletes parent via repo Delete:607. Must reject term; dedicated cancel keeps parent/history. |
| `subscription_service.go:619` `RestoreSubscription` | Includes deleted records then repo Restore:642. Must reject term before any undelete. |
| `subscription_service.go:654` `ExtendSubscription` | Old transaction begins at 655 and locks parent:659, then directly rewrites ExpiresAt:702 and status:708. Positive/negative days both unsupported for term. |
| `subscription_service.go:887` `AdminResetQuota` | Calls ResetUsageWindows:898 to overwrite window usage and anchors. Reject term. |
| `backend/internal/service/subscription_bulk_action.go:68` `BulkSubscriptionAction` | Outer transaction:87 dispatches extend/reset/revoke/restore at 90-100. Guards on single-item methods must still work inside this outer transaction; never upgrade to a term operation while holding legacy locks. Preserve per-item failure behavior. |
| `subscription_service.go:876` `checkAndActivateWindowAt`; `:913` `CheckAndResetWindows`; `:964` `EnsureWindowMaintenance` | Activates original three windows:883; CAS resets daily/weekly/monthly at 920/931/942. All skip/reject term, including a caller-supplied or previously queued stale subscription snapshot. |
| `subscription_service.go:1005` `ValidateAndCheckLimits`; `:1055` `DoWindowMaintenance`; `:1072` `doWindowMaintenance` | In-memory zeroing/needsMaintenance can enqueue later writes. Exclude term before zeroing/enqueue and again at durable write boundary. The queue in `subscription_maintenance_queue.go` is generic; the actual domain behavior is in these service methods. |
| `subscription_service.go:1093` `RecordUsage`; `:1252` `ValidateSubscription` | Direct legacy usage increment and status expiry mutation:1261. Ensure these cannot reinterpret term parents as windows. |
| `backend/internal/repository/user_subscription_repo.go:371/387/403/418/433` | ActivateWindows, ResetUsageWindows, ResetDaily/Weekly/MonthlyUsage are direct mutation sinks. SQL/Ent exclusion protects background/stale callers; a zero affected row due to term exclusion must not be treated as a successful legacy reset by translateConditionalWindowReset:448. |
| `user_subscription_repo.go:471` `IncrementUsage`; `:505` `BatchUpdateExpiredStatus` | Raw increment currently joins groups but has no mode predicate (472-484); batch expiry has only parent status/time predicates (507-513). Exclude term. Parent expires_at is a compatibility projection, not a current-term clock. |
| `backend/internal/service/subscription_expiry_service.go:106/121/191` | runOnce calls batch expiry:110; reminder paging:140 calls sendExpiryReminderIfDue:146. Skip term before sending; reminder currently derives DaysRemaining and source_id from the parent at 195/205. |

Public/admin registration proving reachability: `backend/internal/server/routes/admin.go:704-711` includes assign, bulk-assign, bulk-action, extend, reset, revoke, restore and DELETE alias; handlers live in `backend/internal/handler/admin/subscription_handler.go:138/165/192/209/247/273/291`.

## 2. Refunds and balance restoration

| Baseline path and function | Required protection |
| --- | --- |
| `backend/internal/service/payment_refund.go:208` `PrepareRefund` | Full and partial amounts share this path. Check target/order mode unconditionally before provider/money actions. `deduct=false` bypasses prepDeduct entirely at 249; `force=true` must not bypass a term guard. Do not only guard the full-amount case. |
| `payment_refund.go:257` `prepDeduct` | Subscription refund uses all original SubscriptionDays (261), independent of fraction refunded, then locates an active parent. Do not reinterpret this old arithmetic as term cancellation. |
| `payment_refund.go:298` `ExecuteRefund` | Claims status at 299, deducts ordinary balance:310 or parent days:323, optionally revokes:328, then calls the real provider:343/369. Revalidate mode before status/money mutation so a stale prepared plan or direct internal caller cannot bypass PrepareRefund. |
| `payment_refund.go:421` `QueryAndFinalizeRefund` | Independent pending-refund recovery endpoint; provider query:441, fresh deduction prep:460, finalization:466. Must reject unsupported term even without passing PrepareRefund/ExecuteRefund. |
| `payment_refund.go:475/535` `finalizePendingRefundSuccess` / `applyRefundFinalDeduction` | Own Ent transaction claims order, then invokes old day shortening/revoke at 543-550. Covered by pending entry and sink guard; no new scope-locking operation inside its existing transaction. |
| `payment_refund.go:647/696` `markRefundPending` / `RollbackRefund` | Pending/failure compensation calls RollbackRefund; this can return deducted ordinary balance with UpdateBalance:698 or restore subscription days with ExtendSubscription:705. A term must not be classified as balance or restored through days. Preserve normal balance-order compensation for a user who also owns a Claude card. |
| `payment_refund.go:157/183` `RequestRefund` / `validateRefundRequest` | Public user refund currently allows only balance orders (191-192), so no subscription bypass exists here. Preserve this restriction; do not globally reject normal balance refunds merely because the user has a term subscription. |

Reachability: `backend/internal/server/routes/payment.go:44/91/92`; `backend/internal/handler/admin/payment_handler.go:230/262` calls Prepare/Execute and pending Query separately. Both full and partial success statuses are selected at `payment_refund.go:608-625`.

No subscription-to-wallet refund operation was found in the baseline. The relevant wallet return in this flow is refund rollback at 698, not permission to substitute a cash refund with a balance top-up. Tests must assert zero provider calls and zero status, parent, balance or ledger changes for blocked term full/partial refunds, `deduct=false`, `force=true`, stale execution plans and pending finalization.

## 3. Redemption and paid fulfillment

| Baseline path and function | Required protection |
| --- | --- |
| `backend/internal/service/redeem_service.go:394/402/410/414` | Public redeem, trusted payment and trusted admin all converge on redeem. Guard subscription group mode before transactional Use:482 and again before unsupported mutation if necessary. Rejected term must leave code unused. |
| `redeem_service.go:517-538` | Positive/zero days call AssignOrExtendSubscription (zero defaults to 30); negative days call a separate helper. Both must refuse term. |
| `redeem_service.go:699` `reduceOrCancelSubscription` | Critical bypass: directly calls parent repo UpdateStatus:715, ExtendExpiry:719/725 and UpdateNotes:736, without ExtendSubscription/RevokeSubscription. Guard this path independently. |
| `backend/internal/service/payment_order.go:134` `validateSubOrder` | Checks only group active/type:142-148, then provider invocation in CreateOrder:107. I1 must block selling term through old no-snapshot orders before a payable external order is created. Recheck in createOrderInTx:152 if group mode can change after validation. |
| `backend/internal/service/payment_config_plans.go:135/157` | CreatePlan/UpdatePlan accept days and ForSale without a mode/snapshot check. Prevent exposing unsupported term products for purchase in I1 (at least authoritative checkout rejection); I5 validates/freeze actual term products. |
| `backend/internal/service/payment_fulfillment.go:216/509/540/556` | executeFulfillment dispatches by OrderType; ExecuteSubscriptionFulfillment leases at 526; doSub fetches group:543 then enters ensurePaymentSubscriptionAssigned:547; guard/distribute term before old Ent transaction:561. No-snapshot old order against converted group explicitly fails for handling. Never route it to ExecuteBalanceFulfillment:227. |
| `payment_fulfillment.go:569-616` | Audit/note shortcut can bypass actual assign call:584, then doSub proceeds to affiliate rebate and completed. Mode must be checked before shortcut, not only in assignOrExtendSubscription. |
| `payment_fulfillment.go:39/189/868` | Webhook, duplicate callback and admin RetryFulfillment reach same dispatcher. Guard must protect all three while preserving existing lease/version/idempotency behavior. Paid term recovery in I5 must be independent of new-sale issue_enabled. |

Admin create-and-redeem is separately exposed at `routes/admin.go:564`, handler `admin/redeem_handler.go:168`; public redemption at `routes/user.go:133`. Ordinary positive/negative balance and concurrency redemption are unrelated wallets/limits and retain their existing behavior.

## 4. Auth snapshot and coexistence

| Baseline path and function | Required protection |
| --- | --- |
| `backend/internal/service/api_key_auth_cache_impl.go:17/318/334/443` | Version is 25. Bump it, add mode to both snapshot conversions (group starts at 381/482), and reject old versions from both L1/L2 through applyAuthCacheEntry:328. A version bump alone does not solve same-version stale entries after live group conversion. |
| `backend/internal/service/api_key_auth_cache.go:59`; `backend/internal/repository/api_key_repo.go:172/983` | Snapshot DTO, narrow auth SELECT (group.SubscriptionType at 179), and entity mapper must all carry quota mode. Otherwise a persisted term group materializes as windows. |
| `api_key_repo.go:245` `projectCarpoolBilling` | Existing per-user GPT binding rejects every other group at 261-263. Permit only an actual anthropic/subscription/term relationship; do not project Claude to carpool, mutate a shared Group, or permit ordinary Claude standard groups. |
| `backend/internal/repository/carpool_repository.go:372` `prepareCarpoolUserGroupTx` | GPT open and renew lock every Key:424 and reject any other group:434. Add narrow term coexistence here too. Preserve first-open zero balance/frozen checks:407-420 and binding checks:399-405; do not acquire Claude scope/parent/term locks while these user/Key locks are held. |
| `backend/internal/service/api_key_service.go:450/1034/1071` | Binding and available groups currently use active parent subscription presence. I2 must use actual current term entitlement; future-only parent expiry must not authorize generation, nor user_allowed_groups give fallback wallet permission. I1 generation still refuses term. |

## 5. All gateway route families

The real API-key gateway registration is `backend/internal/server/routes/gateway.go:21`, called by `server/router.go:137`. All below require fail-closed coverage; an HTTP GET is not automatically metadata.

| Real registration / line | Methods and paths |
| --- | --- |
| gateway `/v1` chain:188-197 | Auth at 193, then allowlist/composite; bill-info is registered earlier at 194. |
| 200/209/220/227/239 | POST `/v1/messages`, `/v1/messages/count_tokens`, `/v1/responses`, `/v1/responses/*subpath`, `/v1/chat/completions`. CountTokens forwards real upstream at `gateway_handler.go:2233`; wildcard input-token dispatch also exists at gateway.go:179-180. |
| rootRoute:370-386, 401 | POST `/responses`, `/responses/*subpath`, `/alpha/search`, `/messages/count_tokens`, `/chat/completions`; GET `/responses` is WebSocket generation, GET `/models`, `/models/:model`. No root POST `/messages` is registered in this baseline. |
| codexDirect:387-398 | POST `/backend-api/codex/responses`, wildcard subpath, `/alpha/search`, `/realtime/calls`; GET `/responses` is WS and `/:call_id` is live sideband; GET `/models`. |
| Google chain:344-357 | GET `/v1beta/models`, `/:model`; POST `/v1beta/models/*modelAction` (native generation); uses separate Google auth at 349. |
| Antigravity:491-522 | GET `/antigravity/models`; `/antigravity/v1` Messages, CountTokens, Models, Usage; `/antigravity/v1beta` GET models/detail and POST native model action. ForcePlatform is installed BEFORE auth (499/515), and separate Google auth at 516. Term must not cross platform through these aliases. |
| `/v1` 217-218, 234-237, 246-339 | Live/sideband, Alpha Search, Responses WS, embeddings, images/edits/async, batch-image create/read/cancel/delete, video create/edit/extend/read, TTS/STT/custom voice and Realtime, web/x search. Unsupported term charging routes explicitly refuse. |
| root aliases:373-377, 408-488 | All image/video/audio/search aliases; Seedance tasks at prefixes `/api/v3`, `/v3`, `/v1`, empty, with POST/GET/DELETE. Existing provider/platform checks remain; they are not a term billing implementation. |

Common fail-open sites: `middleware/api_key_auth.go:176` Simple mode returns early, `:202` skips absent SubscriptionService, `:266-269` falls back to balance; Google equivalents are `api_key_auth_google.go:135/169/202`. Term refusal must precede Simple mode and old window checks even if service dependencies are nil. `billing_cache_service.go:736` also returns early in Simple mode then treats missing subscription as balance at 746-759; add independent refusal before both branches.

Auth skipBilling exceptions are exact `/v1/usage`, `/v1/sub2api/billing`, and async-task GET prefixes (`api_key_auth.go:168-172/341-345`). Keep read-only exceptions explicit; do not exempt all GET, count_tokens or arbitrary wildcard subpaths. Metadata may describe expired/unavailable cards but cannot trigger old-maintenance writes or generation.

## 6. Final protected-group cutover gate and settlement drain

Current auth and post-user-wait billing checks use cached Group objects. Messages checks after user wait at `gateway_handler.go:257`, then waits/selects accounts before actual Forward at 479/491 and 896/898. Chat checks at `gateway_handler_chat_completions.go:137`, then account wait and forwards at 278/288/290. Responses checks at `gateway_handler_responses.go:146`, then account wait and forwards at 275/277. A final fresh DB group check must therefore occur after account wait, immediately before actual upstream sends, for configured protected groups even while still standard/windows. Unknown DB state fails closed.

Cross-group fallback is real: `gateway_handler.go:988-1021` clones the Key into another group, checks that group with nil subscription at 1007, clears ForcePlatform and replaces currentSubscription with nil. Shared scheduler fallback is also referenced by Responses at 110-112. A term cannot reach those fallback wallet paths; validate both configuration and final runtime group.

**Critical drain detail:** `gateway_handler.go:2554-2564` submits old standard/windows usage to an asynchronous worker and immediately returns. Only carpool receives special synchronous handling at 2558. HTTP/middleware completion alone is not settlement completion. A protected-group persisted in-flight record must stay unresolved until its old billing task succeeds; known billing failure, client disconnect with ambiguous usage, process crash, or dropped task must not be silently removed by defer or TTL. A zero aggregate must count all instances and unresolved billing work. Pending/reconciliation records must survive restart and block cutover until resolved with evidence.

Old billing sinks explain why this matters: `service/gateway_usage_billing.go:138/294/358/414` has legacy multi-write fallback (387), subscription or balance selection (337-341), and old subscription/balance cache writes (423/490). `repository/usage_billing_repo.go:211-224` directly increments old windows or deducts users.balance. I1 protects entry/drain without rewriting old three-source fingerprint or amount semantics; I3 adds the dedicated term billing branch and no-fallback receipt enforcement.

## Review status

This source review identified required integration points only. No business test, PostgreSQL execution, migration replay, image build, real route execution or browser verification was performed by this review. Candidate I1 diff remains to be reviewed separately.

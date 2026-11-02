# I4 interface and UI preparation (read-only)

Prepared by sol_contract_review while I3 owner source is frozen. No I4 implementation or acceptance is claimed. Basis: actual source plus ui-contracts.md, api-contracts.md, contracts/openapi.json and implement.md I4; I5 payment work is a separate phase.

## Existing ports and missing projections

| Consumer | Actual existing source | I4 boundary |
| --- | --- | --- |
| Manual preview/open/cancel | handler/admin/subscription_quota_handler.go; server/routes/admin.go:702 | I2 real routes already exist. Use their strict DTO, path user ID, authenticated actor, Idempotency-Key and expected parent revision. Preview omits expected_revision. |
| User summaries/history/usage | handler/subscription_quota_handler.go; server/routes/user.go:138; dto/subscription_quota.go | Already return server-projected strings, server_time, current/next/last, pending and request counts. User term usage verifies ownership; never add a frontend user_id parameter. |
| Existing subscription list/active/progress/summary | handler/subscription_handler.go:47/69/91/124; service/subscription_service.go:804/815/1157 | Current responses still use parent windows and group daily/weekly/monthly limits. Backend must add quota_mode and quota_summary from the same domain reader; windows-only calls must not acquire a new term repository dependency. Future parent expiry alone cannot imply a current card. |
| Existing subscription DTO/admin list | handler/dto/types.go:757; mappers.go:889; handler/admin/subscription_handler.go | DTO currently has no quota_mode/quota_summary. Mapping must receive an already-loaded summary; a pure mapper must not perform SQL. Admin projection must keep notes separate from public term DTO. |
| API Key usage metadata | handler/gateway_handler.go:1603, quota-limited helper:1717, unrestricted:1810; middleware/subscription_quota_guard.go | I3 deliberately blocks this term route. I4 needs a term branch before generic key-limit/unrestricted routing, plus metadata allowlist/auth wiring. Scope comes from authenticated Key/user/group. No ordinary balance, parent windows or admission/ledger write. |
| Abnormal request query | OpenAPI GET /admin/subscription-quota/requests | Not implemented in the inspected I3 service/repository/route. Add stable cursor/limit 1..100 (default 20), optional enum status/user/group filters, and an administrator-only sanitized RequestPage. Request projection excludes raw pricing/command/log payloads. |
| Audited resolve | service/subscription_quota_resolution{,_port}.go; domain/subscription_quota_resolution.go; repository/subscription_quota_resolution.go | Domain/service/repository exist, HTTP/wire do not. Strict HTTP DTO must require pointer expected_request_revision and the five verified token counters (zero is valid, absent/null is invalid). Use path row ID plus authenticated actor. No arbitrary cost or JSON command. Accepted/replayed result is not proof of settlement. |

The OpenAPI Request projection has status/revision/cost/timestamps/category and identity IDs, but no evidence/actor read-detail endpoint. The UI can collect and submit evidence/reason and show the returned accepted request plus subsequent list status. Adding a separate audit-history endpoint or exposing resolution_evidence would be a contract change, not an assumed frontend field.

## One allowance source

Proposed additive backend representation: quota_mode = windows|term and quota_summary = SubscriptionQuotaSummary|null. Normalize a legacy missing mode to windows at the API boundary. After normalization, use a discriminated frontend union. A term with missing/unavailable summary displays unavailable; it never falls back to group limits or wallet balance.

All Claude card amounts and statuses come from quota_summary.current_term (or explicit next_terms/last_term state). Display pending separately from used; unknown request count is not a zero-cost estimate. One user/group is one card even with several Keys. Store term summaries by group identity, not Key identity. The existing backend ListSummaries enumerates actual owned terms, so an empty array means no card, not a synthetic free allowance.

Preserve decimal strings in form payloads and responses. A new dedicated term money utility should format validated strings and derive a bounded display-only ratio; never use the existing parseCarpoolAmount invalid-to-zero fallback as financial validation. No client calculation creates a new balance. Existing utils/subscriptionQuota.ts is a windows expiration helper, so avoid overwriting it with a new domain implementation under the same name.

For /v1/usage, add billing_scope=claude_subscription, quota_mode=term and the canonical quota_summary even when mode=quota_limited. Preserve the existing quota/rate_limits as explicitly Key-scoped constraints. The card remainder must not be replaced by Key remaining, nor summed with it. The exact new top-level compatibility fields should be frozen by backend owner before frontend implementation; current KeyUsageView uses untyped resultData and number-only usd(), so accepting a term response without a specific discriminant would render misleading windows/balance rows.

## Proposed frontend files and ownership

- Dedicated types/subscriptionQuota.ts, api/subscriptionQuota.ts, api/admin/subscriptionQuota.ts, admin API barrel; additive Group.subscription_quota_mode and subscription union types in types/index.ts.
- Extend stores/subscriptions.ts for canonical term summaries plus generation and authenticated identity protection. Existing store prevents stale state writes but still returns stale responses/errors to callers; clock sync, errors, toasts and boundary effects must also check current result identity.
- New reusable Claude term card and per-term usage/history component; integrate views/user/SubscriptionsView.vue and common/SubscriptionProgressMini.vue. Preserve the original windows component branches and GPT carpool pages.
- New admin SubscriptionQuotaTermModal.vue (near components/admin/user, consistent with existing CarpoolTermModal.vue); open from UsersView and/or SubscriptionsView, without routing term rows into old assign/extend/reset/restore dialogs. No bulk UI is required.
- New administrator request list/resolve components within subscription administration. Query only sanitized RequestPage; no hardcoded successful settlement after HTTP 202.
- KeyUsageView.vue needs a term-specific card and independent Key-limit section. Chinese/English keys belong in matching locale namespaces; translate known status enums and preserve an unknown enum fallback and free-form evidence/reason text.

Proposed parallel split after I3 acceptance: frontend files above owned by this agent; backend projections, request read port/repository, resolve HTTP/wire, middleware and /v1/usage owned by main developer, subject to explicit coordination. No I4 files have been claimed or edited yet.

## Asynchronous identity contract

Capture modal generation, authenticated identity, target user/group/term, complete payload fingerprint and operation key before each await. Guard success, catch and finally against all applicable captured identities. A close/open or target/payload edit invalidates old preview/submission effects, including spinners and errors. Cancellation is an optimization, not authorization to apply a stale response.

Preview does not reserve a term. Copy the displayed quota/payload for submission; a payload or revision change requires a fresh key and preview, whereas an exact network retry reuses its original key. Do not emit JSON null for optional nonnullable fields. A QUOTA_REVISION_CONFLICT refreshes the canonical summary and invalidates preview while retaining the operator's entered quota. A replay displays the operation's original actual interval, then separately fetches the current summary.

useServerClock uses performance.now after server_time synchronization. Sync only a current response. At starts/expires boundaries, trigger a guarded refetch and leave displayed used/pending/remainder unchanged while the request is unresolved. Do not mint the next card or reset usage locally. Logout/user switch clears store data, timers and outstanding response identities. For verified usage numeric inputs, reject non-safe integers in the browser rather than silently rounding int64 evidence through JavaScript Number.

## Required evidence before I4 acceptance

1. Actual service-backed HTTP tests for legacy subscription projections, /v1/usage with/without Key limits, no current/expired/exhausted/reconciling cards, no financial writes, cross-user isolation, and strict resolve/list DTOs. Pure GPT requests assert no term repo calls.
2. Real Pinia/component tests force A close/B open with late preview/success/error/finally; a newer failure before an older failure; payload changes and exact retry keys; revision conflict preserving quota input; fake performance and wall clock at a held boundary fetch; multi-Key single-card data, zero/pending/unknown distinctions and both locales.
3. Frontend typecheck, lint:check, test:run and build. Existing frontend specs require all four, not only selected new tests.
4. Exact candidate-image browser evidence at desktop and narrow width, keyboard labels/focus/errors, two users with different quotas, multiple Keys and simultaneous unchanged GPT card behavior. Vitest alone does not accept the UI. No production or live-provider calls are authorized for this preparation.

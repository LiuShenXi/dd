# I0 — Production baseline and implementation map

Date: 2026-10-19 (Asia/Shanghai). This receipt records development preparation; no production mutation, model call, card issuance, migration, or deployment was performed.

## Source and evidence

- Workspace: `C:/Users/Administrator/.codex/worktrees/claude-weekly-quota/dd`.
- Branch: `codex/claude-weekly-quota`.
- Starting revision: `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`.
- The coordinator's new read-only receipt is `production-baseline-20261019.json`. Production remains `sub2api:2.8.13-monas-bps.13`, image `sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677`, binary SHA-256 `85d991720e15993813fdfdddc408852fefe90c54314e760d68765cf5ba0767cd`, PostgreSQL 18.
- `python .trellis/tasks/10-19-claude-weekly-quota-architecture/tools/verify_baseline.py --source .` passed: matching HEAD, all 47 source anchors, all 297 original migration checksums exact, no findings. Its separate `evidence/local-baseline.recheck.json` preserves the original snapshots.
- The copied architecture package is retained unchanged except for that new recheck. Original snapshots retain their original dates.

## Change boundary

The behavior gap is that the existing parent subscription cannot express immutable user-specific 168-hour allowances or delayed charges to an original card, and the existing GPT binding rejects every other group. The authoritative behavior lives in group/auth projection, final gateway admission, SQL accounting and subscription/payment write paths.

I1 adds the mode and additive data structure, invalidates old auth snapshots, admits only the explicit Claude term exception to GPT Key coexistence, rejects all new term generation and legacy mutation paths, and adds a protected-group final database check with in-flight tracking while the group is still standard. It does not issue cards or enable generation. The original GPT receipt v1, billing fingerprints, quantization, balances, reset rules, and all 297 migrations remain unchanged. No broad wallet, gateway or UI refactor is planned.

## Applicable development guidance

Read `trellis-before-dev/SKILL.md`, the backend/frontend indexes, backend `carpool-contracts.md` and `release-safety.md`, frontend `carpool-state-contracts.md`, both shared thinking guides, and the actual backend guideline files. The general backend guideline files are placeholders; they do not invent additional conventions. `get_context.py` and `task.py` do not exist, so no Trellis installation or task registration was attempted. `carpool-state-contracts.md` exists under `.trellis/spec/frontend/` and is tracked in this source revision. Historical `docs/OPERATIONS.md` describes older BWH deployment; it is not an executable plan for current `main-server/sub2api2`.

## Real source map

| Contract | Actual owner and required change |
| --- | --- |
| Group mode | `ent/schema/group.go`; service `group.go`; repository `group_repo.go`, `api_key_repo.go` selected field list and entity projection; admin types in `admin_service.go`, validation/mutation in `admin_group.go`, HTTP DTO in `handler/admin/group_handler.go`, shared output DTOs. Default empty legacy mode to windows, validate term only for anthropic subscription with no fallback, and protect downgrade after terms exist. |
| GPT coexistence | `apiKeyRepository.projectCarpoolBilling` and `prepareCarpoolUserGroupTx`. Permit only an identity-consistent anthropic/subscription/term Key, regardless of current card validity. Keep original zero ordinary/frozen balance, binding, platform and other-Key restrictions. GPT operations never acquire a Claude scope/parent/term/request lock. |
| Auth and caches | `api_key_auth.go`, `api_key_auth_google.go`, `api_key_auth_cache.go`, `api_key_auth_cache_impl.go` (starting snapshot version 25), `billing_cache_service.go`. Preserve term identity when dependencies are absent; reject before simple-mode or balance fallback. Both L1 and L2 reject old snapshot versions. |
| Protected group transition | The final pre-forward points after user/account waits in Messages, Chat and Responses are required, even for configured protected groups still in standard mode. A database state check plus durable scoped in-flight admission must serialize with maintenance/transition. A middleware-only or per-process counter cannot prove multi-instance drain. |
| Legacy subscription writes | `subscription_service.go`: assign/bulk/renew, revoke, restore, extend, reset and window maintenance. Dispatch before parent `FOR UPDATE`. `user_subscription_repo.go`, `subscription_expiry_service.go` and maintenance queue must exclude term from legacy window/status/email work. |
| Unified charge | `gateway_usage_billing.go`, `usage_billing.go`, `usage_billing_repo.go`. New branch and fingerprint version only; old branch byte order and carpool receipt codec remain intact. Term snapshot itself selects source, including zero cost. |
| Durable recovery | New request/receipt/ledger repository and worker, with provider in `internal/service/wire.go`, repository provider set, generated `cmd/server/wire_gen.go` and cleanup signature/tests in `cmd/server/wire.go`. Claim leases in a separate short transaction before scope lock. |
| Statistics | `usage_log_repo_insert.go` writes statistics after settlement; `usage_cleanup_repo.go` must preserve unconfirmed term writes. `usage_log_recorded_at` prevents replaying logs removed later by retention. |
| Cache invalidation | Reuse `auth_cache_invalidation_outbox` and `AuthCacheInvalidationWorker`, not synchronous-only cache clearing. Existing outbox stores hashed credential cache keys; domain transactions may enqueue the same SQL projection. No raw credentials enter new tables or evidence. |
| Payment | Create frozen snapshot in `payment_order.go`. Dispatch before `ensurePaymentSubscriptionAssigned` in `payment_fulfillment.go`; the new SQL transaction locks order and verifies lease version, then scope/parent/term and existing assignment audit. Completion/distribution retain the existing recoverable state machine. |
| Refund/redeem | Guard every term refund entry before an external payment action, including `PrepareRefund` with `deduct=false` and independent `QueryAndFinalizeRefund`; do not rely only on later Extend/Revoke. Redeem must reject before applying an old duration grant. |
| SQL migration | Starting highest numeric migration is 244. Allocate fresh 245+ files. Split shared-column addition, NOT VALID FKs, later validation and concurrent indexes with existing `_notx.sql` rules; bounded lock/statement timeouts. No edits to original 297 files. |
| User/admin UI | Existing subscription types/store/API, `views/admin/SubscriptionsView.vue`, `views/user/SubscriptionsView.vue`, `SubscriptionProgressMini.vue`, product and order UI. New discriminated mode and decimal strings; use existing modal generation/identity guards in success, catch and finally. |

## Actual gateway matrix

Source: `backend/internal/server/routes/gateway.go` and the three existing Claude handlers. Router aliases must be tested through `RegisterGatewayRoutes`.

| Paths | Existing dispatch | Term behavior |
| --- | --- | --- |
| `POST /v1/messages` | `GatewayHandler.Messages` for Anthropic | I1 reject; I3 final admission before each actual upstream send. |
| `POST /v1/chat/completions`, `/chat/completions` | `GatewayHandler.ChatCompletions` | Same snapshot and durable billing for SSE and non-SSE. |
| `POST /v1/responses`, `/responses`, `/backend-api/codex/responses` and each `/*subpath` | `GatewayHandler.Responses`; subpath guard first | I3 allows only adapted generation subpaths; metadata/input counts do not grant generation rights. |
| `GET /v1/models`, `/models`, model detail, codex model manifest; usage/billing; count_tokens | Existing metadata/count adapters | Preserve identity/auth; explicit term query projection, no ordinary-balance substitution. |
| `GET .../responses`, live/realtime/alpha/search, images/async/batch, embeddings, videos, voice/search, Seedance `/contents/generations/tasks` aliases | Other existing handlers | Explicit unsupported for term; original GPT/vendor behavior remains unchanged. |
| `/v1beta/models/*modelAction`, `/antigravity/v1/messages`, `/antigravity/v1beta/...` | Google auth and/or ForcePlatform | Term rejects cross-platform dispatch, including simple mode. |

The actual Anthropic forward branches are OAuth/setup-token, API Key passthrough and Bedrock. **Architecture mapping correction:** this production source has no Kiro forwarding implementation. `PlatformKiro` is retained only for legacy metadata and unsupported-platform threshold tests (`domain_constants.go`). No Kiro implementation will be added; the original architecture wording is preserved and this difference prevents a false coverage claim.

## Pricing snapshot feasibility and risks

`ModelPricingResolver.Resolve` owns Group → Channel → LiteLLM/fallback resolution. `ResolvedPricing` includes private `channelPricing` and `longContextPricingEnabled`; serializing only exported fields is incomplete. I3 needs an explicit versioned DTO containing resolved base/interval/request-tier prices, exact flags, time schedule and rule provenance. `BillingService.CalculateCostUnified` accepts a pre-resolved object and reuses `computeTokenBreakdown`, cache breakdown and long-context logic; freeze and restore the complete representation rather than copying a model-price table into the quota domain.

`gateway_usage_billing.go` mutates usage for force-cache and account/global TTL overrides, resolves user/group and peak multipliers, picks requested/mapped/concrete candidates and optionally adopts a returned model. Freeze those classification/selection rules and candidate pricing before upstream; record final account counters separately. A retry that changes user pricing cannot silently retain the old semantics. Errors currently mapped to zero in the old helper need a term-only error-returning path. Existing `resolveUsageBillingRequestID` can honor a client ID, so term uses its own `subquota:UUID` throughout. Four-source validation must reject negative/nonfinite/mixed new inputs while leaving old fingerprint behavior intact.

## R0 validation results

Toolchain: `C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe` (Go 1.27.0). The system PATH Go 1.21.1 attempt was stopped before results and is not counted.

From `backend/` (using the pinned executable):

1. `go test -tags=unit -run '^$' ./...` — PASS, all packages compile; this is not business execution (`I0/go-unit-compile.log`).
2. `go test -tags=unit -run 'Carpool|UsageBilling|SubscriptionAppliesRateMultiplier|APIKeyAuthSnapshotPreservesPerUserBillingIsolation' ./internal/service ./internal/handler/... ./internal/server/... ./internal/repository` — PASS, exit 0 (`I0/gpt-unit-baseline.log`). The same command with `-json` produced `I0/gpt-unit-baseline.jsonl`: 84 top-level tests passed (service 51, handler 14, handler/admin 3, handler/dto 1, repository 15), zero skipped tests. Server/middleware/routes and quotaview had no matching tests in this focused baseline; their compile success is not business coverage.
3. `go test -tags=integration -v -run '^(TestCarpoolBillingBindingPreservesGroupAndAdminThroughAuth|TestCarpoolStandardGroupOpenRenewAndExistingKeyAuth|TestCarpoolStandardOpeningRejectsConflictsWithoutMutation)$' ./internal/repository` — PASS, exit 0, all three named top-level business tests executed, including eight conflict subtests (`I0/gpt-pg-baseline.log`). The original testcontainers harness created fresh synthetic PostgreSQL/Redis, applied all migrations and ran assertions. Process-only settings were `DOCKER_HOST=npipe:////./pipe/docker_engine`, `CI=true`, `SUB2API_TEST_POSTGRES_IMAGE=postgres:18.1-alpine3.23`; CI prevents unavailable Docker from being accepted as skipped success.

These commands do not establish full-suite, final-image, browser, race or performance acceptance. Those remain stage-specific requirements. No production DSN or real user key was supplied.

## I0 exit

Baseline and workspace are verified, existing GPT regressions are repeatable, the required code owners and final-forward boundaries are mapped, and the pricing/Kiro mapping differences are explicit. I1 may proceed; I2 waits for coordinator review of I1.

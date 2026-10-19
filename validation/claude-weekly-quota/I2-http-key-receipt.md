# I2 HTTP and current-card Key entitlement receipt

Owner: `sol_contract_review`. Based on accepted I1 commit `92bd96e6feaa4426bacb2312eed8e2d162ba1bc7`. No commit, production API/DB/model call, or Codex configuration change was made by this owner.

## Implemented boundary

- Added dedicated admin/user quota handlers, public DTO mappers, shared strict request decoding, handler aggregates/providers, and actual `RegisterAdminRoutes` / `RegisterUserRoutes` registration for all nine I2 endpoints (including own term usage). Existing `/admin/users/:id` parameter naming is reused to avoid Gin wildcard conflicts; the target user still comes only from that path. The normal auth, audit, backend/compliance and rate-limit chains remain in place.
- Admin identity is checked before target parsing/service access. User reads always use `AuthSubject.UserID`; payload identity fields are rejected and query `user_id` cannot select another user. Mutation revision uses pointers, accepts zero, and rejects absence/null/negative/non-integer values. Preview excludes revision and does not require an idempotency key. New issuance returns 201 and successful replay 200.
- Strict JSON accepts one object, exact field names and schema types. Unknown/duplicate fields, every explicit null, trailing JSON, and oversized bodies are rejected. Monetary validation/normalization is delegated to the single domain/service implementation. Optional quota/plan omission remains distinguishable from invalid empty/null input. Limit defaults to 20 only when absent; explicit empty/zero/out-of-range/duplicate limits return 400. Cursor interpretation stays in the shared service/repository codec.
- Public DTOs explicitly map the 15 contractual card fields and omit user identity, internal overage/reconciliation details, administrator metadata and source snapshots. Nullable fields remain present; arrays encode `[]`.
- Key Create and cross-group Update, AvailableGroups, GroupVisibility, and administrator rebinding use the authoritative current-term reader for term groups. Future-only/gap schedules and `allowed_groups` never grant term binding/visibility. Exhausted current cards retain Key-management permission; retained same-group identity can still receive benign edits. No permanent group access is auto-granted for term binding. Missing readers and reader errors fail closed.
- Existing windows parent checks and GPT carpool assignment/display rules remain intact. Availability queries invoke the new repository only for this user's loaded term subscription candidates; visibility invokes it only for this user's term candidates. A pure GPT user makes zero new-domain repository calls even when the site has term groups and that repository is failing.

Main developer owns domain/port/service/repository/outbox, service/repository Wire providers, the AdminService field addition, and generated Wire output. This receipt does not claim their transaction tests as this owner's work. I3 generation/admission/billing/recovery and successful resolve endpoints were not implemented here.

## Executed evidence

Windows Go 1.27.0 toolchain, backend working directory:

```text
go test -json -tags=unit -run '^TestSubscriptionQuotaRoutes|^TestSubscriptionQuotaDTO|^TestSubscriptionQuotaKey|^TestGetUserGroupVisibility|^TestAdminService_AdminUpdateAPIKeyGroupID|^TestAPIKey(Create|Update)' -count=1 ./internal/service ./internal/handler/dto ./internal/server/routes
```

Result: exit 0; 40 top-level tests passed, 0 failed, 0 skipped. Package times: service 3.340s, DTO 1.995s, real routes 2.838s. Log: `I2-http-key-windows-unit.jsonl`. Includes the 13 new top-level Router/DTO/Key tests plus existing Key update, administrator rebinding and visibility regression tests. The HTTP chain uses the real service and explicitly checks `QUOTA_REQUIRED` / `INVALID_QUOTA` reasons before any repository call. `gofmt` was applied to owner Go files and `git diff --check` passed.

The Router tests register the actual production admin/user route functions and use the actual quota service/validation with an in-memory repository and controlled auth middleware. They prove routing, role/identity boundaries, DTOs and input/status behavior. They do not prove SQL concurrency, durable operations, outbox delivery or PostgreSQL ownership enforcement. Those require the independent real PostgreSQL T01–T12/T24 suite. Linux race/build evidence is owned by the environment agent and is not claimed in this local receipt.

Owner source hashes and the exact test log hash are recorded in `I2-http-key-source-hashes.json`. Owner implementation is ready for main-agent review and frozen pending a specific review fix.

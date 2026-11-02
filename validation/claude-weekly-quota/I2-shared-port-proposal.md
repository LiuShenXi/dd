# I2 shared Go boundary proposal

This is an interface agreement for the next iteration, not I2 implementation or permission to issue production cards. It follows the existing architecture package. I1 source remains frozen while its final checks run.

## Handler-facing service

Types below live in `internal/service/subscription_quota_port.go`; immutable projections and repository parameters live in `internal/domain/subscription_quota.go`. Existing eight-decimal normalization is reused; HTTP money remains a string.

```go
type SubscriptionQuotaOpenInput struct {
    GroupID int64
    Operation string           // open | renew
    QuotaUSD *string           // nil: copy selected plan; pointer to empty: invalid
    PlanID *int64
    ExpectedRevision *int64    // presence required on mutation; zero is valid
    Notes string
}
type SubscriptionQuotaCancelInput struct {
    ExpectedRevision *int64
    Reason string
}
type SubscriptionQuotaPageInput struct {
    Cursor string
    Limit int                  // default 20; 1..100
}

func NewSubscriptionQuotaService(repo SubscriptionQuotaRepository, cfg *config.Config) *SubscriptionQuotaService
func (s *SubscriptionQuotaService) Preview(ctx context.Context, userID, actorID int64, in SubscriptionQuotaOpenInput) (*domain.SubscriptionQuotaPreview, error)
func (s *SubscriptionQuotaService) OpenTerm(ctx context.Context, userID, actorID int64, in SubscriptionQuotaOpenInput, idempotencyKey string) (*domain.SubscriptionQuotaMutationResult, error)
func (s *SubscriptionQuotaService) CancelTerm(ctx context.Context, termID, actorID int64, in SubscriptionQuotaCancelInput, idempotencyKey string) (*domain.SubscriptionQuotaMutationResult, error)
func (s *SubscriptionQuotaService) GetSummary(ctx context.Context, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error)
func (s *SubscriptionQuotaService) ListSummaries(ctx context.Context, userID int64) ([]domain.SubscriptionQuotaSummary, error)
func (s *SubscriptionQuotaService) ListTerms(ctx context.Context, userID, groupID int64, page SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaTermPage, error)
func (s *SubscriptionQuotaService) ListUsage(ctx context.Context, userID, termID int64, page SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaUsagePage, error)
```

Handler authentication owns actor/target selection. Ordinary-user routes always pass the authenticated user ID and never a body/query user ID. Term ownership failures return the same 404 as absence. Admin handlers verify the administrator role before loading the target. Strict request decoding rejects unknown JSON fields; the default Gin binder is insufficient for the contract's `additionalProperties:false`.

Preview is read-only, does not create a parent, and ignores `ExpectedRevision` (not part of its HTTP input). Mutation requires its presence. Handler retries retain one key per exact payload and report 201 for a new open/renew result, 200 for replay. Cancellation retains original starts/expires timestamps. Server-owned projections return fixed-eight-decimal strings and nonnil arrays.

## Current-card entitlement read port

```go
type SubscriptionQuotaEntitlementReader interface {
    HasCurrentTerm(ctx context.Context, userID, groupID int64) (bool, error)
    ListCurrentTermGroupIDs(ctx context.Context, userID int64) ([]int64, error)
}
```

The repository implements this through one shared SQL predicate: matching user/group/parent identity, anthropic/subscription/term group, noncancelled term, `starts_at <= clock_timestamp() AND clock_timestamp() < expires_at`, and active user/group identity. This is permission to bind/view that group; final I3 admission separately checks remaining quota, pending/reconciliation and Key status. An exhausted current card may retain Key-management visibility. A future-only schedule or an actual gap is not a current entitlement. Database errors propagate; an absent reader cannot silently authorize term groups using parent expiry.

The HTTP/Key owner will connect this reader to `canUserBindGroup`, `GetAvailableGroups`, `GetUserGroupVisibility`, and the admin Key-group update path. Keep GPT binding and ordinary group policies unchanged. Constructors retain existing public compatibility via setters/providers where appropriate; the actual startup provider must inject the nonnil read port before term behavior is enabled.

## Repository operations and transaction ownership

```go
type SubscriptionQuotaRepository interface {
    SubscriptionQuotaEntitlementReader
    PreviewTerm(context.Context, domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaPreview, error)
    OpenTerm(context.Context, domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaMutationResult, error)
    CancelTerm(context.Context, domain.SubscriptionQuotaCancelParams) (*domain.SubscriptionQuotaMutationResult, error)
    GetSummary(context.Context, int64, int64) (*domain.SubscriptionQuotaSummary, error)
    ListSummaries(context.Context, int64) ([]domain.SubscriptionQuotaSummary, error)
    ListTerms(context.Context, int64, int64, domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaTermPage, error)
    ListUsage(context.Context, int64, int64, domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaUsagePage, error)
}
```

The SQL repository owns scope lock -> operation replay -> parent -> ordered terms -> late entity checks, a refreshed database clock, parent revision and auth-invalidation outbox. Service owns validation and feature-gate policy without performing an Ent transaction. I5 later reuses an internal `openTermTx` after acquiring/verifying the payment order lock; it must not wrap this public SQL transaction inside an existing Ent transaction.

Final exported struct field names will match the contract JSON names through explicit tags. Pagination uses stable numeric-ID cursors encoded by a shared codec, validates group/user ownership, and returns empty arrays. No token, prompt, Authorization, upstream payload or raw Key belongs in operations or snapshots.

## Agreed interpretation requiring dedicated tests

1. Store normalized intent together with the original resolved plan snapshot. A successful replay compares against that original snapshot, before checking today's plan/default/schedule/revision. Plan edits/deletion cannot invalidate a successful original replay; changed explicit amount/plan/intent conflicts.
2. A manually selected plan must match the group, have a positive default allowance and fixed 168-hour duration. `ForSale` is not required for an administrator's manual operation. Sales eligibility remains I5.
3. `open` conflicts with any unfinished, uncancelled current or future card. Cancelling a current card permits an immediate new `open` only if no later uncancelled card remains. Renew always appends after the last uncancelled scheduled expiry; it never moves a future card or carries unused quota.
4. `quota_revision` changes only on schedule/rights mutations, not ordinary usage. A successful replay wins before a now-stale `ExpectedRevision`. Simultaneous independent renewals serialize; the second stale preview may return conflict and can be explicitly retried with a refreshed revision/new key.

I2 acceptance remains T01–T12 and T24, with real PostgreSQL, actual admin/user router tests and the current-card Key binding cases. Generation remains disabled.

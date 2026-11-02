package service

import (
	"context"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type UserConcurrencyScope string

const (
	UserConcurrencyLegacy             UserConcurrencyScope = "legacy_user"
	UserConcurrencyClaudeSubscription UserConcurrencyScope = "claude_subscription"
)

type UserScopeMemberKind string

const (
	UserScopeActive  UserScopeMemberKind = "active"
	UserScopeWaiting UserScopeMemberKind = "waiting"
)

type UserScopeStatus string

const (
	UserScopeAcquired    UserScopeStatus = "acquired"
	UserScopeFull        UserScopeStatus = "full"
	UserScopeMissing     UserScopeStatus = "missing"
	UserScopeUnavailable UserScopeStatus = "unavailable"
)

type UserScopeOperation struct {
	Scope    UserConcurrencyScope
	UserID   int64
	MemberID string // server UUID: lease ID for active, ticket ID for waiting
	TicketID string // original waiting ticket for Promote
	Kind     UserScopeMemberKind
	Limit    int
	LeaseTTL time.Duration
}

type UserScopeResult struct {
	Status     UserScopeStatus
	Active     int
	Waiting    int
	ServerTime time.Time
	ExpiresAt  time.Time
}

type UserScopeStats struct {
	Active     int
	Waiting    int
	ServerTime time.Time
}

// Separate from the legacy ConcurrencyCache interface: existing GPT callers,
// mocks, keys and error policy remain unchanged. New methods accept only the
// Claude scope; legacy callers continue through the existing adapters.
type UserScopedConcurrencyCache interface {
	TryAcquireScope(context.Context, UserScopeOperation) (UserScopeResult, error)
	TryEnqueueScope(context.Context, UserScopeOperation) (UserScopeResult, error)
	PromoteScope(context.Context, UserScopeOperation) (UserScopeResult, error)
	RenewScope(context.Context, UserScopeOperation) (UserScopeResult, error)
	ReleaseScope(context.Context, UserScopeOperation) (UserScopeResult, error)
	GetScopeStats(context.Context, UserConcurrencyScope, int64) (UserScopeStats, error)
}

// Uses the legacy keys/count semantics through a separate bounded client.
type BoundedLegacyUserConcurrencyReader interface {
	GetLegacyUserConcurrencyBounded(context.Context, int64) (int, error)
}

const MaxClaudeSubscriptionConcurrency = 2147483647

func ValidClaudeSubscriptionConcurrency(value int) bool {
	return value > 0 && value <= MaxClaudeSubscriptionConcurrency
}

// Holds a user policy read lock only around one bounded Redis operation, never
// around queue waiting, account selection or database billing admission.
type ClaudeConcurrencyPolicyRepository interface {
	WithClaudeConcurrencyLimit(context.Context, int64, int, func(context.Context, int) error) error
}

var (
	ErrInvalidClaudeConcurrency     = infraerrors.BadRequest("INVALID_CLAUDE_SUBSCRIPTION_CONCURRENCY", "Claude concurrency must be a positive integer")
	ErrTermConcurrencyNotConfigured = infraerrors.ServiceUnavailable("TERM_CONCURRENCY_NOT_CONFIGURED", "Claude subscription concurrency is not configured")
	ErrTermConcurrencyUnavailable   = infraerrors.ServiceUnavailable("TERM_CONCURRENCY_UNAVAILABLE", "Claude subscription concurrency is unavailable")
	ErrTermConcurrencyQueueFull     = infraerrors.TooManyRequests("TERM_CONCURRENCY_QUEUE_FULL", "Claude subscription concurrency queue is full")
	ErrTermConcurrencyWaitTimeout   = infraerrors.TooManyRequests("TERM_CONCURRENCY_WAIT_TIMEOUT", "Claude subscription concurrency wait timed out")
)

type UserScopeLease struct {
	ctx     context.Context
	release func()
	once    sync.Once
}

func NewUserScopeLease(ctx context.Context, release func()) *UserScopeLease {
	return &UserScopeLease{ctx: ctx, release: release}
}

func (l *UserScopeLease) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}
func (l *UserScopeLease) Release() {
	if l != nil {
		l.once.Do(func() {
			if l.release != nil {
				l.release()
			}
		})
	}
}

// onWaiting emits the caller's existing protocol-specific streaming ping. Any
// ping error cancels waiting and reclaims only this request's ticket/lease.
type ClaudeSubscriptionConcurrency interface {
	AcquireClaudeSubscriptionLease(context.Context, int64, time.Duration, func() error) (*UserScopeLease, error)
}

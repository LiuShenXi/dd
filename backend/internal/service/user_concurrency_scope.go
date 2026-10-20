package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const (
	ClaudeConcurrencyLeaseTTL         = 60 * time.Second
	ClaudeConcurrencyHeartbeat        = 15 * time.Second
	ClaudeConcurrencyOperationTimeout = 2 * time.Second
	claudeConcurrencySafetyMargin     = 5 * time.Second
)

func (s *ConcurrencyService) ConfigureClaudeSubscriptionConcurrency(policy ClaudeConcurrencyPolicyRepository, defaultLimit int) {
	s.claudePolicy = policy
	s.claudeDefault = defaultLimit
}

func (s *ConcurrencyService) CloseClaudeConcurrencyCache() error {
	if s != nil {
		if closer, ok := s.cache.(interface{ CloseScopeCache() error }); ok {
			return closer.CloseScopeCache()
		}
	}
	return nil
}

type userScopeKeeper struct {
	cache  UserScopedConcurrencyCache
	op     UserScopeOperation
	ctx    context.Context
	cancel context.CancelCauseFunc
	stop   chan struct{}
	done   chan struct{}
}

func userScopeRemaining(result UserScopeResult, started time.Time) time.Duration {
	// Redis and host wall clocks may differ. Only Redis's remaining duration
	// is transferred to a local monotonic deadline, minus observed operation time.
	return result.ExpiresAt.Sub(result.ServerTime) - time.Since(started) - claudeConcurrencySafetyMargin
}

func newUserScopeKeeper(parent context.Context, cache UserScopedConcurrencyCache, op UserScopeOperation, result UserScopeResult, started time.Time) (*userScopeKeeper, error) {
	remaining := userScopeRemaining(result, started)
	if remaining <= 0 {
		return nil, ErrTermConcurrencyUnavailable
	}
	ctx, cancel := context.WithCancelCause(parent)
	k := &userScopeKeeper{cache: cache, op: op, ctx: ctx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{})}
	go k.run(remaining)
	return k, nil
}

func (k *userScopeKeeper) run(remaining time.Duration) {
	defer close(k.done)
	ticker := time.NewTicker(ClaudeConcurrencyHeartbeat)
	defer ticker.Stop()
	deadline := time.NewTimer(remaining)
	defer deadline.Stop()
	for {
		select {
		case <-k.stop:
			return
		case <-deadline.C:
			k.cancel(ErrTermConcurrencyUnavailable)
			return
		case <-ticker.C:
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), ClaudeConcurrencyOperationTimeout)
			result, err := k.cache.RenewScope(ctx, k.op)
			cancel()
			if err != nil {
				continue
			}
			if result.Status != UserScopeAcquired {
				k.cancel(ErrTermConcurrencyUnavailable)
				return
			}
			remaining = userScopeRemaining(result, started)
			if remaining <= 0 {
				k.cancel(ErrTermConcurrencyUnavailable)
				return
			}
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
			deadline.Reset(remaining)
		}
	}
}

func (k *userScopeKeeper) finish() { close(k.stop); <-k.done; k.cancel(nil) }

func releaseUserScope(cache UserScopedConcurrencyCache, op UserScopeOperation) {
	ctx, cancel := context.WithTimeout(context.Background(), ClaudeConcurrencyOperationTimeout)
	defer cancel()
	if _, err := cache.ReleaseScope(ctx, op); err != nil {
		logger.LegacyPrintf("service.concurrency", "Claude scope cleanup remains unconfirmed user_id=%d kind=%s error=%v", op.UserID, op.Kind, err)
	}
}

func (s *ConcurrencyService) claudePolicyOperation(ctx context.Context, userID int64, apply func(context.Context, int) (UserScopeResult, error)) (UserScopeResult, time.Time, error) {
	started := time.Now()
	if s.claudePolicy == nil {
		return UserScopeResult{}, started, ErrTermConcurrencyUnavailable
	}
	opCtx, cancel := context.WithTimeout(ctx, ClaudeConcurrencyOperationTimeout)
	defer cancel()
	var result UserScopeResult
	err := s.claudePolicy.WithClaudeConcurrencyLimit(opCtx, userID, s.claudeDefault, func(inner context.Context, limit int) error {
		var err error
		result, err = apply(inner, limit)
		return err
	})
	if err != nil && !errors.Is(err, ErrTermConcurrencyNotConfigured) {
		err = ErrTermConcurrencyUnavailable.WithCause(err)
	}
	return result, started, err
}

func (s *ConcurrencyService) AcquireClaudeSubscriptionLease(parent context.Context, userID int64, waitTimeout time.Duration, onWaiting func() error) (lease *UserScopeLease, err error) {
	if s == nil || userID <= 0 {
		return nil, ErrTermConcurrencyUnavailable
	}
	cache, ok := s.cache.(UserScopedConcurrencyCache)
	if !ok {
		return nil, ErrTermConcurrencyUnavailable
	}
	ctx := parent
	active := UserScopeOperation{Scope: UserConcurrencyClaudeSubscription, UserID: userID, MemberID: uuid.NewString(), Kind: UserScopeActive, LeaseTTL: ClaudeConcurrencyLeaseTTL}
	waiting := UserScopeOperation{Scope: UserConcurrencyClaudeSubscription, UserID: userID, MemberID: uuid.NewString(), Kind: UserScopeWaiting, LeaseTTL: ClaudeConcurrencyLeaseTTL}
	defer func() {
		if err != nil {
			releaseUserScope(cache, waiting)
			releaseUserScope(cache, active)
		}
	}()
	result, started, err := s.claudePolicyOperation(ctx, userID, func(inner context.Context, limit int) (UserScopeResult, error) {
		op := active
		op.Limit = limit
		return cache.TryAcquireScope(inner, op)
	})
	if err != nil {
		return nil, err
	}
	finishActive := func(result UserScopeResult, started time.Time) (*UserScopeLease, error) {
		keeper, err := newUserScopeKeeper(parent, cache, active, result, started)
		if err != nil {
			return nil, err
		}
		return NewUserScopeLease(keeper.ctx, func() { keeper.finish(); releaseUserScope(cache, active) }), nil
	}
	if result.Status == UserScopeAcquired {
		return finishActive(result, started)
	}
	if result.Status != UserScopeFull {
		return nil, ErrTermConcurrencyUnavailable
	}
	if waitTimeout <= 0 {
		return nil, ErrTermConcurrencyWaitTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	result, started, err = s.claudePolicyOperation(waitCtx, userID, func(inner context.Context, limit int) (UserScopeResult, error) {
		op := waiting
		op.Limit = max(1, CalculateMaxWait(limit)-limit)
		return cache.TryEnqueueScope(inner, op)
	})
	if err != nil {
		return nil, err
	}
	if result.Status == UserScopeFull {
		return nil, ErrTermConcurrencyQueueFull
	}
	if result.Status != UserScopeAcquired {
		return nil, ErrTermConcurrencyUnavailable
	}
	keeper, err := newUserScopeKeeper(waitCtx, cache, waiting, result, started)
	if err != nil {
		return nil, err
	}
	defer keeper.finish()
	defer releaseUserScope(cache, waiting)
	var lastPing time.Time
	for {
		if onWaiting != nil && time.Since(lastPing) >= ClaudeConcurrencyHeartbeat {
			if err = onWaiting(); err != nil {
				return nil, err
			}
			lastPing = time.Now()
		}
		timer := time.NewTimer(time.Duration(100+rand.IntN(150)) * time.Millisecond)
		select {
		case <-keeper.ctx.Done():
			timer.Stop()
			if parent.Err() != nil {
				return nil, parent.Err()
			}
			if waitCtx.Err() != nil {
				return nil, ErrTermConcurrencyWaitTimeout
			}
			return nil, ErrTermConcurrencyUnavailable
		case <-timer.C:
		}
		result, started, err = s.claudePolicyOperation(waitCtx, userID, func(inner context.Context, limit int) (UserScopeResult, error) {
			op := active
			op.TicketID = waiting.MemberID
			op.Limit = limit
			return cache.PromoteScope(inner, op)
		})
		if err != nil {
			if parent.Err() != nil {
				return nil, parent.Err()
			}
			if waitCtx.Err() != nil {
				return nil, ErrTermConcurrencyWaitTimeout
			}
			return nil, err
		}
		if result.Status == UserScopeAcquired {
			return finishActive(result, started)
		}
		if result.Status != UserScopeFull {
			return nil, ErrTermConcurrencyUnavailable
		}
	}
}

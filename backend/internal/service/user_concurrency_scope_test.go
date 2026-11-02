//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scopePolicyFunc func(context.Context, int64, int, func(context.Context, int) error) error

func (f scopePolicyFunc) WithClaudeConcurrencyLimit(ctx context.Context, id int64, def int, apply func(context.Context, int) error) error {
	return f(ctx, id, def, apply)
}

type scopeUnitCache struct {
	ConcurrencyCache
	mu          sync.Mutex
	active      map[string]bool
	waiting     map[string]bool
	fail        bool
	legacyError bool
	legacyReads int
	releases    int
}

func newScopeUnitCache() *scopeUnitCache {
	return &scopeUnitCache{active: map[string]bool{}, waiting: map[string]bool{}}
}
func (c *scopeUnitCache) result() UserScopeResult {
	return UserScopeResult{Status: UserScopeAcquired, ServerTime: time.Now().Add(4 * time.Hour), ExpiresAt: time.Now().Add(4*time.Hour + ClaudeConcurrencyLeaseTTL)}
}
func (c *scopeUnitCache) TryAcquireScope(_ context.Context, in UserScopeOperation) (UserScopeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return UserScopeResult{}, errors.New("redis unavailable")
	}
	r := c.result()
	if len(c.active) >= in.Limit {
		r.Status = UserScopeFull
	} else {
		c.active[in.MemberID] = true
	}
	return r, nil
}
func (c *scopeUnitCache) TryEnqueueScope(_ context.Context, in UserScopeOperation) (UserScopeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waiting[in.MemberID] = true
	return c.result(), nil
}
func (c *scopeUnitCache) PromoteScope(_ context.Context, in UserScopeOperation) (UserScopeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.result()
	if len(c.active) >= in.Limit {
		r.Status = UserScopeFull
	} else {
		delete(c.waiting, in.TicketID)
		c.active[in.MemberID] = true
	}
	return r, nil
}
func (c *scopeUnitCache) RenewScope(_ context.Context, in UserScopeOperation) (UserScopeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result(), nil
}
func (c *scopeUnitCache) ReleaseScope(_ context.Context, in UserScopeOperation) (UserScopeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, in.MemberID)
	delete(c.waiting, in.MemberID)
	c.releases++
	return c.result(), nil
}
func (c *scopeUnitCache) GetScopeStats(context.Context, UserConcurrencyScope, int64) (UserScopeStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return UserScopeStats{}, errors.New("redis unavailable")
	}
	return UserScopeStats{Active: len(c.active), Waiting: len(c.waiting)}, nil
}
func (c *scopeUnitCache) GetUserConcurrency(context.Context, int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyReads++
	if c.legacyError {
		return 0, errors.New("legacy unavailable")
	}
	return 5, nil
}

func (c *scopeUnitCache) GetLegacyUserConcurrencyBounded(ctx context.Context, id int64) (int, error) {
	return c.GetUserConcurrency(ctx, id)
}
func newUnitScopeService(cache *scopeUnitCache, limit int) *ConcurrencyService {
	s := NewConcurrencyService(cache)
	s.ConfigureClaudeSubscriptionConcurrency(scopePolicyFunc(func(ctx context.Context, _ int64, def int, apply func(context.Context, int) error) error {
		if def <= 0 {
			return ErrTermConcurrencyNotConfigured
		}
		return apply(ctx, def)
	}), limit)
	return s
}

func TestClaudeSubscriptionConcurrencyMissingDependencies(t *testing.T) {
	var nilService *ConcurrencyService
	_, err := nilService.AcquireClaudeSubscriptionLease(context.Background(), 1, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyUnavailable)
	s := NewConcurrencyService(newScopeUnitCache())
	_, err = s.AcquireClaudeSubscriptionLease(context.Background(), 1, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyUnavailable)
	s = newUnitScopeService(newScopeUnitCache(), 0)
	_, err = s.AcquireClaudeSubscriptionLease(context.Background(), 1, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyNotConfigured)
}

func TestClaudeSubscriptionConcurrencyCancellationKeepsSlotUntilFinish(t *testing.T) {
	cache := newScopeUnitCache()
	s := newUnitScopeService(cache, 3)
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := s.AcquireClaudeSubscriptionLease(ctx, 1, time.Second, nil)
	require.NoError(t, err)
	cancel()
	require.Eventually(t, func() bool { return lease.Context().Err() != nil }, time.Second, time.Millisecond)
	cache.mu.Lock()
	require.Len(t, cache.active, 1)
	require.Zero(t, cache.releases)
	cache.mu.Unlock()
	lease.Release()
	lease.Release()
	cache.mu.Lock()
	require.Empty(t, cache.active)
	require.Equal(t, 1, cache.releases)
	cache.mu.Unlock()
}

func TestClaudeSubscriptionConcurrencyUnknownAttemptCleansBothIDs(t *testing.T) {
	cache := newScopeUnitCache()
	cache.fail = true
	s := newUnitScopeService(cache, 3)
	_, err := s.AcquireClaudeSubscriptionLease(context.Background(), 1, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyUnavailable)
	cache.mu.Lock()
	require.Equal(t, 2, cache.releases)
	require.Empty(t, cache.active)
	require.Empty(t, cache.waiting)
	cache.mu.Unlock()
}

func TestClaudeSubscriptionConcurrencyCommitFailureReclaimsConfirmedRedisLease(t *testing.T) {
	cache := newScopeUnitCache()
	s := newUnitScopeService(cache, 3)
	s.claudePolicy = scopePolicyFunc(func(ctx context.Context, _ int64, _ int, apply func(context.Context, int) error) error {
		if err := apply(ctx, 3); err != nil {
			return err
		}
		return errors.New("database commit response lost")
	})
	_, err := s.AcquireClaudeSubscriptionLease(context.Background(), 1, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyUnavailable)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Empty(t, cache.active)
	require.Empty(t, cache.waiting)
	require.Equal(t, 2, cache.releases)
}

func TestClaudeSubscriptionConcurrencyWaitingTimeoutReclaimsOnlyTicket(t *testing.T) {
	cache := newScopeUnitCache()
	cache.active["other-request"] = true
	s := newUnitScopeService(cache, 1)
	pings := 0
	_, err := s.AcquireClaudeSubscriptionLease(context.Background(), 1, 40*time.Millisecond, func() error { pings++; return nil })
	require.ErrorIs(t, err, ErrTermConcurrencyWaitTimeout)
	require.Equal(t, 1, pings)
	cache.mu.Lock()
	require.Equal(t, map[string]bool{"other-request": true}, cache.active)
	require.Empty(t, cache.waiting)
	cache.mu.Unlock()
}

func TestClaudeSubscriptionConcurrencyClockSkewUsesRemainingDuration(t *testing.T) {
	start := time.Now()
	server := start.Add(24 * time.Hour)
	remaining := userScopeRemaining(UserScopeResult{ServerTime: server, ExpiresAt: server.Add(time.Minute)}, start)
	require.Greater(t, remaining, 54*time.Second)
	require.LessOrEqual(t, remaining, 55*time.Second)
	server = start.Add(-24 * time.Hour)
	remaining = userScopeRemaining(UserScopeResult{ServerTime: server, ExpiresAt: server.Add(time.Minute)}, start)
	require.Greater(t, remaining, 54*time.Second)
	require.LessOrEqual(t, remaining, 55*time.Second)
}

func TestClaudeSubscriptionConcurrencySafetyDeadlineCancelsActualContext(t *testing.T) {
	cache := newScopeUnitCache()
	server := time.Now().Add(24 * time.Hour)
	keeper, err := newUserScopeKeeper(context.Background(), cache, UserScopeOperation{}, UserScopeResult{ServerTime: server, ExpiresAt: server.Add(claudeConcurrencySafetyMargin + 30*time.Millisecond)}, time.Now())
	require.NoError(t, err)
	defer keeper.finish()
	select {
	case <-keeper.ctx.Done():
		require.ErrorIs(t, context.Cause(keeper.ctx), ErrTermConcurrencyUnavailable)
	case <-time.After(time.Second):
		t.Fatal("local safety deadline did not cancel forward context")
	}
}

func TestClaudeSubscriptionConcurrencyProjectionIndependentAvailability(t *testing.T) {
	cache := newScopeUnitCache()
	s := newUnitScopeService(cache, 3)
	u := &User{ID: 1, Concurrency: 5}
	v := s.ClaudeConcurrencyView(context.Background(), u)
	require.Equal(t, "available", v.ClaudeConcurrencyStatus)
	require.Equal(t, 0, *v.ClaudeCurrentConcurrency)
	require.Equal(t, 3, *v.EffectiveClaudeSubscriptionConcurrency)
	require.Equal(t, 5, *v.LegacyCurrentConcurrency)
	cache.fail = true
	v = s.ClaudeConcurrencyView(context.Background(), u)
	require.Equal(t, "unavailable", v.ClaudeConcurrencyStatus)
	require.Nil(t, v.ClaudeCurrentConcurrency)
	require.Nil(t, v.ClaudeWaitingCount)
	require.Equal(t, "available", v.LegacyConcurrencyStatus)
	cache.fail = false
	cache.legacyError = true
	v = s.ClaudeConcurrencyView(context.Background(), u)
	require.Equal(t, "available", v.ClaudeConcurrencyStatus)
	require.Nil(t, v.LegacyCurrentConcurrency)
	require.Equal(t, "unavailable", v.LegacyConcurrencyStatus)
	value := 2
	u.ClaudeSubscriptionConcurrency = &value
	v = s.ClaudeConcurrencyView(context.Background(), u)
	require.Equal(t, 2, *v.EffectiveClaudeSubscriptionConcurrency)
}

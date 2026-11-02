//go:build unit

package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scopeMetricsBlockedWriter struct{ unblock <-chan struct{} }

func (w scopeMetricsBlockedWriter) Write(p []byte) (int, error) {
	<-w.unblock
	return len(p), nil
}

func TestClaudeConcurrencyMetricsCannotBlockOnLogSink(t *testing.T) {
	unblock := make(chan struct{})
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(scopeMetricsBlockedWriter{unblock}, nil)))
	defer func() { close(unblock); slog.SetDefault(previous) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		observeClaudeScopeOperation("renew", UserScopeActive, UserScopeResult{}, errors.New("synthetic failure"))
		observeClaudeScopeOperation("lost_lease", UserScopeActive, UserScopeResult{Status: UserScopeMissing}, nil)
		observeClaudeScopeOperation("safety_deadline", UserScopeActive, UserScopeResult{}, nil)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lease observation waited for log I/O and can postpone cancellation")
	}
}

func TestClaudeConcurrencyMetricsTrackActualQueuePromotionAndCleanup(t *testing.T) {
	before := GetSubscriptionQuotaMetricsSnapshot().ClaudeConcurrency.Operations
	cache := newScopeUnitCache()
	cache.active["other-request"] = true
	s := newUnitScopeService(cache, 1)
	lease, err := s.AcquireClaudeSubscriptionLease(context.Background(), 75123, time.Second, func() error {
		cache.mu.Lock()
		delete(cache.active, "other-request")
		cache.mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	lease.Release()
	lease.Release()
	after := GetSubscriptionQuotaMetricsSnapshot().ClaudeConcurrency.Operations
	for _, key := range []string{"acquire.active.full", "enqueue.waiting.acquired", "promote.active.acquired", "cleanup.waiting.acquired", "cleanup.active.acquired"} {
		require.Equal(t, before[key]+1, after[key], key)
	}
	for key := range after {
		require.NotContains(t, key, "75123")
		require.NotContains(t, key, "other-request")
	}
}

type scopeMetricsRenewCache struct {
	*scopeUnitCache
	renewResult UserScopeResult
	renewError  error
}

func (c *scopeMetricsRenewCache) RenewScope(context.Context, UserScopeOperation) (UserScopeResult, error) {
	return c.renewResult, c.renewError
}

func TestClaudeConcurrencyMetricsDistinguishLostLeaseAndRenewFailureDeadline(t *testing.T) {
	before := GetClaudeConcurrencyMetricsSnapshot().Operations
	// Both run the real keeper's heartbeat, with independent active/waiting
	// outcomes. A transport error retains the prior deadline; missing ownership
	// cancels immediately without waiting for Redis's original expiry.
	missing := &scopeMetricsRenewCache{scopeUnitCache: newScopeUnitCache(), renewResult: UserScopeResult{Status: UserScopeMissing}}
	failed := &scopeMetricsRenewCache{scopeUnitCache: newScopeUnitCache(), renewError: errors.New("synthetic transport failure")}
	server := time.Now()
	active, err := newUserScopeKeeper(context.Background(), missing, UserScopeOperation{Kind: UserScopeActive}, UserScopeResult{ServerTime: server, ExpiresAt: server.Add(time.Minute)}, time.Now())
	require.NoError(t, err)
	defer active.finish()
	waiting, err := newUserScopeKeeper(context.Background(), failed, UserScopeOperation{Kind: UserScopeWaiting}, UserScopeResult{ServerTime: server, ExpiresAt: server.Add(ClaudeConcurrencyHeartbeat + claudeConcurrencySafetyMargin + 250*time.Millisecond)}, time.Now())
	require.NoError(t, err)
	defer waiting.finish()
	for _, keeper := range []*userScopeKeeper{active, waiting} {
		select {
		case <-keeper.ctx.Done():
			require.ErrorIs(t, context.Cause(keeper.ctx), ErrTermConcurrencyUnavailable)
		case <-time.After(ClaudeConcurrencyHeartbeat + time.Second):
			t.Fatal("keeper did not cancel its forward context")
		}
	}
	after := GetClaudeConcurrencyMetricsSnapshot().Operations
	for _, key := range []string{"renew.active.missing", "lost_lease.active.missing", "renew.waiting.error", "safety_deadline.waiting.unavailable"} {
		require.Equal(t, before[key]+1, after[key], key)
	}
}

func TestClaudeConcurrencyMetricsRecordFailedAcquireWithoutLegacyReads(t *testing.T) {
	before := GetClaudeConcurrencyMetricsSnapshot().Operations
	cache := newScopeUnitCache()
	cache.fail = true
	_, err := newUnitScopeService(cache, 3).AcquireClaudeSubscriptionLease(context.Background(), 99127, time.Second, nil)
	require.ErrorIs(t, err, ErrTermConcurrencyUnavailable)
	after := GetClaudeConcurrencyMetricsSnapshot().Operations
	require.Equal(t, before["acquire.active.error"]+1, after["acquire.active.error"])
	require.Equal(t, before["cleanup.active.acquired"]+1, after["cleanup.active.acquired"])
	require.Equal(t, before["cleanup.waiting.acquired"]+1, after["cleanup.waiting.acquired"])
	require.Zero(t, cache.legacyReads)
	// Returned snapshots must not grant mutation access to live counters.
	after["acquire.active.error"] = 0
	require.Equal(t, before["acquire.active.error"]+1, GetClaudeConcurrencyMetricsSnapshot().Operations["acquire.active.error"])
}

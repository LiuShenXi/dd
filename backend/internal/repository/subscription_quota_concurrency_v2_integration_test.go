//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// These clients deliberately bypass testRedis's prefix hook: the production
// scope adapter creates a separate bounded client from Options. Unique user IDs
// and exact-key cleanup isolate both clients without modifying that adapter.
type quotaConcurrencyV2Fixture struct {
	ctx    context.Context
	user   int64
	rdb    *redis.Client
	caches [2]*concurrencyCache
}

func newQuotaConcurrencyV2Fixture(t *testing.T) *quotaConcurrencyV2Fixture {
	t.Helper()
	f := &quotaConcurrencyV2Fixture{ctx: context.Background(), user: time.Now().UnixNano()}
	opts := *integrationRedis.Options()
	f.rdb = redis.NewClient(&opts)
	for i := range f.caches {
		client := redis.NewClient(&opts)
		f.caches[i] = NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	}
	t.Cleanup(func() {
		keys, err := userScopeKeys(service.UserConcurrencyClaudeSubscription, f.user)
		require.NoError(t, err)
		keys = append(keys, userSlotKeyPrefix+strconv.FormatInt(f.user, 10), liveUserSlotKeyPrefix+strconv.FormatInt(f.user, 10), waitQueueKeyPrefix+strconv.FormatInt(f.user, 10))
		require.NoError(t, f.rdb.Del(context.Background(), keys...).Err())
		require.NoError(t, f.rdb.ZRem(context.Background(), userActiveIndexKey, strconv.FormatInt(f.user, 10)).Err())
		for _, cache := range f.caches {
			_ = cache.CloseScopeCache()
			require.NoError(t, cache.rdb.Close())
		}
		require.NoError(t, f.rdb.Close())
	})
	return f
}

func (f *quotaConcurrencyV2Fixture) operation(kind service.UserScopeMemberKind, limit int, ttl time.Duration) service.UserScopeOperation {
	return service.UserScopeOperation{Scope: service.UserConcurrencyClaudeSubscription, UserID: f.user, MemberID: uuid.NewString(), Kind: kind, Limit: limit, LeaseTTL: ttl}
}

func (f *quotaConcurrencyV2Fixture) stats(t *testing.T) service.UserScopeStats {
	t.Helper()
	stats, err := f.caches[1].GetScopeStats(f.ctx, service.UserConcurrencyClaudeSubscription, f.user)
	require.NoError(t, err)
	return stats
}

func (f *quotaConcurrencyV2Fixture) legacySnapshot(t *testing.T) []string {
	t.Helper()
	var result []string
	for _, key := range []string{userSlotKeyPrefix + strconv.FormatInt(f.user, 10), liveUserSlotKeyPrefix + strconv.FormatInt(f.user, 10), waitQueueKeyPrefix + strconv.FormatInt(f.user, 10)} {
		value, err := f.rdb.Dump(f.ctx, key).Result()
		if err == redis.Nil {
			value, err = "<absent>", nil
		}
		require.NoError(t, err)
		result = append(result, value)
	}
	return result
}

func TestSubscriptionQuotaConcurrencyV2AtomicTwoClientsFivePlusTwo(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	for i := 0; i < 5; i++ {
		ok, err := f.caches[i%2].AcquireUserSlot(f.ctx, f.user, 5, uuid.NewString())
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := f.caches[0].AcquireUserSlot(f.ctx, f.user, 5, uuid.NewString())
	require.NoError(t, err)
	require.False(t, ok)
	for i := 0; i < 3; i++ {
		ok, err = f.caches[0].IncrementWaitCount(f.ctx, f.user, 3)
		require.NoError(t, err)
		require.True(t, ok)
	}
	legacy := f.legacySnapshot(t)
	type outcome struct {
		result service.UserScopeResult
		err    error
	}
	results := make(chan outcome, 40)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 40; i++ {
		workers.Add(1)
		go func(instance int) {
			defer workers.Done()
			<-start
			result, err := f.caches[instance].TryAcquireScope(f.ctx, f.operation(service.UserScopeActive, 2, time.Minute))
			results <- outcome{result, err}
		}(i % 2)
	}
	close(start)
	workers.Wait()
	close(results)
	acquired, full := 0, 0
	for outcome := range results {
		require.NoError(t, outcome.err)
		require.LessOrEqual(t, outcome.result.Active, 2)
		switch outcome.result.Status {
		case service.UserScopeAcquired:
			acquired++
		case service.UserScopeFull:
			full++
		default:
			t.Fatalf("unexpected atomic acquire status %q", outcome.result.Status)
		}
	}
	require.Equal(t, 2, acquired)
	require.Equal(t, 38, full)
	stats := f.stats(t)
	require.Equal(t, 2, stats.Active)
	require.Equal(t, 0, stats.Waiting)
	require.Equal(t, legacy, f.legacySnapshot(t), "Claude admission must preserve all GPT active/live/waiting rows byte for byte")
	// Reverse direction: once the Claude pool and its waiting pool are full,
	// releasing one GPT member must allow another GPT member immediately.
	waiter, err := f.caches[0].TryEnqueueScope(f.ctx, f.operation(service.UserScopeWaiting, 1, time.Minute))
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, waiter.Status)
	legacyKey := userSlotKeyPrefix + strconv.FormatInt(f.user, 10)
	legacyMembers, err := f.rdb.ZRange(f.ctx, legacyKey, 0, 0).Result()
	require.NoError(t, err)
	require.Len(t, legacyMembers, 1)
	require.NoError(t, f.caches[0].ReleaseUserSlot(f.ctx, f.user, legacyMembers[0]))
	ok, err = f.caches[1].AcquireUserSlot(f.ctx, f.user, 5, uuid.NewString())
	require.NoError(t, err)
	require.True(t, ok, "full Claude active and waiting pools cannot consume GPT capacity")
	require.Equal(t, 2, f.stats(t).Active)
	require.Equal(t, 1, f.stats(t).Waiting)
	count, err := f.caches[1].GetUserConcurrency(f.ctx, f.user)
	require.NoError(t, err)
	require.Equal(t, 5, count)
	projection := service.NewConcurrencyService(f.caches[0])
	projection.ConfigureClaudeSubscriptionConcurrency(nil, 3)
	override := 2
	view := projection.ClaudeConcurrencyView(f.ctx, &service.User{ID: f.user, Concurrency: 5, ClaudeSubscriptionConcurrency: &override})
	require.Equal(t, "available", view.LegacyConcurrencyStatus)
	require.Equal(t, "available", view.ClaudeConcurrencyStatus)
	require.NotNil(t, view.LegacyCurrentConcurrency)
	require.NotNil(t, view.ClaudeCurrentConcurrency)
	require.Equal(t, 5, *view.LegacyCurrentConcurrency)
	require.Equal(t, 2, *view.ClaudeCurrentConcurrency)
	// Replacing one GPT member intentionally changes only its active member set.
	require.Equal(t, legacy[1:], f.legacySnapshot(t)[1:], "Claude operations must preserve GPT live and waiting pools byte for byte")
	t.Log("actual Redis: two independent clients, 40 simultaneous Claude attempts admitted exactly 2 while all 5 GPT slots and 3 GPT waiters remained intact")
}

func TestSubscriptionQuotaConcurrencyV2WaitingTicketAtomicPromotion(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	active := f.operation(service.UserScopeActive, 2, time.Minute)
	result, err := f.caches[0].TryAcquireScope(f.ctx, active)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, result.Status)
	ticket := f.operation(service.UserScopeWaiting, 1, time.Minute)
	result, err = f.caches[0].TryEnqueueScope(f.ctx, ticket)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, result.Status)
	replay, err := f.caches[1].TryEnqueueScope(f.ctx, ticket)
	require.NoError(t, err)
	require.Equal(t, 1, replay.Waiting)
	full, err := f.caches[1].TryEnqueueScope(f.ctx, f.operation(service.UserScopeWaiting, 1, time.Minute))
	require.NoError(t, err)
	require.Equal(t, service.UserScopeFull, full.Status)
	type promotion struct {
		op     service.UserScopeOperation
		result service.UserScopeResult
		err    error
	}
	results := make(chan promotion, 24)
	start := make(chan struct{})
	for i := 0; i < 24; i++ {
		go func(instance int) {
			<-start
			op := f.operation(service.UserScopeActive, 2, time.Minute)
			op.TicketID = ticket.MemberID
			out, err := f.caches[instance].PromoteScope(f.ctx, op)
			results <- promotion{op, out, err}
		}(i % 2)
	}
	close(start)
	var winner service.UserScopeOperation
	acquired := 0
	for i := 0; i < 24; i++ {
		out := <-results
		require.NoError(t, out.err)
		if out.result.Status == service.UserScopeAcquired {
			acquired++
			winner = out.op
		} else {
			require.Equal(t, service.UserScopeMissing, out.result.Status)
		}
	}
	require.Equal(t, 1, acquired, "one waiting ticket cannot become multiple active members across instances")
	replay, err = f.caches[1].PromoteScope(f.ctx, winner)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, replay.Status)
	require.Equal(t, 2, replay.Active)
	require.Zero(t, replay.Waiting)
	_, err = f.caches[0].ReleaseScope(f.ctx, winner)
	require.NoError(t, err)
	_, err = f.caches[0].ReleaseScope(f.ctx, active)
	require.NoError(t, err)
	require.Zero(t, f.stats(t).Active)
}

func TestSubscriptionQuotaConcurrencyV2RenewExpiryAndMaximumKeyTTL(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	long := f.operation(service.UserScopeActive, 2, 3*time.Second)
	first, err := f.caches[0].TryAcquireScope(f.ctx, long)
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, first.ExpiresAt.Sub(first.ServerTime), "expiry is derived from Redis TIME")
	short := f.operation(service.UserScopeActive, 2, 150*time.Millisecond)
	_, err = f.caches[1].TryAcquireScope(f.ctx, short)
	require.NoError(t, err)
	keys, err := userScopeKeys(long.Scope, f.user)
	require.NoError(t, err)
	ttl, err := f.rdb.PTTL(f.ctx, keys[0]).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 2500*time.Millisecond, "a later short member must not shorten the live long member's key TTL")
	long.LeaseTTL = 5 * time.Millisecond
	renewed, err := f.caches[1].RenewScope(f.ctx, long)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, renewed.Status)
	require.False(t, renewed.ExpiresAt.Before(first.ExpiresAt), "short renewal never moves an existing deadline backwards")
	require.Eventually(t, func() bool { return f.stats(t).Active == 1 }, 2*time.Second, 10*time.Millisecond)
	missing, err := f.caches[0].RenewScope(f.ctx, short)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, missing.Status, "a lost lease is not silently re-created")
	_, err = f.caches[0].ReleaseScope(f.ctx, long)
	require.NoError(t, err)
	missing, err = f.caches[1].ReleaseScope(f.ctx, long)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, missing.Status)
	require.Zero(t, f.stats(t).Active)
}

func TestSubscriptionQuotaConcurrencyV2UnknownCleanupBlocksLateCommands(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	active := f.operation(service.UserScopeActive, 2, time.Minute)
	ticket := f.operation(service.UserScopeWaiting, 2, time.Minute)
	for _, op := range []service.UserScopeOperation{active, ticket} {
		released, err := f.caches[0].ReleaseScope(f.ctx, op)
		require.NoError(t, err)
		require.Equal(t, service.UserScopeMissing, released.Status)
	}
	late, err := f.caches[1].TryAcquireScope(f.ctx, active)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, late.Status)
	late, err = f.caches[1].TryEnqueueScope(f.ctx, ticket)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, late.Status)
	promote := f.operation(service.UserScopeActive, 2, time.Minute)
	promote.TicketID = ticket.MemberID
	late, err = f.caches[1].PromoteScope(f.ctx, promote)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, late.Status)
	late, err = f.caches[1].RenewScope(f.ctx, active)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeMissing, late.Status)
	require.Zero(t, f.stats(t).Active)
	require.Zero(t, f.stats(t).Waiting)
	t.Log("actual Redis cleanup tombstones reject delayed acquire/enqueue/promote/renew of the original server UUIDs")
}

func TestSubscriptionQuotaConcurrencyV2UnavailableNeverFallsBackToGPT(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	ok, err := f.caches[0].AcquireUserSlot(f.ctx, f.user, 5, uuid.NewString())
	require.NoError(t, err)
	require.True(t, ok)
	legacy := f.legacySnapshot(t)
	op := f.operation(service.UserScopeActive, 2, time.Minute)
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	out, err := f.caches[0].TryAcquireScope(cancelled, op)
	require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
	require.Equal(t, service.UserScopeUnavailable, out.Status)
	require.Zero(t, f.stats(t).Active)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	options := *integrationRedis.Options()
	options.Addr = address // an intentionally closed, task-chosen loopback port
	badClient := redis.NewClient(&options)
	bad := NewConcurrencyCache(badClient, 15, 900).(*concurrencyCache)
	t.Cleanup(func() { _ = bad.CloseScopeCache(); _ = badClient.Close() })
	started := time.Now()
	out, err = bad.TryAcquireScope(f.ctx, op)
	require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
	require.Equal(t, service.UserScopeUnavailable, out.Status)
	require.Less(t, time.Since(started), 3*time.Second)
	good, err := f.caches[1].TryAcquireScope(f.ctx, op)
	require.NoError(t, err)
	require.Equal(t, service.UserScopeAcquired, good.Status)
	require.NoError(t, f.caches[0].CloseScopeCache())
	out, err = f.caches[0].RenewScope(f.ctx, op)
	require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
	require.Equal(t, service.UserScopeUnavailable, out.Status)
	require.Equal(t, 1, f.stats(t).Active, "one failed client must not clear the other instance's valid member")
	require.Equal(t, legacy, f.legacySnapshot(t))
}

func newQuotaConcurrencyV2PolicyFixture(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db := newUpgrade026Database(t)
	require.NoError(t, ApplyMigrations(context.Background(), db))
	var user int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,concurrency,balance) VALUES($1,'synthetic-no-login',5,19.12345678) RETURNING id`, "scope-"+uuid.NewString()+"@example.invalid").Scan(&user))
	return db, user
}

func TestSubscriptionQuotaConcurrencyV2PolicyDefaultOverrideAndUpdateLock(t *testing.T) {
	db, user := newQuotaConcurrencyV2PolicyFixture(t)
	policy := NewClaudeConcurrencyPolicyRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var limits []int
	apply := func(_ context.Context, limit int) error { limits = append(limits, limit); return nil }
	require.NoError(t, policy.WithClaudeConcurrencyLimit(ctx, user, 3, apply))
	require.Equal(t, []int{3}, limits, "no override uses the explicit production-selected default 3")
	_, err := db.ExecContext(ctx, `UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, user)
	require.NoError(t, err)
	require.NoError(t, policy.WithClaudeConcurrencyLimit(ctx, user, 3, apply))
	require.Equal(t, []int{3, 2}, limits)
	_, err = db.ExecContext(ctx, `UPDATE users SET claude_subscription_concurrency=0 WHERE id=$1`, user)
	require.Error(t, err, "database constraint rejects a non-positive override")
	_, err = db.ExecContext(ctx, `UPDATE users SET claude_subscription_concurrency=NULL WHERE id=$1`, user)
	require.NoError(t, err)
	require.ErrorIs(t, policy.WithClaudeConcurrencyLimit(ctx, user, 0, apply), service.ErrTermConcurrencyNotConfigured)
	require.Equal(t, []int{3, 2}, limits, "missing default must never invoke a Redis admission callback")
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	readDone := make(chan error, 1)
	go func() {
		readDone <- policy.WithClaudeConcurrencyLimit(ctx, user, 3, func(context.Context, int) error { close(entered); <-release; return nil })
	}()
	<-entered
	writeDone := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(ctx, `UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1 /* scope-policy-update-wait */`, user)
		writeDone <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE users SET claude_subscription_concurrency=2%scope-policy-update-wait%')`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond, "actual policy callback holds the database read lock until its bounded Redis decision finishes")
	close(release)
	require.NoError(t, <-readDone)
	require.NoError(t, <-writeDone)
	limits = nil
	require.NoError(t, policy.WithClaudeConcurrencyLimit(ctx, user, 3, apply))
	require.Equal(t, []int{2}, limits, "next operation reads the committed override instead of an auth-cache snapshot")
	var original int
	var balance string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT concurrency,balance::text FROM users WHERE id=$1`, user).Scan(&original, &balance))
	require.Equal(t, 5, original)
	require.Equal(t, "19.12345678", balance)
}

func TestSubscriptionQuotaConcurrencyV2ServiceWaitingCancellationTimeoutAndPromotion(t *testing.T) {
	db, user := newQuotaConcurrencyV2PolicyFixture(t)
	f := newQuotaConcurrencyV2Fixture(t)
	// The isolated DB's real user identity is used for policy and Redis alike;
	// it is scoped to this harness, with explicit exact-key cleanup below.
	f.user = user
	services := [2]*service.ConcurrencyService{service.NewConcurrencyService(f.caches[0]), service.NewConcurrencyService(f.caches[1])}
	for _, api := range services {
		api.ConfigureClaudeSubscriptionConcurrency(NewClaudeConcurrencyPolicyRepository(db), 3)
	}
	var defaultLeases []*service.UserScopeLease
	for i := 0; i < 3; i++ {
		lease, err := services[i%2].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
		require.NoError(t, err)
		defaultLeases = append(defaultLeases, lease)
		defer lease.Release()
	}
	require.Equal(t, 3, f.stats(t).Active, "nullable override uses actual default-three Redis capacity")
	_, err := services[0].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.ErrorIs(t, err, service.ErrTermConcurrencyWaitTimeout)
	for _, lease := range defaultLeases {
		lease.Release()
	}
	require.Zero(t, f.stats(t).Active)
	_, err = db.Exec(`UPDATE users SET claude_subscription_concurrency=1 WHERE id=$1`, user)
	require.NoError(t, err)
	owner, err := services[0].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.NoError(t, err)
	defer owner.Release()
	ctx, cancel := context.WithCancel(f.ctx)
	waitDone := make(chan error, 1)
	go func() {
		lease, err := services[1].AcquireClaudeSubscriptionLease(ctx, user, 3*time.Second, nil)
		if lease != nil {
			lease.Release()
		}
		waitDone <- err
	}()
	require.Eventually(t, func() bool { return f.stats(t).Waiting == 1 }, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-waitDone, context.Canceled)
	require.Eventually(t, func() bool { s := f.stats(t); return s.Active == 1 && s.Waiting == 0 }, 2*time.Second, 10*time.Millisecond, "cancelled request cleans its waiting ticket using an independent context")
	_, err = services[1].AcquireClaudeSubscriptionLease(f.ctx, user, 200*time.Millisecond, nil)
	require.ErrorIs(t, err, service.ErrTermConcurrencyWaitTimeout)
	require.Zero(t, f.stats(t).Waiting)
	acquired := make(chan *service.UserScopeLease, 1)
	go func() {
		lease, err := services[1].AcquireClaudeSubscriptionLease(f.ctx, user, 3*time.Second, nil)
		if err != nil {
			waitDone <- fmt.Errorf("promotion: %w", err)
			return
		}
		acquired <- lease
	}()
	require.Eventually(t, func() bool { return f.stats(t).Waiting == 1 }, 2*time.Second, 10*time.Millisecond)
	owner.Release()
	var promoted *service.UserScopeLease
	select {
	case promoted = <-acquired:
	case err := <-waitDone:
		require.NoError(t, err)
	case <-time.After(4 * time.Second):
		t.Fatal("actual Redis waiting member was not promoted after release")
	}
	require.NotNil(t, promoted)
	require.NoError(t, promoted.Context().Err())
	require.Equal(t, 1, f.stats(t).Active)
	require.Zero(t, f.stats(t).Waiting)
	promoted.Release()
	promoted.Release()
	require.Zero(t, f.stats(t).Active)
}

func TestSubscriptionQuotaConcurrencyV2PolicyReductionKeepsExistingLeases(t *testing.T) {
	db, user := newQuotaConcurrencyV2PolicyFixture(t)
	f := newQuotaConcurrencyV2Fixture(t)
	f.user = user
	_, err := db.Exec(`UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, user)
	require.NoError(t, err)
	apis := [2]*service.ConcurrencyService{service.NewConcurrencyService(f.caches[0]), service.NewConcurrencyService(f.caches[1])}
	for _, api := range apis {
		api.ConfigureClaudeSubscriptionConcurrency(NewClaudeConcurrencyPolicyRepository(db), 3)
	}
	first, err := apis[0].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.NoError(t, err)
	defer first.Release()
	second, err := apis[1].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.NoError(t, err)
	defer second.Release()
	require.Equal(t, 2, f.stats(t).Active)
	_, err = db.Exec(`UPDATE users SET claude_subscription_concurrency=1 WHERE id=$1`, user)
	require.NoError(t, err)
	guard := subscriptionQuotaBillingMoneyGuard(t, db)
	require.NoError(t, first.Context().Err())
	require.NoError(t, second.Context().Err())
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	result := make(chan *service.UserScopeLease, 1)
	failures := make(chan error, 1)
	go func() {
		lease, err := apis[0].AcquireClaudeSubscriptionLease(ctx, user, 4*time.Second, nil)
		if err != nil {
			failures <- err
			return
		}
		result <- lease
	}()
	require.Eventually(t, func() bool { return f.stats(t).Waiting == 1 }, 2*time.Second, 10*time.Millisecond)
	first.Release()
	require.Equal(t, 1, f.stats(t).Active)
	require.NoError(t, second.Context().Err(), "lowering policy does not cancel an already valid stream")
	require.Never(t, func() bool {
		select {
		case lease := <-result:
			lease.Release()
			return true
		default:
			return false
		}
	}, 600*time.Millisecond, 20*time.Millisecond, "active count equal to the reduced limit must not promote the waiter")
	require.Equal(t, 1, f.stats(t).Waiting)
	second.Release()
	var promoted *service.UserScopeLease
	select {
	case promoted = <-result:
	case err := <-failures:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("reduced-limit waiter did not promote after active reached zero")
	}
	require.NotNil(t, promoted)
	defer promoted.Release()
	require.Equal(t, 1, f.stats(t).Active)
	require.Zero(t, f.stats(t).Waiting)
	_, err = apis[1].AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.ErrorIs(t, err, service.ErrTermConcurrencyWaitTimeout)
	promoted.Release()
	promoted.Release()
	require.Zero(t, f.stats(t).Active)
	guard()
}

func TestSubscriptionQuotaConcurrencyV2ProtectedBeforeUserScopeDurability(t *testing.T) {
	db, user := newQuotaConcurrencyV2PolicyFixture(t)
	for _, scenario := range []string{"stale_standard_cache_database_term", "cancel_before_actual_attempt", "final_gate_reuses_marker_unknown_stays_held", "key_disabled_during_queue"} {
		t.Run(scenario, func(t *testing.T) {
			var group, keyID int64
			require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES($1,'anthropic','standard') RETURNING id`, "synthetic-protected-"+uuid.NewString()).Scan(&group))
			require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,$3,'Synthetic pre-user barrier') RETURNING id`, user, group, "synthetic-no-provider-"+uuid.NewString()).Scan(&keyID))
			key := &service.APIKey{ID: keyID, UserID: user, GroupID: &group, Status: service.StatusActive, Group: &service.Group{ID: group, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard, SubscriptionQuotaMode: "windows", Status: service.StatusActive}}
			repo := NewSubscriptionQuotaProtectedGroupRepository(db)
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeStandard}, nil)
			defer billing.Stop()
			billing.SetSubscriptionQuotaProtectedGroups(service.NewSubscriptionQuotaProtectedGroupService(repo, []int64{group}))
			guard := subscriptionQuotaBillingMoneyGuard(t, db)
			if scenario == "stale_standard_cache_database_term" {
				_, err := db.Exec(`UPDATE groups SET subscription_type='subscription',subscription_quota_mode='term' WHERE id=$1`, group)
				require.NoError(t, err)
				_, finish, err := billing.BeginProtectedUserConcurrency(context.Background(), key)
				require.ErrorIs(t, err, service.ErrSubscriptionQuotaProtectedUnavailable)
				finish()
				count, err := repo.CountProtectedGroupUnfinished(context.Background(), group)
				require.NoError(t, err)
				require.Zero(t, count)
				guard()
				return
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scoped, finish, err := billing.BeginProtectedUserConcurrency(ctx, key)
			require.NoError(t, err)
			count, err := repo.CountProtectedGroupUnfinished(context.Background(), group)
			require.NoError(t, err)
			require.EqualValues(t, 1, count, "pre-user marker is durable before any queue/account/upstream operation")
			var originalID string
			require.NoError(t, db.QueryRow(`SELECT request_id FROM subscription_quota_protected_requests WHERE group_id=$1`, group).Scan(&originalID))
			switch scenario {
			case "cancel_before_actual_attempt":
				cancel()
				finish()
				finish()
				var state, reason string
				require.NoError(t, db.QueryRow(`SELECT state,reason FROM subscription_quota_protected_requests WHERE request_id=$1`, originalID).Scan(&state, &reason))
				require.Equal(t, "no_charge", state)
				require.Equal(t, "no_upstream_attempt_before_user_scope_exit", reason)
				count, err = repo.CountProtectedGroupUnfinished(context.Background(), group)
				require.NoError(t, err)
				require.Zero(t, count)
			case "final_gate_reuses_marker_unknown_stays_held":
				request, err := billing.AdmitSubscriptionQuotaProtectedGroup(scoped, key, key, service.PlatformAnthropic)
				require.NoError(t, err)
				require.NotNil(t, request)
				finish()
				finish()
				cancel()
				var rows int
				require.NoError(t, db.QueryRow(`SELECT count(*) FROM subscription_quota_protected_requests WHERE group_id=$1`, group).Scan(&rows))
				require.Equal(t, 1, rows)
				require.NoError(t, request.ForwardEnded())
				var state string
				require.NoError(t, db.QueryRow(`SELECT state FROM subscription_quota_protected_requests WHERE request_id=$1`, originalID).Scan(&state))
				require.Equal(t, "reconcile_required", state)
				count, err = repo.CountProtectedGroupUnfinished(context.Background(), group)
				require.NoError(t, err)
				require.EqualValues(t, 1, count, "actual-attempt ambiguity cannot be declared drained by pre-user cleanup")
			case "key_disabled_during_queue":
				_, err := db.Exec(`UPDATE api_keys SET status='disabled' WHERE id=$1`, keyID)
				require.NoError(t, err)
				_, err = billing.AdmitSubscriptionQuotaProtectedGroup(scoped, key, key, service.PlatformAnthropic)
				require.ErrorIs(t, err, service.ErrSubscriptionQuotaProtectedUnavailable)
				finish()
				count, err = repo.CountProtectedGroupUnfinished(context.Background(), group)
				require.NoError(t, err)
				require.Zero(t, count, "final DB recheck fails before an actual attempt, so cleanup may finish only this marker")
			}
			guard()
		})
	}
	t.Log("actual PG BeginProtected→final gate/cleanup semantics only; complete production conversion/drain seam is deferred to I6, not simulated by test-owned conversion SQL")
}

func TestSubscriptionQuotaConcurrencyV2StatisticsPoolFaultIsBoundedUnknown(t *testing.T) {
	f := newQuotaConcurrencyV2Fixture(t)
	ok, err := f.caches[0].AcquireUserSlot(f.ctx, f.user, 5, uuid.NewString())
	require.NoError(t, err)
	require.True(t, ok)
	op := f.operation(service.UserScopeActive, 2, time.Minute)
	_, err = f.caches[1].TryAcquireScope(f.ctx, op)
	require.NoError(t, err)
	legacy := f.legacySnapshot(t)
	options := *integrationRedis.Options()
	options.PoolSize = 1
	client := redis.NewClient(&options)
	cache := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	t.Cleanup(func() { _ = cache.CloseScopeCache(); _ = client.Close() })
	scope := cache.scopedRedisClient()
	require.NoError(t, scope.Ping(f.ctx).Err())
	blockKey := "cwq-v2-statistics-block:" + uuid.NewString()
	t.Cleanup(func() { require.NoError(t, f.rdb.Del(context.Background(), blockKey).Err()) })
	blockCtx, cancel := context.WithTimeout(f.ctx, 4*time.Second)
	defer cancel()
	blocked := make(chan error, 1)
	go func() { blocked <- scope.BLPop(blockCtx, 4*time.Second, blockKey).Err() }()
	require.Eventually(t, func() bool { return scope.PoolStats().TotalConns == 1 && scope.PoolStats().IdleConns == 0 }, time.Second, 10*time.Millisecond)
	api := service.NewConcurrencyService(cache)
	api.ConfigureClaudeSubscriptionConcurrency(nil, 3)
	started := time.Now()
	view := api.ClaudeConcurrencyView(f.ctx, &service.User{ID: f.user, Concurrency: 5})
	elapsed := time.Since(started)
	require.Less(t, elapsed, 3*time.Second, "both namespace reads must share the bounded two-second projection deadline")
	require.Equal(t, "unavailable", view.LegacyConcurrencyStatus)
	require.Nil(t, view.LegacyCurrentConcurrency)
	require.Equal(t, "unavailable", view.ClaudeConcurrencyStatus)
	require.Nil(t, view.ClaudeCurrentConcurrency)
	require.Nil(t, view.ClaudeWaitingCount)
	require.Equal(t, 1, f.stats(t).Active)
	require.Equal(t, legacy, f.legacySnapshot(t))
	<-blocked
	view = api.ClaudeConcurrencyView(f.ctx, &service.User{ID: f.user, Concurrency: 5})
	require.Equal(t, "available", view.LegacyConcurrencyStatus)
	require.Equal(t, "available", view.ClaudeConcurrencyStatus)
	require.Equal(t, 1, *view.LegacyCurrentConcurrency)
	require.Equal(t, 1, *view.ClaudeCurrentConcurrency)
	t.Logf("actual Redis occupied scope pool: combined legacy/Claude statistics elapsed=%s both_unknown=true no_wallet_or_legacy_slot_mutation=true", elapsed)
}

func TestSubscriptionQuotaConcurrencyV2RenewalFaultCancelsBeforeLeaseExpiry(t *testing.T) {
	db, user := newQuotaConcurrencyV2PolicyFixture(t)
	f := newQuotaConcurrencyV2Fixture(t)
	f.user = user
	api := service.NewConcurrencyService(f.caches[0])
	api.ConfigureClaudeSubscriptionConcurrency(NewClaudeConcurrencyPolicyRepository(db), 3)
	ok, err := f.caches[1].AcquireUserSlot(f.ctx, user, 5, uuid.NewString())
	require.NoError(t, err)
	require.True(t, ok)
	legacy := f.legacySnapshot(t)
	lease, err := api.AcquireClaudeSubscriptionLease(f.ctx, user, 0, nil)
	require.NoError(t, err)
	defer lease.Release()
	keys, err := userScopeKeys(service.UserConcurrencyClaudeSubscription, user)
	require.NoError(t, err)
	members, err := f.rdb.ZRange(f.ctx, keys[0], 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, members, 1)
	started := time.Now()
	require.NoError(t, f.caches[0].CloseScopeCache()) // real closed Redis client, not a fake renewal result
	select {
	case <-lease.Context().Done():
	case <-time.After(58 * time.Second):
		t.Fatal("failed renewal did not cancel forwarding before the original sixty-second lease expires")
	}
	elapsed := time.Since(started)
	require.Greater(t, elapsed, 45*time.Second, "a single transient heartbeat failure does not immediately cancel a confirmed sixty-second lease")
	require.Less(t, elapsed, 58*time.Second)
	require.ErrorIs(t, context.Cause(lease.Context()), service.ErrTermConcurrencyUnavailable)
	remaining, err := f.rdb.ZScore(f.ctx, keys[0], members[0]).Result()
	require.NoError(t, err)
	now, err := f.rdb.Time(f.ctx).Result()
	require.NoError(t, err)
	require.Greater(t, int64(remaining), now.UnixMilli(), "forward context is cancelled while the last confirmed Redis member is still valid")
	require.Equal(t, legacy, f.legacySnapshot(t), "failed Claude renewal cannot fall back into or clear original GPT slots")
	lease.Release()
	lease.Release()
	t.Logf("actual service/Redis fault elapsed=%s cancellation_before_original_60s_expiry=true original_member_not_recreated=true GPT_slot_unchanged=true; actual Router cancellation is separate evidence", elapsed)
}

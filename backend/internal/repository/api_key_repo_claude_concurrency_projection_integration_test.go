//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type claudeProjectionNativeReads struct {
	service.APIKeyRepository
	reads atomic.Int64
}

func (r *claudeProjectionNativeReads) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	r.reads.Add(1)
	return r.APIKeyRepository.GetByKeyForAuth(ctx, key)
}

type claudeProjectionRedisReads struct {
	service.APIKeyCache
	reads atomic.Int64
}

func (c *claudeProjectionRedisReads) GetAuthCache(ctx context.Context, key string) (*service.APIKeyAuthCacheEntry, error) {
	c.reads.Add(1)
	return c.APIKeyCache.GetAuthCache(ctx, key)
}

func TestClaudeSubscriptionConcurrencyNativeAuthColdL1L2AndV27Reload(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	client := integrationEntClient
	group, err := client.Group.Create().SetName(fmt.Sprintf("claude-auth-projection-%d", suffix)).
		SetPlatform(service.PlatformAnthropic).SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscription).SetSubscriptionQuotaMode("term").SetRateMultiplier(1).Save(ctx)
	require.NoError(t, err)
	a, err := client.User.Create().SetEmail(fmt.Sprintf("claude-auth-a-%d@example.test", suffix)).SetPasswordHash("synthetic-hash").
		SetRole(service.RoleUser).SetStatus(service.StatusActive).SetConcurrency(5).SetClaudeSubscriptionConcurrency(2).SetBalance(17.125).Save(ctx)
	require.NoError(t, err)
	b, err := client.User.Create().SetEmail(fmt.Sprintf("claude-auth-b-%d@example.test", suffix)).SetPasswordHash("synthetic-hash").
		SetRole(service.RoleUser).SetStatus(service.StatusActive).SetConcurrency(5).SetBalance(29.25).Save(ctx)
	require.NoError(t, err)
	native := NewAPIKeyRepository(client, integrationDB)
	keys := []*service.APIKey{
		{UserID: a.ID, Name: "a-1"}, {UserID: a.ID, Name: "a-2"}, {UserID: b.ID, Name: "b"},
	}
	for i, key := range keys {
		key.Key, key.Status, key.GroupID = fmt.Sprintf("synthetic-native-projection-%d-%d", suffix, i), service.StatusActive, &group.ID
		require.NoError(t, native.Create(ctx, key))
	}
	t.Cleanup(func() {
		for _, key := range keys {
			_, err := integrationDB.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox WHERE cache_key=encode(sha256(convert_to($1,'UTF8')),'hex')`, key.Key)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, key.ID)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1,$2)`, a.ID, b.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE id=$1`, group.ID)
		require.NoError(t, err)
	})
	before := make(map[int64]string)
	for _, id := range []int64{a.ID, b.ID} {
		var fingerprint string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT md5(row_to_json(u)::text) FROM users u WHERE id=$1`, id).Scan(&fingerprint))
		before[id] = fingerprint
	}
	rdb := testRedis(t)
	cache := &claudeProjectionRedisReads{APIKeyCache: NewAPIKeyCache(rdb)}
	repo := &claudeProjectionNativeReads{APIKeyRepository: native}
	cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 1000, L1TTLSeconds: 60, L2TTLSeconds: 60}}
	api := service.NewAPIKeyService(repo, nil, nil, nil, nil, cache, cfg)
	scopeCache := NewConcurrencyCache(rdb, 15, 30)
	t.Cleanup(func() { require.NoError(t, scopeCache.(interface{ CloseScopeCache() error }).CloseScopeCache()) })
	projection := service.NewConcurrencyService(scopeCache)
	projection.ConfigureClaudeSubscriptionConcurrency(nil, 3)
	assertKey := func(t *testing.T, key *service.APIKey, loaded *service.APIKey) {
		t.Helper()
		require.NotNil(t, loaded.User)
		require.Equal(t, key.UserID, loaded.UserID)
		require.Equal(t, key.UserID, loaded.User.ID)
		require.Equal(t, 5, loaded.User.Concurrency)
		view := projection.ClaudeConcurrencyView(ctx, loaded.User)
		require.Equal(t, 3, view.DefaultClaudeSubscriptionConcurrency)
		require.Equal(t, "available", view.ClaudeConcurrencyStatus)
		require.Equal(t, "available", view.LegacyConcurrencyStatus)
		if key.UserID == a.ID {
			require.NotNil(t, loaded.User.ClaudeSubscriptionConcurrency)
			require.Equal(t, 2, *loaded.User.ClaudeSubscriptionConcurrency)
			require.Equal(t, 2, *view.EffectiveClaudeSubscriptionConcurrency)
			require.Equal(t, 17.125, loaded.User.Balance)
		} else {
			require.Nil(t, loaded.User.ClaudeSubscriptionConcurrency)
			require.Equal(t, 3, *view.EffectiveClaudeSubscriptionConcurrency)
			require.Equal(t, 29.25, loaded.User.Balance)
		}
	}
	cacheID := func(key string) string { sum := sha256.Sum256([]byte(key)); return hex.EncodeToString(sum[:]) }
	t.Run("native_cold", func(t *testing.T) {
		for _, key := range keys {
			loaded, err := api.GetByKey(ctx, key.Key)
			require.NoError(t, err)
			assertKey(t, key, loaded)
			entry, err := cache.APIKeyCache.GetAuthCache(ctx, cacheID(key.Key))
			require.NoError(t, err)
			require.Equal(t, 28, entry.Snapshot.Version)
		}
		require.Equal(t, int64(len(keys)), repo.reads.Load())
	})
	t.Run("materialized_l1", func(t *testing.T) {
		for _, key := range keys {
			var loaded *service.APIKey
			require.Eventually(t, func() bool {
				beforeRead := cache.reads.Load()
				var loadErr error
				loaded, loadErr = api.GetByKey(ctx, key.Key)
				return loadErr == nil && loaded != nil && cache.reads.Load() == beforeRead
			}, time.Second, 10*time.Millisecond, "a real L1 hit must bypass both Redis and native loading")
			assertKey(t, key, loaded)
		}
		require.Equal(t, int64(len(keys)), repo.reads.Load())
	})
	l2 := service.NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60}})
	t.Run("new_instance_l2", func(t *testing.T) {
		beforeReads := cache.reads.Load()
		for _, key := range keys {
			loaded, err := l2.GetByKey(ctx, key.Key)
			require.NoError(t, err)
			assertKey(t, key, loaded)
		}
		require.Equal(t, beforeReads+int64(len(keys)), cache.reads.Load())
		require.Equal(t, int64(len(keys)), repo.reads.Load(), "fresh service must read actual Redis without native calls")
	})
	t.Run("bad_v27_l2_reload", func(t *testing.T) {
		for _, key := range keys {
			id := cacheID(key.Key)
			entry, err := cache.APIKeyCache.GetAuthCache(ctx, id)
			require.NoError(t, err)
			entry.Snapshot.Version, entry.Snapshot.User.ClaudeSubscriptionConcurrency = 27, nil
			require.NoError(t, cache.SetAuthCache(ctx, id, entry, time.Minute))
			loaded, err := l2.GetByKey(ctx, key.Key)
			require.NoError(t, err)
			assertKey(t, key, loaded)
			fresh, err := cache.APIKeyCache.GetAuthCache(ctx, id)
			require.NoError(t, err)
			require.Equal(t, 28, fresh.Snapshot.Version)
		}
		require.Equal(t, int64(2*len(keys)), repo.reads.Load())
	})
	for id, fingerprint := range before {
		var after string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT md5(row_to_json(u)::text) FROM users u WHERE id=$1`, id).Scan(&after))
		require.Equal(t, fingerprint, after, "auth/profile reads must not update GPT policy, wallets or user identity")
	}
}

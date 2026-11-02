//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type claudeAuthMaterializedCache struct {
	authCacheStub
	entries map[string]*APIKeyAuthCacheEntry
}

func (c *claudeAuthMaterializedCache) GetAuthCache(_ context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	entry := c.entries[key]
	if entry == nil {
		return nil, redis.Nil
	}
	return entry, nil
}
func (c *claudeAuthMaterializedCache) SetAuthCache(_ context.Context, key string, entry *APIKeyAuthCacheEntry, _ time.Duration) error {
	c.entries[key] = entry
	return nil
}
func (c *claudeAuthMaterializedCache) DeleteAuthCache(_ context.Context, key string) error {
	delete(c.entries, key)
	return nil
}

func TestClaudeSubscriptionConcurrencyAdminUpdateRefreshesTwoKeysL1AndL2(t *testing.T) {
	ctx := context.Background()
	old := 2
	users := &claudeUserUpdateRepo{user: User{ID: 7, Role: RoleUser, Status: StatusActive, Concurrency: 5, ClaudeSubscriptionConcurrency: &old}}
	reads := 0
	keys := []string{"synthetic-claude-key-a", "synthetic-claude-key-b"}
	repo := &authRepoStub{
		listKeysByUserID: func(_ context.Context, id int64) ([]string, error) { require.Equal(t, int64(7), id); return keys, nil },
		getByKeyForAuth: func(_ context.Context, key string) (*APIKey, error) {
			reads++
			user := users.user
			id := int64(11)
			if key == keys[1] {
				id = 12
			}
			return &APIKey{ID: id, UserID: 7, User: &user, Status: StatusActive}, nil
		},
	}
	cache := &claudeAuthMaterializedCache{entries: map[string]*APIKeyAuthCacheEntry{}}
	api := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 1000, L1TTLSeconds: 60, L2TTLSeconds: 60}})
	defer api.authCacheL1.Close()
	for _, key := range keys {
		value, err := api.GetByKey(ctx, key)
		require.NoError(t, err)
		require.Equal(t, 2, *value.User.ClaudeSubscriptionConcurrency)
	}
	api.authCacheL1.Wait()
	require.Equal(t, 2, reads)
	require.Len(t, cache.entries, 2)
	for _, key := range keys {
		_, ok := api.authCacheL1.Get(api.authCacheKey(key))
		require.True(t, ok)
	}
	admin := &adminServiceImpl{userRepo: users, authCacheInvalidator: api}
	next := 3
	_, err := admin.UpdateUser(ctx, 7, &UpdateUserInput{ActorAdminID: 9, ClaudeSubscriptionConcurrency: &next})
	require.NoError(t, err)
	api.authCacheL1.Wait()
	require.Empty(t, cache.entries)
	for _, key := range keys {
		_, ok := api.authCacheL1.Get(api.authCacheKey(key))
		require.False(t, ok)
	}
	for _, key := range keys {
		value, err := api.GetByKey(ctx, key)
		require.NoError(t, err)
		require.Equal(t, 3, *value.User.ClaudeSubscriptionConcurrency)
		require.Equal(t, 5, value.User.Concurrency)
	}
	require.Equal(t, 4, reads)
	require.Len(t, cache.entries, 2)
}

type claudeUserUpdateRepo struct {
	UserRepository
	user   User
	fields []UserUpdateFields
}

func (r *claudeUserUpdateRepo) GetByID(context.Context, int64) (*User, error) {
	copy := r.user
	return &copy, nil
}
func (r *claudeUserUpdateRepo) Update(_ context.Context, user *User, fields UserUpdateFields) error {
	r.user = *user
	r.fields = append(r.fields, fields)
	return nil
}

func TestClaudeSubscriptionConcurrencyAdminUpdatePreservesLegacyAndInvalidatesAllKeys(t *testing.T) {
	repo := &claudeUserUpdateRepo{user: User{ID: 7, Role: RoleUser, Status: StatusActive, Concurrency: 5, Balance: 17.12345678}}
	invalidator := &authCacheInvalidatorStub{}
	s := &adminServiceImpl{userRepo: repo, authCacheInvalidator: invalidator}
	value := 2
	user, err := s.UpdateUser(context.Background(), 7, &UpdateUserInput{ActorAdminID: 9, ClaudeSubscriptionConcurrency: &value})
	require.NoError(t, err)
	require.Equal(t, 5, user.Concurrency)
	require.Equal(t, 17.12345678, user.Balance)
	require.Equal(t, 2, *user.ClaudeSubscriptionConcurrency)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Equal(t, UserUpdateFields{ClaudeSubscriptionConcurrency: true}, repo.fields[0])
	_, err = s.UpdateUser(context.Background(), 7, &UpdateUserInput{ActorAdminID: 9})
	require.NoError(t, err)
	require.Equal(t, 2, *repo.user.ClaudeSubscriptionConcurrency)
	require.False(t, repo.fields[1].ClaudeSubscriptionConcurrency)
	require.Len(t, invalidator.userIDs, 1)
}

func TestClaudeSubscriptionConcurrencyAdminRejectsInvalidWithoutRepository(t *testing.T) {
	s := &adminServiceImpl{}
	for _, value := range []int{0, -1, 2147483648} {
		_, err := s.UpdateUser(context.Background(), 1, &UpdateUserInput{ClaudeSubscriptionConcurrency: &value})
		require.ErrorIs(t, err, ErrInvalidClaudeConcurrency)
		_, err = s.CreateUser(context.Background(), &CreateUserInput{ClaudeSubscriptionConcurrency: &value})
		require.ErrorIs(t, err, ErrInvalidClaudeConcurrency)
	}
}

func TestClaudeSubscriptionConcurrencyAuthSnapshotRoundtripAndIndependence(t *testing.T) {
	value := 2
	s := &APIKeyService{}
	key := &APIKey{ID: 4, UserID: 7, User: &User{ID: 7, Concurrency: 5, ClaudeSubscriptionConcurrency: &value}}
	snapshot := s.snapshotFromAPIKey(context.Background(), key)
	require.Equal(t, 28, snapshot.Version)
	require.Equal(t, 2, *snapshot.User.ClaudeSubscriptionConcurrency)
	value = 8
	require.Equal(t, 2, *snapshot.User.ClaudeSubscriptionConcurrency)
	loaded := s.snapshotToAPIKey("synthetic", snapshot)
	require.Equal(t, 5, loaded.User.Concurrency)
	require.Equal(t, 2, *loaded.User.ClaudeSubscriptionConcurrency)
	*snapshot.User.ClaudeSubscriptionConcurrency = 9
	require.Equal(t, 2, *loaded.User.ClaudeSubscriptionConcurrency)
}

func TestClaudeSubscriptionConcurrencyAuthSnapshotRejectsIncompleteV27(t *testing.T) {
	s := &APIKeyService{}
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version: 27, APIKeyID: 4, UserID: 7,
		User: APIKeyAuthUserSnapshot{ID: 7, Concurrency: 5},
	}}
	key, used, err := s.applyAuthCacheEntry("synthetic", entry)
	require.NoError(t, err)
	require.False(t, used, "v27 may have lost a real override in the native partial projection")
	require.Nil(t, key)
}

func TestClaudeSubscriptionConcurrencyAuthV27L1AndL2HitsReload(t *testing.T) {
	for _, layer := range []string{"l1", "l2"} {
		t.Run(layer, func(t *testing.T) {
			value, reads := 2, 0
			repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
				reads++
				return &APIKey{ID: 4, UserID: 7, Status: StatusActive, User: &User{ID: 7, Status: StatusActive, Concurrency: 5, ClaudeSubscriptionConcurrency: &value}}, nil
			}}
			cache := &claudeAuthMaterializedCache{entries: map[string]*APIKeyAuthCacheEntry{}}
			api := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 1000, L1TTLSeconds: 60, L2TTLSeconds: 60}})
			t.Cleanup(api.authCacheL1.Close)
			const key = "synthetic-v27-projection"
			cacheKey := api.authCacheKey(key)
			stale := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 27, APIKeyID: 4, UserID: 7, User: APIKeyAuthUserSnapshot{ID: 7, Concurrency: 5}}}
			if layer == "l1" {
				require.True(t, api.authCacheL1.SetWithTTL(cacheKey, stale, 1, time.Minute))
				api.authCacheL1.Wait()
				_, found := api.authCacheL1.Get(cacheKey)
				require.True(t, found, "exercise a materialized stale L1, not an empty-cache reload")
			} else {
				cache.entries[cacheKey] = stale
			}
			loaded, err := api.GetByKey(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, 1, reads)
			require.Equal(t, 2, *loaded.User.ClaudeSubscriptionConcurrency)
			require.Equal(t, 5, loaded.User.Concurrency)
			require.Equal(t, 28, cache.entries[cacheKey].Snapshot.Version)
			api.authCacheL1.Wait()
			entry, found := api.authCacheL1.Get(cacheKey)
			require.True(t, found)
			require.Equal(t, 28, entry.(*APIKeyAuthCacheEntry).Snapshot.Version)
		})
	}
}

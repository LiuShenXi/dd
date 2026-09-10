package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

type releaseKeyRepo struct {
	APIKeyRepository
	err error
}

func (r *releaseKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error) {
	return []string{"moved-key"}, r.err
}
func (r *releaseKeyRepo) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	return []string{"moved-key", "admin-key"}, r.err
}

func TestReleaseInvalidationClearsMergedLocalAndSharedKeys(t *testing.T) {
	cache := &authInvalidationCacheStub{}
	s := &APIKeyService{apiKeyRepo: &releaseKeyRepo{}, cache: cache}
	var err error
	s.authCacheL1, err = ristretto.NewCache(&ristretto.Config{NumCounters: 100, MaxCost: 10, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(s.authCacheL1.Close)
	for _, key := range []string{"moved-key", "admin-key"} {
		s.authCacheL1.SetWithTTL(s.authCacheKey(key), &APIKeyAuthCacheEntry{}, 1, time.Minute)
	}
	s.authCacheL1.Wait()
	require.NoError(t, s.InvalidateReleaseAuthCache(context.Background(), []int64{1}, []int64{2, 3}))
	require.Len(t, cache.deleted, 2)
	require.Len(t, cache.published, 2)
	for _, key := range []string{"moved-key", "admin-key"} {
		_, found := s.authCacheL1.Get(s.authCacheKey(key))
		require.False(t, found)
	}
}

func TestReleaseInvalidationPropagatesEveryDependencyFailure(t *testing.T) {
	for _, stage := range []string{"repository", "redis", "publish"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("unavailable")
			repo := &releaseKeyRepo{}
			cache := &authInvalidationCacheStub{}
			switch stage {
			case "repository":
				repo.err = failure
			case "redis":
				cache.deleteFn = func(context.Context, string) error { return failure }
			case "publish":
				cache.publishFn = func(context.Context, string) error { return failure }
			}
			s := &APIKeyService{apiKeyRepo: repo, cache: cache}
			require.ErrorIs(t, s.InvalidateReleaseAuthCache(context.Background(), []int64{1}, []int64{2}), failure)
		})
	}
}

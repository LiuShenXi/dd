package service

import (
	"context"
	"errors"
	"fmt"
)

// InvalidateReleaseAuthCache is the synchronous release barrier counterpart of
// normal best-effort invalidation. Call only after request/usage quiescence.
func (s *APIKeyService) InvalidateReleaseAuthCache(ctx context.Context, userIDs, groupIDs []int64) error {
	if s == nil || s.apiKeyRepo == nil {
		return errors.New("API key repository unavailable")
	}
	keys := make(map[string]struct{})
	for _, id := range userIDs {
		values, err := s.apiKeyRepo.ListKeysByUserID(ctx, id)
		if err != nil {
			return fmt.Errorf("list release user keys: %w", err)
		}
		for _, key := range values {
			if key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	for _, id := range groupIDs {
		values, err := s.apiKeyRepo.ListKeysByGroupID(ctx, id)
		if err != nil {
			return fmt.Errorf("list release group keys: %w", err)
		}
		for _, key := range values {
			if key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	for key := range keys {
		cacheKey := s.authCacheKey(key)
		if s.authCacheL1 != nil {
			s.authCacheL1.Del(cacheKey)
		}
		if s.authNegativeCacheL1 != nil {
			s.authNegativeCacheL1.Del(cacheKey)
		}
		if s.cache != nil {
			if err := s.cache.DeleteAuthCache(ctx, cacheKey); err != nil {
				return fmt.Errorf("delete release auth cache: %w", err)
			}
			if err := s.cache.PublishAuthCacheInvalidation(ctx, cacheKey); err != nil {
				return fmt.Errorf("publish release auth invalidation: %w", err)
			}
		}
	}
	// Ristretto operations use a buffered worker; wait before allowing new lookups.
	if s.authCacheL1 != nil {
		s.authCacheL1.Wait()
	}
	if s.authNegativeCacheL1 != nil {
		s.authNegativeCacheL1.Wait()
	}
	return ctx.Err()
}

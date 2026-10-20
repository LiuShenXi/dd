package service

import (
	"context"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type protectedConcurrencyContextKey struct{}
type protectedConcurrencyState struct {
	request                *SubscriptionQuotaProtectedRequest
	userID, keyID, groupID int64
	mu                     sync.Mutex
	attempted              bool
}

// Only protected legacy groups take this durable barrier. Conversion must hold
// the group lock and require zero unfinished records. It includes queue waiting.
func (s *BillingCacheService) BeginProtectedUserConcurrency(ctx context.Context, key *APIKey) (context.Context, func(), error) {
	noop := func() {}
	if key != nil && key.Group != nil && key.Group.HasTermQuotaMode() {
		return ctx, noop, nil
	}
	request, err := s.AdmitSubscriptionQuotaProtectedGroup(ctx, key, key, PlatformAnthropic)
	if err != nil {
		return ctx, noop, err
	}
	if request == nil {
		return ctx, noop, nil
	}
	state := &protectedConcurrencyState{request: request, userID: key.UserID, keyID: key.ID, groupID: protectedGroupID(key)}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			state.mu.Lock()
			attempted := state.attempted
			state.mu.Unlock()
			if !attempted {
				err := request.persist(func(cleanup context.Context) error {
					return request.repo.FinishProtectedGroup(cleanup, request.id, "no_charge", "no_upstream_attempt_before_user_scope_exit")
				})
				if err != nil {
					logger.LegacyPrintf("service.concurrency", "protected no-send cleanup remains held group_id=%d error=%v", state.groupID, err)
				}
			}
		})
	}
	return context.WithValue(ctx, protectedConcurrencyContextKey{}, state), finish, nil
}

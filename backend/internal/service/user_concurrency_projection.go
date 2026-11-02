package service

import (
	"context"
	"strconv"
	"sync"
)

func ProvideUserService(userRepo UserRepository, settingRepo SettingRepository, invalidator APIKeyAuthCacheInvalidator, billing BillingCache, concurrency *ConcurrencyService) *UserService {
	s := NewUserService(userRepo, settingRepo, invalidator, billing)
	s.claudeConcurrency = concurrency
	return s
}

func cloneConcurrencyOverride(value *int) *int {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func (s *ConcurrencyService) ProjectClaudeConcurrencyUsers(ctx context.Context, users []User) {
	ctx, cancel := context.WithTimeout(ctx, ClaudeConcurrencyOperationTimeout)
	defer cancel()
	var wg sync.WaitGroup
	work := make(chan int)
	for worker := 0; worker < min(8, len(users)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				users[i].ClaudeConcurrencyView = s.ClaudeConcurrencyView(ctx, &users[i])
			}
		}()
	}
	for i := range users {
		work <- i
	}
	close(work)
	wg.Wait()
}

// A missing Redis reading is unknown, never an observed zero.
type ClaudeConcurrencyView struct {
	LegacyCurrentConcurrency               *int   `json:"legacy_current_concurrency"`
	LegacyConcurrencyStatus                string `json:"legacy_concurrency_status"`
	DefaultClaudeSubscriptionConcurrency   int    `json:"default_claude_subscription_concurrency"`
	EffectiveClaudeSubscriptionConcurrency *int   `json:"effective_claude_subscription_concurrency"`
	ClaudeCurrentConcurrency               *int   `json:"claude_current_concurrency"`
	ClaudeWaitingCount                     *int   `json:"claude_waiting_count"`
	ClaudeConcurrencyStatus                string `json:"claude_concurrency_status"`
}

func claudeConcurrencyAuditValue(value *int) string {
	if value == nil {
		return "default"
	}
	return strconv.Itoa(*value)
}

func (s *ConcurrencyService) ClaudeConcurrencyView(ctx context.Context, user *User) ClaudeConcurrencyView {
	out := ClaudeConcurrencyView{ClaudeConcurrencyStatus: "unavailable", LegacyConcurrencyStatus: "unavailable"}
	if s == nil || user == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, ClaudeConcurrencyOperationTimeout)
	defer cancel()
	var reads sync.WaitGroup
	if legacy, ok := s.cache.(BoundedLegacyUserConcurrencyReader); ok {
		reads.Add(1)
		go func() {
			defer reads.Done()
			count, err := legacy.GetLegacyUserConcurrencyBounded(ctx, user.ID)
			if err == nil {
				out.LegacyCurrentConcurrency = &count
				out.LegacyConcurrencyStatus = "available"
			}
		}()
	}
	finish := func() ClaudeConcurrencyView { reads.Wait(); return out }
	out.DefaultClaudeSubscriptionConcurrency = s.claudeDefault
	limit := s.claudeDefault
	if user.ClaudeSubscriptionConcurrency != nil {
		limit = *user.ClaudeSubscriptionConcurrency
	}
	if !ValidClaudeSubscriptionConcurrency(limit) {
		out.ClaudeConcurrencyStatus = "not_configured"
		return finish()
	}
	out.EffectiveClaudeSubscriptionConcurrency = &limit
	cache, ok := s.cache.(UserScopedConcurrencyCache)
	if !ok {
		return finish()
	}
	stats, err := cache.GetScopeStats(ctx, UserConcurrencyClaudeSubscription, user.ID)
	if err != nil {
		return finish()
	}
	out.ClaudeConcurrencyStatus = "available"
	out.ClaudeCurrentConcurrency = &stats.Active
	out.ClaudeWaitingCount = &stats.Waiting
	return finish()
}

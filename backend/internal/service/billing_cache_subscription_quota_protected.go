package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

func (s *BillingCacheService) SetSubscriptionQuotaProtectedGroups(gate *SubscriptionQuotaProtectedGroupService) {
	if s != nil {
		s.subscriptionQuotaProtectedGroups = gate
	}
}

func (s *BillingCacheService) SubscriptionQuotaProtectedGroups() *SubscriptionQuotaProtectedGroupService {
	if s == nil {
		return nil
	}
	return s.subscriptionQuotaProtectedGroups
}

func (s *BillingCacheService) AdmitSubscriptionQuotaProtectedGroup(ctx context.Context, original, current *APIKey, platform string) (*SubscriptionQuotaProtectedRequest, error) {
	if s == nil {
		return nil, nil
	}
	if s.subscriptionQuotaProtectedGroups != nil {
		if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple && (s.subscriptionQuotaProtectedGroups.Protects(protectedGroupID(original)) || s.subscriptionQuotaProtectedGroups.Protects(protectedGroupID(current))) {
			return nil, ErrSubscriptionQuotaProtectedUnavailable
		}
		return s.subscriptionQuotaProtectedGroups.Admit(ctx, original, current, platform)
	}
	if s.cfg != nil {
		for _, id := range s.cfg.SubscriptionQuota.ProtectedGroupIDs {
			if id == protectedGroupID(original) || id == protectedGroupID(current) {
				return nil, ErrSubscriptionQuotaProtectedUnavailable
			}
		}
	}
	return nil, nil
}

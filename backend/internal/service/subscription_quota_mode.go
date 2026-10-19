package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"net/http"
)

var (
	ErrSubscriptionQuotaBillingUnavailable    = infraerrors.New(http.StatusServiceUnavailable, "TERM_BILLING_UNAVAILABLE", "Claude subscription quota billing is unavailable")
	ErrLegacySubscriptionOperationUnsupported = infraerrors.Conflict("LEGACY_SUBSCRIPTION_OPERATION_UNSUPPORTED", "use the Claude quota card operation for this subscription")
	ErrSubscriptionQuotaGroupInvalid          = infraerrors.BadRequest("TERM_GROUP_INVALID", "term quota requires an Anthropic subscription group without fallback")
)

func ValidateSubscriptionQuotaMode(group *Group) error {
	if group == nil {
		return nil
	}
	switch group.EffectiveSubscriptionQuotaMode() {
	case domain.SubscriptionQuotaModeWindows:
		return nil
	case domain.SubscriptionQuotaModeTerm:
		if group.IsTermQuota() && group.FallbackGroupID == nil && group.FallbackGroupIDOnInvalidRequest == nil {
			return nil
		}
	}
	return ErrSubscriptionQuotaGroupInvalid
}

func legacySubscriptionModeError(sub *UserSubscription) error {
	if sub != nil && sub.Group != nil && sub.Group.EffectiveSubscriptionQuotaMode() != domain.SubscriptionQuotaModeWindows {
		return ErrLegacySubscriptionOperationUnsupported
	}
	return nil
}

func (s *SubscriptionService) rejectLegacyQuotaGroup(ctx context.Context, groupID int64) error {
	if s == nil || s.groupRepo == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return err
	}
	if group == nil || group.EffectiveSubscriptionQuotaMode() != domain.SubscriptionQuotaModeWindows {
		return ErrLegacySubscriptionOperationUnsupported
	}
	return nil
}

package service

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// This preflight applies even to force/deduct=false and pending-refund recovery.
// New quota orders cannot be refunded through a legacy days/balance operation.
func (s *PaymentService) rejectLegacyQuotaOrder(ctx context.Context, order *dbent.PaymentOrder) error {
	if order == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	if order.SubscriptionQuotaSnapshot != nil {
		return ErrLegacySubscriptionOperationUnsupported
	}
	if order.OrderType != payment.OrderTypeSubscription {
		return nil
	}
	if order.SubscriptionGroupID == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	if s.groupRepo != nil {
		group, err := s.groupRepo.GetByID(ctx, *order.SubscriptionGroupID)
		if err != nil {
			return err
		}
		if group == nil || group.EffectiveSubscriptionQuotaMode() != domain.SubscriptionQuotaModeWindows {
			return ErrLegacySubscriptionOperationUnsupported
		}
		return nil
	}
	if s.entClient == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	group, err := s.entClient.Group.Get(ctx, *order.SubscriptionGroupID)
	if err != nil {
		return err
	}
	if domain.NormalizeSubscriptionQuotaMode(group.SubscriptionQuotaMode) != domain.SubscriptionQuotaModeWindows {
		return ErrLegacySubscriptionOperationUnsupported
	}
	return nil
}

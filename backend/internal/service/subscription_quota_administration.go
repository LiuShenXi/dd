package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type SubscriptionQuotaRequestQueryInput struct {
	SubscriptionQuotaPageInput
	Status  string
	UserID  *int64
	GroupID *int64
}

type SubscriptionQuotaRequestReader interface {
	ListRequests(context.Context, domain.SubscriptionQuotaRequestQueryParams) (*domain.SubscriptionQuotaRequestPage, error)
}

func ValidSubscriptionQuotaRequestStatus(status string) bool {
	switch status {
	case "", "admitted", "usage_known", "settled", "no_charge", "reconcile_required":
		return true
	}
	return false
}

func (s *SubscriptionQuotaService) ListRequests(ctx context.Context, in SubscriptionQuotaRequestQueryInput) (*domain.SubscriptionQuotaRequestPage, error) {
	if !ValidSubscriptionQuotaRequestStatus(in.Status) || in.UserID != nil && *in.UserID <= 0 || in.GroupID != nil && *in.GroupID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	page, err := subscriptionQuotaPage(in.SubscriptionQuotaPageInput)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	reader, ok := s.repo.(SubscriptionQuotaRequestReader)
	if !ok {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	p := domain.SubscriptionQuotaRequestQueryParams{SubscriptionQuotaPageParams: page, Status: in.Status}
	if in.UserID != nil {
		p.UserID = *in.UserID
	}
	if in.GroupID != nil {
		p.GroupID = *in.GroupID
	}
	return reader.ListRequests(ctx, p)
}

func ProvideSubscriptionQuotaResolutionService(repo SubscriptionQuotaResolutionRepository, gateway *GatewayService) *SubscriptionQuotaResolutionService {
	return NewSubscriptionQuotaResolutionService(repo, gateway)
}

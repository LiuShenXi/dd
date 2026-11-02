package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type SubscriptionQuotaOpenInput struct {
	GroupID          int64
	Operation        string
	QuotaUSD         *string
	PlanID           *int64
	ExpectedRevision *int64
	Notes            string
}
type SubscriptionQuotaCancelInput struct {
	ExpectedRevision *int64
	Reason           string
}
type SubscriptionQuotaPageInput struct {
	Cursor string
	Limit  int
}

type SubscriptionQuotaEntitlementReader interface {
	HasCurrentTerm(context.Context, int64, int64) (bool, error)
	ListCurrentTermGroupIDs(context.Context, int64) ([]int64, error)
}
type SubscriptionQuotaRepository interface {
	SubscriptionQuotaEntitlementReader
	PreviewTerm(context.Context, domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaPreview, error)
	OpenTerm(context.Context, domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaMutationResult, error)
	CancelTerm(context.Context, domain.SubscriptionQuotaCancelParams) (*domain.SubscriptionQuotaMutationResult, error)
	GetSummary(context.Context, int64, int64) (*domain.SubscriptionQuotaSummary, error)
	ListSummaries(context.Context, int64) ([]domain.SubscriptionQuotaSummary, error)
	ListTerms(context.Context, int64, int64, domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaTermPage, error)
	ListUsage(context.Context, int64, int64, domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaUsagePage, error)
}

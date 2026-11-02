package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type SubscriptionQuotaResolveInput struct {
	ExpectedRequestRevision *int64                          `json:"expected_request_revision"`
	Resolution              string                          `json:"resolution"`
	EvidenceReference       string                          `json:"evidence_reference"`
	Reason                  string                          `json:"reason"`
	VerifiedUsage           *SubscriptionQuotaVerifiedUsage `json:"verified_usage,omitempty"`
}

type SubscriptionQuotaResolutionRepository interface {
	ResolveRequest(context.Context, domain.SubscriptionQuotaResolutionParams) (*domain.SubscriptionQuotaResolveResult, error)
}

type SubscriptionQuotaVerifiedCostCalculator interface {
	CalculateSubscriptionQuotaVerifiedCost(json.RawMessage, SubscriptionQuotaVerifiedUsage, time.Time) (*SubscriptionQuotaCalculatedCost, error)
}

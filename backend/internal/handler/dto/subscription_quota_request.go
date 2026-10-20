package dto

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"time"
)

type SubscriptionQuotaRequest struct {
	ID                int64      `json:"id"`
	RequestID         string     `json:"request_id"`
	UserID            int64      `json:"user_id"`
	GroupID           int64      `json:"group_id"`
	TermID            int64      `json:"term_id"`
	APIKeyID          int64      `json:"api_key_id"`
	Status            string     `json:"status"`
	Revision          int64      `json:"revision"`
	ActualCostUSD     *string    `json:"actual_cost_usd"`
	AdmittedAt        time.Time  `json:"admitted_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	SettledAt         *time.Time `json:"settled_at"`
	LastErrorCategory *string    `json:"last_error_category"`
}

type SubscriptionQuotaRequestPage struct {
	Items      []SubscriptionQuotaRequest `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
	HasMore    bool                       `json:"has_more"`
}

type SubscriptionQuotaResolveResult struct {
	Request  SubscriptionQuotaRequest `json:"request"`
	Replayed bool                     `json:"replayed"`
}

func SubscriptionQuotaRequestFromDomain(r domain.SubscriptionQuotaRequest) SubscriptionQuotaRequest {
	return SubscriptionQuotaRequest{r.ID, r.RequestID, r.UserID, r.GroupID, r.TermID, r.APIKeyID, r.Status, r.Revision, r.ActualCostUSD, r.AdmittedAt, r.UpdatedAt, r.SettledAt, r.LastErrorCategory}
}

func SubscriptionQuotaRequestPageFromDomain(p *domain.SubscriptionQuotaRequestPage) *SubscriptionQuotaRequestPage {
	if p == nil {
		return nil
	}
	out := &SubscriptionQuotaRequestPage{Items: []SubscriptionQuotaRequest{}, NextCursor: p.NextCursor, HasMore: p.HasMore}
	for _, r := range p.Items {
		out.Items = append(out.Items, SubscriptionQuotaRequestFromDomain(r))
	}
	return out
}

func SubscriptionQuotaResolveFromDomain(r *domain.SubscriptionQuotaResolveResult) *SubscriptionQuotaResolveResult {
	if r == nil {
		return nil
	}
	return &SubscriptionQuotaResolveResult{Request: SubscriptionQuotaRequestFromDomain(r.Request), Replayed: r.Replayed}
}

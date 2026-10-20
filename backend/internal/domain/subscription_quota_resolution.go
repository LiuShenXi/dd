package domain

import (
	"encoding/json"
	"time"
)

// This projection is administrator-only. It contains no pricing or settlement
// command, and its accepted state is not a claim that settlement has finished.
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

type SubscriptionQuotaResolveResult struct {
	Request  SubscriptionQuotaRequest `json:"request"`
	Replayed bool                     `json:"replayed"`
}

type SubscriptionQuotaRequestPage struct {
	Items      []SubscriptionQuotaRequest `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
	HasMore    bool                       `json:"has_more"`
}

type SubscriptionQuotaRequestQueryParams struct {
	SubscriptionQuotaPageParams
	Status  string
	UserID  int64
	GroupID int64
}

// PrepareKnown is an internal pure callback, invoked only after the repository
// has serialized and rechecked the unknown request. It is never an HTTP field.
type SubscriptionQuotaResolutionParams struct {
	RequestRowID            int64
	ActorID                 int64
	ExpectedRequestRevision int64
	Resolution              string
	EvidenceReference       string
	Reason                  string
	IdempotencyKey          string
	NormalizedIntent        json.RawMessage
	PrepareKnown            func(SubscriptionQuotaKnownUsage) (SubscriptionQuotaKnownUsage, error)
}

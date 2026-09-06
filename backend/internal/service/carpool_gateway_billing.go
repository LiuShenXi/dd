package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
)

var (
	ErrCarpoolBillingUnavailable = errors.New("carpool billing service is unavailable")
	ErrCarpoolAdmissionRequired  = errors.New("carpool request has no final admission snapshot")
	ErrCarpoolUsageUnknown       = errors.New("carpool usage is missing or ambiguous")
)

// CarpoolGatewayBilling is the durable boundary used by gateway admission and
// settlement. Implementations must persist Admit before upstream forwarding.
type CarpoolGatewayBilling interface {
	Admit(ctx context.Context, userID, apiKeyID, groupID int64, requestID string, admittedAt time.Time) (*domain.CarpoolBillingSnapshot, error)
	PersistKnownUsage(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot, cost decimal.Decimal, payload json.RawMessage) error
	RecoverPendingReceipts(ctx context.Context, limit int) ([]domain.CarpoolKnownUsage, error)
	MarkReconcileRequired(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot, reason string) error
}

type carpoolBillingSnapshotContextKey struct{}

func ContextWithCarpoolBillingSnapshot(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil {
		return ctx
	}
	copySnapshot := *snapshot
	return context.WithValue(ctx, carpoolBillingSnapshotContextKey{}, &copySnapshot)
}

func CarpoolBillingSnapshotFromContext(ctx context.Context) (*domain.CarpoolBillingSnapshot, bool) {
	if ctx == nil {
		return nil, false
	}
	snapshot, ok := ctx.Value(carpoolBillingSnapshotContextKey{}).(*domain.CarpoolBillingSnapshot)
	if !ok || snapshot == nil {
		return nil, false
	}
	copySnapshot := *snapshot
	return &copySnapshot, true
}

// NewCarpoolAdmissionRequestID creates one identity per actual HTTP request or
// WebSocket turn. Client-provided request IDs are intentionally excluded: a
// later request must never replay a settled intent and reach upstream for free.
func NewCarpoolAdmissionRequestID() string {
	return "carpool:" + generateRequestID()
}

func (s *BillingCacheService) SetCarpoolGatewayBilling(billing CarpoolGatewayBilling) {
	if s != nil {
		s.carpoolGatewayBilling = billing
	}
}

func (s *BillingCacheService) CarpoolGatewayBilling() CarpoolGatewayBilling {
	if s == nil {
		return nil
	}
	return s.carpoolGatewayBilling
}

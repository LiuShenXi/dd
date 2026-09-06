//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type carpoolGatewayBillingStub struct {
	admitInputAt   time.Time
	returnedAt     time.Time
	nilSnapshot    bool
	marked         int
	markedReason   string
	markedSnapshot *domain.CarpoolBillingSnapshot
}

func (s *carpoolGatewayBillingStub) Admit(_ context.Context, userID, apiKeyID, groupID int64, requestID string, admittedAt time.Time) (*domain.CarpoolBillingSnapshot, error) {
	s.admitInputAt = admittedAt
	if s.nilSnapshot {
		return nil, nil
	}
	return &domain.CarpoolBillingSnapshot{
		BillingRequestID: 91,
		RequestID:        requestID,
		UserID:           userID,
		APIKeyID:         apiKeyID,
		GroupID:          groupID,
		TermID:           41,
		CycleID:          42,
		AdmittedAt:       s.returnedAt,
	}, nil
}

func TestAdmitCarpoolTurnRejectsNilSnapshot(t *testing.T) {
	billing := &carpoolGatewayBillingStub{nilSnapshot: true}
	cache := &service.BillingCacheService{}
	cache.SetCarpoolGatewayBilling(billing)
	h := &OpenAIGatewayHandler{billingCacheService: cache}
	apiKey := &service.APIKey{ID: 12, UserID: 11, Group: &service.Group{
		ID: 13, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeCarpool,
	}}

	ctx, snapshot, err := h.admitCarpoolTurn(context.Background(), apiKey)
	require.ErrorIs(t, err, service.ErrCarpoolBillingUnavailable)
	require.Nil(t, snapshot)
	_, ok := service.CarpoolBillingSnapshotFromContext(ctx)
	require.False(t, ok)
}

func (*carpoolGatewayBillingStub) PersistKnownUsage(context.Context, *domain.CarpoolBillingSnapshot, decimal.Decimal, json.RawMessage) error {
	return nil
}

func (*carpoolGatewayBillingStub) RecoverPendingReceipts(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
	return nil, nil
}

func (s *carpoolGatewayBillingStub) MarkReconcileRequired(_ context.Context, snapshot *domain.CarpoolBillingSnapshot, reason string) error {
	s.marked++
	s.markedReason = reason
	s.markedSnapshot = snapshot
	return nil
}

func TestAdmitCarpoolTurnUsesCanonicalReturnedAdmissionTime(t *testing.T) {
	canonical := time.Date(2026, 9, 6, 14, 15, 16, 987654000, time.UTC)
	billing := &carpoolGatewayBillingStub{returnedAt: canonical}
	cache := &service.BillingCacheService{}
	cache.SetCarpoolGatewayBilling(billing)
	h := &OpenAIGatewayHandler{billingCacheService: cache}
	apiKey := &service.APIKey{
		ID:     12,
		UserID: 11,
		Group: &service.Group{
			ID:               13,
			Platform:         service.PlatformOpenAI,
			SubscriptionType: service.SubscriptionTypeCarpool,
		},
	}

	ctx, snapshot, err := h.admitCarpoolTurn(context.Background(), apiKey)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, canonical, snapshot.AdmittedAt)
	require.NotEqual(t, canonical, billing.admitInputAt,
		"the service return, not the nanosecond request timestamp, is authoritative")
	fromContext, ok := service.CarpoolBillingSnapshotFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, canonical, fromContext.AdmittedAt)
}

func TestCarpoolHTTPAdmissionStateMarksOnlyUnknownTerminalUsage(t *testing.T) {
	canonical := time.Date(2026, 9, 6, 14, 15, 16, 987654000, time.UTC)
	billing := &carpoolGatewayBillingStub{returnedAt: canonical}
	cache := &service.BillingCacheService{}
	cache.SetCarpoolGatewayBilling(billing)
	h := &OpenAIGatewayHandler{billingCacheService: cache}
	apiKey := &service.APIKey{ID: 12, UserID: 11, Group: &service.Group{
		ID: 13, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeCarpool,
	}}

	t.Run("terminal failure after admission marks reconciliation once", func(t *testing.T) {
		c := newTestGinContext()
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		require.True(t, h.admitCarpoolForForward(c, apiKey))
		h.reconcileCarpoolHTTPUsage(c, "terminal failure")
		h.reconcileCarpoolHTTPUsage(c, "duplicate")
		require.Equal(t, 1, billing.marked)
		require.Equal(t, "terminal failure", billing.markedReason)
		require.Equal(t, canonical, billing.markedSnapshot.AdmittedAt)
	})

	t.Run("durable known usage suppresses terminal reconciliation", func(t *testing.T) {
		billing.marked = 0
		c := newTestGinContext()
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		require.True(t, h.admitCarpoolForForward(c, apiKey))
		h.recordCarpoolHTTPUsageResult(c, c.Request.Context(), nil, "unused")
		h.reconcileCarpoolHTTPUsage(c, "terminal failure")
		require.Zero(t, billing.marked)
	})

	t.Run("synchronous usage failure marks reconciliation once", func(t *testing.T) {
		billing.marked = 0
		c := newTestGinContext()
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		require.True(t, h.admitCarpoolForForward(c, apiKey))
		h.recordCarpoolHTTPUsageResult(c, c.Request.Context(), errors.New("settlement failed"), "usage failed")
		h.reconcileCarpoolHTTPUsage(c, "terminal duplicate")
		require.Equal(t, 1, billing.marked)
		require.Equal(t, "usage failed", billing.markedReason)
	})
}

func TestCarpoolWSTurnAdmissionStoreRemapsLogicalTurnAcrossAdapterRetry(t *testing.T) {
	store := &carpoolWSTurnAdmissionStore{}
	original := &domain.CarpoolBillingSnapshot{
		BillingRequestID: 91,
		RequestID:        "carpool:logical-turn",
		TermID:           41,
		CycleID:          42,
		AdmittedAt:       time.Date(2026, 9, 6, 14, 15, 16, 987654000, time.UTC),
	}
	store.store(3, original)

	store.remapLatestTo(1)
	got, ok := store.load(1)
	require.True(t, ok)
	require.Equal(t, original, got)
	_, oldExists := store.load(3)
	require.False(t, oldExists)

	got.RequestID = "mutated-copy"
	storedAgain, ok := store.load(1)
	require.True(t, ok)
	require.Equal(t, original.RequestID, storedAgain.RequestID)

	drained := store.drain()
	require.Len(t, drained, 1)
	require.Equal(t, original.RequestID, drained[0].RequestID)
	_, remains := store.load(1)
	require.False(t, remains)
}

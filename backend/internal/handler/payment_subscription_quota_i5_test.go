package handler

import (
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPaymentOrderResponseFrozenQuotaI5(t *testing.T) {
	snapshot := domain.SubscriptionQuotaOrderSnapshot{SchemaVersion: 1, Mode: "term", GroupID: 7, Platform: "anthropic", DurationSeconds: 604800, QuotaUSD: "125.12345678", SourcePlanID: 9, PlanName: "Original", GroupName: "Claude"}
	order := &dbent.PaymentOrder{SubscriptionQuotaSnapshot: snapshot.Map(), ProviderSnapshot: map[string]any{"merchant_id": "private"}}
	result := sanitizePaymentOrderForResponse(order)
	require.Equal(t, &snapshot, result.SubscriptionQuotaSnapshot)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"quota_usd":"125.12345678"`)
	require.NotContains(t, string(encoded), "merchant_id")
	order.SubscriptionQuotaSnapshot["secret"] = "private"
	result = sanitizePaymentOrderForResponse(order)
	require.Nil(t, result.SubscriptionQuotaSnapshot)
	encoded, err = json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

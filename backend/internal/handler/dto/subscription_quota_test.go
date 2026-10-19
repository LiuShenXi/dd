package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaDTOPrivacyAndRequiredFields(t *testing.T) {
	term := domain.SubscriptionQuotaTerm{ID: 1, UserID: 99, RawOverageUSD: "200.00000000", ReconcileRequiredCount: 4, QuotaUSD: "1.00000000", RemainingUSD: "0.00000000"}
	summary := SubscriptionQuotaSummaryFromDomain(&domain.SubscriptionQuotaSummary{CurrentTerm: &term, LastTerm: &term})
	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &data))
	require.Len(t, data, 7)
	require.Equal(t, "[]", string(data["next_terms"]))
	var card map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data["current_term"], &card))
	require.Len(t, card, 15)
	require.Equal(t, "null", string(card["cancelled_at"]))
	require.NotContains(t, card, "user_id")
	require.NotContains(t, card, "raw_overage_usd")
	require.NotContains(t, card, "reconcile_required_count")
	require.Equal(t, `"0.00000000"`, string(card["remaining_usd"]))
}

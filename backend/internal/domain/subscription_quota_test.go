package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaMoneyContract(t *testing.T) {
	for input, want := range map[string]string{"1": "1.00000000", "0.00000001": "0.00000001", "999999999999.99999999": "999999999999.99999999", "100.12345678": "100.12345678"} {
		got, err := NormalizeSubscriptionQuotaMoney(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got)
	}
	for _, input := range []string{"", "0", "0.00000000", "-1", "+1", "NaN", "Infinity", "1e2", " 1", "1 ", "01", "0001", "0000000000000000000000000", "1.", ".1", "1.000000001", "1000000000000"} {
		_, err := NormalizeSubscriptionQuotaMoney(input)
		require.Error(t, err, input)
	}
}
func TestSubscriptionQuotaProjectionBoundaries(t *testing.T) {
	start := time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC)
	term := SubscriptionQuotaTerm{StartsAt: start, ExpiresAt: start.Add(SubscriptionQuotaDuration), QuotaUSD: "100", UsedUSD: "20", PendingUSD: "5", LifecycleState: "active"}
	for _, tc := range []struct {
		at                time.Time
		status, remaining string
	}{{start.Add(-time.Nanosecond), "scheduled", "0.00000000"}, {start, "active", "75.00000000"}, {term.ExpiresAt.Add(-time.Nanosecond), "active", "75.00000000"}, {term.ExpiresAt, "expired", "0.00000000"}, {term.ExpiresAt.Add(time.Second), "expired", "0.00000000"}} {
		got := ProjectSubscriptionQuotaTerm(term, tc.at)
		require.Equal(t, tc.status, got.Status)
		require.Equal(t, tc.remaining, got.RemainingUSD)
	}
	term.UsedUSD = "110"
	got := ProjectSubscriptionQuotaTerm(term, start)
	require.Equal(t, "exhausted", got.Status)
	require.Equal(t, "110.00000000", got.UsedUSD)
	require.Equal(t, "10.00000000", got.RawOverageUSD)
	term.ReconcileRequiredCount = 1
	require.Equal(t, "reconciling", ProjectSubscriptionQuotaTerm(term, start).Status)
	term.CancelledAt = &start
	got = ProjectSubscriptionQuotaTerm(term, start)
	require.Equal(t, "cancelled", got.Status)
	require.Equal(t, "0.00000000", got.RemainingUSD)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(data), "raw_overage")
	require.NotContains(t, string(data), "user_id")
	require.NotContains(t, string(data), "reconcile_required_count")
}
func TestSubscriptionQuotaCursorContract(t *testing.T) {
	for _, id := range []int64{1, 10, 9223372036854775807} {
		got, err := DecodeSubscriptionQuotaCursor(EncodeSubscriptionQuotaCursor(id))
		require.NoError(t, err)
		require.Equal(t, id, got)
	}
	for _, cursor := range []string{"!", "MA", "LTE", "MDE", "MQ==", "OTIyMzM3MjAzNjg1NDc3NTgwOA"} {
		_, err := DecodeSubscriptionQuotaCursor(cursor)
		require.Error(t, err, cursor)
	}
}

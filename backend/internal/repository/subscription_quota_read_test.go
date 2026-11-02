//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaSummaryLastTermUsesActualEnd(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cancelled := start.Add(24 * time.Hour)
	terms := []domain.SubscriptionQuotaTerm{
		{ID: 1, TermNo: 1, StartsAt: start, ExpiresAt: start.Add(168 * time.Hour), QuotaUSD: "100", UsedUSD: "20", PendingUSD: "0"},
		{ID: 2, TermNo: 2, StartsAt: start.Add(168 * time.Hour), ExpiresAt: start.Add(336 * time.Hour), CancelledAt: &cancelled, QuotaUSD: "100", UsedUSD: "0", PendingUSD: "0"},
	}
	summary := subscriptionQuotaSummary(3, 4, terms, start.Add(8*24*time.Hour))
	require.Nil(t, summary.CurrentTerm)
	require.Empty(t, summary.NextTerms)
	require.NotNil(t, summary.LastTerm)
	require.Equal(t, int64(1), summary.LastTerm.ID)
	require.Equal(t, "expired", summary.LastTerm.Status)
	// A later cancellation becomes the latest actual end; identical timestamps
	// have a stable ID tie-breaker independently of term number.
	later := start.Add(9 * 24 * time.Hour)
	terms[1].CancelledAt = &later
	summary = subscriptionQuotaSummary(3, 5, terms, later)
	require.Equal(t, int64(2), summary.LastTerm.ID)
	tie := terms[0].ExpiresAt
	terms[1].CancelledAt = &tie
	summary = subscriptionQuotaSummary(3, 6, terms, start.Add(10*24*time.Hour))
	require.Equal(t, int64(2), summary.LastTerm.ID)
}

//go:build unit

package service

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionQuotaRolloutExplicitAllocations(t *testing.T) {
	_, err := NormalizeSubscriptionQuotaAllocations(nil)
	require.ErrorIs(t, err, ErrSubscriptionQuotaInput)
	empty, err := NormalizeSubscriptionQuotaAllocations([]domain.SubscriptionQuotaAllocation{})
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
	original := []domain.SubscriptionQuotaAllocation{{UserID: 9, QuotaUSD: "300"}, {UserID: 2, QuotaUSD: "100.50"}}
	out, err := NormalizeSubscriptionQuotaAllocations(original)
	require.NoError(t, err)
	require.Equal(t, []domain.SubscriptionQuotaAllocation{{UserID: 2, QuotaUSD: "100.50000000"}, {UserID: 9, QuotaUSD: "300.00000000"}}, out)
	require.EqualValues(t, 9, original[0].UserID)
	_, err = NormalizeSubscriptionQuotaAllocations([]domain.SubscriptionQuotaAllocation{{UserID: 2, QuotaUSD: "100"}, {UserID: 2, QuotaUSD: "300"}})
	require.ErrorIs(t, err, ErrSubscriptionQuotaInput)
	for _, q := range []string{"0", "-1", "1e2", " 1", "NaN", "0.000000001"} {
		_, err = NormalizeSubscriptionQuotaAllocations([]domain.SubscriptionQuotaAllocation{{UserID: 2, QuotaUSD: q}})
		require.ErrorIs(t, err, ErrSubscriptionQuotaInvalid)
	}
}

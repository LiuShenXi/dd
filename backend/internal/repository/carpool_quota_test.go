package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolBaseRemainingPercent(t *testing.T) {
	tests := []struct {
		name      string
		quota     string
		base      string
		available string
		want      *string
	}{
		{name: "full", quota: "550", base: "550", available: "550", want: stringPointer("100")},
		{name: "half", quota: "550", base: "275", available: "300", want: stringPointer("50")},
		{name: "total debt limits visible base", quota: "550", base: "275", available: "200", want: stringPointer("36.36363636")},
		{name: "negative base", quota: "550", base: "-1", available: "20", want: stringPointer("0")},
		{name: "overfull capped", quota: "550", base: "600", available: "600", want: stringPointer("100")},
		{name: "invalid denominator", quota: "0", base: "10", available: "10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := carpoolBaseRemainingPercent(domain.CarpoolCycle{
				BaseQuotaUSD: decimal.RequireFromString(tt.quota), BaseBalanceUSD: decimal.RequireFromString(tt.base),
			}, decimal.RequireFromString(tt.available))
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, *tt.want, got.String())
		})
	}
}

func stringPointer(value string) *string { return &value }

//go:build unit

package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaLegacyDTOOmitsTermWindowsPreservesAdminAndUnavailable(t *testing.T) {
	now := time.Now()
	actor := int64(9)
	sub := &service.UserSubscription{ID: 1, UserID: 11, GroupID: 6, Group: &service.Group{ID: 6, SubscriptionQuotaMode: "term"}, DailyUsageUSD: 999, WeeklyUsageUSD: 999, MonthlyUsageUSD: 999, DailyWindowStart: &now, AssignedBy: &actor, Notes: "admin-only note", QuotaSummary: &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", NextTerms: []domain.SubscriptionQuotaTerm{}}}
	for _, admin := range []bool{false, true} {
		var value any = UserSubscriptionFromService(sub)
		if admin {
			value = UserSubscriptionFromServiceAdmin(sub)
		}
		data, err := json.Marshal(value)
		require.NoError(t, err)
		var object map[string]any
		require.NoError(t, json.Unmarshal(data, &object))
		require.Equal(t, "term", object["quota_mode"])
		for _, key := range []string{"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd", "daily_window_start", "weekly_window_start", "monthly_window_start"} {
			require.NotContains(t, object, key)
		}
		if admin {
			require.Equal(t, "admin-only note", object["notes"])
			require.Equal(t, float64(9), object["assigned_by"])
		} else {
			require.NotContains(t, object, "notes")
			require.NotContains(t, object, "assigned_by")
		}
	}
	sub.QuotaSummary = nil
	data, err := json.Marshal(UserSubscriptionFromService(sub))
	require.NoError(t, err)
	require.Contains(t, string(data), `"quota_summary":null`)
	require.Contains(t, string(data), `"quota_mode":"term"`)
}

func TestSubscriptionQuotaLegacyDTOWindowsRetainsOriginalZeroFields(t *testing.T) {
	data, err := json.Marshal(UserSubscriptionFromService(&service.UserSubscription{ID: 1, WeeklyUsageUSD: 5}))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "windows", out["quota_mode"])
	require.Equal(t, float64(0), out["daily_usage_usd"])
	require.Equal(t, float64(5), out["weekly_usage_usd"])
	require.Contains(t, out, "daily_window_start")
	require.Nil(t, out["quota_summary"])
}

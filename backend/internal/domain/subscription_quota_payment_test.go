package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionQuotaOrderSnapshotStrict(t *testing.T) {
	snapshot := SubscriptionQuotaOrderSnapshot{SchemaVersion: 1, Mode: "term", GroupID: 123, Platform: "anthropic", DurationSeconds: 604800, QuotaUSD: "123.45678901", SourcePlanID: 321, PlanName: "Claude Weekly", GroupName: "Claude", ProductName: "Week"}
	parsed, err := ParseSubscriptionQuotaOrderSnapshot(snapshot.Map())
	require.NoError(t, err)
	require.Equal(t, snapshot, *parsed)
	legacy, err := ParseSubscriptionQuotaOrderSnapshot(nil)
	require.NoError(t, err)
	require.Nil(t, legacy)
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"empty", func(m map[string]any) {
			for k := range m {
				delete(m, k)
			}
		}},
		{"future_version", func(m map[string]any) { m["schema_version"] = 2 }},
		{"fractional_id", func(m map[string]any) { m["group_id"] = 123.5 }},
		{"wrong_platform", func(m map[string]any) { m["platform"] = "openai" }},
		{"non_week", func(m map[string]any) { m["duration_seconds"] = 604799 }},
		{"numeric_money", func(m map[string]any) { m["quota_usd"] = 123.4 }},
		{"noncanonical_money", func(m map[string]any) { m["quota_usd"] = "123.4" }},
		{"zero_money", func(m map[string]any) { m["quota_usd"] = "0.00000000" }},
		{"missing_plan", func(m map[string]any) { delete(m, "source_plan_id") }},
		{"unknown_secret", func(m map[string]any) { m["api_key"] = "must-never-be-projected" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := snapshot.Map()
			tc.change(raw)
			result, err := ParseSubscriptionQuotaOrderSnapshot(raw)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

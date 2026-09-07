package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolPlanSnapshotQuotasPreserveLegacyThirtyDayTerms(t *testing.T) {
	tests := []struct {
		weekly string
		cycle5 string
		boost  string
		total  string
	}{
		{weekly: "550", cycle5: "157", boost: "55", total: "2357"},
		{weekly: "700", cycle5: "200", boost: "70", total: "3000"},
		{weekly: "1100", cycle5: "314", boost: "110", total: "4714"},
	}
	for _, tt := range tests {
		plan := CarpoolPlan{WeeklyQuotaUSD: decimal.RequireFromString(tt.weekly), DurationDays: 30, CycleDays: 7, BoostRatio: decimal.RequireFromString("0.10"), BoostCount: 2}
		snapshot := plan.Snapshot()
		require.Equal(t, CarpoolResetModeFixed, snapshot.ResetMode)
		require.True(t, snapshot.Cycle5QuotaUSD.Equal(decimal.RequireFromString(tt.cycle5)))
		require.True(t, snapshot.BoostAmountUSD.Equal(decimal.RequireFromString(tt.boost)))
		cycles := BuildCarpoolCycles(time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC), snapshot)
		require.Len(t, cycles, 5)
		total := decimal.Zero
		for _, cycle := range cycles {
			total = total.Add(cycle.BaseQuotaUSD)
		}
		require.True(t, total.Equal(decimal.RequireFromString(tt.total)))
		require.Equal(t, 48*time.Hour, cycles[4].EndsAt.Sub(cycles[4].StartsAt))
	}
}

func TestCarpoolPlanSnapshotQuotasUseFourFullCyclesForTwentyEightDayTerms(t *testing.T) {
	tests := []struct {
		weekly string
		boost  string
		total  string
	}{
		{weekly: "550", boost: "55", total: "2200"},
		{weekly: "700", boost: "70", total: "2800"},
		{weekly: "1100", boost: "110", total: "4400"},
	}
	start := time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		plan := CarpoolPlan{WeeklyQuotaUSD: decimal.RequireFromString(tt.weekly), DurationDays: 28, CycleDays: 7, BoostRatio: decimal.RequireFromString("0.10"), BoostCount: 3}
		snapshot := plan.Snapshot()
		require.Equal(t, CarpoolResetModeRolling, snapshot.ResetMode)
		require.Equal(t, 3, snapshot.BoostCount)
		require.True(t, snapshot.BoostAmountUSD.Equal(decimal.RequireFromString(tt.boost)))
		cycles := BuildCarpoolCycles(start, snapshot)
		require.Len(t, cycles, 4)
		total := decimal.Zero
		for index, cycle := range cycles {
			require.Equal(t, index+1, cycle.CycleNo)
			require.Equal(t, 7*24*time.Hour, cycle.EndsAt.Sub(cycle.StartsAt))
			require.True(t, cycle.BaseQuotaUSD.Equal(plan.WeeklyQuotaUSD))
			total = total.Add(cycle.BaseQuotaUSD)
		}
		require.True(t, total.Equal(decimal.RequireFromString(tt.total)))
		require.Equal(t, start.Add(28*24*time.Hour), cycles[3].EndsAt)
	}
}

func TestBuildCarpoolOpeningCyclesRollingCreatesOnlyNeededPeriods(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	snapshot := CarpoolPlan{WeeklyQuotaUSD: decimal.NewFromInt(550), DurationDays: 28, CycleDays: 7, BoostRatio: decimal.NewFromFloat(0.1), BoostCount: 2}.Snapshot()

	initial := BuildCarpoolOpeningCycles(start, start, snapshot, nil)
	require.Len(t, initial, 1)
	require.Equal(t, start.Add(7*24*time.Hour), initial[0].EndsAt)

	catchup := BuildCarpoolOpeningCycles(start, start.Add(15*24*time.Hour), snapshot, nil)
	require.Len(t, catchup, 3)
	require.Equal(t, start.Add(14*24*time.Hour), catchup[2].StartsAt)
	require.Equal(t, start.Add(21*24*time.Hour), catchup[2].EndsAt)
	for _, cycle := range catchup {
		require.True(t, snapshot.WeeklyQuotaUSD.Equal(cycle.BaseQuotaUSD))
	}
}

func TestBuildCarpoolOpeningCyclesRollingClampsTakeoverDeadlineAtExpiry(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(27 * 24 * time.Hour)
	requested := now.Add(3 * 24 * time.Hour)
	snapshot := CarpoolPlan{WeeklyQuotaUSD: decimal.NewFromInt(550), DurationDays: 28, CycleDays: 7, BoostRatio: decimal.NewFromFloat(0.1), BoostCount: 2}.Snapshot()

	cycles := BuildCarpoolOpeningCycles(start, now, snapshot, &requested)
	require.Len(t, cycles, 4)
	require.Equal(t, start.Add(28*24*time.Hour), cycles[3].EndsAt)
	require.Nil(t, CarpoolNextNaturalResetAt(CarpoolTermActive, start.Add(28*24*time.Hour), now, []CarpoolCycle{{StartsAt: cycles[3].StartsAt, EndsAt: cycles[3].EndsAt, State: CarpoolCycleActive}}))
}

func TestCarpoolPlanSnapshotMissingResetModeIsFixed(t *testing.T) {
	snapshot := CarpoolPlanSnapshot{DurationDays: 28, CycleDays: 7}
	require.Equal(t, CarpoolResetModeFixed, snapshot.EffectiveResetMode())
	snapshot.NormalizeResetMode()
	require.Equal(t, CarpoolResetModeFixed, snapshot.ResetMode)
}

func TestBuildCarpoolCyclesUsesHalfOpenBoundaries(t *testing.T) {
	start := time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC)
	snapshot := CarpoolPlan{WeeklyQuotaUSD: decimal.NewFromInt(550), DurationDays: 30, CycleDays: 7, BoostRatio: decimal.NewFromFloat(0.1), BoostCount: 2}.Snapshot()
	cycles := BuildCarpoolCycles(start, snapshot)
	for index := 1; index < len(cycles); index++ {
		require.Equal(t, cycles[index-1].EndsAt, cycles[index].StartsAt)
	}
	require.Equal(t, start.Add(30*24*time.Hour), cycles[4].EndsAt)
}

func TestBuildCarpoolCyclesSupportsLegacyOneCycleWithoutBoosts(t *testing.T) {
	start := time.Date(2026, 9, 4, 13, 16, 25, 0, time.UTC)
	snapshot := CarpoolPlan{
		WeeklyQuotaUSD: decimal.NewFromInt(550),
		DurationDays:   7,
		CycleDays:      7,
		BoostRatio:     decimal.NewFromFloat(0.1),
		BoostCount:     0,
	}.Snapshot()

	cycles := BuildCarpoolCycles(start, snapshot)
	require.Len(t, cycles, 1)
	require.Equal(t, start, cycles[0].StartsAt)
	require.Equal(t, start.Add(7*24*time.Hour), cycles[0].EndsAt)
	require.True(t, cycles[0].BaseQuotaUSD.Equal(decimal.NewFromInt(550)))
	require.Zero(t, snapshot.BoostCount)
}

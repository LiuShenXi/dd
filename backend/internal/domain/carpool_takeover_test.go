package domain

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBuildCarpoolTakeoverCyclesUsesIndependentAnchorAndRegistrationExpiry(t *testing.T) {
	anchor := time.Date(2026, 9, 8, 9, 40, 16, 0, time.FixedZone("CST", 8*3600))
	registered := time.Date(2026, 9, 4, 21, 16, 25, 123456000, anchor.Location())
	at := anchor.Add(10 * time.Hour)
	for _, days := range []int{7, 28} {
		t.Run(strconv.Itoa(days)+"days", func(t *testing.T) {
			snapshot := CarpoolPlan{DurationDays: 28, CycleDays: 7, WeeklyQuotaUSD: decimal.NewFromInt(550)}.Snapshot()
			if days == 7 {
				snapshot = snapshot.WithDurationOverride(7)
			}
			cycles, err := BuildCarpoolTakeoverCycles(registered, at, snapshot, &CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor})
			require.NoError(t, err)
			require.Len(t, cycles, 1)
			require.Equal(t, 1, cycles[0].CycleNo)
			require.True(t, cycles[0].StartsAt.Equal(anchor))
			end := anchor.Add(7 * 24 * time.Hour)
			if days == 7 {
				end = registered.Add(7 * 24 * time.Hour)
			}
			require.True(t, cycles[0].EndsAt.Equal(end))
			require.True(t, decimal.NewFromInt(550).Equal(cycles[0].BaseQuotaUSD), "short remaining membership never truncates the imported quota target")
		})
	}
}

func TestBuildCarpoolTakeoverCyclesRejectsInvalidAnchorOrDeadline(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	at := start.Add(9 * 24 * time.Hour)
	snapshot := CarpoolPlan{DurationDays: 28, CycleDays: 7, WeeklyQuotaUSD: decimal.NewFromInt(550)}.Snapshot()
	for _, anchor := range []time.Time{start.Add(-time.Second), at.Add(time.Second), at.Add(-7 * 24 * time.Hour)} {
		_, err := BuildCarpoolTakeoverCycles(start, at, snapshot, &CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor})
		require.Error(t, err)
	}
	anchor := at.Add(-time.Hour)
	wrongDeadline := anchor.Add(6 * 24 * time.Hour)
	_, err := BuildCarpoolTakeoverCycles(start, at, snapshot, &CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor, NextNaturalResetAt: &wrongDeadline})
	require.Error(t, err)
	matchingDeadline := anchor.Add(7 * 24 * time.Hour)
	_, err = BuildCarpoolTakeoverCycles(start, at, snapshot, &CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor, NextNaturalResetAt: &matchingDeadline})
	require.NoError(t, err)
}

func TestBuildCarpoolTakeoverCyclesWithoutAnchorPreservesExistingBehavior(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	at := start.Add(15 * 24 * time.Hour)
	for _, days := range []int{7, 28, 30} {
		snapshot := CarpoolPlan{DurationDays: days, CycleDays: 7, WeeklyQuotaUSD: decimal.NewFromInt(550)}.Snapshot()
		actual, err := BuildCarpoolTakeoverCycles(start, at, snapshot, &CarpoolTakeoverInput{})
		require.NoError(t, err)
		require.Equal(t, BuildCarpoolOpeningCycles(start, at, snapshot, nil), actual)
	}
}

func TestCarpoolSnapshotOverridesSurviveJSONAndRenewUsingLatestRules(t *testing.T) {
	for _, quota := range []int64{450, 1000} {
		plan := CarpoolPlan{ID: 11, Code: "special", WeeklyQuotaUSD: decimal.NewFromInt(550), DurationDays: 28, CycleDays: 7, BoostRatio: decimal.RequireFromString("0.1"), BoostCount: 2}
		imported := plan.Snapshot().WithWeeklyQuotaOverride(decimal.NewFromInt(quota)).WithDurationOverride(7)
		payload, err := json.Marshal(imported)
		require.NoError(t, err)
		var previous CarpoolPlanSnapshot
		require.NoError(t, json.Unmarshal(payload, &previous))
		plan.Version = 2
		plan.BoostCount = 3
		plan.BoostRatio = decimal.RequireFromString("0.2")
		latest := plan.Snapshot()
		renewed := latest.WithRenewalOverrides(previous)
		require.Equal(t, 2, renewed.Version)
		require.Equal(t, 3, renewed.BoostCount)
		require.True(t, decimal.NewFromInt(quota).Equal(renewed.WeeklyQuotaUSD))
		require.True(t, decimal.NewFromInt(quota).Mul(plan.BoostRatio).Equal(renewed.BoostAmountUSD))
		require.Equal(t, 7, renewed.DurationDays)
		require.Equal(t, CarpoolResetModeFixed, renewed.ResetMode)
		require.Equal(t, latest, latest.WithRenewalOverrides(CarpoolPlanSnapshot{WeeklyQuotaUSD: decimal.NewFromInt(123), DurationDays: 30}), "unmarked legacy snapshots continue adopting latest defaults")
		require.False(t, latest.WeeklyQuotaCustomized)
		require.False(t, latest.DurationCustomized)
	}
}

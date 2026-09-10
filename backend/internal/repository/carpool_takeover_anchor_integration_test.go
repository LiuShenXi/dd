//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolTakeoverAnchorPreviewOpeningAndBalanceConservation(t *testing.T) {
	for _, days := range []int{7, 28} {
		t.Run(strconv.Itoa(days)+"days", func(t *testing.T) {
			ctx := context.Background()
			repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
			if days == 7 {
				planID = insertSyntheticCarpoolPlan(t, ctx, "anchor-week-"+uuid.NewString()[:8], 1, true)
				_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_plans SET duration_days=7 WHERE id=$1`, planID)
				require.NoError(t, err)
			}
			carpoolService := service.NewCarpoolService(repo)
			now := carpoolDatabaseNow(t, ctx)
			registered := now.Add(-4 * 24 * time.Hour)
			anchor := now.Add(-3 * time.Hour)
			opening := "900.12345678"
			_, err := integrationDB.ExecContext(ctx, `UPDATE users SET balance=$2,created_at=$3 WHERE id=$1`, userID, opening, registered)
			require.NoError(t, err)
			takeover := &domain.CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor, CurrentBaseBalanceUSD: decimal.RequireFromString(opening)}
			preview, err := carpoolService.Preview(ctx, userID, planID, &registered, "takeover", takeover)
			require.NoError(t, err)
			key := uuid.NewString()
			input := service.CarpoolOpenInput{PlanID: planID, GroupID: groupID, StartsAt: &registered, Mode: "takeover", Takeover: takeover}
			opened, err := carpoolService.Open(ctx, userID, userID, input, key)
			require.NoError(t, err)
			require.Len(t, opened.Cycles, 1)
			require.Len(t, preview.Cycles, 1)
			require.Equal(t, "import", preview.Cycles[0].InitialAction)
			require.Equal(t, preview.StartsAt, opened.StartsAt)
			require.Equal(t, preview.ExpiresAt, opened.ExpiresAt)
			require.Equal(t, registered.Add(time.Duration(days)*24*time.Hour), opened.ExpiresAt)
			require.Equal(t, anchor, opened.CurrentCycle.StartsAt)
			require.Equal(t, preview.Cycles[0].EndsAt, opened.CurrentCycle.EndsAt)
			require.True(t, decimal.RequireFromString(opening).Equal(opened.CurrentCycle.BaseBalanceUSD), "even above-plan remaining quota must not be truncated")
			require.False(t, opened.HistoryComplete)
			require.NotNil(t, opened.StatisticsSince)
			require.Zero(t, opened.BoostUsed)
			if days == 7 {
				require.Equal(t, opened.ExpiresAt, opened.CurrentCycle.EndsAt)
				require.Nil(t, opened.NextNaturalResetAt)
			} else {
				require.Equal(t, anchor.Add(7*24*time.Hour), opened.CurrentCycle.EndsAt)
			}
			replayed, err := carpoolService.Open(ctx, userID, userID, input, key)
			require.NoError(t, err)
			require.Equal(t, opened.ID, replayed.ID)
			var balance, ledgerNet string
			var ledgerCount, initialGrants int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&balance))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta_usd),0)::text,COUNT(*),COUNT(*) FILTER (WHERE event_type='cycle_initial') FROM carpool_ledger WHERE term_id=$1`, opened.ID).Scan(&ledgerNet, &ledgerCount, &initialGrants))
			require.True(t, decimal.RequireFromString(opening).Equal(decimal.RequireFromString(balance)))
			require.True(t, decimal.RequireFromString(opening).Equal(decimal.RequireFromString(ledgerNet)))
			require.Equal(t, 1, ledgerCount)
			require.Zero(t, initialGrants)
		})
	}
}

func TestCarpoolTakeoverAnchorPreviewAndOpeningRejectSameInvalidWindow(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	carpoolService := service.NewCarpoolService(repo)
	now := carpoolDatabaseNow(t, ctx)
	registered := now.Add(-10 * 24 * time.Hour)
	for _, anchor := range []time.Time{registered.Add(-time.Hour), now.Add(time.Hour), now.Add(-8 * 24 * time.Hour)} {
		takeover := &domain.CarpoolTakeoverInput{CurrentCycleStartsAt: &anchor}
		_, err := carpoolService.Preview(ctx, userID, planID, &registered, "takeover", takeover)
		require.ErrorIs(t, err, service.ErrCarpoolInvalidNaturalReset)
		_, err = carpoolService.Open(ctx, userID, userID, service.CarpoolOpenInput{PlanID: planID, GroupID: groupID, StartsAt: &registered, Mode: "takeover", Takeover: takeover}, uuid.NewString())
		require.ErrorIs(t, err, service.ErrCarpoolInvalidNaturalReset)
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_terms WHERE user_id=$1`, userID).Scan(&count))
	require.Zero(t, count)
}

func TestCarpoolCustomizedSnapshotDefaultRenewalAndExplicitPlanChoice(t *testing.T) {
	for _, quota := range []int64{450, 1000} {
		t.Run(strconv.FormatInt(quota, 10), func(t *testing.T) {
			ctx := context.Background()
			repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
			plan, err := repo.GetPreviewPlan(ctx, userID, planID)
			require.NoError(t, err)
			snapshot := plan.Snapshot().WithWeeklyQuotaOverride(decimal.NewFromInt(quota)).WithDurationOverride(7)
			rawSnapshot, err := json.Marshal(snapshot)
			require.NoError(t, err)
			start := carpoolDatabaseNow(t, ctx).Add(-2 * 24 * time.Hour)
			expiry := start.Add(7 * 24 * time.Hour)
			var sourceID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,boost_used,source_mode,history_complete,created_by) VALUES($1,1,$2,$3,$4::jsonb,$5,$6,'active',0,'takeover',false,$1) RETURNING id`, userID, groupID, planID, string(rawSnapshot), start, expiry).Scan(&sourceID))
			carpoolService := service.NewCarpoolService(repo)
			renewKey := uuid.NewString()
			preview, err := carpoolService.PreviewRenewal(ctx, userID, sourceID, 0)
			require.NoError(t, err)
			renewed, err := carpoolService.Renew(ctx, sourceID, userID, service.CarpoolRenewInput{}, renewKey)
			require.NoError(t, err)
			require.Equal(t, preview.StartsAt, renewed.StartsAt)
			require.Equal(t, preview.ExpiresAt, renewed.ExpiresAt)
			require.True(t, preview.Plan.WeeklyQuotaUSD.Equal(renewed.PlanSnapshot.WeeklyQuotaUSD))
			require.Equal(t, expiry, renewed.StartsAt)
			require.Equal(t, expiry.Add(7*24*time.Hour), renewed.ExpiresAt)
			require.True(t, renewed.PlanSnapshot.WeeklyQuotaCustomized)
			require.True(t, renewed.PlanSnapshot.DurationCustomized)
			require.True(t, decimal.NewFromInt(quota).Equal(renewed.PlanSnapshot.WeeklyQuotaUSD))
			require.True(t, decimal.NewFromInt(quota).Mul(plan.BoostRatio).Equal(renewed.PlanSnapshot.BoostAmountUSD))
			require.Len(t, renewed.Cycles, 1)
			require.True(t, decimal.NewFromInt(quota).Equal(renewed.Cycles[0].BaseQuotaUSD))
			replayed, err := carpoolService.Renew(ctx, sourceID, userID, service.CarpoolRenewInput{}, renewKey)
			require.NoError(t, err)
			require.Equal(t, renewed.ID, replayed.ID)
			explicit, err := carpoolService.Renew(ctx, renewed.ID, userID, service.CarpoolRenewInput{PlanID: planID}, uuid.NewString())
			require.NoError(t, err)
			require.Equal(t, renewed.ExpiresAt.Add(28*24*time.Hour), explicit.ExpiresAt)
			require.False(t, explicit.PlanSnapshot.WeeklyQuotaCustomized)
			require.False(t, explicit.PlanSnapshot.DurationCustomized)
			require.True(t, plan.WeeklyQuotaUSD.Equal(explicit.PlanSnapshot.WeeklyQuotaUSD))
			unchangedPlan, err := repo.GetPreviewPlan(ctx, userID, planID)
			require.NoError(t, err)
			require.Equal(t, plan, unchangedPlan)
		})
	}
}

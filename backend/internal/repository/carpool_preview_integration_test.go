//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolPreview_ValidatesTargetUserAndLatestEnabledPlan(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewCarpoolRepository(integrationDB)
	carpoolService := service.NewCarpoolService(repo)
	user := mustCreateUser(t, client, &service.User{
		Email:        "carpool-preview-" + uuid.NewString() + "@example.com",
		PasswordHash: "synthetic-hash",
	})

	plans, err := repo.ListPlans(ctx, true)
	require.NoError(t, err)
	require.NotEmpty(t, plans)
	preview, err := carpoolService.Preview(ctx, user.ID, plans[0].ID, nil, "new", nil)
	require.NoError(t, err)
	require.Equal(t, plans[0].ID, preview.Plan.PlanID)
	require.Equal(t, 28, preview.Plan.DurationDays)
	require.Equal(t, 2, preview.Plan.BoostCount)
	require.Equal(t, preview.StartsAt.Add(28*24*time.Hour), preview.ExpiresAt)
	require.Len(t, preview.Cycles, 4)
	for index, cycle := range preview.Cycles {
		require.Equal(t, index+1, cycle.CycleNo)
		require.Equal(t, 7*24*time.Hour, cycle.EndsAt.Sub(cycle.StartsAt))
		require.True(t, preview.Plan.WeeklyQuotaUSD.Equal(cycle.BaseQuotaUSD))
	}
	_, err = carpoolService.Preview(ctx, user.ID, plans[0].ID, nil, "new", &domain.CarpoolTakeoverInput{})
	require.Error(t, err)
	_, err = carpoolService.Preview(ctx, user.ID, plans[0].ID, nil, "takeover", &domain.CarpoolTakeoverInput{BoostUsed: preview.Plan.BoostCount + 1})
	require.Error(t, err)
	negativeHistoricalUsage := decimal.RequireFromString("-0.00000001")
	_, err = carpoolService.Preview(ctx, user.ID, plans[0].ID, nil, "takeover", &domain.CarpoolTakeoverInput{HistoricalUsedUSD: &negativeHistoricalUsage})
	require.Error(t, err)

	_, err = carpoolService.Preview(ctx, user.ID+9_000_000_000, plans[0].ID, nil, "new", nil)
	require.ErrorIs(t, err, service.ErrCarpoolNotFound)

	disabledID := insertSyntheticCarpoolPlan(t, ctx, "disabled-"+uuid.NewString()[:8], 1, false)
	_, err = carpoolService.Preview(ctx, user.ID, disabledID, nil, "new", nil)
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)

	code := "superseded-" + uuid.NewString()[:8]
	supersededID := insertSyntheticCarpoolPlan(t, ctx, code, 1, true)
	_ = insertSyntheticCarpoolPlan(t, ctx, code, 2, true)
	_, err = carpoolService.Preview(ctx, user.ID, supersededID, nil, "new", nil)
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)
}

func TestCarpoolPlanVersionAndRenewalUseFixedCurrentRules(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, _ := newSyntheticCarpoolTermDependencies(t)
	code := "rules-v14-" + uuid.NewString()[:8]
	legacyPlanID := insertSyntheticCarpoolPlan(t, ctx, code, 1, true)
	legacyTerm, legacyCycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:    userID,
		ScopeID:   domain.CarpoolGlobalScopeID,
		GroupID:   groupID,
		PlanID:    legacyPlanID,
		ActorID:   userID,
		Mode:      "new",
		Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	require.Len(t, legacyCycles, 5)
	require.Equal(t, 30, legacyTerm.PlanSnapshot.DurationDays)
	require.Equal(t, 2, legacyTerm.PlanSnapshot.BoostCount)

	carpoolService := service.NewCarpoolService(repo)
	currentPlan, err := carpoolService.CreatePlanVersion(ctx, legacyPlanID, userID, service.CarpoolPlanVersionInput{
		Name:           "Custom retained tier",
		ListPriceCNY:   decimal.RequireFromString("321.00"),
		WeeklyQuotaUSD: decimal.RequireFromString("654.00000000"),
		BoostRatio:     decimal.RequireFromString("0.10000000"),
		BoostCount:     3,
		Enabled:        true,
	}, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, legacyTerm.PlanSnapshot.Version+1, currentPlan.Version)
	require.Equal(t, 28, currentPlan.DurationDays)
	require.Equal(t, 7, currentPlan.CycleDays)
	require.Equal(t, 3, currentPlan.BoostCount)
	require.True(t, decimal.RequireFromString("0.10000000").Equal(currentPlan.BoostRatio))

	renewKey := uuid.NewString()
	renewed, err := carpoolService.Renew(ctx, legacyTerm.ID, userID, service.CarpoolRenewInput{}, renewKey)
	require.NoError(t, err)
	require.Equal(t, currentPlan.ID, renewed.PlanID)
	require.Equal(t, legacyTerm.ExpiresAt, renewed.StartsAt)
	require.Equal(t, renewed.StartsAt.Add(28*24*time.Hour), renewed.ExpiresAt)
	require.Equal(t, 28, renewed.PlanSnapshot.DurationDays)
	require.Equal(t, 3, renewed.PlanSnapshot.BoostCount)
	require.Len(t, renewed.Cycles, 4)

	storedLegacy, err := repo.GetTerm(ctx, legacyTerm.ID)
	require.NoError(t, err)
	require.Equal(t, legacyTerm.ExpiresAt, storedLegacy.ExpiresAt)
	require.Equal(t, 30, storedLegacy.PlanSnapshot.DurationDays)
	require.Equal(t, 2, storedLegacy.PlanSnapshot.BoostCount)
	storedLegacyCycles, err := repo.ListTermCycles(ctx, legacyTerm.ID)
	require.NoError(t, err)
	require.Len(t, storedLegacyCycles, 5)

	disabledPlan, err := carpoolService.CreatePlanVersion(ctx, currentPlan.ID, userID, service.CarpoolPlanVersionInput{
		Name:           currentPlan.Name,
		ListPriceCNY:   currentPlan.ListPriceCNY,
		WeeklyQuotaUSD: currentPlan.WeeklyQuotaUSD,
		BoostRatio:     currentPlan.BoostRatio,
		BoostCount:     currentPlan.BoostCount,
		Enabled:        false,
	}, uuid.NewString())
	require.NoError(t, err)
	require.False(t, disabledPlan.Enabled)
	replayed, err := carpoolService.Renew(ctx, legacyTerm.ID, userID, service.CarpoolRenewInput{}, renewKey)
	require.NoError(t, err)
	renewedJSON, err := json.Marshal(renewed)
	require.NoError(t, err)
	replayedJSON, err := json.Marshal(replayed)
	require.NoError(t, err)
	require.JSONEq(t, string(renewedJSON), string(replayedJSON))
	require.Equal(t, string(renewedJSON), string(replayedJSON), "replay must preserve the exact response representation")
	_, err = carpoolService.Renew(ctx, legacyTerm.ID, userID, service.CarpoolRenewInput{}, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)

	plans, err := repo.ListPlans(ctx, true)
	require.NoError(t, err)
	var otherTierPlanID int64
	for _, plan := range plans {
		if plan.Code != legacyTerm.PlanSnapshot.Code && plan.DurationDays == 28 {
			otherTierPlanID = plan.ID
			break
		}
	}
	require.NotZero(t, otherTierPlanID)
	upgraded, err := carpoolService.Renew(ctx, renewed.ID, userID, service.CarpoolRenewInput{PlanID: otherTierPlanID}, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, otherTierPlanID, upgraded.PlanID)
	require.Equal(t, renewed.ExpiresAt, upgraded.StartsAt)
	require.Equal(t, upgraded.StartsAt.Add(28*24*time.Hour), upgraded.ExpiresAt)
	require.Equal(t, userID, upgraded.UserID)
	require.Equal(t, groupID, upgraded.GroupID)
	require.Len(t, upgraded.Cycles, 4)
}

func TestCarpoolCreateTermRejectsSupersededPlan(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, _ := newSyntheticCarpoolTermDependencies(t)
	code := "open-superseded-" + uuid.NewString()[:8]
	supersededID := insertSyntheticCarpoolPlan(t, ctx, code, 1, true)
	_ = insertSyntheticCarpoolPlan(t, ctx, code, 2, true)

	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:  userID,
		ScopeID: domain.CarpoolGlobalScopeID,
		GroupID: groupID,
		PlanID:  supersededID,
		ActorID: userID,
		Mode:    "new",
		Operation: domain.CarpoolOperation{
			Kind:        "open_term",
			ActorID:     userID,
			Key:         uuid.NewString(),
			Fingerprint: uuid.NewString(),
		},
	})
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)
	require.Nil(t, term)
	require.Nil(t, cycles)
}

func insertSyntheticCarpoolPlan(t *testing.T, ctx context.Context, code string, version int, enabled bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_plans(
			code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,
			boost_ratio,boost_count,enabled,version
		) VALUES($1,$1,1.00,10.00000000,30,7,0.10000000,2,$2,$3)
		RETURNING id
	`, code, enabled, version).Scan(&id))
	return id
}

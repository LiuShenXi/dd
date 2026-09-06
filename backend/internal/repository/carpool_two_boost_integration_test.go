//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCarpoolTwoBoostMigration_PreservesVersionsAndReplays(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE carpool_plans (LIKE public.carpool_plans INCLUDING ALL) ON COMMIT DROP`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO carpool_plans
		(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version)
		VALUES ('active','Custom active',345.67,575.125,28,7,0.1,3,TRUE,1),
		('paused','Custom paused',432.10,725.5,28,7,0.1,3,FALSE,1),
		('current','Already two',99,125,28,7,0.1,2,TRUE,1)`)
	require.NoError(t, err)
	var before string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY code)::text FROM carpool_plans p WHERE version=1`).Scan(&before))
	migration, err := migrations.FS.ReadFile("240_carpool_two_boost_default.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var after string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY code)::text FROM carpool_plans p WHERE version=1`).Scan(&after))
	assert.Equal(t, before, after)
	var count int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM carpool_plans`).Scan(&count))
	assert.Equal(t, 5, count)
	var unchanged bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT bool_and(
		n.name=o.name AND n.list_price_cny=o.list_price_cny AND n.weekly_quota_usd=o.weekly_quota_usd
		AND n.duration_days=o.duration_days AND n.cycle_days=o.cycle_days AND n.boost_ratio=o.boost_ratio
		AND n.enabled=o.enabled AND n.boost_count=2)
		FROM carpool_plans n JOIN carpool_plans o USING(code) WHERE n.version=2 AND o.version=1`).Scan(&unchanged))
	assert.True(t, unchanged)
	var defaultCount int
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO carpool_plans(code,name,list_price_cny,weekly_quota_usd,version)
		VALUES('default','Default two',99,125,1) RETURNING boost_count`).Scan(&defaultCount))
	assert.Equal(t, 2, defaultCount)
}

func TestCarpoolPlanVersion_TwoBoostsPreserveExistingThreeBoostTerm(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, _ := newSyntheticCarpoolTermDependencies(t)
	oldID := insertSyntheticCarpoolPlan(t, ctx, "boost-param-"+uuid.NewString()[:8], 1, true)
	input := domain.CarpoolPlan{Name: "Configurable boost count", ListPriceCNY: decimal.NewFromInt(330),
		WeeklyQuotaUSD: decimal.NewFromInt(550), BoostRatio: decimal.New(1, -1), BoostCount: 3, Enabled: true}
	three, err := repo.CreatePlanVersion(ctx, oldID, input, carpoolTestOperation("plan_version", userID))
	require.NoError(t, err)
	term, _, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{UserID: userID, ScopeID: domain.CarpoolGlobalScopeID,
		GroupID: groupID, PlanID: three.ID, ActorID: userID, Mode: "new", Operation: carpoolTestOperation("open_term", userID)})
	require.NoError(t, err)
	for _, invalid := range []int{-1, 0, 1, 4} {
		input.BoostCount = invalid
		_, err = repo.CreatePlanVersion(ctx, three.ID, input, carpoolTestOperation("plan_version", userID))
		require.Error(t, err)
	}
	input.BoostCount = 2
	two, err := repo.CreatePlanVersion(ctx, three.ID, input, carpoolTestOperation("plan_version", userID))
	require.NoError(t, err)
	assert.Equal(t, 2, two.BoostCount)
	carpoolService := service.NewCarpoolService(repo)
	for range 3 {
		claim, err := carpoolService.ClaimBoost(ctx, userID, uuid.NewString())
		require.NoError(t, err)
		assert.Equal(t, 3, claim.Total)
	}
	stored, err := repo.GetTerm(ctx, term.ID)
	require.NoError(t, err)
	requireLifecycleJSONEqual(t, term.PlanSnapshot, stored.PlanSnapshot)
	assert.Equal(t, 3, stored.BoostUsed)
	renewed, err := carpoolService.Renew(ctx, term.ID, userID, service.CarpoolRenewInput{}, uuid.NewString())
	require.NoError(t, err)
	assert.Equal(t, two.ID, renewed.PlanID)
	assert.Equal(t, 2, renewed.PlanSnapshot.BoostCount)
	assert.Equal(t, 0, renewed.BoostUsed)
}

//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCarpoolLegacyPlanMigrationAllowsOneCycleWithoutChangingDefaults(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE carpool_plans (LIKE public.carpool_plans INCLUDING ALL) ON COMMIT DROP`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("241_carpool_legacy_one_cycle_plan.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}

	var durationDays, boostCount int
	err = tx.QueryRowContext(ctx, `INSERT INTO carpool_plans
		(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version)
		VALUES ('legacy_week','Legacy one-cycle',0,550,7,7,0.1,0,FALSE,1)
		RETURNING duration_days,boost_count`).Scan(&durationDays, &boostCount)
	require.NoError(t, err)
	assert.Equal(t, 7, durationDays)
	assert.Zero(t, boostCount)

	var defaultDuration, defaultBoosts int
	err = tx.QueryRowContext(ctx, `INSERT INTO carpool_plans
		(code,name,list_price_cny,weekly_quota_usd,version)
		VALUES ('standard_default','Standard default',330,550,1)
		RETURNING duration_days,boost_count`).Scan(&defaultDuration, &defaultBoosts)
	require.NoError(t, err)
	assert.Equal(t, 28, defaultDuration)
	assert.Equal(t, 2, defaultBoosts)
}

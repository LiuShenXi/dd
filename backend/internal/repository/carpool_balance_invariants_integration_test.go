//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolTakeoverClosure_PreservesNetDebtInBaseOnly(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	startsAt := carpoolDatabaseNow(t, ctx).Add(-time.Hour)
	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:   userID,
		ScopeID:  domain.CarpoolGlobalScopeID,
		GroupID:  groupID,
		PlanID:   planID,
		ActorID:  userID,
		StartsAt: &startsAt,
		Mode:     "takeover",
		Takeover: &domain.CarpoolTakeoverInput{
			CurrentBaseBalanceUSD:   decimal.RequireFromString("-20.00000000"),
			CurrentBoostBalanceUSD:  decimal.RequireFromString("5.00000000"),
			CurrentManualBalanceUSD: decimal.RequireFromString("3.00000000"),
			HistoryComplete:         false,
		},
		Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	require.NotNil(t, term)
	require.NotEmpty(t, cycles)
	current := cycles[0]
	require.Equal(t, domain.CarpoolCycleActive, current.State)

	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET state='closing' WHERE id=$1`, current.ID)
	require.NoError(t, err)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, current.ID, carpoolDatabaseNow(t, ctx)))

	base, boost, manual, state := carpoolCycleBalances(t, ctx, current.ID)
	require.Equal(t, domain.CarpoolCycleClosed, state)
	require.True(t, base.Equal(decimal.RequireFromString("-12.00000000")))
	require.True(t, boost.IsZero())
	require.True(t, manual.IsZero())
	require.True(t, carpoolLedgerNet(t, ctx, current.ID).Equal(base.Add(boost).Add(manual)))
}

func TestCarpoolAdjustmentClosure_PreservesNetAndNeverCreatesNonBaseDebt(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:    userID,
		ScopeID:   domain.CarpoolGlobalScopeID,
		GroupID:   groupID,
		PlanID:    planID,
		ActorID:   userID,
		Mode:      "new",
		Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	require.NotNil(t, term)
	require.NotEmpty(t, cycles)
	current := cycles[0]
	base, boost, manual, state := carpoolCycleBalances(t, ctx, current.ID)
	require.Equal(t, domain.CarpoolCycleActive, state)
	require.Truef(t, base.Equal(decimal.RequireFromString("550.00000000")), "four_seat fixture must start at 550: base=%s boost=%s manual=%s", base, boost, manual)
	require.True(t, boost.IsZero())
	require.True(t, manual.IsZero())
	require.Truef(t, carpoolLedgerNet(t, ctx, current.ID).Equal(base), "initial ledger does not match base balance")

	_, err = repo.AdjustCycle(ctx, current.ID, userID, domain.CarpoolBucketBase, decimal.RequireFromString("-560.00000000"), "create base debt", nil, carpoolTestOperation("adjustment", userID))
	require.NoError(t, err)
	base, boost, manual, _ = carpoolCycleBalances(t, ctx, current.ID)
	require.Truef(t, base.Equal(decimal.RequireFromString("-10.00000000")), "unexpected balances after base adjustment: base=%s boost=%s manual=%s", base, boost, manual)
	require.Truef(t, carpoolLedgerNet(t, ctx, current.ID).Equal(base.Add(boost).Add(manual)), "ledger mismatch after base adjustment")
	_, err = repo.AdjustCycle(ctx, current.ID, userID, domain.CarpoolBucketBoost, decimal.RequireFromString("-4.00000000"), "route excess boost debit to base", nil, carpoolTestOperation("adjustment", userID))
	require.NoError(t, err)
	base, boost, manual, _ = carpoolCycleBalances(t, ctx, current.ID)
	require.Truef(t, base.Equal(decimal.RequireFromString("-14.00000000")), "unexpected balances after boost adjustment: base=%s boost=%s manual=%s", base, boost, manual)
	require.Truef(t, carpoolLedgerNet(t, ctx, current.ID).Equal(base.Add(boost).Add(manual)), "ledger mismatch after boost adjustment")
	_, err = repo.AdjustCycle(ctx, current.ID, userID, domain.CarpoolBucketManual, decimal.RequireFromString("6.00000000"), "offset base debt", nil, carpoolTestOperation("adjustment", userID))
	require.NoError(t, err)

	base, boost, manual, state = carpoolCycleBalances(t, ctx, current.ID)
	require.Equal(t, domain.CarpoolCycleActive, state)
	require.Truef(t, base.Equal(decimal.RequireFromString("-8.00000000")), "unexpected balances before close: base=%s boost=%s manual=%s", base, boost, manual)
	require.True(t, boost.IsZero())
	require.True(t, manual.IsZero())

	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET state='closing' WHERE id=$1`, current.ID)
	require.NoError(t, err)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, current.ID, carpoolDatabaseNow(t, ctx)))

	base, boost, manual, state = carpoolCycleBalances(t, ctx, current.ID)
	require.Equal(t, domain.CarpoolCycleClosed, state)
	require.Truef(t, base.Equal(decimal.RequireFromString("-8.00000000")), "unexpected balances after close: base=%s boost=%s manual=%s", base, boost, manual)
	require.True(t, boost.IsZero())
	require.True(t, manual.IsZero())
	require.True(t, carpoolLedgerNet(t, ctx, current.ID).Equal(base.Add(boost).Add(manual)))
}

func carpoolTestOperation(kind string, actorID int64) domain.CarpoolOperation {
	return domain.CarpoolOperation{Kind: kind, ActorID: actorID, Key: uuid.NewString(), Fingerprint: uuid.NewString()}
}

func carpoolDatabaseNow(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now))
	return now
}

func carpoolCycleBalances(t *testing.T, ctx context.Context, cycleID int64) (decimal.Decimal, decimal.Decimal, decimal.Decimal, string) {
	t.Helper()
	var baseRaw, boostRaw, manualRaw, state string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT base_balance_usd::text,boost_balance_usd::text,manual_balance_usd::text,state
		FROM carpool_cycles WHERE id=$1
	`, cycleID).Scan(&baseRaw, &boostRaw, &manualRaw, &state))
	base, err := decimal.NewFromString(baseRaw)
	require.NoError(t, err)
	boost, err := decimal.NewFromString(boostRaw)
	require.NoError(t, err)
	manual, err := decimal.NewFromString(manualRaw)
	require.NoError(t, err)
	return base, boost, manual, state
}

func carpoolLedgerNet(t *testing.T, ctx context.Context, cycleID int64) decimal.Decimal {
	t.Helper()
	var raw string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta_usd),0)::text FROM carpool_ledger WHERE cycle_id=$1`, cycleID).Scan(&raw))
	value, err := decimal.NewFromString(raw)
	require.NoError(t, err)
	return value
}

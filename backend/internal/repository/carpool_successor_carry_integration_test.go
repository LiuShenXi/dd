//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type successorCarryFixture struct {
	billing *carpoolUsageBillingFixture
	scopeID int64
	slotAt  time.Time
}

func newSuccessorCarryFixture(t *testing.T, cost string) *successorCarryFixture {
	t.Helper()
	ctx := context.Background()
	billing := newCarpoolUsageBillingFixture(t)
	slotAt, _ := service.NextCarpoolResetSchedule(carpoolDatabaseNow(t, ctx), nil)
	scopeID := nextResetIntegrationScopeID()
	startsAt := slotAt.Add(-24 * time.Hour)
	expiresAt := startsAt.Add(28 * 24 * time.Hour)
	admittedAt := slotAt.Add(-30 * time.Minute)
	billing.cost = decimal.RequireFromString(cost)
	billing.snapshot.AdmittedAt = admittedAt

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE carpool_terms
		SET scope_id=$1,starts_at=$2,expires_at=$3,
			plan_snapshot=jsonb_set(plan_snapshot,'{reset_mode}','"rolling"'::jsonb,TRUE)
		WHERE id=$4
	`, scopeID, startsAt, expiresAt, billing.snapshot.TermID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE carpool_cycles
		SET starts_at=$1,ends_at=$2,base_balance_usd=0,
			boost_balance_usd=5,manual_balance_usd=10,state='active'
		WHERE id=$3
	`, startsAt, slotAt.Add(24*time.Hour), billing.cycleID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE carpool_billing_requests
		SET admitted_at=$1,actual_cost_usd=$2,status='usage_known'
		WHERE id=$3
	`, admittedAt, billing.cost.StringFixed(8), billing.snapshot.BillingRequestID)
	require.NoError(t, err)
	for bucket, amount := range map[string]string{
		domain.CarpoolBucketBoost:  "5.00000000",
		domain.CarpoolBucketManual: "10.00000000",
	} {
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO carpool_ledger(
				user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reason,effective_at
			) VALUES($1,$2,$3,'adjustment',$4,$5,$6,'synthetic carry balance',$7)
		`, billing.snapshot.UserID, billing.snapshot.TermID, billing.cycleID, bucket, amount,
			"successor-carry:"+uuid.NewString(), startsAt)
		require.NoError(t, err)
	}
	return &successorCarryFixture{billing: billing, scopeID: scopeID, slotAt: slotAt}
}

func (f *successorCarryFixture) reset(t *testing.T, qualificationAt time.Time) *domain.CarpoolResetBatch {
	t.Helper()
	repo := resetRepositoryAt(qualificationAt)
	batch := registerResetQualification(t, repo, f.scopeID, f.billing.snapshot.UserID)
	repo.clockSQL = resetClockSQL(*batch.ScheduledAt)
	completed, err := repo.ExecuteResetBatch(context.Background(), batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	return completed
}

func (f *successorCarryFixture) settle(t *testing.T) {
	t.Helper()
	result, err := f.billing.repo.Apply(context.Background(), f.billing.command(false))
	require.NoError(t, err)
	require.True(t, result.Applied)
}

func requireCycleAmounts(t *testing.T, cycleID int64, base, boost, manual string) {
	t.Helper()
	actualBase, actualBoost, actualManual, _ := carpoolCycleBalances(t, context.Background(), cycleID)
	require.True(t, actualBase.Equal(decimal.RequireFromString(base)), "base: %s", actualBase)
	require.True(t, actualBoost.Equal(decimal.RequireFromString(boost)), "boost: %s", actualBoost)
	require.True(t, actualManual.Equal(decimal.RequireFromString(manual)), "manual: %s", actualManual)
}

func requireCarryStatus(t *testing.T, sourceCycleID int64, want string) {
	t.Helper()
	var status string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT status FROM carpool_cycle_carryovers WHERE source_cycle_id=$1`, sourceCycleID).Scan(&status))
	require.Equal(t, want, status)
}

func TestCarpoolSuccessorCarry_NoPendingReceiptTransfersSignedBalancesImmediately(t *testing.T) {
	ctx := context.Background()
	fixture := newSuccessorCarryFixture(t, "1")
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM carpool_billing_requests WHERE id=$1`, fixture.billing.snapshot.BillingRequestID)
	require.NoError(t, err)
	fixture.reset(t, fixture.slotAt.Add(-time.Hour))

	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	requireCycleAmounts(t, cycles[0].ID, "0", "0", "0")
	requireCycleAmounts(t, cycles[1].ID, "550", "5", "10")
	requireCarryStatus(t, cycles[0].ID, "completed")
	require.Equal(t, 4, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE event_key LIKE $1`, fmt.Sprintf("reset_carry:%%:%d:%%", cycles[0].ID)))
	require.True(t, carpoolLedgerNet(t, ctx, cycles[0].ID).IsZero())
	require.True(t, carpoolLedgerNet(t, ctx, cycles[1].ID).Equal(decimal.RequireFromString("565")))
}

func TestCarpoolSuccessorCarry_LateSettlementTransfersOnlyResidualOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newSuccessorCarryFixture(t, "8")
	fixture.reset(t, fixture.slotAt.Add(-time.Hour))
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	requireCycleAmounts(t, cycles[0].ID, "0", "5", "10")
	requireCycleAmounts(t, cycles[1].ID, "550", "0", "0")
	requireCarryStatus(t, cycles[0].ID, "pending")

	fixture.settle(t)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, cycles[0].ID, fixture.slotAt.Add(time.Minute)))
	requireCycleAmounts(t, cycles[0].ID, "0", "0", "0")
	requireCycleAmounts(t, cycles[1].ID, "550", "0", "7")
	requireCarryStatus(t, cycles[0].ID, "completed")
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE event_key LIKE $1`, fmt.Sprintf("reset_carry:%%:%d:%%", cycles[0].ID)))

	replayed, err := fixture.billing.repo.Apply(ctx, fixture.billing.command(false))
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, cycles[0].ID, fixture.slotAt.Add(2*time.Minute)))
	requireCycleAmounts(t, cycles[1].ID, "550", "0", "7")
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE event_key LIKE $1`, fmt.Sprintf("reset_carry:%%:%d:%%", cycles[0].ID)))
	require.True(t, carpoolLedgerNet(t, ctx, cycles[0].ID).Equal(decimal.Zero))
	require.True(t, carpoolLedgerNet(t, ctx, cycles[1].ID).Equal(decimal.RequireFromString("557")))
}

func TestCarpoolSuccessorCarry_MultipleSpecialResetsReachLatestActiveCycle(t *testing.T) {
	ctx := context.Background()
	fixture := newSuccessorCarryFixture(t, "3")
	first := fixture.reset(t, fixture.slotAt.Add(-time.Hour))
	second := fixture.reset(t, first.CompletedAt.Add(48*time.Hour-time.Hour))
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, cycles, 3)
	require.Equal(t, second.EffectiveAt, &cycles[2].StartsAt)

	fixture.settle(t)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, cycles[0].ID, cycles[2].StartsAt.Add(time.Minute)))
	requireCycleAmounts(t, cycles[1].ID, "550", "0", "0")
	requireCycleAmounts(t, cycles[2].ID, "550", "2", "10")
	requireCarryStatus(t, cycles[0].ID, "completed")
}

func TestCarpoolSuccessorCarry_NaturalBoundaryExpiresDeferredBalance(t *testing.T) {
	ctx := context.Background()
	fixture := newSuccessorCarryFixture(t, "1")
	fixture.reset(t, fixture.slotAt.Add(-time.Hour))
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	naturalAt := cycles[1].EndsAt
	_, err = NewCarpoolRepository(integrationDB).EnsureCurrentCycle(ctx, fixture.billing.snapshot.TermID, naturalAt)
	require.NoError(t, err)

	fixture.settle(t)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, cycles[0].ID, naturalAt.Add(time.Minute)))
	requireCarryStatus(t, cycles[0].ID, "expired")
	allCycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, allCycles, 3)
	requireCycleAmounts(t, allCycles[2].ID, "550", "0", "0")
}

func TestCarpoolSuccessorCarry_TermExpiryNeverResurrectsDeferredBalance(t *testing.T) {
	ctx := context.Background()
	fixture := newSuccessorCarryFixture(t, "1")
	fixture.reset(t, fixture.slotAt.Add(-time.Hour))
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, fixture.billing.snapshot.TermID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	var expiresAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT expires_at FROM carpool_terms WHERE id=$1`, fixture.billing.snapshot.TermID).Scan(&expiresAt))
	_, err = NewCarpoolRepository(integrationDB).EnsureCurrentCycle(ctx, fixture.billing.snapshot.TermID, expiresAt)
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)

	fixture.settle(t)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, cycles[0].ID, expiresAt.Add(time.Minute)))
	requireCarryStatus(t, cycles[0].ID, "expired")
	requireCycleAmounts(t, cycles[1].ID, "550", "0", "0")
}

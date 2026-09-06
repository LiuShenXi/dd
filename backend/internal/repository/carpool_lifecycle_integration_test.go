//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolLifecycle_MaintenanceAfterTwoWeekDowntimeGrantsOnlyCurrentCycle(t *testing.T) {
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
	require.Len(t, cycles, 4)

	now := carpoolDatabaseNow(t, ctx)
	termStart := now.Add(-15 * 24 * time.Hour)
	moveLifecycleTerm(t, ctx, term, cycles, termStart, domain.CarpoolTermActive, map[int]string{
		1: domain.CarpoolCycleActive,
		2: domain.CarpoolCycleScheduled,
		3: domain.CarpoolCycleScheduled,
		4: domain.CarpoolCycleScheduled,
	})

	require.NoError(t, repo.MaintainCycles(ctx, now, 200))
	assertLifecycleCycleStates(t, ctx, term.ID, []string{
		domain.CarpoolCycleClosed,
		domain.CarpoolCycleMissed,
		domain.CarpoolCycleActive,
		domain.CarpoolCycleScheduled,
	})
	assertLifecycleInitialGrantCounts(t, ctx, term.ID, []int{1, 0, 1, 0})

	var ledgerRowsBefore int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE term_id=$1`, term.ID).Scan(&ledgerRowsBefore))
	require.NoError(t, repo.MaintainCycles(ctx, now, 200))
	assertLifecycleInitialGrantCounts(t, ctx, term.ID, []int{1, 0, 1, 0})
	var ledgerRowsAfter int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE term_id=$1`, term.ID).Scan(&ledgerRowsAfter))
	require.Equal(t, ledgerRowsBefore, ledgerRowsAfter, "repeated maintenance must not add grants or close entries")

	futureRepo, futureUserID, futureGroupID, futurePlanID := newSyntheticCarpoolTermDependencies(t)
	futureStart := now.Add(48 * time.Hour)
	futureTerm, futureCycles, err := futureRepo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:    futureUserID,
		ScopeID:   domain.CarpoolGlobalScopeID,
		GroupID:   futureGroupID,
		PlanID:    futurePlanID,
		ActorID:   futureUserID,
		StartsAt:  &futureStart,
		Mode:      "new",
		Operation: carpoolTestOperation("open_term", futureUserID),
	})
	require.NoError(t, err)
	require.Len(t, futureCycles, 4)
	require.NoError(t, futureRepo.MaintainCycles(ctx, now, 200))

	storedFuture, err := futureRepo.GetTerm(ctx, futureTerm.ID)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolTermPending, storedFuture.Status)
	assertLifecycleCycleStates(t, ctx, futureTerm.ID, []string{
		domain.CarpoolCycleScheduled,
		domain.CarpoolCycleScheduled,
		domain.CarpoolCycleScheduled,
		domain.CarpoolCycleScheduled,
	})
	assertLifecycleInitialGrantCounts(t, ctx, futureTerm.ID, []int{0, 0, 0, 0})
}

func TestCarpoolLifecycle_BoostReplaySurvivesExpiryAndRenewal(t *testing.T) {
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
	require.Len(t, cycles, 4)

	carpoolService := service.NewCarpoolService(repo)
	boostKey := uuid.NewString()
	first, err := carpoolService.ClaimBoost(ctx, userID, boostKey)
	require.NoError(t, err)
	require.NotNil(t, first)

	now := carpoolDatabaseNow(t, ctx)
	expiredStart := now.Add(-28*24*time.Hour - time.Minute)
	moveLifecycleTerm(t, ctx, term, cycles, expiredStart, domain.CarpoolTermActive, map[int]string{
		1: domain.CarpoolCycleActive,
		2: domain.CarpoolCycleScheduled,
		3: domain.CarpoolCycleScheduled,
		4: domain.CarpoolCycleScheduled,
	})
	require.NoError(t, repo.MaintainCycles(ctx, now, 200))

	afterExpiry, err := carpoolService.ClaimBoost(ctx, userID, boostKey)
	require.NoError(t, err)
	requireLifecycleJSONEqual(t, first, afterExpiry)
	_, err = carpoolService.ClaimBoost(ctx, userID, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)
	assertLifecycleBoostUsage(t, ctx, term.ID, 1, 1)

	renewed, err := carpoolService.Renew(ctx, term.ID, userID, service.CarpoolRenewInput{PlanID: planID}, uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, renewed)
	require.Equal(t, domain.CarpoolTermActive, renewed.Status)
	require.NotNil(t, renewed.CurrentCycle)
	require.Equal(t, domain.CarpoolCycleActive, renewed.CurrentCycle.State)
	require.Equal(t, 0, renewed.BoostUsed)

	afterRenewal, err := carpoolService.ClaimBoost(ctx, userID, boostKey)
	require.NoError(t, err)
	requireLifecycleJSONEqual(t, first, afterRenewal)
	assertLifecycleBoostUsage(t, ctx, term.ID, 1, 1)
	assertLifecycleBoostUsage(t, ctx, renewed.ID, 0, 0)
}

func TestCarpoolLifecycle_FourConcurrentBoostClaimsGrantExactlyTwoAndRefreshQuota(t *testing.T) {
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
	require.Len(t, cycles, 4)
	require.Equal(t, 2, term.PlanSnapshot.BoostCount)

	var ordinaryBefore string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&ordinaryBefore))
	carpoolService := service.NewCarpoolService(repo)
	type boostOutcome struct {
		key    string
		result *domain.CarpoolBoostResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan boostOutcome, 4)
	for index := 0; index < 4; index++ {
		key := uuid.NewString()
		go func() {
			<-start
			result, claimErr := carpoolService.ClaimBoost(ctx, userID, key)
			outcomes <- boostOutcome{key: key, result: result, err: claimErr}
		}()
	}
	close(start)

	succeeded := make([]boostOutcome, 0, 2)
	exhausted := 0
	for index := 0; index < 4; index++ {
		outcome := <-outcomes
		switch {
		case outcome.err == nil:
			require.NotNil(t, outcome.result)
			require.Equal(t, 2, outcome.result.Total)
			require.True(t, decimal.RequireFromString("55.00000000").Equal(outcome.result.AmountUSD))
			succeeded = append(succeeded, outcome)
		case errors.Is(outcome.err, service.ErrCarpoolBoostExhausted):
			exhausted++
		default:
			require.NoError(t, outcome.err)
		}
	}
	require.Len(t, succeeded, 2)
	require.Equal(t, 2, exhausted)
	assertLifecycleBoostUsage(t, ctx, term.ID, 2, 2)

	replayed, err := carpoolService.ClaimBoost(ctx, userID, succeeded[0].key)
	require.NoError(t, err)
	requireLifecycleJSONEqual(t, succeeded[0].result, replayed)
	assertLifecycleBoostUsage(t, ctx, term.ID, 2, 2)

	details, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, details.Quota)
	require.True(t, decimal.RequireFromString("660.00000000").Equal(details.Quota.AvailableUSD))
	var ordinaryAfter string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&ordinaryAfter))
	require.Equal(t, ordinaryBefore, ordinaryAfter)
}

func TestCarpoolLifecycle_UserDetailsCrossRenewalUsageAndResetCount(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	firstCost := decimal.RequireFromString("10.25000000")
	_, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "first old-term usage", &firstCost)
	require.NoError(t, err)

	secondCost := decimal.RequireFromString("4.50000000")
	secondBillingID, secondRequestID := insertLifecycleBillingRequest(t, ctx, fixture, fixture.termID, fixture.cycleID, fixture.admittedAt.Add(time.Second), nil)
	_, err = fixture.service.ReconcileBillingException(ctx, secondBillingID, fixture.userID, service.CarpoolBillingReconcileInput{
		Confirmed:     true,
		Resolution:    domain.CarpoolBillingResolutionActualCost,
		ActualCostUSD: &secondCost,
		Reason:        "second old-term usage",
	}, uuid.NewString())
	require.NoError(t, err)

	var secondUsageLedgerID int64
	var secondUsageDelta string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT id,delta_usd::text FROM carpool_ledger
		WHERE request_id=$1 AND event_type='usage'
	`, secondRequestID).Scan(&secondUsageLedgerID, &secondUsageDelta))
	require.Equal(t, secondCost.Neg().StringFixed(8), secondUsageDelta)
	_, err = fixture.repo.AdjustCycle(ctx, fixture.cycleID, fixture.userID, domain.CarpoolBucketBase, secondCost, "reverse second old-term usage", &secondUsageLedgerID, carpoolTestOperation("adjustment", fixture.userID))
	require.NoError(t, err)

	now := carpoolDatabaseNow(t, ctx)
	oldStart := now.Add(-28*24*time.Hour - time.Minute)
	oldExpiry := now.Add(-time.Minute)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE carpool_terms SET starts_at=$1,expires_at=$2,status='expired',updated_at=NOW() WHERE id=$3
	`, oldStart, oldExpiry, fixture.termID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE carpool_cycles SET starts_at=$1,ends_at=$2,state='closing',updated_at=NOW() WHERE id=$3
	`, oldStart, oldExpiry, fixture.cycleID)
	require.NoError(t, err)
	require.NoError(t, closeCarpoolCycle(ctx, integrationDB, fixture.cycleID, now))

	renewed, err := fixture.service.Renew(ctx, fixture.termID, fixture.userID, service.CarpoolRenewInput{}, uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, renewed.CurrentCycle)
	renewedCycleID := renewed.CurrentCycle.ID

	renewedCost := decimal.RequireFromString("6.75000000")
	renewedBillingID, _ := insertLifecycleBillingRequest(t, ctx, fixture, renewed.ID, renewedCycleID, now, nil)
	_, err = fixture.service.ReconcileBillingException(ctx, renewedBillingID, fixture.userID, service.CarpoolBillingReconcileInput{
		Confirmed:     true,
		Resolution:    domain.CarpoolBillingResolutionActualCost,
		ActualCostUSD: &renewedCost,
		Reason:        "renewed-term usage",
	}, uuid.NewString())
	require.NoError(t, err)

	statisticsSince := oldStart.Add(24 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET history_complete=FALSE,statistics_since=$1 WHERE id=$2`, statisticsSince, fixture.termID)
	require.NoError(t, err)
	insertLifecycleResetTarget(t, ctx, fixture.termID, fixture.cycleID, "succeeded")
	insertLifecycleResetTarget(t, ctx, fixture.termID, fixture.cycleID, "succeeded")
	insertLifecycleResetTarget(t, ctx, renewed.ID, renewedCycleID, "succeeded")
	insertLifecycleResetTarget(t, ctx, renewed.ID, renewedCycleID, "failed")

	details, err := fixture.repo.GetUserDetails(ctx, fixture.userID)
	require.NoError(t, err)
	require.NotNil(t, details)
	require.True(t, firstCost.Add(renewedCost).Equal(details.Usage.TotalUsedUSD))
	require.False(t, details.Usage.HistoryComplete)
	require.NotNil(t, details.Usage.StatisticsSince)
	require.Equal(t, statisticsSince.UTC(), details.Usage.StatisticsSince.UTC())
	require.NotNil(t, details.Term)
	require.Equal(t, renewed.StartsAt.UTC(), details.Term.StartsAt.UTC(), "the current renewal must be selected")
	require.Equal(t, domain.CarpoolTermActive, details.Term.Status)
	require.Equal(t, 1, details.Term.ResetCount, "only successful resets from the selected term count")
	require.Equal(t, "current_term", details.Term.ResetCountBasis)
	require.Len(t, details.Term.ResetEvents, 1)
	require.Equal(t, renewed.CurrentCycle.CycleNo, details.Term.ResetEvents[0].CycleNo)
	require.True(t, renewed.CurrentCycle.BaseQuotaUSD.Equal(details.Term.ResetEvents[0].TargetQuotaUSD))
	require.False(t, details.Term.ResetEvents[0].OccurredAt.IsZero())
	require.NotNil(t, details.Quota)
	require.True(t, decimal.RequireFromString("543.25000000").Equal(details.Quota.AvailableUSD))
}

func TestCarpoolLifecycle_UnresolvedReceiptsHoldClosingUntilOriginalCycleSettles(t *testing.T) {
	for _, status := range []string{"admitted", "usage_known", "settling", "reconcile_required"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
			now := carpoolDatabaseNow(t, ctx)
			originalStart := now.Add(-2 * time.Hour)
			originalEnd := now.Add(-time.Hour)
			admittedAt := originalStart.Add(30 * time.Minute)
			seedLifecycleOpeningLedger(t, ctx, fixture, now)

			_, err := integrationDB.ExecContext(ctx, `
				UPDATE carpool_cycles
				SET starts_at=$1,ends_at=$2,state='closing',updated_at=NOW()
				WHERE id=$3
			`, originalStart, originalEnd, fixture.cycleID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `
				UPDATE carpool_billing_requests SET admitted_at=$1 WHERE id=$2
			`, admittedAt, fixture.billingRequestID)
			require.NoError(t, err)
			fixture.admittedAt = admittedAt

			var currentCycleID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				INSERT INTO carpool_cycles(
					term_id,cycle_no,starts_at,ends_at,base_quota_usd,
					base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at
				) VALUES($1,2,$2,$3,550.00000000,0,0,0,'active',$2)
				RETURNING id
			`, fixture.termID, originalEnd, originalEnd.Add(7*24*time.Hour)).Scan(&currentCycleID))

			cost := decimal.RequireFromString("5.25000000")
			var persistedCost any
			if status != "admitted" {
				persistedCost = cost.StringFixed(8)
			}
			_, err = integrationDB.ExecContext(ctx, `
				UPDATE carpool_billing_requests
				SET status=$1,actual_cost_usd=$2,last_error='lifecycle_test_pending',updated_at=NOW()
				WHERE id=$3
			`, status, persistedCost, fixture.billingRequestID)
			require.NoError(t, err)

			require.NoError(t, fixture.repo.MaintainCycles(ctx, now, 200))
			require.Equal(t, domain.CarpoolCycleClosing, lifecycleCycleState(t, ctx, fixture.cycleID))
			require.NoError(t, closeCarpoolCycle(ctx, integrationDB, fixture.cycleID, now))
			require.Equal(t, domain.CarpoolCycleClosing, lifecycleCycleState(t, ctx, fixture.cycleID))

			snapshot := lifecycleBillingSnapshot(fixture)
			switch status {
			case "admitted":
				require.NoError(t, fixture.repo.MarkReconcileRequired(ctx, snapshot, "receipt_missing_timeout"))
			case "usage_known":
				require.NoError(t, fixture.repo.MarkKnownUsageReconcileRequired(ctx, snapshot, "automatic_settlement_retry_exhausted"))
			case "settling":
				_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_billing_requests SET status='reconcile_required' WHERE id=$1`, fixture.billingRequestID)
				require.NoError(t, err)
			}

			settled, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "settle original admission cycle", &cost)
			require.NoError(t, err)
			require.Equal(t, "settled", settled.Status)

			var originalUsageCount, currentUsageCount int
			var originalUsageNet string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*),COALESCE(SUM(delta_usd),0)::text
				FROM carpool_ledger WHERE cycle_id=$1 AND request_id=$2 AND event_type='usage'
			`, fixture.cycleID, fixture.requestID).Scan(&originalUsageCount, &originalUsageNet))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM carpool_ledger
				WHERE cycle_id=$1 AND request_id=$2 AND event_type='usage'
			`, currentCycleID, fixture.requestID).Scan(&currentUsageCount))
			require.Equal(t, 1, originalUsageCount)
			require.Equal(t, cost.Neg().StringFixed(8), originalUsageNet)
			require.Zero(t, currentUsageCount, "settlement must not move to the current cycle")

			require.NoError(t, fixture.repo.MaintainCycles(ctx, now, 200))
			base, boost, manual, finalState := carpoolCycleBalances(t, ctx, fixture.cycleID)
			require.Equal(t, domain.CarpoolCycleClosed, finalState)
			require.True(t, carpoolLedgerNet(t, ctx, fixture.cycleID).Equal(base.Add(boost).Add(manual)))
		})
	}
}

func moveLifecycleTerm(t *testing.T, ctx context.Context, term *domain.CarpoolTerm, cycles []domain.CarpoolCycle, startsAt time.Time, status string, states map[int]string) {
	t.Helper()
	specs := domain.BuildCarpoolCycles(startsAt, term.PlanSnapshot)
	require.Len(t, specs, len(cycles))
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET starts_at=$1,expires_at=$2,status=$3,updated_at=NOW() WHERE id=$4`, startsAt, startsAt.Add(time.Duration(term.PlanSnapshot.DurationDays)*24*time.Hour), status, term.ID)
	require.NoError(t, err)
	for i := range cycles {
		state := states[cycles[i].CycleNo]
		require.NotEmpty(t, state)
		var activatedAt any
		if state == domain.CarpoolCycleActive {
			activatedAt = specs[i].StartsAt
		}
		_, err = integrationDB.ExecContext(ctx, `
			UPDATE carpool_cycles
			SET starts_at=$1,ends_at=$2,state=$3,
				activated_at=$4,
				closed_at=NULL,updated_at=NOW()
			WHERE id=$5
		`, specs[i].StartsAt, specs[i].EndsAt, state, activatedAt, cycles[i].ID)
		require.NoError(t, err)
	}
}

func assertLifecycleCycleStates(t *testing.T, ctx context.Context, termID int64, expected []string) {
	t.Helper()
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, termID)
	require.NoError(t, err)
	require.Len(t, cycles, len(expected))
	for i := range cycles {
		require.Equal(t, i+1, cycles[i].CycleNo)
		require.Equal(t, expected[i], cycles[i].State)
	}
}

func assertLifecycleInitialGrantCounts(t *testing.T, ctx context.Context, termID int64, expected []int) {
	t.Helper()
	for cycleNo, count := range expected {
		var actual int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM carpool_ledger l
			JOIN carpool_cycles c ON c.id=l.cycle_id
			WHERE c.term_id=$1 AND c.cycle_no=$2 AND l.event_type='cycle_initial'
		`, termID, cycleNo+1).Scan(&actual))
		require.Equal(t, count, actual, "unexpected initial grant count for cycle %d", cycleNo+1)
	}
}

func assertLifecycleBoostUsage(t *testing.T, ctx context.Context, termID int64, expectedUsed, expectedLedger int) {
	t.Helper()
	var used, ledger int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT boost_used FROM carpool_terms WHERE id=$1`, termID).Scan(&used))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE term_id=$1 AND event_type='boost'`, termID).Scan(&ledger))
	require.Equal(t, expectedUsed, used)
	require.Equal(t, expectedLedger, ledger)
}

func insertLifecycleBillingRequest(t *testing.T, ctx context.Context, fixture *carpoolBillingReconciliationFixture, termID, cycleID int64, admittedAt time.Time, actualCost *decimal.Decimal) (int64, string) {
	t.Helper()
	requestID := "carpool:lifecycle:" + uuid.NewString()
	var persistedCost any
	if actualCost != nil {
		persistedCost = actualCost.StringFixed(8)
	}
	var billingID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_billing_requests(
			request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,
			status,request_fingerprint,actual_cost_usd,last_error
		) VALUES($1,$2,$3,$4,$5,$6,$7,'reconcile_required',$8,$9,'lifecycle_test_reconcile')
		RETURNING id
	`, requestID, fixture.apiKeyID, fixture.userID, fixture.groupID, termID, cycleID, admittedAt, uuid.NewString(), persistedCost).Scan(&billingID))
	return billingID, requestID
}

func insertLifecycleResetTarget(t *testing.T, ctx context.Context, termID, cycleID int64, status string) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO carpool_reset_scope_states(scope_id) VALUES($1) ON CONFLICT(scope_id) DO NOTHING`, domain.CarpoolGlobalScopeID)
	require.NoError(t, err)
	var batchID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_reset_batches(
			scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,effective_at,
			completed_at,announcement_state,qualification_source,source_event_key_hash
		) VALUES($1,'completed',clock_timestamp(),clock_timestamp(),clock_timestamp(),
			clock_timestamp(),clock_timestamp(),clock_timestamp(),'published','administrator',$2)
		RETURNING id
	`, domain.CarpoolGlobalScopeID, uuid.NewString()).Scan(&batchID))
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
		VALUES($1,$2,$3,$4,0.00000000,clock_timestamp())
	`, batchID, termID, cycleID, status)
	require.NoError(t, err)
}

func seedLifecycleOpeningLedger(t *testing.T, ctx context.Context, fixture *carpoolBillingReconciliationFixture, effectiveAt time.Time) {
	t.Helper()
	for _, grant := range []struct {
		bucket string
		amount string
	}{
		{bucket: domain.CarpoolBucketBase, amount: "100.00000000"},
		{bucket: domain.CarpoolBucketBoost, amount: "7.00000000"},
		{bucket: domain.CarpoolBucketManual, amount: "9.00000000"},
	} {
		_, err := integrationDB.ExecContext(ctx, `
			INSERT INTO carpool_ledger(
				user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reason,effective_at
			) VALUES($1,$2,$3,'test_opening',$4,$5,$6,'lifecycle fixture opening balance',$7)
		`, fixture.userID, fixture.termID, fixture.cycleID, grant.bucket, grant.amount, "lifecycle:opening:"+uuid.NewString(), effectiveAt)
		require.NoError(t, err)
	}
}

func lifecycleBillingSnapshot(fixture *carpoolBillingReconciliationFixture) domain.CarpoolBillingSnapshot {
	return domain.CarpoolBillingSnapshot{
		BillingRequestID: fixture.billingRequestID,
		RequestID:        fixture.requestID,
		UserID:           fixture.userID,
		APIKeyID:         fixture.apiKeyID,
		GroupID:          fixture.groupID,
		TermID:           fixture.termID,
		CycleID:          fixture.cycleID,
		AdmittedAt:       fixture.admittedAt,
	}
}

func lifecycleCycleState(t *testing.T, ctx context.Context, cycleID int64) string {
	t.Helper()
	var state string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT state FROM carpool_cycles WHERE id=$1`, cycleID).Scan(&state))
	return state
}

func requireLifecycleJSONEqual(t *testing.T, expected, actual any) {
	t.Helper()
	expectedJSON, err := json.Marshal(expected)
	require.NoError(t, err)
	actualJSON, err := json.Marshal(actual)
	require.NoError(t, err)
	require.Equal(t, string(expectedJSON), string(actualJSON))
}

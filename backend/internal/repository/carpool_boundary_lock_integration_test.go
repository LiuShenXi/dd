//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolAdmissionLockWait_RetriesIntoCurrentCycle(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	boundary, nextCycleID := prepareCarpoolBoundaryTransition(t, ctx, fixture)

	blocker, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var lockedKeyID int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM api_keys WHERE id=$1 FOR UPDATE`, fixture.apiKeyID).Scan(&lockedKeyID))

	type admissionResult struct {
		snapshot *domain.CarpoolBillingSnapshot
		err      error
	}
	resultCh := make(chan admissionResult, 1)
	requestID := "carpool:boundary-admission:" + uuid.NewString()
	go func() {
		snapshot, admitErr := fixture.repo.Admit(ctx, fixture.userID, fixture.apiKeyID, fixture.groupID, requestID, time.Time{})
		resultCh <- admissionResult{snapshot: snapshot, err: admitErr}
	}()

	requireCarpoolQueryBlocked(t, ctx, "FOR SHARE OF k,u,g")
	waitForCarpoolDatabaseTime(t, ctx, boundary)
	require.NoError(t, blocker.Commit())

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.NotNil(t, result.snapshot)
		require.Equal(t, nextCycleID, result.snapshot.CycleID)
		require.False(t, result.snapshot.AdmittedAt.Before(boundary))
	case <-time.After(5 * time.Second):
		require.Fail(t, "admission did not resume after API-key lock release")
	}

	var oldCycleRequests, nextCycleRequests int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_requests WHERE request_id=$1 AND cycle_id=$2`, requestID, fixture.cycleID).Scan(&oldCycleRequests))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_requests WHERE request_id=$1 AND cycle_id=$2`, requestID, nextCycleID).Scan(&nextCycleRequests))
	require.Zero(t, oldCycleRequests)
	require.Equal(t, 1, nextCycleRequests)
}

func TestCarpoolBoostLockWait_RetriesIntoCurrentCycle(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	boundary, nextCycleID := prepareCarpoolBoundaryTransition(t, ctx, fixture)

	blocker, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var lockedCycleID int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM carpool_cycles WHERE id=$1 FOR UPDATE`, fixture.cycleID).Scan(&lockedCycleID))

	type boostResult struct {
		err error
	}
	resultCh := make(chan boostResult, 1)
	key := uuid.NewString()
	go func() {
		_, boostErr := fixture.repo.ClaimBoost(ctx, fixture.userID, key, uuid.NewString())
		resultCh <- boostResult{err: boostErr}
	}()

	requireCarpoolQueryBlocked(t, ctx, "FROM carpool_cycles WHERE term_id=$1 AND starts_at <= $2")
	waitForCarpoolDatabaseTime(t, ctx, boundary)
	require.NoError(t, blocker.Commit())

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "boost did not resume after cycle lock release")
	}

	var oldCycleBoosts, nextCycleBoosts int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE event_type='boost' AND cycle_id=$1`, fixture.cycleID).Scan(&oldCycleBoosts))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE event_type='boost' AND cycle_id=$1`, nextCycleID).Scan(&nextCycleBoosts))
	require.Zero(t, oldCycleBoosts)
	require.Equal(t, 1, nextCycleBoosts)
}

func TestCarpoolUserDetailsLockWait_RetriesIntoCurrentCycleWithoutHistoricalGrant(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	boundary, nextCycleID := prepareCarpoolBoundaryTransition(t, ctx, fixture)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE carpool_cycles
		SET state='scheduled',base_balance_usd=0,boost_balance_usd=0,manual_balance_usd=0,activated_at=NULL
		WHERE id=$1
	`, fixture.cycleID)
	require.NoError(t, err)

	blocker, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var lockedCycleID int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM carpool_cycles WHERE id=$1 FOR UPDATE`, fixture.cycleID).Scan(&lockedCycleID))

	type detailsResult struct {
		details *domain.CarpoolUserDetails
		err     error
	}
	resultCh := make(chan detailsResult, 1)
	go func() {
		details, detailsErr := fixture.repo.GetUserDetails(ctx, fixture.userID)
		resultCh <- detailsResult{details: details, err: detailsErr}
	}()

	requireCarpoolQueryBlocked(t, ctx, "FROM carpool_cycles WHERE term_id=$1 AND starts_at <= $2")
	waitForCarpoolDatabaseTime(t, ctx, boundary)
	require.NoError(t, blocker.Commit())

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.NotNil(t, result.details)
		require.False(t, result.details.ServerNow.Before(boundary))
		require.NotNil(t, result.details.Term)
		require.NotNil(t, result.details.Term.CurrentCycleNo)
		require.Equal(t, 2, *result.details.Term.CurrentCycleNo)
		require.NotNil(t, result.details.Quota)
		require.Equal(t, "550", result.details.Quota.AvailableUSD.String())
	case <-time.After(5 * time.Second):
		require.Fail(t, "user details did not resume after cycle lock release")
	}

	var oldCycleState, nextCycleState string
	var oldCycleGrants, nextCycleGrants int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT state FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&oldCycleState))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT state FROM carpool_cycles WHERE id=$1`, nextCycleID).Scan(&nextCycleState))
	require.Equal(t, domain.CarpoolCycleMissed, oldCycleState)
	require.Equal(t, domain.CarpoolCycleActive, nextCycleState)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE event_type='cycle_initial' AND cycle_id=$1`, fixture.cycleID).Scan(&oldCycleGrants))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE event_type='cycle_initial' AND cycle_id=$1`, nextCycleID).Scan(&nextCycleGrants))
	require.Zero(t, oldCycleGrants)
	require.Equal(t, 1, nextCycleGrants)
}

func prepareCarpoolBoundaryTransition(t *testing.T, ctx context.Context, fixture *carpoolBillingReconciliationFixture) (time.Time, int64) {
	t.Helper()
	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now))
	boundary := now.Add(2 * time.Second)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		UPDATE carpool_cycles SET ends_at=$1 WHERE id=$2
		RETURNING ends_at
	`, boundary, fixture.cycleID).Scan(&boundary))
	var nextCycleID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_cycles(
			term_id,cycle_no,starts_at,ends_at,base_quota_usd,
			base_balance_usd,boost_balance_usd,manual_balance_usd,state
		) VALUES($1,2,$2,$3,550.00000000,0,0,0,'scheduled')
		RETURNING id
	`, fixture.termID, boundary, boundary.Add(7*24*time.Hour)).Scan(&nextCycleID))
	return boundary, nextCycleID
}

func requireCarpoolQueryBlocked(t *testing.T, ctx context.Context, queryFragment string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pg_stat_activity
				WHERE datname=current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type='Lock'
				  AND query LIKE '%' || $1 || '%'
			)
		`, queryFragment).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "expected query containing %q to wait on a database lock", queryFragment)
}

func waitForCarpoolDatabaseTime(t *testing.T, ctx context.Context, boundary time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		var reached bool
		err := integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp() >= $1`, boundary).Scan(&reached)
		return err == nil && reached
	}, 5*time.Second, 10*time.Millisecond, "database clock did not reach cycle boundary")
}

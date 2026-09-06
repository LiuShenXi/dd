//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolScopeMembershipLock_SerializesTermCreation(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)

	blocker, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	require.NoError(t, lockCarpoolScopeMembershipTx(ctx, blocker, domain.CarpoolGlobalScopeID))

	type createResult struct {
		term *domain.CarpoolTerm
		err  error
	}
	resultCh := make(chan createResult, 1)
	go func() {
		term, _, createErr := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
			UserID:  userID,
			ScopeID: domain.CarpoolGlobalScopeID,
			GroupID: groupID,
			PlanID:  planID,
			ActorID: userID,
			Mode:    "new",
			Operation: domain.CarpoolOperation{
				Kind:        "open_term",
				ActorID:     userID,
				Key:         uuid.NewString(),
				Fingerprint: uuid.NewString(),
			},
		})
		resultCh <- createResult{term: term, err: createErr}
	}()

	deadline := time.Now().Add(5 * time.Second)
	waitObserved := false
	for time.Now().Before(deadline) {
		var waiting int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pg_locks
			WHERE locktype='advisory' AND classid=$1::oid AND objid=$2::oid AND NOT granted
		`, uint32(carpoolScopeMembershipLockNamespace), uint32(domain.CarpoolGlobalScopeID)).Scan(&waiting))
		if waiting > 0 {
			waitObserved = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, waitObserved, "term creation must wait for the shared reset/open membership lock")
	select {
	case result := <-resultCh:
		require.Failf(t, "term creation escaped scope lock", "result=%+v err=%v", result.term, result.err)
	default:
	}

	require.NoError(t, blocker.Commit())
	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.NotNil(t, result.term)
	case <-time.After(5 * time.Second):
		require.Fail(t, "term creation did not resume after scope lock release")
	}
}

func TestCarpoolUserDetails_ResetCountIncludesSuccessfulZeroGrantTarget(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:  userID,
		ScopeID: domain.CarpoolGlobalScopeID,
		GroupID: groupID,
		PlanID:  planID,
		ActorID: userID,
		Mode:    "new",
		Operation: domain.CarpoolOperation{
			Kind:        "open_term",
			ActorID:     userID,
			Key:         uuid.NewString(),
			Fingerprint: uuid.NewString(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, term)
	require.NotEmpty(t, cycles)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO carpool_reset_scope_states(scope_id) VALUES($1)
		ON CONFLICT (scope_id) DO NOTHING
	`, domain.CarpoolGlobalScopeID)
	require.NoError(t, err)
	var batchID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_reset_batches(
			scope_id,status,detected_at,qualified_at,effective_at,completed_at,
			announcement_state,qualification_source,source_event_key_hash
		) VALUES($1,'completed',clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp(),
			'published','administrator',$2)
		RETURNING id
	`, domain.CarpoolGlobalScopeID, uuid.NewString()).Scan(&batchID))
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
		VALUES($1,$2,$3,'succeeded',0.00000000,clock_timestamp())
	`, batchID, term.ID, cycles[0].ID)
	require.NoError(t, err)

	otherRepo, otherUserID, otherGroupID, otherPlanID := newSyntheticCarpoolTermDependencies(t)
	otherTerm, otherCycles, err := otherRepo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID:    otherUserID,
		ScopeID:   domain.CarpoolGlobalScopeID,
		GroupID:   otherGroupID,
		PlanID:    otherPlanID,
		ActorID:   otherUserID,
		Mode:      "new",
		Operation: carpoolTestOperation("open_term", otherUserID),
	})
	require.NoError(t, err)
	require.NotNil(t, otherTerm)
	require.NotEmpty(t, otherCycles)

	for _, fixture := range []struct {
		batchStatus  string
		targetStatus string
		cycleID      int64
	}{
		{batchStatus: "needs_review", targetStatus: "succeeded", cycleID: cycles[0].ID},
		{batchStatus: "completed", targetStatus: "failed", cycleID: cycles[0].ID},
		{batchStatus: "completed", targetStatus: "succeeded", cycleID: otherCycles[0].ID},
	} {
		var excludedBatchID int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO carpool_reset_batches(
				scope_id,status,detected_at,qualified_at,effective_at,completed_at,
				announcement_state,qualification_source,source_event_key_hash
			) VALUES($1,$2,clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp(),
				'published','administrator',$3)
			RETURNING id
		`, domain.CarpoolGlobalScopeID, fixture.batchStatus, uuid.NewString()).Scan(&excludedBatchID))
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
			VALUES($1,$2,$3,$4,0.00000000,clock_timestamp())
		`, excludedBatchID, term.ID, fixture.cycleID, fixture.targetStatus)
		require.NoError(t, err)
	}

	details, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, details)
	require.NotNil(t, details.Term)
	require.Equal(t, 1, details.Term.ResetCount)
	require.Equal(t, "current_term", details.Term.ResetCountBasis)
	require.Len(t, details.Term.ResetEvents, 1)
	require.Equal(t, cycles[0].CycleNo, details.Term.ResetEvents[0].CycleNo)
	require.True(t, cycles[0].BaseQuotaUSD.Equal(details.Term.ResetEvents[0].TargetQuotaUSD))
	require.False(t, details.Term.ResetEvents[0].OccurredAt.IsZero())
	require.NotNil(t, details.Quota)
	require.True(t, cycles[0].AvailableUSD().Equal(details.Quota.AvailableUSD))
}

func TestCarpoolUserDetails_QuotaIsNullWithoutAnActiveCoveredCycle(t *testing.T) {
	ctx := context.Background()
	now := carpoolDatabaseNow(t, ctx)
	for _, fixture := range []struct {
		name     string
		startsAt time.Time
		status   string
	}{
		{name: "pending", startsAt: now.Add(24 * time.Hour), status: domain.CarpoolTermPending},
		{name: "expired", startsAt: now.Add(-29 * 24 * time.Hour), status: domain.CarpoolTermExpired},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
			term, _, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
				UserID:    userID,
				ScopeID:   domain.CarpoolGlobalScopeID,
				GroupID:   groupID,
				PlanID:    planID,
				ActorID:   userID,
				StartsAt:  &fixture.startsAt,
				Mode:      "new",
				Operation: carpoolTestOperation("open_term", userID),
			})
			require.NoError(t, err)
			require.Equal(t, fixture.status, term.Status)

			details, err := repo.GetUserDetails(ctx, userID)
			require.NoError(t, err)
			require.NotNil(t, details.Term)
			require.Equal(t, fixture.status, details.Term.Status)
			require.Nil(t, details.Quota)
			require.Empty(t, details.Term.ResetEvents)
		})
	}
}

func newSyntheticCarpoolTermDependencies(t *testing.T) (*CarpoolRepository, int64, int64, int64) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewCarpoolRepository(integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        "carpool-membership-" + uuid.NewString() + "@example.com",
		PasswordHash: "synthetic-hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "carpool-membership-" + uuid.NewString(),
		SubscriptionType: service.SubscriptionTypeCarpool,
	})
	plans, err := repo.ListPlans(ctx, true)
	require.NoError(t, err)
	var planID int64
	for i := range plans {
		if plans[i].Code == "four_seat" {
			planID = plans[i].ID
			break
		}
	}
	require.NotZero(t, planID, "four_seat fixture plan is required")
	return repo, user.ID, group.ID, planID
}

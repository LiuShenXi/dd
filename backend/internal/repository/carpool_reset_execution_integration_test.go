//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var resetIntegrationScopeSequence atomic.Int64

type resetExecutionMember struct {
	userID       int64
	termID       int64
	cycleID      int64
	baseQuotaUSD decimal.Decimal
	baseBefore   decimal.Decimal
}

func TestCarpoolResetExecution_PreciseCooldownAndFinalTimestamps(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	lastSuccessful := slotAt.Add(-48*time.Hour + 20*time.Second)
	scheduledAt := slotAt.Add(20 * time.Second)
	completedAt := scheduledAt.Add(5 * time.Second)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, scheduledAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	setResetScopeState(t, scopeID, &lastSuccessful)

	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	require.Equal(t, slotAt, *batch.SlotAt)
	require.Equal(t, scheduledAt, *batch.ScheduledAt)

	repo.clockSQL = resetClockSQL(scheduledAt.Add(-time.Second))
	_, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.ErrorIs(t, err, service.ErrCarpoolResetNotDue)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})

	repo.clockSQL = resetClockAfterTargetSQL(batch.ID, scheduledAt, completedAt)
	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	require.NotNil(t, completed.EffectiveAt)
	require.NotNil(t, completed.CompletedAt)
	require.Equal(t, scheduledAt, *completed.EffectiveAt)
	require.Equal(t, completedAt, *completed.CompletedAt)
	require.Equal(t, 1, completed.TargetCount)
	require.True(t, completed.GrantedUSD.Equal(decimal.RequireFromString("450")))

	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	require.Equal(t, domain.CarpoolCycleClosing, cycles[0].State)
	require.True(t, member.baseBefore.Equal(cycles[0].BaseBalanceUSD))
	require.Equal(t, domain.CarpoolCycleActive, cycles[1].State)
	require.True(t, member.baseQuotaUSD.Equal(cycles[1].BaseBalanceUSD))
	var storedLast time.Time
	var pendingBatchID *int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT last_successful_reset_at,pending_batch_id FROM carpool_reset_scope_states WHERE scope_id=$1`, scopeID).Scan(&storedLast, &pendingBatchID))
	require.Equal(t, completedAt, storedLast)
	require.Nil(t, pendingBatchID)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='completed'`, batch.ID))
}

func TestCarpoolResetExecution_ConsecutiveRollingSuccessesMoveDeadlineKeepExpiry(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	firstSlot, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, firstSlot, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	originalID, originalStart, originalEnd, originalExpiry := resetCycleWindow(t, member)

	firstRepo := resetRepositoryAt(firstSlot.Add(-time.Hour))
	firstBatch := registerResetQualification(t, firstRepo, scopeID, member.userID)
	firstRepo.clockSQL = resetClockSQL(*firstBatch.ScheduledAt)
	first, err := firstRepo.ExecuteResetBatch(ctx, firstBatch.ID, nil)
	require.NoError(t, err)
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	firstEnd, firstExpiry := cycles[1].EndsAt, originalExpiry
	require.Equal(t, originalExpiry, firstExpiry)
	require.Equal(t, first.EffectiveAt.Add(7*24*time.Hour), firstEnd)
	sameCycle, err := NewCarpoolRepository(integrationDB).EnsureCurrentCycle(ctx, member.termID, originalEnd.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, cycles[1].ID, sameCycle.ID, "the obsolete fixed boundary must not create or grant a period")
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))

	secondRepo := resetRepositoryAt(firstEnd.Add(-6 * 24 * time.Hour))
	secondBatch := registerResetQualification(t, secondRepo, scopeID, member.userID)
	secondRepo.clockSQL = resetClockSQL(*secondBatch.ScheduledAt)
	second, err := secondRepo.ExecuteResetBatch(ctx, secondBatch.ID, nil)
	require.NoError(t, err)
	cycles, err = NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 3)
	currentID, currentStart, currentEnd, currentExpiry := cycles[2].ID, cycles[2].StartsAt, cycles[2].EndsAt, originalExpiry
	require.NotEqual(t, originalID, currentID)
	require.NotEqual(t, originalStart, currentStart)
	require.Equal(t, originalExpiry, currentExpiry)
	require.Equal(t, second.EffectiveAt.Add(7*24*time.Hour), currentEnd)
	require.True(t, currentEnd.After(firstEnd))
}

func TestCarpoolResetExecution_RollingZeroGrantReplayMovesDeadlineOnce(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("550"))
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	op := carpoolTestOperation("reset_execute", member.userID)
	repo.clockSQL = resetClockSQL(*batch.ScheduledAt)
	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, &op)
	require.NoError(t, err)
	require.True(t, completed.GrantedUSD.IsZero())
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	shiftedEnd := cycles[1].EndsAt
	require.Equal(t, completed.EffectiveAt.Add(7*24*time.Hour), shiftedEnd)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=$1 AND granted_usd=0`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset'`, batch.ID))

	repo.clockSQL = resetClockSQL(shiftedEnd.Add(-time.Hour))
	replayed, err := repo.ExecuteResetBatch(ctx, batch.ID, &op)
	require.NoError(t, err)
	require.Equal(t, completed.ID, replayed.ID)
	cycles, err = NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	replayEnd := cycles[1].EndsAt
	require.Equal(t, shiftedEnd, replayEnd)
}

func TestCarpoolResetExecution_RollingDay27SuccessCapsAtExpiry(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	expiresAt := slotAt.Add(24 * time.Hour)
	startsAt := expiresAt.Add(-28 * 24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET starts_at=$1,expires_at=$2 WHERE id=$3`, startsAt, expiresAt, member.termID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET starts_at=$1,ends_at=$2 WHERE id=$3`, slotAt.Add(-time.Hour), expiresAt, member.cycleID)
	require.NoError(t, err)

	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	repo.clockSQL = resetClockSQL(*batch.ScheduledAt)
	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	cycleStart, cycleEnd, storedExpiry := cycles[1].StartsAt, cycles[1].EndsAt, expiresAt
	require.Equal(t, slotAt, cycleStart)
	require.Equal(t, expiresAt, cycleEnd)
	require.Equal(t, expiresAt, storedExpiry)
	require.Nil(t, domain.CarpoolNextNaturalResetAt(domain.CarpoolTermActive, storedExpiry, *completed.EffectiveAt, []domain.CarpoolCycle{{StartsAt: cycleStart, EndsAt: cycleEnd, State: domain.CarpoolCycleActive}}))
	_, err = NewCarpoolRepository(integrationDB).EnsureCurrentCycle(ctx, member.termID, storedExpiry)
	require.ErrorIs(t, err, service.ErrCarpoolUnavailable)
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))
}

func TestCarpoolResetExecution_RollingNaturalBoundaryRaceGrantsOnce(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	_, _, _, originalExpiry := resetCycleWindow(t, member)
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET ends_at=$1 WHERE id=$2`, slotAt, member.cycleID)
	require.NoError(t, err)

	resetRepo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, resetRepo, scopeID, member.userID)
	resetRepo.clockSQL = resetClockSQL(slotAt)
	cycleRepo := NewCarpoolRepository(integrationDB)
	start := make(chan struct{})
	ensureResult := make(chan error, 1)
	resetResult := make(chan error, 1)
	go func() {
		<-start
		_, ensureErr := cycleRepo.EnsureCurrentCycle(ctx, member.termID, slotAt)
		ensureResult <- ensureErr
	}()
	go func() {
		<-start
		_, resetErr := resetRepo.ExecuteResetBatch(ctx, batch.ID, nil)
		resetResult <- resetErr
	}()
	close(start)
	require.NoError(t, <-ensureResult)
	require.NoError(t, <-resetResult)

	cycles, err := cycleRepo.ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 3)
	require.Equal(t, domain.CarpoolCycleClosing, cycles[0].State)
	require.Equal(t, domain.CarpoolCycleClosing, cycles[1].State)
	require.Equal(t, cycles[1].StartsAt, cycles[1].EndsAt)
	require.Equal(t, domain.CarpoolCycleActive, cycles[2].State)
	require.Equal(t, slotAt, cycles[2].StartsAt)
	require.True(t, decimal.RequireFromString("550").Equal(cycles[2].BaseBalanceUSD))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE cycle_id=$1 AND event_type='cycle_initial'`, cycles[1].ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset'`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=$1 AND cycle_id=$2 AND granted_usd=0`, batch.ID, cycles[2].ID))
	_, _, _, storedExpiry := resetCycleWindow(t, member)
	require.Equal(t, originalExpiry, storedExpiry)
}

func TestCarpoolResetExecution_SameMinuteCooldownCorrectionPreservesSeconds(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	originalLastSuccessful := slotAt.Add(-48*time.Hour + 10*time.Second)
	latestLastSuccessful := slotAt.Add(-48*time.Hour + 20*time.Second)
	originalScheduledAt := slotAt.Add(10 * time.Second)
	correctedScheduledAt := slotAt.Add(20 * time.Second)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, originalScheduledAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	_, _, originalEnd, _ := resetCycleWindow(t, member)
	setResetScopeState(t, scopeID, &originalLastSuccessful)

	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	require.Equal(t, originalScheduledAt, *batch.ScheduledAt)
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_reset_scope_states SET last_successful_reset_at=$1 WHERE scope_id=$2`, latestLastSuccessful, scopeID)
	require.NoError(t, err)

	repo.clockSQL = resetClockSQL(originalScheduledAt)
	corrected, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusScheduled, corrected.Status)
	require.Equal(t, slotAt, *corrected.SlotAt)
	require.Equal(t, correctedScheduledAt, *corrected.ScheduledAt)
	require.Equal(t, 1, corrected.ScheduleRevision)
	require.NotNil(t, corrected.DelayReason)
	require.Equal(t, "cooldown", *corrected.DelayReason)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})
	_, _, unchangedEnd, _ := resetCycleWindow(t, member)
	require.Equal(t, originalEnd, unchangedEnd)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1`, batch.ID))
}

func TestCarpoolResetExecution_InvalidFutureScheduleIsCorrected(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)

	invalidScheduledAt := slotAt.Add(2 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_reset_batches SET scheduled_at=$1 WHERE id=$2`, invalidScheduledAt, batch.ID)
	require.NoError(t, err)

	corrected, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusScheduled, corrected.Status)
	require.Equal(t, slotAt, *corrected.SlotAt)
	require.Equal(t, slotAt, *corrected.ScheduledAt)
	require.Equal(t, 1, corrected.ScheduleRevision)
	require.NotNil(t, corrected.DelayReason)
	require.Equal(t, "invalid_schedule", *corrected.DelayReason)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1`, batch.ID))
}

func TestCarpoolResetExecution_FinalClockCrossingMinuteRollsBackAndReschedules(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	startedAt := slotAt.Add(59 * time.Second)
	finishedAt := slotAt.Add(time.Minute)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, startedAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)

	repo.clockSQL = resetClockAfterTargetSQL(batch.ID, startedAt, finishedAt)
	rescheduled, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusScheduled, rescheduled.Status)
	require.Equal(t, 1, rescheduled.ScheduleRevision)
	require.NotNil(t, rescheduled.DelayReason)
	require.Equal(t, "execution_boundary_moved", *rescheduled.DelayReason)
	require.NotNil(t, rescheduled.SlotAt)
	require.Equal(t, slotAt.AddDate(0, 0, 1), *rescheduled.SlotAt)
	require.Equal(t, *rescheduled.SlotAt, *rescheduled.ScheduledAt)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1`, batch.ID))
}

func TestCarpoolResetExecution_FinalTermBoundaryRollsBackAndReschedules(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	startedAt := slotAt.Add(10 * time.Second)
	boundaryAt := slotAt.Add(15 * time.Second)
	completedAt := slotAt.Add(20 * time.Second)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, startedAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET expires_at=$1 WHERE id=$2`, boundaryAt, member.termID)
	require.NoError(t, err)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)

	repo.clockSQL = resetClockAfterTargetSQL(batch.ID, startedAt, completedAt)
	rescheduled, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusScheduled, rescheduled.Status)
	require.Equal(t, 1, rescheduled.ScheduleRevision)
	require.NotNil(t, rescheduled.DelayReason)
	require.Equal(t, "execution_boundary_moved", *rescheduled.DelayReason)
	require.Equal(t, slotAt.AddDate(0, 0, 1), *rescheduled.SlotAt)
	require.Equal(t, *rescheduled.SlotAt, *rescheduled.ScheduledAt)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1`, batch.ID))
}

func TestCarpoolResetExecution_FinalCycleBoundaryRollsBackAndReschedules(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	startedAt := slotAt.Add(10 * time.Second)
	boundaryAt := slotAt.Add(15 * time.Second)
	completedAt := slotAt.Add(20 * time.Second)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, startedAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET ends_at=$1 WHERE id=$2`, boundaryAt, member.cycleID)
	require.NoError(t, err)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)

	repo.clockSQL = resetClockAfterTargetSQL(batch.ID, startedAt, completedAt)
	rescheduled, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusScheduled, rescheduled.Status)
	require.Equal(t, 1, rescheduled.ScheduleRevision)
	require.NotNil(t, rescheduled.DelayReason)
	require.Equal(t, "execution_boundary_moved", *rescheduled.DelayReason)
	require.Equal(t, slotAt.AddDate(0, 0, 1), *rescheduled.SlotAt)
	require.Equal(t, *rescheduled.SlotAt, *rescheduled.ScheduledAt)
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{member})
	_, _, storedEnd, _ := resetCycleWindow(t, member)
	require.Equal(t, boundaryAt, storedEnd)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1`, batch.ID))
}

func TestCarpoolResetExecution_AllOrNothingRetryIncludesZeroGrantCycleFive(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	needsGrant := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("157"), decimal.RequireFromString("57"))
	zeroGrant := createResetExecutionMember(t, scopeID, slotAt, 5,
		decimal.RequireFromString("157"), decimal.RequireFromString("157"))
	require.Less(t, needsGrant.termID, zeroGrant.termID)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, needsGrant.userID)
	_, _, rollingEndBefore, _ := resetCycleWindow(t, needsGrant)

	functionName := "fail_reset_target_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	triggerName := functionName + "_trigger"
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.term_id = %d THEN
				RAISE EXCEPTION 'forced reset target failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON carpool_reset_targets
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, functionName, zeroGrant.termID, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON carpool_reset_targets`, triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	repo.clockSQL = resetClockSQL(slotAt)
	_, err = repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.ErrorContains(t, err, "forced reset target failure")
	requireResetExecutionUntouched(t, batch.ID, []resetExecutionMember{needsGrant, zeroGrant})
	_, _, rollingEndAfterFailure, _ := resetCycleWindow(t, needsGrant)
	require.Equal(t, rollingEndBefore, rollingEndAfterFailure)
	var lastSuccessful *time.Time
	var pendingBatchID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT last_successful_reset_at,pending_batch_id FROM carpool_reset_scope_states WHERE scope_id=$1`, scopeID).Scan(&lastSuccessful, &pendingBatchID))
	require.Nil(t, lastSuccessful)
	require.Equal(t, batch.ID, pendingBatchID)

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER %s ON carpool_reset_targets`, triggerName))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP FUNCTION %s()`, functionName))
	require.NoError(t, err)

	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	require.Equal(t, 2, completed.TargetCount)
	require.True(t, completed.GrantedUSD.Equal(decimal.RequireFromString("100")))
	needCycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, needsGrant.termID)
	require.NoError(t, err)
	zeroCycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, zeroGrant.termID)
	require.NoError(t, err)
	require.Len(t, needCycles, 2)
	require.Len(t, zeroCycles, 1, "legacy fixed snapshots still refill in place")
	require.True(t, needCycles[1].BaseBalanceUSD.Equal(needsGrant.baseQuotaUSD))
	require.True(t, zeroCycles[0].BaseBalanceUSD.Equal(zeroGrant.baseQuotaUSD))
	rollingEndAfterSuccess := needCycles[1].EndsAt
	require.Equal(t, completed.EffectiveAt.Add(7*24*time.Hour), rollingEndAfterSuccess)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=$1 AND term_id=$2 AND granted_usd=0`, batch.ID, zeroGrant.termID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset'`, batch.ID))
}

func TestCarpoolResetExecution_RollingCycleNumberCanExceedFive(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("550"))
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET cycle_no=5 WHERE id=$1`, member.cycleID)
	require.NoError(t, err)

	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	repo.clockSQL = resetClockSQL(*batch.ScheduledAt)
	_, err = repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)

	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	require.Equal(t, 5, cycles[0].CycleNo)
	require.Equal(t, 6, cycles[1].CycleNo)
	require.Equal(t, domain.CarpoolCycleActive, cycles[1].State)
}

func TestCarpoolResetExecution_RollingNegativeBaseGrantMatchesSuccessor(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, slotAt, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("-10"))
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)
	repo.clockSQL = resetClockSQL(*batch.ScheduledAt)
	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.True(t, decimal.RequireFromString("550").Equal(completed.GrantedUSD))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset' AND delta_usd=550`, batch.ID))
}

func nextResetIntegrationScopeID() int64 {
	return 2_000_000_000 + resetIntegrationScopeSequence.Add(1)
}

func resetRepositoryAt(now time.Time) *CarpoolResetRepository {
	return &CarpoolResetRepository{db: integrationDB, clockSQL: resetClockSQL(now)}
}

func resetClockSQL(now time.Time) string {
	return fmt.Sprintf(`SELECT TIMESTAMPTZ '%s'`, now.UTC().Format(time.RFC3339Nano))
}

func resetClockAfterTargetSQL(batchID int64, beforeTarget, afterTarget time.Time) string {
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS(SELECT 1 FROM carpool_reset_targets WHERE batch_id=%d) THEN TIMESTAMPTZ '%s' ELSE TIMESTAMPTZ '%s' END`,
		batchID, afterTarget.UTC().Format(time.RFC3339Nano), beforeTarget.UTC().Format(time.RFC3339Nano))
}

func setResetScopeState(t *testing.T, scopeID int64, lastSuccessful *time.Time) {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `
		INSERT INTO carpool_reset_scope_states(scope_id,last_successful_reset_at)
		VALUES($1,$2)
		ON CONFLICT(scope_id) DO UPDATE SET last_successful_reset_at=EXCLUDED.last_successful_reset_at,pending_batch_id=NULL
	`, scopeID, lastSuccessful)
	require.NoError(t, err)
}

func registerResetQualification(t *testing.T, repo *CarpoolResetRepository, scopeID, actorID int64) *domain.CarpoolResetBatch {
	t.Helper()
	batch, err := repo.RegisterResetQualification(context.Background(), scopeID, "source:"+uuid.NewString(), "synthetic integration qualification", carpoolTestOperation("reset_qualification", actorID))
	require.NoError(t, err)
	require.NotNil(t, batch)
	return batch
}

func createResetIntegrationActor(t *testing.T) int64 {
	t.Helper()
	user := mustCreateUser(t, testEntClient(t), &service.User{
		Email:        "carpool-reset-actor-" + uuid.NewString() + "@example.com",
		PasswordHash: "synthetic-hash",
	})
	return user.ID
}

func createResetExecutionMember(t *testing.T, scopeID int64, executionAt time.Time, cycleNo int, baseQuota, baseBalance decimal.Decimal) resetExecutionMember {
	t.Helper()
	ctx := context.Background()
	carpoolRepo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	if cycleNo == 5 {
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT id FROM carpool_plans
			WHERE code='four_seat' AND duration_days=30 AND cycle_days=7
			ORDER BY version DESC,id DESC LIMIT 1
		`).Scan(&planID))
	}
	plan, err := carpoolRepo.GetPlan(ctx, planID)
	require.NoError(t, err)
	snapshot := plan.Snapshot()
	snapshotJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	termStartsAt := executionAt.Add(-time.Duration((cycleNo-1)*7)*24*time.Hour - time.Hour)
	termExpiresAt := termStartsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	var termID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_terms(
			user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,
			status,source_mode,history_complete,statistics_since,created_by
		) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,'active','new',TRUE,$6,$1)
		RETURNING id
	`, userID, scopeID, groupID, planID, string(snapshotJSON), termStartsAt, termExpiresAt).Scan(&termID))
	cycleStartsAt := executionAt.Add(-time.Hour)
	cycleEndsAt := executionAt.Add(24 * time.Hour)
	var cycleID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_cycles(
			term_id,cycle_no,starts_at,ends_at,base_quota_usd,
			base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at
		) VALUES($1,$2,$3,$4,$5,$6,0,0,'active',$3)
		RETURNING id
	`, termID, cycleNo, cycleStartsAt, cycleEndsAt, baseQuota.StringFixed(8), baseBalance.StringFixed(8)).Scan(&cycleID))
	return resetExecutionMember{
		userID: userID, termID: termID, cycleID: cycleID,
		baseQuotaUSD: baseQuota, baseBefore: baseBalance,
	}
}

func requireResetExecutionUntouched(t *testing.T, batchID int64, members []resetExecutionMember) {
	t.Helper()
	for _, member := range members {
		require.True(t, resetCycleBaseBalance(t, member.cycleID).Equal(member.baseBefore))
	}
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=$1`, batchID))
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset'`, batchID))
	var status string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT status FROM carpool_reset_batches WHERE id=$1`, batchID).Scan(&status))
	require.Equal(t, domain.CarpoolResetStatusScheduled, status)
}

func resetCycleBaseBalance(t *testing.T, cycleID int64) decimal.Decimal {
	t.Helper()
	var raw string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, cycleID).Scan(&raw))
	value, err := decimal.NewFromString(raw)
	require.NoError(t, err)
	return value
}

func resetCycleWindow(t *testing.T, member resetExecutionMember) (int64, time.Time, time.Time, time.Time) {
	t.Helper()
	var cycleID int64
	var startsAt, endsAt, expiresAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
		SELECT c.id,c.starts_at,c.ends_at,t.expires_at
		FROM carpool_cycles c JOIN carpool_terms t ON t.id=c.term_id
		WHERE c.id=$1
	`, member.cycleID).Scan(&cycleID, &startsAt, &endsAt, &expiresAt))
	return cycleID, startsAt, endsAt, expiresAt
}

func resetRowCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), query, args...).Scan(&count))
	return count
}

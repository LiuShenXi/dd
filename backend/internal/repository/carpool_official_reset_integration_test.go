//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolOfficialReset_BypassesCardScheduleAndPreservesScopeState(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	now := carpoolDatabaseNow(t, ctx)
	member := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("-10"))
	lastSuccessful := now.Add(-time.Hour)
	setResetScopeState(t, domain.CarpoolGlobalScopeID, &lastSuccessful)
	repo := resetRepositoryAt(now)
	card := registerResetQualification(t, repo, domain.CarpoolGlobalScopeID, member.userID)

	var revision int64
	var pendingID int64
	var storedLast time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT revision,pending_batch_id,last_successful_reset_at FROM carpool_reset_scope_states WHERE scope_id=$1`, domain.CarpoolGlobalScopeID).Scan(&revision, &pendingID, &storedLast))
	qualifiedBefore := resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE batch_id=$1`, card.ID)
	cardOutboxBefore := resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1`, card.ID)

	op := carpoolTestOperation("reset_official", member.userID)
	completed, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, op)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	require.Equal(t, "official", completed.TriggerKind)
	require.Nil(t, completed.SlotAt)
	require.Nil(t, completed.ScheduledAt)
	require.NotNil(t, completed.EffectiveAt)
	require.NotNil(t, completed.CompletedAt)
	require.Equal(t, 1, completed.TargetCount)
	require.True(t, decimal.RequireFromString("550").Equal(completed.GrantedUSD), "negative historical base debt must not inflate a successor grant")

	var revisionAfter int64
	var pendingAfter int64
	var lastAfter time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT revision,pending_batch_id,last_successful_reset_at FROM carpool_reset_scope_states WHERE scope_id=$1`, domain.CarpoolGlobalScopeID).Scan(&revisionAfter, &pendingAfter, &lastAfter))
	require.Equal(t, revision, revisionAfter)
	require.Equal(t, pendingID, pendingAfter)
	require.Equal(t, storedLast, lastAfter)
	require.Equal(t, card.ID, pendingAfter)
	require.Equal(t, qualifiedBefore, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE batch_id=$1`, card.ID))
	require.Equal(t, cardOutboxBefore, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1`, card.ID))
	batches, _, err := repo.ListResetBatches(ctx, domain.CarpoolResetBatchFilters{Page: 1, PageSize: 20, ScopeID: int64Pointer(domain.CarpoolGlobalScopeID)})
	require.NoError(t, err)
	var storedCard *domain.CarpoolResetBatch
	for i := range batches {
		if batches[i].ID == card.ID {
			storedCard = &batches[i]
		}
	}
	require.NotNil(t, storedCard)
	require.Equal(t, "card", storedCard.TriggerKind)
	require.Equal(t, domain.CarpoolResetStatusScheduled, storedCard.Status)

	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, member.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	require.Equal(t, domain.CarpoolCycleClosing, cycles[0].State)
	require.Equal(t, domain.CarpoolCycleActive, cycles[1].State)
	require.Equal(t, 2, cycles[1].CycleNo)
	require.True(t, decimal.RequireFromString("550").Equal(cycles[1].BaseBalanceUSD))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_ledger WHERE reset_batch_id=$1 AND event_type='reset' AND reason='official global reset successor'`, completed.ID))

	replayed, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, op)
	require.NoError(t, err)
	require.Equal(t, completed.ID, replayed.ID)
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))

	second, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, carpoolTestOperation("reset_official", member.userID))
	require.NoError(t, err)
	require.NotEqual(t, completed.ID, second.ID)
	require.Equal(t, 3, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))
}

func TestCarpoolOfficialReset_ZeroTargetsIsAuditedCompletion(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	actorID := createResetIntegrationActor(t)
	repo := resetRepositoryAt(carpoolDatabaseNow(t, ctx))
	batch, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, carpoolTestOperation("reset_official", actorID))
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, batch.Status)
	require.Equal(t, "official", batch.TriggerKind)
	require.Zero(t, batch.TargetCount)
	require.True(t, batch.GrantedUSD.IsZero())
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='completed'`, batch.ID))
	var outboxID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT id FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='completed'`, batch.ID).Scan(&outboxID))
	published, err := repo.publishResetAnnouncement(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, published)
	var title, content string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT title,content FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='completed'`, batch.ID).Scan(&title, &content))
	require.Equal(t, "官方拼车重置完成", title)
	require.Contains(t, content, "仅适用于重置生效时已生效且未到期的拼车会员")
}

func TestCarpoolOfficialReset_RollbackLeavesNoBatchOrSuccessor(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	now := carpoolDatabaseNow(t, ctx)
	member := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	secondMember := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1,
		decimal.RequireFromString("550"), decimal.RequireFromString("200"))
	repo := resetRepositoryAt(now)
	op := carpoolTestOperation("reset_official", member.userID)
	officialBatchesBefore := resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1 AND evidence->>'trigger_kind'='official'`, domain.CarpoolGlobalScopeID)
	functionName := "fail_official_target_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	triggerName := functionName + "_trigger"
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.term_id = %d THEN RAISE EXCEPTION 'forced official target failure'; END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON carpool_reset_targets
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, functionName, secondMember.termID, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON carpool_reset_targets`, triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	_, err = repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, op)
	require.ErrorContains(t, err, "forced official target failure")
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, secondMember.termID))
	require.Equal(t, officialBatchesBefore, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1 AND evidence->>'trigger_kind'='official'`, domain.CarpoolGlobalScopeID))

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER %s ON carpool_reset_targets`, triggerName))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP FUNCTION %s()`, functionName))
	require.NoError(t, err)
	completed, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, op)
	require.NoError(t, err)
	require.Equal(t, 2, completed.TargetCount)
}

func TestCarpoolOfficialReset_SelectsOnlyEffectiveMembersAndCapsExpiry(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	now := carpoolDatabaseNow(t, ctx)
	first := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	lastDay := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("200"))
	expiresAt := now.Add(24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET expires_at=$1 WHERE id=$2`, expiresAt, lastDay.termID)
	require.NoError(t, err)

	future := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("300"))
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET status='pending',starts_at=$1,expires_at=$2 WHERE id=$3`, now.Add(24*time.Hour), now.Add(29*24*time.Hour), future.termID)
	require.NoError(t, err)
	expired := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("300"))
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET status='expired' WHERE id=$1`, expired.termID)
	require.NoError(t, err)
	terminated := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("300"))
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET status='terminated',terminated_at=$2 WHERE id=$1`, terminated.termID, now)
	require.NoError(t, err)

	repo := resetRepositoryAt(now)
	batch, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, carpoolTestOperation("reset_official", first.userID))
	require.NoError(t, err)
	require.Equal(t, 2, batch.TargetCount)
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=$1`, batch.ID))
	for _, excluded := range []resetExecutionMember{future, expired, terminated} {
		require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, excluded.termID))
	}
	cycles, err := NewCarpoolRepository(integrationDB).ListTermCycles(ctx, lastDay.termID)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	require.Equal(t, expiresAt, cycles[1].EndsAt)
}

func TestCarpoolOfficialReset_ConcurrentSameOperationCreatesOneBatch(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	now := carpoolDatabaseNow(t, ctx)
	member := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 1, decimal.RequireFromString("550"), decimal.RequireFromString("100"))
	repo := resetRepositoryAt(now)
	op := carpoolTestOperation("reset_official", member.userID)
	before := resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE evidence->>'trigger_kind'='official'`)
	type result struct {
		batch *domain.CarpoolResetBatch
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			batch, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, op)
			results <- result{batch: batch, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var batchID int64
	for got := range results {
		require.NoError(t, got.err)
		require.NotNil(t, got.batch)
		if batchID == 0 {
			batchID = got.batch.ID
		}
		require.Equal(t, batchID, got.batch.ID)
	}
	require.Equal(t, before+1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE evidence->>'trigger_kind'='official'`))
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))
}

func TestCarpoolOfficialReset_LegacyFixedSnapshotRefillsInPlace(t *testing.T) {
	ctx := context.Background()
	officialResetIsolateGlobalScope(t)
	now := carpoolDatabaseNow(t, ctx)
	member := createResetExecutionMember(t, domain.CarpoolGlobalScopeID, now, 5,
		decimal.RequireFromString("157"), decimal.RequireFromString("57"))
	repo := resetRepositoryAt(now)
	batch, err := repo.ExecuteOfficialReset(ctx, domain.CarpoolGlobalScopeID, carpoolTestOperation("reset_official", member.userID))
	require.NoError(t, err)
	require.Equal(t, 1, batch.TargetCount)
	require.True(t, decimal.RequireFromString("100").Equal(batch.GrantedUSD))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, member.termID))
	require.True(t, member.baseQuotaUSD.Equal(resetCycleBaseBalance(t, member.cycleID)))
}

func officialResetIsolateGlobalScope(t *testing.T) {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE carpool_terms SET status='terminated',terminated_at=clock_timestamp(),termination_reason='official reset test isolation' WHERE scope_id=$1 AND status IN ('pending','active')`, domain.CarpoolGlobalScopeID)
	require.NoError(t, err)
}

func int64Pointer(value int64) *int64 { return &value }

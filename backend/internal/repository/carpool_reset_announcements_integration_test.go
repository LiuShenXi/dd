//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolResetAnnouncements_PublishCorrectOriginalAndTargetCurrentOrFutureMembers(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	current := createResetExecutionMember(t, scopeID, slotAt, 1,
		resetDecimal("550"), resetDecimal("100"))
	future := createResetExecutionMember(t, scopeID, slotAt, 1,
		resetDecimal("550"), resetDecimal("100"))
	expired := createResetExecutionMember(t, scopeID, slotAt, 1,
		resetDecimal("550"), resetDecimal("100"))
	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET status='pending',starts_at=$1,expires_at=$2 WHERE id=$3`, slotAt.Add(24*time.Hour), slotAt.Add(31*24*time.Hour), future.termID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET starts_at=$1,expires_at=$2 WHERE id=$3`, slotAt.Add(-31*24*time.Hour), slotAt.Add(-time.Hour), expired.termID)
	require.NoError(t, err)

	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, current.userID)
	makeOnlyResetOutboxesDue(t, batch.ID)
	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)

	var originalID int64
	var originalContent, targetingJSON string
	var originalRevision int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT id,content,targeting::text,source_revision FROM announcements
		WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'
	`, batch.ID).Scan(&originalID, &originalContent, &targetingJSON, &originalRevision))
	require.Contains(t, originalContent, slotAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"))
	require.Equal(t, 0, originalRevision)
	var targeting domain.AnnouncementTargeting
	require.NoError(t, json.Unmarshal([]byte(targetingJSON), &targeting))
	require.True(t, targeting.MatchesWithCarpool(0, nil, map[int64]struct{}{scopeID: {}}))
	require.False(t, targeting.MatchesWithCarpool(0, nil, map[int64]struct{}{scopeID + 1: {}}))

	for _, tc := range []struct {
		name   string
		userID int64
		want   bool
	}{
		{name: "current", userID: current.userID, want: true},
		{name: "future", userID: future.userID, want: true},
		{name: "expired", userID: expired.userID, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopes, listErr := repo.ListAnnouncementCarpoolScopes(ctx, tc.userID, slotAt.Add(-time.Hour))
			require.NoError(t, listErr)
			_, exists := scopes[scopeID]
			require.Equal(t, tc.want, exists)
		})
	}

	invalidScheduledAt := slotAt.Add(2 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_reset_batches SET scheduled_at=$1 WHERE id=$2`, invalidScheduledAt, batch.ID)
	require.NoError(t, err)
	repo.clockSQL = resetClockSQL(slotAt.Add(2 * time.Hour))
	corrected, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, 1, corrected.ScheduleRevision)
	require.Equal(t, slotAt.AddDate(0, 0, 1), *corrected.ScheduledAt)
	makeOnlyResetOutboxesDue(t, batch.ID)
	published, err = repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)

	var correctedOriginalID int64
	var correctedOriginalContent string
	var correctedOriginalRevision int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT id,content,source_revision FROM announcements
		WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'
	`, batch.ID).Scan(&correctedOriginalID, &correctedOriginalContent, &correctedOriginalRevision))
	require.Equal(t, originalID, correctedOriginalID)
	require.NotEqual(t, originalContent, correctedOriginalContent)
	require.Contains(t, correctedOriginalContent, corrected.ScheduledAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"))
	require.Equal(t, 1, correctedOriginalRevision)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='correction' AND source_revision=1`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='correction' AND schedule_revision=1 AND status='published' AND original_announcement_id=$2`, batch.ID, originalID))
}

func TestCarpoolResetAnnouncements_StaleRevisionIsSuppressed(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	scopeID := nextResetIntegrationScopeID()
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, createResetIntegrationActor(t))

	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_reset_batches SET scheduled_at=$1 WHERE id=$2`, slotAt.Add(2*time.Hour), batch.ID)
	require.NoError(t, err)
	repo.clockSQL = resetClockSQL(slotAt.Add(2 * time.Hour))
	corrected, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, 1, corrected.ScheduleRevision)
	makeOnlyResetOutboxesDue(t, batch.ID)

	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='qualification' AND schedule_revision=0 AND status='failed' AND last_error='superseded'`, batch.ID))
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_revision=0`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification' AND source_revision=1`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='correction' AND source_revision=1`, batch.ID))
}

func TestCarpoolResetAnnouncements_FailureIsIsolated(t *testing.T) {
	ctx := context.Background()
	badScopeID := nextResetIntegrationScopeID()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO carpool_reset_scope_states(scope_id) VALUES($1)`, badScopeID)
	require.NoError(t, err)
	var badBatchID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_reset_batches(
			scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,
			announcement_state,qualification_source,source_event_key_hash
		) VALUES($1,'scheduled',clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp(),
			'pending','administrator',$2)
		RETURNING id
	`, badScopeID, resetIntegrationHash("bad-batch")).Scan(&badBatchID))
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_reset_scope_states SET pending_batch_id=$1 WHERE scope_id=$2`, badBatchID, badScopeID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO carpool_reset_announcement_outbox(
			batch_id,scope_id,event_kind,schedule_revision,status,title,content,next_attempt_at
		) VALUES($1,$2,'completed',0,'pending','bad completion','must fail',clock_timestamp()-INTERVAL '1 second')
	`, badBatchID, badScopeID)
	require.NoError(t, err)

	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	goodScopeID := nextResetIntegrationScopeID()
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	goodBatch := registerResetQualification(t, repo, goodScopeID, createResetIntegrationActor(t))
	makeOnlyResetOutboxesDue(t, badBatchID, goodBatch.ID)

	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='completed' AND status='failed' AND last_error='publication_failed'`, badBatchID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='qualification' AND status='published'`, goodBatch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'`, goodBatch.ID))
}

func TestCarpoolResetAnnouncements_SQLFailureIsIsolatedAndRetriesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	badBatch := registerResetQualification(t, repo, nextResetIntegrationScopeID(), createResetIntegrationActor(t))
	goodBatch := registerResetQualification(t, repo, nextResetIntegrationScopeID(), createResetIntegrationActor(t))

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "fail_reset_ann_" + suffix
	triggerName := "trg_reset_ann_" + suffix
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.source_type = 'carpool_reset' AND NEW.source_id = %d THEN
				RAISE EXCEPTION 'forced reset announcement insert failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON announcements
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, functionName, badBatch.ID, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON announcements`, triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	makeOnlyResetOutboxesDue(t, badBatch.ID, goodBatch.ID)
	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)

	var failedStatus, failedError string
	var failedAttempts int
	var nextAttemptAt, failureRecordedAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,attempts,last_error,next_attempt_at,clock_timestamp()
		FROM carpool_reset_announcement_outbox
		WHERE batch_id=$1 AND event_kind='qualification'
	`, badBatch.ID).Scan(&failedStatus, &failedAttempts, &failedError, &nextAttemptAt, &failureRecordedAt))
	require.Equal(t, "failed", failedStatus)
	require.Equal(t, 1, failedAttempts)
	require.Equal(t, "publication_failed", failedError)
	require.True(t, nextAttemptAt.After(failureRecordedAt))
	require.WithinDuration(t, failureRecordedAt.Add(30*time.Second), nextAttemptAt, 2*time.Second)
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1`, badBatch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_announcement_outbox WHERE batch_id=$1 AND event_kind='qualification' AND status='published' AND attempts=1`, goodBatch.ID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'`, goodBatch.ID))

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER %s ON announcements`, triggerName))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP FUNCTION %s()`, functionName))
	require.NoError(t, err)

	makeOnlyResetOutboxesDue(t, badBatch.ID)
	published, err = repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)
	published, err = repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Zero(t, published)

	var recoveredStatus string
	var recoveredAttempts int
	var errorCleared, announcementLinked bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,attempts,last_error IS NULL,announcement_id IS NOT NULL
		FROM carpool_reset_announcement_outbox
		WHERE batch_id=$1 AND event_kind='qualification'
	`, badBatch.ID).Scan(&recoveredStatus, &recoveredAttempts, &errorCleared, &announcementLinked))
	require.Equal(t, "published", recoveredStatus)
	require.Equal(t, 2, recoveredAttempts)
	require.True(t, errorCleared)
	require.True(t, announcementLinked)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'`, badBatch.ID))
}

func TestCarpoolResetAnnouncements_RecalculatesRelativeDateAtDelayedPublication(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	baseSlot, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	targetSlot := baseSlot.AddDate(0, 0, 1)
	qualificationAt := baseSlot.Add(-time.Hour)
	publicationAt := targetSlot.Add(-time.Hour)
	lastSuccessful := targetSlot.Add(-48 * time.Hour)
	scopeID := nextResetIntegrationScopeID()
	setResetScopeState(t, scopeID, &lastSuccessful)
	repo := resetRepositoryAt(qualificationAt)
	batch := registerResetQualification(t, repo, scopeID, createResetIntegrationActor(t))
	require.Equal(t, targetSlot, *batch.ScheduledAt)

	var queuedContent string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT content FROM carpool_reset_announcement_outbox
		WHERE batch_id=$1 AND event_kind='qualification'
	`, batch.ID).Scan(&queuedContent))
	require.Contains(t, queuedContent, "明天晚上十点")

	repo.clockSQL = resetClockSQL(publicationAt)
	makeOnlyResetOutboxesDue(t, batch.ID)
	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)

	var publishedContent string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT content FROM announcements
		WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification'
	`, batch.ID).Scan(&publishedContent))
	require.Contains(t, publishedContent, "今天晚上十点")
	require.NotContains(t, publishedContent, "明天晚上十点")
	require.Contains(t, publishedContent, targetSlot.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"))
}

func TestCarpoolResetAnnouncements_CompletedBatchSuppressesObsoletePromises(t *testing.T) {
	ctx := context.Background()
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	rescheduledExecutionAt := slotAt.AddDate(0, 0, 1)
	scopeID := nextResetIntegrationScopeID()
	member := createResetExecutionMember(t, scopeID, rescheduledExecutionAt, 1,
		resetDecimal("550"), resetDecimal("100"))
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	batch := registerResetQualification(t, repo, scopeID, member.userID)

	_, err := integrationDB.ExecContext(ctx, `UPDATE carpool_reset_batches SET scheduled_at=$1 WHERE id=$2`, slotAt.Add(2*time.Hour), batch.ID)
	require.NoError(t, err)
	repo.clockSQL = resetClockSQL(slotAt.Add(2 * time.Hour))
	corrected, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, 1, corrected.ScheduleRevision)
	require.Equal(t, rescheduledExecutionAt, *corrected.ScheduledAt)

	repo.clockSQL = resetClockSQL(*corrected.ScheduledAt)
	completed, err := repo.ExecuteResetBatch(ctx, batch.ID, nil)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetStatusCompleted, completed.Status)
	require.Equal(t, 1, completed.TargetCount)

	makeOnlyResetOutboxesDue(t, batch.ID)
	published, err := repo.PublishResetAnnouncements(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Equal(t, 2, resetRowCount(t, `
		SELECT COUNT(*) FROM carpool_reset_announcement_outbox
		WHERE batch_id=$1 AND event_kind IN ('qualification','correction')
		  AND status='failed' AND attempts=1 AND last_error='superseded'
		  AND next_attempt_at='infinity'::timestamptz
	`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `
		SELECT COUNT(*) FROM carpool_reset_announcement_outbox
		WHERE batch_id=$1 AND event_kind='completed' AND status='published' AND attempts=1
	`, batch.ID))
	require.Zero(t, resetRowCount(t, `
		SELECT COUNT(*) FROM announcements
		WHERE source_type='carpool_reset' AND source_id=$1
		  AND source_event_kind IN ('qualification','correction')
	`, batch.ID))
	require.Equal(t, 1, resetRowCount(t, `
		SELECT COUNT(*) FROM announcements
		WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='completed'
	`, batch.ID))
}

func makeOnlyResetOutboxesDue(t *testing.T, batchIDs ...int64) {
	t.Helper()
	require.NotEmpty(t, batchIDs)
	ids := append([]int64(nil), batchIDs...)
	_, err := integrationDB.ExecContext(context.Background(), `
		UPDATE carpool_reset_announcement_outbox
		SET next_attempt_at=CASE
			WHEN batch_id=ANY($1) THEN clock_timestamp()-INTERVAL '1 second'
			ELSE 'infinity'::timestamptz
		END
		WHERE status IN ('pending','failed')
	`, pq.Array(ids))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
			UPDATE carpool_reset_announcement_outbox
			SET next_attempt_at='infinity'::timestamptz
			WHERE batch_id=ANY($1) AND status IN ('pending','failed')
		`, pq.Array(ids))
	})
}

func resetDecimal(raw string) decimal.Decimal {
	return decimal.RequireFromString(raw)
}

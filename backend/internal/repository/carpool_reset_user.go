package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

func (r *CarpoolResetRepository) ListAnnouncementCarpoolScopes(ctx context.Context, userID int64, now time.Time) (map[int64]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT scope_id FROM carpool_terms WHERE user_id=$1 AND status IN ('pending','active') AND expires_at>$2`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scopes := make(map[int64]struct{})
	for rows.Next() {
		var scopeID int64
		if err := rows.Scan(&scopeID); err != nil {
			return nil, err
		}
		scopes[scopeID] = struct{}{}
	}
	return scopes, rows.Err()
}

func (r *CarpoolResetRepository) UserResetWindow(ctx context.Context, userID int64, now time.Time) (domain.CarpoolUserResetWindow, error) {
	window := domain.CarpoolUserResetWindow{Status: "none"}
	var scopeID int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT scope_id FROM carpool_terms
		WHERE user_id=$1 AND status IN ('pending','active') AND expires_at>$2
		ORDER BY CASE WHEN starts_at<=$2 THEN 0 ELSE 1 END,starts_at,id LIMIT 1`, userID, now).Scan(&scopeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return window, nil
		}
		return window, err
	}
	var batchStatus string
	var scheduledAt sql.NullTime
	var revision int64
	var delayReason sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT status,scheduled_at,schedule_revision,delay_reason
		FROM carpool_reset_batches
		WHERE scope_id=$1 AND status IN ('scheduled','running') AND qualified_at IS NOT NULL
		ORDER BY scheduled_at,id LIMIT 1`, scopeID).Scan(&batchStatus, &scheduledAt, &revision, &delayReason)
	if errors.Is(err, sql.ErrNoRows) {
		var completedRevision int64
		completedErr := r.db.QueryRowContext(ctx, `
			SELECT b.schedule_revision FROM carpool_reset_batches b
			JOIN carpool_reset_targets target ON target.batch_id=b.id AND target.status='succeeded'
			JOIN carpool_terms term ON term.id=target.term_id
			WHERE term.user_id=$1 AND b.scope_id=$2 AND b.status='completed'
			ORDER BY b.completed_at DESC,b.id DESC LIMIT 1`, userID, scopeID).Scan(&completedRevision)
		if completedErr == nil {
			window.Status = "completed"
			window.ScheduleRevision = completedRevision
			window.EligibleForMe = true
			return window, nil
		}
		if errors.Is(completedErr, sql.ErrNoRows) {
			return window, nil
		}
		return window, completedErr
	}
	if err != nil {
		return window, err
	}
	window.Status = "scheduled"
	if batchStatus == domain.CarpoolResetStatusRunning {
		window.Status = "executing"
	} else if delayReason.Valid {
		window.Status = "delayed"
	}
	window.ScheduledAt = nullableTime(scheduledAt)
	window.ScheduleRevision = revision
	if !scheduledAt.Valid {
		return window, nil
	}
	var eligible bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM carpool_terms WHERE user_id=$1 AND scope_id=$2 AND status IN ('pending','active') AND starts_at<=$3 AND expires_at>$3)`, userID, scopeID, scheduledAt.Time).Scan(&eligible); err != nil {
		return window, err
	}
	window.EligibleForMe = eligible
	if !eligible {
		reason := "term_not_covered"
		window.IneligibleReason = &reason
	}
	return window, nil
}

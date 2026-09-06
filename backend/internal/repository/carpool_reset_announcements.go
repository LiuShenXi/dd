package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

func enqueueResetAnnouncementTx(ctx context.Context, tx *sql.Tx, batch *domain.CarpoolResetBatch, eventKind string, now time.Time) error {
	if batch == nil {
		return errors.New("reset batch is required")
	}
	title, content, err := resetAnnouncementCopy(batch, eventKind, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_announcement_outbox(batch_id,scope_id,event_kind,schedule_revision,status,title,content,next_attempt_at) VALUES($1,$2,$3,$4,'pending',$5,$6,$7) ON CONFLICT(batch_id,event_kind,schedule_revision) DO NOTHING`, batch.ID, batch.ScopeID, eventKind, batch.ScheduleRevision, title, content, now)
	return err
}

func resetAnnouncementCopy(batch *domain.CarpoolResetBatch, eventKind string, now time.Time) (string, string, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	switch eventKind {
	case "qualification", "correction":
		if batch.ScheduledAt == nil {
			return "", "", errors.New("scheduled reset announcement requires scheduled_at")
		}
		scheduled := batch.ScheduledAt.In(location)
		relative := resetRelativeDate(now.In(location), scheduled)
		absolute := scheduled.Format("2006-01-02 15:04:05")
		if eventKind == "qualification" {
			return "拼车重置通知", fmt.Sprintf("已获取到新的重置额度资格，%s晚上十点大家额度加满！\n\n计划时间：%s（北京时间）", relative, absolute), nil
		}
		return "拼车重置改期通知", fmt.Sprintf("拼车重置计划已调整至%s晚上十点。\n\n新计划时间：%s（北京时间）", relative, absolute), nil
	case "completed":
		if batch.CompletedAt == nil {
			return "", "", errors.New("completed reset announcement requires completed_at")
		}
		return "拼车重置完成", fmt.Sprintf("本次拼车额度重置已完成。\n\n实际时间：%s（北京时间）", batch.CompletedAt.In(location).Format("2006-01-02 15:04:05")), nil
	default:
		return "", "", fmt.Errorf("unsupported reset announcement event %q", eventKind)
	}
}

func resetRelativeDate(now, scheduled time.Time) string {
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	scheduledDate := time.Date(scheduled.Year(), scheduled.Month(), scheduled.Day(), 0, 0, 0, 0, scheduled.Location())
	days := int(scheduledDate.Sub(nowDate) / (24 * time.Hour))
	switch days {
	case 0:
		return "今天"
	case 1:
		return "明天"
	case 2:
		return "后天"
	default:
		return scheduled.Format("1 月 2 日")
	}
}

func (r *CarpoolResetRepository) PublishResetAnnouncements(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM carpool_reset_announcement_outbox WHERE status IN ('pending','failed') AND next_attempt_at <= clock_timestamp() ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	published := 0
	for _, id := range ids {
		ok, publishErr := r.publishResetAnnouncement(ctx, id)
		if publishErr != nil {
			r.recordResetAnnouncementFailure(ctx, id)
			continue
		}
		if ok {
			published++
		}
	}
	return published, nil
}

func (r *CarpoolResetRepository) publishResetAnnouncement(ctx context.Context, outboxID int64) (_ bool, err error) {
	var batchID int64
	if err := r.db.QueryRowContext(ctx, `SELECT batch_id FROM carpool_reset_announcement_outbox WHERE id=$1`, outboxID).Scan(&batchID); err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var batch domain.CarpoolResetBatch
	var scheduledAt, completedAt sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT scope_id,status,schedule_revision,scheduled_at,completed_at FROM carpool_reset_batches WHERE id=$1 FOR UPDATE`, batchID).Scan(&batch.ScopeID, &batch.Status, &batch.ScheduleRevision, &scheduledAt, &completedAt); err != nil {
		return false, err
	}
	batch.ID = batchID
	batch.ScheduledAt = nullableTime(scheduledAt)
	batch.CompletedAt = nullableTime(completedAt)
	var eventKind, outboxStatus string
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT event_kind,schedule_revision,status FROM carpool_reset_announcement_outbox WHERE id=$1 FOR UPDATE`, outboxID).Scan(&eventKind, &revision, &outboxStatus); err != nil {
		return false, err
	}
	if outboxStatus != "pending" && outboxStatus != "failed" {
		return false, tx.Commit()
	}
	if eventKind == "completed" {
		if batch.Status != domain.CarpoolResetStatusCompleted {
			return false, errors.New("completion announcement before batch completion")
		}
	} else if revision != batch.ScheduleRevision || batch.Status == domain.CarpoolResetStatusCompleted || batch.Status == domain.CarpoolResetStatusCancelled {
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_announcement_outbox SET status='failed',attempts=attempts+1,last_error='superseded',next_attempt_at='infinity',updated_at=clock_timestamp() WHERE id=$1`, outboxID); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return false, err
	}
	title, content, err := resetAnnouncementCopy(&batch, eventKind, now)
	if err != nil {
		return false, err
	}
	targeting, err := json.Marshal(domain.AnnouncementTargeting{AnyOf: []domain.AnnouncementConditionGroup{{AllOf: []domain.AnnouncementCondition{{Type: domain.AnnouncementConditionTypeCarpoolScope, Operator: domain.AnnouncementOperatorIn, ScopeIDs: []int64{batch.ScopeID}}}}}})
	if err != nil {
		return false, err
	}
	var announcementID int64
	if eventKind == "correction" {
		qualificationTitle, qualificationContent, copyErr := resetAnnouncementCopy(&batch, "qualification", now)
		if copyErr != nil {
			return false, copyErr
		}
		var originalID int64
		originalErr := tx.QueryRowContext(ctx, `SELECT id FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification' ORDER BY id LIMIT 1 FOR UPDATE`, batchID).Scan(&originalID)
		if originalErr == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE announcements SET title=$1,content=$2,updated_at=$3,source_revision=$4 WHERE id=$5`, qualificationTitle, qualificationContent, now, revision, originalID); err != nil {
				return false, err
			}
		} else if errors.Is(originalErr, sql.ErrNoRows) {
			if err = tx.QueryRowContext(ctx, `INSERT INTO announcements(title,content,status,notify_mode,targeting,starts_at,source_type,source_id,source_event_kind,source_revision,created_at,updated_at) VALUES($1,$2,'active','popup',$3::jsonb,$4,'carpool_reset',$5,'qualification',$6,$4,$4) ON CONFLICT(source_type,source_id,source_event_kind,source_revision) WHERE source_type IS NOT NULL AND source_id IS NOT NULL AND source_event_kind IS NOT NULL DO UPDATE SET title=EXCLUDED.title,content=EXCLUDED.content,updated_at=EXCLUDED.updated_at RETURNING id`, qualificationTitle, qualificationContent, string(targeting), now, batchID, revision).Scan(&originalID); err != nil {
				return false, err
			}
		} else {
			return false, originalErr
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO announcements(title,content,status,notify_mode,targeting,starts_at,source_type,source_id,source_event_kind,source_revision,created_at,updated_at) VALUES($1,$2,'active','popup',$3::jsonb,$4,'carpool_reset',$5,'correction',$6,$4,$4) ON CONFLICT(source_type,source_id,source_event_kind,source_revision) WHERE source_type IS NOT NULL AND source_id IS NOT NULL AND source_event_kind IS NOT NULL DO UPDATE SET title=EXCLUDED.title,content=EXCLUDED.content,updated_at=EXCLUDED.updated_at RETURNING id`, title, content, string(targeting), now, batchID, revision).Scan(&announcementID); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_announcement_outbox SET original_announcement_id=$1 WHERE id=$2`, originalID, outboxID); err != nil {
			return false, err
		}
	} else {
		if err = tx.QueryRowContext(ctx, `INSERT INTO announcements(title,content,status,notify_mode,targeting,starts_at,source_type,source_id,source_event_kind,source_revision,created_at,updated_at) VALUES($1,$2,'active','popup',$3::jsonb,$4,'carpool_reset',$5,$6,$7,$4,$4) ON CONFLICT(source_type,source_id,source_event_kind,source_revision) WHERE source_type IS NOT NULL AND source_id IS NOT NULL AND source_event_kind IS NOT NULL DO UPDATE SET updated_at=EXCLUDED.updated_at RETURNING id`, title, content, string(targeting), now, batchID, eventKind, revision).Scan(&announcementID); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_announcement_outbox SET status='published',announcement_id=$1,attempts=attempts+1,last_error=NULL,published_at=$2,updated_at=$2 WHERE id=$3`, announcementID, now, outboxID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET announcement_state='published',updated_at=$1 WHERE id=$2 AND schedule_revision=$3`, now, batchID, revision); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *CarpoolResetRepository) recordResetAnnouncementFailure(ctx context.Context, outboxID int64) {
	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = r.db.ExecContext(retryCtx, `UPDATE carpool_reset_announcement_outbox SET status='failed',attempts=attempts+1,last_error='publication_failed',next_attempt_at=clock_timestamp()+CASE WHEN attempts >= 5 THEN INTERVAL '15 minutes' ELSE INTERVAL '30 seconds' * power(2,attempts) END,updated_at=clock_timestamp() WHERE id=$1 AND status IN ('pending','failed')`, outboxID)
}

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type CarpoolResetRepository struct {
	db       *sql.DB
	clockSQL string
}

func NewCarpoolResetRepository(db *sql.DB) *CarpoolResetRepository {
	return &CarpoolResetRepository{db: db, clockSQL: `SELECT clock_timestamp()`}
}

const resetBatchSelect = `SELECT b.id,b.scope_id,b.status,b.detected_at,b.qualified_at,b.slot_at,b.scheduled_at,b.schedule_revision,b.effective_at,b.completed_at,b.delay_reason,b.announcement_state,b.qualification_source,b.source_event_key_hash,(SELECT COUNT(*) FROM carpool_reset_targets t WHERE t.batch_id=b.id AND t.status='succeeded'),COALESCE((SELECT SUM(t.granted_usd) FROM carpool_reset_targets t WHERE t.batch_id=b.id AND t.status='succeeded'),0)::text FROM carpool_reset_batches b`

func scanResetBatch(row carpoolRowScanner) (*domain.CarpoolResetBatch, error) {
	var batch domain.CarpoolResetBatch
	var qualifiedAt, slotAt, scheduledAt, effectiveAt, completedAt sql.NullTime
	var delayReason sql.NullString
	var granted string
	if err := row.Scan(
		&batch.ID, &batch.ScopeID, &batch.Status, &batch.DetectedAt,
		&qualifiedAt, &slotAt, &scheduledAt, &batch.ScheduleRevision,
		&effectiveAt, &completedAt, &delayReason, &batch.AnnouncementState,
		&batch.QualificationSource, &batch.SourceEventKeyHash,
		&batch.TargetCount, &granted,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	batch.QualifiedAt = nullableTime(qualifiedAt)
	batch.SlotAt = nullableTime(slotAt)
	batch.ScheduledAt = nullableTime(scheduledAt)
	batch.EffectiveAt = nullableTime(effectiveAt)
	batch.CompletedAt = nullableTime(completedAt)
	if delayReason.Valid {
		batch.DelayReason = &delayReason.String
	}
	amount, err := decimal.NewFromString(granted)
	if err != nil {
		return nil, err
	}
	batch.GrantedUSD = amount
	return &batch, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func getResetBatchTx(ctx context.Context, tx *sql.Tx, batchID int64, forUpdate bool) (*domain.CarpoolResetBatch, error) {
	query := resetBatchSelect + ` WHERE b.id=$1`
	if forUpdate {
		query += ` FOR UPDATE OF b`
	}
	return scanResetBatch(tx.QueryRowContext(ctx, query, batchID))
}

func (r *CarpoolResetRepository) ListResetBatches(ctx context.Context, filters domain.CarpoolResetBatchFilters) ([]domain.CarpoolResetBatch, int64, error) {
	normalizeResetPagination(&filters.Page, &filters.PageSize)
	if !validResetStatus(filters.Status) {
		return nil, 0, service.ErrCarpoolResetInvalid
	}
	where := []string{"TRUE"}
	args := []any{}
	if filters.ScopeID != nil {
		args = append(args, *filters.ScopeID)
		where = append(where, fmt.Sprintf("b.scope_id=$%d", len(args)))
	}
	if filters.Status != "" {
		args = append(args, filters.Status)
		where = append(where, fmt.Sprintf("b.status=$%d", len(args)))
	}
	condition := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_reset_batches b WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)
	query := resetBatchSelect + ` WHERE ` + condition + fmt.Sprintf(" ORDER BY b.detected_at DESC,b.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.CarpoolResetBatch, 0)
	for rows.Next() {
		batch, scanErr := scanResetBatch(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *batch)
	}
	return items, total, rows.Err()
}

func (r *CarpoolResetRepository) RegisterResetQualification(ctx context.Context, scopeID int64, sourceEventKey, reason string, operation domain.CarpoolOperation) (_ *domain.CarpoolResetBatch, err error) {
	if scopeID <= 0 || strings.TrimSpace(sourceEventKey) == "" || strings.TrimSpace(reason) == "" {
		return nil, service.ErrCarpoolResetInvalid
	}
	if err = validateOperation(operation, "reset_qualification"); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached domain.CarpoolResetBatch
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}

	lastSuccessful, pendingBatchID, err := lockResetScopeTx(ctx, tx, scopeID)
	if err != nil {
		return nil, err
	}
	sourceHash := operationKeyHash(fmt.Sprintf("reset-source:%d:%s", scopeID, strings.TrimSpace(sourceEventKey)))
	var duplicateID int64
	duplicateErr := tx.QueryRowContext(ctx, `SELECT batch_id FROM carpool_reset_qualifications WHERE scope_id=$1 AND source_event_key_hash=$2`, scopeID, sourceHash).Scan(&duplicateID)
	if duplicateErr == nil {
		return nil, service.ErrCarpoolIdempotencyConflict
	}
	if !errors.Is(duplicateErr, sql.ErrNoRows) {
		return nil, duplicateErr
	}

	var batch *domain.CarpoolResetBatch
	if pendingBatchID != nil {
		batch, err = getResetBatchTx(ctx, tx, *pendingBatchID, true)
	}
	if err != nil && !errors.Is(err, service.ErrCarpoolNotFound) {
		return nil, err
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if batch == nil || !isPendingResetStatus(batch.Status) {
		batch, err = createScheduledResetBatchTx(ctx, tx, scopeID, "administrator", sourceHash, now, lastSuccessful)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_qualifications(scope_id,batch_id,source,source_event_key_hash,reason,confirmed_at) VALUES($1,$2,'administrator',$3,$4,$5)`, scopeID, batch.ID, sourceHash, strings.TrimSpace(reason), now); err != nil {
		return nil, err
	}
	if err = recordOperationTx(ctx, tx, operation, "reset_batch", batch.ID, batch); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return batch, nil
}

func (r *CarpoolResetRepository) ScheduleResetBatch(ctx context.Context, batchID int64, reason string, operation domain.CarpoolOperation) (_ *domain.CarpoolResetBatch, err error) {
	if batchID <= 0 || strings.TrimSpace(reason) == "" {
		return nil, service.ErrCarpoolResetInvalid
	}
	if err = validateOperation(operation, "reset_schedule"); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached domain.CarpoolResetBatch
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}
	var scopeID int64
	if err = tx.QueryRowContext(ctx, `SELECT scope_id FROM carpool_reset_batches WHERE id=$1`, batchID).Scan(&scopeID); err != nil {
		return nil, translateResetNotFound(err)
	}
	lastSuccessful, pendingBatchID, err := lockResetScopeTx(ctx, tx, scopeID)
	if err != nil {
		return nil, err
	}
	batch, err := getResetBatchTx(ctx, tx, batchID, true)
	if err != nil {
		return nil, err
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if batch.Status == domain.CarpoolResetStatusCompleted || batch.Status == domain.CarpoolResetStatusCancelled {
		return nil, service.ErrCarpoolResetUnavailable
	}
	if batch.Status == domain.CarpoolResetStatusNeedsReview {
		if pendingBatchID != nil && *pendingBatchID != batch.ID {
			return nil, service.ErrCarpoolResetUnavailable
		}
		sourceHash := operationKeyHash(fmt.Sprintf("reset-review:%d:%s", batch.ID, operation.Key))
		slotAt, scheduledAt := service.NextCarpoolResetSchedule(now, lastSuccessful)
		revision := batch.ScheduleRevision + 1
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET status='scheduled',qualified_at=$1,slot_at=$2,scheduled_at=$3,schedule_revision=$4,delay_reason=$5,announcement_state='pending',updated_at=$1 WHERE id=$6 AND status='needs_review'`, now, slotAt, scheduledAt, revision, strings.TrimSpace(reason), batch.ID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_scope_states SET pending_batch_id=$1,revision=revision+1,updated_at=$2 WHERE scope_id=$3`, batch.ID, now, scopeID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_qualifications(scope_id,batch_id,source,source_event_key_hash,reason,confirmed_at) VALUES($1,$2,'administrator',$3,$4,$5)`, scopeID, batch.ID, sourceHash, strings.TrimSpace(reason), now); err != nil {
			return nil, err
		}
		batch.Status = domain.CarpoolResetStatusScheduled
		batch.QualifiedAt = &now
		batch.SlotAt = &slotAt
		batch.ScheduledAt = &scheduledAt
		batch.ScheduleRevision = revision
		batch.DelayReason = &reason
		batch.AnnouncementState = "pending"
		if err = enqueueResetAnnouncementTx(ctx, tx, batch, "qualification", now); err != nil {
			return nil, err
		}
		if err = recordOperationTx(ctx, tx, operation, "reset_batch", batch.ID, batch); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	}
	if batch.Status == domain.CarpoolResetStatusScheduled && batch.SlotAt != nil && batch.ScheduledAt != nil && batch.ScheduledAt.After(now) && service.ValidCarpoolResetSchedule(*batch.SlotAt, *batch.ScheduledAt, lastSuccessful) {
		if err = recordOperationTx(ctx, tx, operation, "reset_batch", batch.ID, batch); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	}
	batch, err = rescheduleResetBatchTx(ctx, tx, batch, now, lastSuccessful, strings.TrimSpace(reason))
	if err != nil {
		return nil, err
	}
	if err = recordOperationTx(ctx, tx, operation, "reset_batch", batch.ID, batch); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return batch, nil
}

func lockResetScopeTx(ctx context.Context, tx *sql.Tx, scopeID int64) (*time.Time, *int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO carpool_reset_scope_states(scope_id) VALUES($1) ON CONFLICT(scope_id) DO NOTHING`, scopeID); err != nil {
		return nil, nil, err
	}
	var lastSuccessful sql.NullTime
	var pendingBatchID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT last_successful_reset_at,pending_batch_id FROM carpool_reset_scope_states WHERE scope_id=$1 FOR UPDATE`, scopeID).Scan(&lastSuccessful, &pendingBatchID); err != nil {
		return nil, nil, err
	}
	if err := lockCarpoolScopeMembershipTx(ctx, tx, scopeID); err != nil {
		return nil, nil, err
	}
	var last *time.Time
	if lastSuccessful.Valid {
		value := lastSuccessful.Time
		last = &value
	}
	var pending *int64
	if pendingBatchID.Valid {
		value := pendingBatchID.Int64
		pending = &value
	}
	return last, pending, nil
}

func (r *CarpoolResetRepository) resetDatabaseNowTx(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	query := r.clockSQL
	if strings.TrimSpace(query) == "" {
		query = `SELECT clock_timestamp()`
	}
	if err := tx.QueryRowContext(ctx, query).Scan(&now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func createScheduledResetBatchTx(ctx context.Context, tx *sql.Tx, scopeID int64, source, sourceHash string, now time.Time, lastSuccessful *time.Time) (*domain.CarpoolResetBatch, error) {
	slotAt, scheduledAt := service.NextCarpoolResetSchedule(now, lastSuccessful)
	batch := &domain.CarpoolResetBatch{
		ScopeID: scopeID, Status: domain.CarpoolResetStatusScheduled, DetectedAt: now,
		QualifiedAt: &now, SlotAt: &slotAt, ScheduledAt: &scheduledAt,
		ScheduleRevision: 0, AnnouncementState: "pending",
		QualificationSource: source, SourceEventKeyHash: sourceHash,
		GrantedUSD: decimal.Zero,
	}
	err := tx.QueryRowContext(ctx, `INSERT INTO carpool_reset_batches(scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,schedule_revision,evidence,announcement_state,qualification_source,source_event_key_hash) VALUES($1,$2,$3,$3,$4,$5,0,'{}'::jsonb,'pending',$6,$7) RETURNING id`, scopeID, batch.Status, now, slotAt, scheduledAt, source, sourceHash).Scan(&batch.ID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_scope_states SET pending_batch_id=$1,revision=revision+1,updated_at=$2 WHERE scope_id=$3`, batch.ID, now, scopeID); err != nil {
		return nil, err
	}
	if err = enqueueResetAnnouncementTx(ctx, tx, batch, "qualification", now); err != nil {
		return nil, err
	}
	return batch, nil
}

func rescheduleResetBatchTx(ctx context.Context, tx *sql.Tx, batch *domain.CarpoolResetBatch, now time.Time, lastSuccessful *time.Time, reason string) (*domain.CarpoolResetBatch, error) {
	slotAt, scheduledAt := service.NextCarpoolResetSchedule(now, lastSuccessful)
	revision := batch.ScheduleRevision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET status='scheduled',slot_at=$1,scheduled_at=$2,schedule_revision=$3,delay_reason=$4,announcement_state='correction_pending',updated_at=$5 WHERE id=$6`, slotAt, scheduledAt, revision, reason, now, batch.ID); err != nil {
		return nil, err
	}
	batch.Status = domain.CarpoolResetStatusScheduled
	batch.SlotAt = &slotAt
	batch.ScheduledAt = &scheduledAt
	batch.ScheduleRevision = revision
	batch.DelayReason = &reason
	batch.AnnouncementState = "correction_pending"
	if err := enqueueResetAnnouncementTx(ctx, tx, batch, "correction", now); err != nil {
		return nil, err
	}
	return batch, nil
}

func normalizeResetPagination(page, pageSize *int) {
	if *page < 1 {
		*page = 1
	}
	if *pageSize < 1 {
		*pageSize = 20
	}
	if *pageSize > 200 {
		*pageSize = 200
	}
}

func validResetStatus(status string) bool {
	switch status {
	case "", domain.CarpoolResetStatusQualified, domain.CarpoolResetStatusScheduled, domain.CarpoolResetStatusRunning, domain.CarpoolResetStatusCompleted, domain.CarpoolResetStatusCancelled, domain.CarpoolResetStatusNeedsReview:
		return true
	default:
		return false
	}
}

func isPendingResetStatus(status string) bool {
	return status == domain.CarpoolResetStatusQualified || status == domain.CarpoolResetStatusScheduled || status == domain.CarpoolResetStatusRunning
}

func translateResetNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrCarpoolNotFound
	}
	return err
}

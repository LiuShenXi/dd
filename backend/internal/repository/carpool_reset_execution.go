package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func (r *CarpoolResetRepository) ListDueResetBatchIDs(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM carpool_reset_batches WHERE status='scheduled' AND scheduled_at <= clock_timestamp() ORDER BY scheduled_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *CarpoolResetRepository) ExecuteResetBatch(ctx context.Context, batchID int64, operation *domain.CarpoolOperation) (_ *domain.CarpoolResetBatch, err error) {
	if batchID <= 0 {
		return nil, service.ErrCarpoolResetInvalid
	}
	if operation != nil {
		if err = validateOperation(*operation, "reset_execute"); err != nil {
			return nil, err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if operation != nil {
		if err = lockOperationTx(ctx, tx, *operation); err != nil {
			return nil, err
		}
		if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, *operation); lookupErr != nil {
			return nil, lookupErr
		} else if replayed {
			var cached domain.CarpoolResetBatch
			if err = replayOperationResponse(response, &cached); err != nil {
				return nil, err
			}
			return &cached, nil
		}
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
	if batch.Status == domain.CarpoolResetStatusCompleted || batch.Status == domain.CarpoolResetStatusCancelled || batch.QualifiedAt == nil || batch.SlotAt == nil || batch.ScheduledAt == nil {
		return nil, service.ErrCarpoolResetUnavailable
	}
	if pendingBatchID == nil || *pendingBatchID != batch.ID {
		return nil, service.ErrCarpoolResetUnavailable
	}
	lockedTerms, err := lockResetTermsAndCyclesTx(ctx, tx, scopeID)
	if err != nil {
		return nil, err
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	switch state := service.CarpoolResetExecutionState(now, *batch.SlotAt, *batch.ScheduledAt, lastSuccessful); state {
	case "not_due":
		return nil, service.ErrCarpoolResetNotDue
	case "invalid":
		batch, err = rescheduleResetBatchTx(ctx, tx, batch, now, lastSuccessful, "invalid_schedule")
		if err != nil {
			return nil, err
		}
		if operation != nil {
			if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	case "cooldown":
		if lastSuccessful == nil {
			return nil, service.ErrCarpoolResetUnavailable
		}
		cooldownAt := lastSuccessful.Add(48 * time.Hour)
		if cooldownAt.Before(batch.SlotAt.Add(time.Minute)) {
			batch, err = delayResetWithinCurrentSlotTx(ctx, tx, batch, now, cooldownAt, state)
		} else {
			batch, err = rescheduleResetBatchTx(ctx, tx, batch, now, lastSuccessful, state)
		}
		if err != nil {
			return nil, err
		}
		if operation != nil {
			if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	case "missed":
		batch, err = rescheduleResetBatchTx(ctx, tx, batch, now, lastSuccessful, state)
		if err != nil {
			return nil, err
		}
		if operation != nil {
			if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	case "due":
	default:
		return nil, service.ErrCarpoolResetUnavailable
	}

	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET status='running',updated_at=$1 WHERE id=$2 AND status IN ('qualified','scheduled','running')`, now, batch.ID); err != nil {
		return nil, err
	}
	targetWindows := make([]resetTargetWindow, 0, len(lockedTerms))
	for _, term := range lockedTerms {
		if now.Before(term.StartsAt) || !now.Before(term.ExpiresAt) {
			continue
		}
		cycle, ensureErr := ensureCurrentCycleTx(ctx, tx, term.ID, now)
		if ensureErr != nil {
			if errors.Is(ensureErr, service.ErrCarpoolUnavailable) {
				continue
			}
			return nil, ensureErr
		}
		targetWindows = append(targetWindows, resetTargetWindow{
			TermID: term.ID, TermStartsAt: term.StartsAt, TermExpiresAt: term.ExpiresAt,
			CycleStartsAt: cycle.StartsAt, CycleEndsAt: cycle.EndsAt,
		})
		grant := cycle.BaseQuotaUSD.Sub(cycle.BaseBalanceUSD).Round(8)
		if grant.IsNegative() {
			grant = decimal.Zero
		}
		if grant.IsPositive() {
			if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET base_balance_usd=base_balance_usd+$1,updated_at=$2,revision=revision+1 WHERE id=$3 AND term_id=$4`, grant.StringFixed(8), now, cycle.ID, term.ID); err != nil {
				return nil, err
			}
			batchIDValue := batch.ID
			if err = insertLedgerTx(ctx, tx, term.UserID, term.ID, cycle.ID, "reset", domain.CarpoolBucketBase, grant, fmt.Sprintf("reset:%d:%d", batch.ID, cycle.ID), nil, nil, &batchIDValue, nil, nil, nil, "qualified special reset", now); err != nil {
				return nil, err
			}
		}
		if term.ResetMode == domain.CarpoolResetModeRolling {
			nextDeadline := now.Add(7 * 24 * time.Hour)
			if nextDeadline.After(term.ExpiresAt) {
				nextDeadline = term.ExpiresAt
			}
			if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET ends_at=$1,updated_at=$2,revision=revision+1 WHERE id=$3 AND term_id=$4`, nextDeadline, now, cycle.ID, term.ID); err != nil {
				return nil, err
			}
			cycle.EndsAt = nextDeadline
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at) VALUES($1,$2,$3,'succeeded',$4,$5)`, batch.ID, term.ID, cycle.ID, grant.StringFixed(8), now); err != nil {
			return nil, err
		}
	}
	completedAt, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if resetExecutionBoundaryMoved(now, completedAt, batch, lastSuccessful, lockedTerms, targetWindows) {
		previousRevision := batch.ScheduleRevision
		if err = tx.Rollback(); err != nil {
			return nil, err
		}
		return r.rescheduleResetBatchAfterRollback(ctx, batch.ID, previousRevision, operation, "execution_boundary_moved")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET status='completed',effective_at=$1,completed_at=$2,delay_reason=NULL,updated_at=$2 WHERE id=$3`, now, completedAt, batch.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_scope_states SET last_successful_reset_at=$1,pending_batch_id=NULL,revision=revision+1,updated_at=$1 WHERE scope_id=$2 AND pending_batch_id=$3`, completedAt, scopeID, batch.ID); err != nil {
		return nil, err
	}
	batch, err = getResetBatchTx(ctx, tx, batch.ID, false)
	if err != nil {
		return nil, err
	}
	if err = enqueueResetAnnouncementTx(ctx, tx, batch, "completed", completedAt); err != nil {
		return nil, err
	}
	if operation != nil {
		if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return batch, nil
}

type lockedResetTerm struct {
	ID        int64
	UserID    int64
	StartsAt  time.Time
	ExpiresAt time.Time
	ResetMode string
}

type resetTargetWindow struct {
	TermID        int64
	TermStartsAt  time.Time
	TermExpiresAt time.Time
	CycleStartsAt time.Time
	CycleEndsAt   time.Time
}

func resetExecutionBoundaryMoved(startedAt, completedAt time.Time, batch *domain.CarpoolResetBatch, lastSuccessful *time.Time, terms []lockedResetTerm, targets []resetTargetWindow) bool {
	if batch.SlotAt == nil || batch.ScheduledAt == nil || service.CarpoolResetExecutionState(completedAt, *batch.SlotAt, *batch.ScheduledAt, lastSuccessful) != "due" {
		return true
	}
	for _, term := range terms {
		activeAtStart := !startedAt.Before(term.StartsAt) && startedAt.Before(term.ExpiresAt)
		activeAtCompletion := !completedAt.Before(term.StartsAt) && completedAt.Before(term.ExpiresAt)
		if activeAtStart != activeAtCompletion {
			return true
		}
	}
	for _, target := range targets {
		if completedAt.Before(target.TermStartsAt) || !completedAt.Before(target.TermExpiresAt) || completedAt.Before(target.CycleStartsAt) || !completedAt.Before(target.CycleEndsAt) {
			return true
		}
	}
	return false
}

func (r *CarpoolResetRepository) rescheduleResetBatchAfterRollback(ctx context.Context, batchID int64, previousRevision int, operation *domain.CarpoolOperation, reason string) (_ *domain.CarpoolResetBatch, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if operation != nil {
		if err = lockOperationTx(ctx, tx, *operation); err != nil {
			return nil, err
		}
		if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, *operation); lookupErr != nil {
			return nil, lookupErr
		} else if replayed {
			var cached domain.CarpoolResetBatch
			if err = replayOperationResponse(response, &cached); err != nil {
				return nil, err
			}
			return &cached, nil
		}
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
	if batch.Status == domain.CarpoolResetStatusCompleted {
		if operation != nil {
			if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return batch, nil
	}
	if batch.Status == domain.CarpoolResetStatusCancelled || pendingBatchID == nil || *pendingBatchID != batch.ID {
		return nil, service.ErrCarpoolResetUnavailable
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if batch.ScheduleRevision <= previousRevision {
		batch, err = rescheduleResetBatchTx(ctx, tx, batch, now, lastSuccessful, reason)
		if err != nil {
			return nil, err
		}
	}
	if operation != nil {
		if err = recordOperationTx(ctx, tx, *operation, "reset_batch", batch.ID, batch); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return batch, nil
}

func lockResetTermsAndCyclesTx(ctx context.Context, tx *sql.Tx, scopeID int64) ([]lockedResetTerm, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,starts_at,expires_at,COALESCE(plan_snapshot->>'reset_mode','fixed') FROM carpool_terms WHERE scope_id=$1 AND status IN ('pending','active') ORDER BY id FOR UPDATE`, scopeID)
	if err != nil {
		return nil, err
	}
	terms := make([]lockedResetTerm, 0)
	for rows.Next() {
		var term lockedResetTerm
		if err := rows.Scan(&term.ID, &term.UserID, &term.StartsAt, &term.ExpiresAt, &term.ResetMode); err != nil {
			_ = rows.Close()
			return nil, err
		}
		terms = append(terms, term)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, term := range terms {
		cycleRows, queryErr := tx.QueryContext(ctx, `SELECT id FROM carpool_cycles WHERE term_id=$1 ORDER BY id FOR UPDATE`, term.ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for cycleRows.Next() {
			var cycleID int64
			if scanErr := cycleRows.Scan(&cycleID); scanErr != nil {
				_ = cycleRows.Close()
				return nil, scanErr
			}
		}
		if rowsErr := cycleRows.Err(); rowsErr != nil {
			_ = cycleRows.Close()
			return nil, rowsErr
		}
		if closeErr := cycleRows.Close(); closeErr != nil {
			return nil, closeErr
		}
	}
	return terms, nil
}

func delayResetWithinCurrentSlotTx(ctx context.Context, tx *sql.Tx, batch *domain.CarpoolResetBatch, now, scheduledAt time.Time, reason string) (*domain.CarpoolResetBatch, error) {
	revision := batch.ScheduleRevision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET status='scheduled',scheduled_at=$1,schedule_revision=$2,delay_reason=$3,announcement_state='correction_pending',updated_at=$4 WHERE id=$5`, scheduledAt, revision, reason, now, batch.ID); err != nil {
		return nil, err
	}
	batch.Status = domain.CarpoolResetStatusScheduled
	batch.ScheduledAt = &scheduledAt
	batch.ScheduleRevision = revision
	batch.DelayReason = &reason
	batch.AnnouncementState = "correction_pending"
	if err := enqueueResetAnnouncementTx(ctx, tx, batch, "correction", now); err != nil {
		return nil, err
	}
	return batch, nil
}

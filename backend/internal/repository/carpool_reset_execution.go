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
		cycle, _, _, applyErr := applyResetToTermTx(ctx, tx, term, batch.ID, now, "qualified special reset")
		if applyErr != nil {
			if errors.Is(applyErr, service.ErrCarpoolUnavailable) {
				continue
			}
			return nil, applyErr
		}
		targetWindows = append(targetWindows, resetTargetWindow{
			TermID: term.ID, TermStartsAt: term.StartsAt, TermExpiresAt: term.ExpiresAt,
			CycleStartsAt: cycle.StartsAt, CycleEndsAt: cycle.EndsAt,
		})
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

func (r *CarpoolResetRepository) ExecuteOfficialReset(ctx context.Context, scopeID int64, operation domain.CarpoolOperation) (_ *domain.CarpoolResetBatch, err error) {
	if scopeID != domain.CarpoolGlobalScopeID {
		return nil, service.ErrCarpoolResetInvalid
	}
	if err = validateOperation(operation, "reset_official"); err != nil {
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
	if _, _, err = lockResetScopeTx(ctx, tx, scopeID); err != nil {
		return nil, err
	}
	lockedTerms, err := lockResetTermsAndCyclesTx(ctx, tx, scopeID)
	if err != nil {
		return nil, err
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	batch := &domain.CarpoolResetBatch{
		ScopeID: scopeID, Status: domain.CarpoolResetStatusCompleted, DetectedAt: now,
		QualifiedAt: &now, EffectiveAt: &now,
		AnnouncementState: "pending", TriggerKind: "official",
		QualificationSource: "administrator", SourceEventKeyHash: operationRequestID(operation),
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO carpool_reset_batches(scope_id,status,detected_at,qualified_at,effective_at,schedule_revision,evidence,announcement_state,qualification_source,source_event_key_hash) VALUES($1,'completed',$2,$2,$2,0,'{"trigger_kind":"official"}'::jsonb,'pending','administrator',$3) RETURNING id`, scopeID, now, batch.SourceEventKeyHash).Scan(&batch.ID); err != nil {
		return nil, err
	}
	for _, term := range lockedTerms {
		if now.Before(term.StartsAt) || !now.Before(term.ExpiresAt) {
			continue
		}
		_, _, _, applyErr := applyResetToTermTx(ctx, tx, term, batch.ID, now, "official global reset")
		if applyErr != nil {
			if errors.Is(applyErr, service.ErrCarpoolUnavailable) {
				continue
			}
			return nil, applyErr
		}
	}
	completedAt, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_batches SET completed_at=$1,updated_at=$1 WHERE id=$2`, completedAt, batch.ID); err != nil {
		return nil, err
	}
	batch, err = getResetBatchTx(ctx, tx, batch.ID, false)
	if err != nil {
		return nil, err
	}
	if err = enqueueResetAnnouncementTx(ctx, tx, batch, "completed", completedAt); err != nil {
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

func applyResetToTermTx(ctx context.Context, tx *sql.Tx, term lockedResetTerm, batchID int64, now time.Time, reason string) (*domain.CarpoolCycle, *domain.CarpoolCycle, decimal.Decimal, error) {
	cycle, err := ensureCurrentCycleTx(ctx, tx, term.ID, now)
	if err != nil {
		return nil, nil, decimal.Zero, err
	}
	grant := cycle.BaseQuotaUSD.Sub(cycle.BaseBalanceUSD).Round(8)
	if term.ResetMode == domain.CarpoolResetModeRolling {
		grant = cycle.BaseQuotaUSD.Sub(decimal.Max(cycle.BaseBalanceUSD, decimal.Zero)).Round(8)
	}
	if grant.IsNegative() {
		grant = decimal.Zero
	}
	targetCycle := cycle
	if term.ResetMode == domain.CarpoolResetModeRolling {
		targetCycle, err = advanceRollingCycleForSpecialResetTx(ctx, tx, term, cycle, batchID, now, reason)
		if err != nil {
			return nil, nil, decimal.Zero, err
		}
	} else if grant.IsPositive() {
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET base_balance_usd=base_balance_usd+$1,updated_at=$2,revision=revision+1 WHERE id=$3 AND term_id=$4`, grant.StringFixed(8), now, cycle.ID, term.ID); err != nil {
			return nil, nil, decimal.Zero, err
		}
		batchIDValue := batchID
		if err = insertLedgerTx(ctx, tx, term.UserID, term.ID, cycle.ID, "reset", domain.CarpoolBucketBase, grant, fmt.Sprintf("reset:%d:%d", batchID, cycle.ID), nil, nil, &batchIDValue, nil, nil, nil, reason, now); err != nil {
			return nil, nil, decimal.Zero, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at) VALUES($1,$2,$3,'succeeded',$4,$5)`, batchID, term.ID, targetCycle.ID, grant.StringFixed(8), now); err != nil {
		return nil, nil, decimal.Zero, err
	}
	return cycle, targetCycle, grant, nil
}

func advanceRollingCycleForSpecialResetTx(ctx context.Context, tx *sql.Tx, term lockedResetTerm, current *domain.CarpoolCycle, batchID int64, now time.Time, reason string) (*domain.CarpoolCycle, error) {
	endsAt := now.Add(7 * 24 * time.Hour)
	if endsAt.After(term.ExpiresAt) {
		endsAt = term.ExpiresAt
	}
	if !now.Before(endsAt) {
		return nil, service.ErrCarpoolUnavailable
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_requests WHERE cycle_id=$1 AND status IN ('admitted','usage_known','settling','reconcile_required')`, current.ID).Scan(&unresolved); err != nil {
		return nil, err
	}
	deferred := unresolved > 0
	boost, manual := current.BoostBalanceUSD, current.ManualBalanceUSD
	if deferred {
		boost, manual = decimal.Zero, decimal.Zero
	}
	res, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET ends_at=$1,boost_balance_usd=CASE WHEN $2 THEN boost_balance_usd ELSE 0 END,manual_balance_usd=CASE WHEN $2 THEN manual_balance_usd ELSE 0 END,state='closing',updated_at=$1,revision=revision+1 WHERE id=$3 AND term_id=$4 AND state='active'`, now, deferred, current.ID, term.ID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return nil, service.ErrCarpoolUnavailable
	}
	next := &domain.CarpoolCycle{
		TermID: term.ID, CycleNo: current.CycleNo + 1, StartsAt: now, EndsAt: endsAt,
		BaseQuotaUSD: current.BaseQuotaUSD, BaseBalanceUSD: current.BaseQuotaUSD,
		BoostBalanceUSD: boost, ManualBalanceUSD: manual,
		State: domain.CarpoolCycleActive,
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,$2,$3,$4,$5,$5,$6,$7,'active',$3) RETURNING id`, term.ID, next.CycleNo, now, endsAt, next.BaseQuotaUSD.StringFixed(8), next.BoostBalanceUSD.StringFixed(8), next.ManualBalanceUSD.StringFixed(8)).Scan(&next.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO carpool_cycle_carryovers(source_cycle_id,initial_target_cycle_id,reset_batch_id,expires_at,status) VALUES($1,$2,$3,$4,$5)`, current.ID, next.ID, batchID, endsAt, map[bool]string{true: "pending", false: "completed"}[deferred]); err != nil {
		return nil, err
	}
	batchIDValue := batchID
	if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, next.ID, "reset", domain.CarpoolBucketBase, next.BaseQuotaUSD, fmt.Sprintf("reset:%d:%d", batchID, next.ID), nil, nil, &batchIDValue, nil, nil, nil, reason+" successor", now); err != nil {
		return nil, err
	}
	if deferred {
		return next, nil
	}
	for _, carry := range []struct {
		bucket string
		amount decimal.Decimal
	}{{domain.CarpoolBucketBoost, current.BoostBalanceUSD}, {domain.CarpoolBucketManual, current.ManualBalanceUSD}} {
		if carry.amount.IsZero() {
			continue
		}
		prefix := fmt.Sprintf("reset_carry:%d:%d:%s", batchID, current.ID, carry.bucket)
		if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, current.ID, "reset_carry", carry.bucket, carry.amount.Neg(), prefix+":out", nil, nil, &batchIDValue, nil, nil, nil, "special reset balance transfer out", now); err != nil {
			return nil, err
		}
		if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, next.ID, "reset_carry", carry.bucket, carry.amount, prefix+":in", nil, nil, &batchIDValue, nil, nil, nil, "special reset balance transfer in", now); err != nil {
			return nil, err
		}
	}
	return next, nil
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

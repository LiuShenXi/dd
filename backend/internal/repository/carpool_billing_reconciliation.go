package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func (r *CarpoolRepository) ReconcileBillingException(
	ctx context.Context,
	billingRequestID, actorID int64,
	resolution string,
	actualCost *decimal.Decimal,
	reason string,
	operation domain.CarpoolOperation,
) (_ *domain.CarpoolBillingException, err error) {
	if err = validateOperation(operation, "billing_reconcile"); err != nil {
		return nil, err
	}
	if actorID != operation.ActorID {
		return nil, errors.New("carpool operation actor mismatch")
	}
	if billingRequestID <= 0 || strings.TrimSpace(reason) == "" {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	resolvedCost := decimal.Zero
	if actualCost != nil {
		resolvedCost = actualCost.Round(8)
	}
	if resolution == domain.CarpoolBillingResolutionNoCost {
		if !resolvedCost.IsZero() {
			return nil, service.ErrCarpoolInvalidRelationship
		}
	} else if resolution != domain.CarpoolBillingResolutionActualCost || !resolvedCost.IsPositive() {
		return nil, service.ErrCarpoolInvalidRelationship
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
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
		var cached domain.CarpoolBillingException
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}

	// Read immutable relationship fields without a row lock so the required
	// lock order remains usage dedup -> cycle -> billing receipt.
	var snapshot domain.CarpoolBillingSnapshot
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,user_id,api_key_id,group_id,term_id,cycle_id,admitted_at FROM carpool_billing_requests WHERE id=$1`, billingRequestID).Scan(
		&snapshot.BillingRequestID, &snapshot.RequestID, &snapshot.UserID, &snapshot.APIKeyID,
		&snapshot.GroupID, &snapshot.TermID, &snapshot.CycleID, &snapshot.AdmittedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCarpoolNotFound
	}
	if err != nil {
		return nil, err
	}

	cmd := &service.UsageBillingCommand{
		RequestID:       snapshot.RequestID,
		APIKeyID:        snapshot.APIKeyID,
		UserID:          snapshot.UserID,
		CarpoolCost:     resolvedCost,
		CarpoolSnapshot: &snapshot,
	}
	cmd.Normalize()
	if err = cmd.Validate(); err != nil {
		return nil, err
	}
	billingRepo := &usageBillingRepository{carpoolRepo: r}
	applied, claimErr := billingRepo.claimUsageBillingKey(ctx, tx, cmd)
	if claimErr != nil {
		if errors.Is(claimErr, service.ErrUsageBillingRequestConflict) {
			return nil, service.ErrCarpoolIdempotencyConflict
		}
		return nil, claimErr
	}
	if !applied {
		return nil, service.ErrCarpoolIdempotencyConflict
	}

	var lockedCycleID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_cycles WHERE id=$1 FOR UPDATE`, snapshot.CycleID).Scan(&lockedCycleID); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCarpoolNotFound
	} else if err != nil {
		return nil, err
	}

	item := &domain.CarpoolBillingException{AuxiliaryUsageReconstructed: false}
	var status string
	var knownCost, lastError sql.NullString
	var hasBillingPayload bool
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,status,actual_cost_usd::text,retry_count,last_error,updated_at,billing_payload IS NOT NULL FROM carpool_billing_requests WHERE id=$1 FOR UPDATE`, billingRequestID).Scan(
		&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.GroupID, &item.TermID,
		&item.CycleID, &item.AdmittedAt, &status, &knownCost, &item.RetryCount, &lastError,
		&item.UpdatedAt, &hasBillingPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCarpoolNotFound
	}
	if err != nil {
		return nil, err
	}
	if lockedCycleID != item.CycleID || item.ID != snapshot.BillingRequestID || item.RequestID != snapshot.RequestID ||
		item.APIKeyID != snapshot.APIKeyID || item.UserID != snapshot.UserID || item.GroupID != snapshot.GroupID ||
		item.TermID != snapshot.TermID || !item.AdmittedAt.Equal(snapshot.AdmittedAt) || status != "reconcile_required" {
		return nil, service.ErrCarpoolIdempotencyConflict
	}
	if knownCost.Valid {
		persistedCost, parseErr := decimal.NewFromString(knownCost.String)
		if parseErr != nil {
			return nil, parseErr
		}
		item.KnownCostUSD = &persistedCost
		if !persistedCost.Equal(resolvedCost) {
			return nil, service.ErrCarpoolIdempotencyConflict
		}
	} else if hasBillingPayload {
		return nil, service.ErrCarpoolIdempotencyConflict
	}
	if lastError.Valid {
		item.SanitizedError = sanitizeBillingExceptionDiagnostic(lastError.String)
	}

	update, err := tx.ExecContext(ctx, `UPDATE carpool_billing_requests SET status='usage_known',actual_cost_usd=COALESCE(actual_cost_usd,$1),updated_at=clock_timestamp() WHERE id=$2 AND status='reconcile_required' AND (actual_cost_usd IS NULL OR actual_cost_usd=$1)`, resolvedCost.StringFixed(8), billingRequestID)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := update.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, service.ErrCarpoolIdempotencyConflict
	}
	if err = billingRepo.applyUsageBillingEffects(ctx, tx, cmd, &service.UsageBillingApplyResult{Applied: true}); err != nil {
		return nil, err
	}

	storedReason := strings.TrimSpace(reason)
	if sanitized := sanitizeBillingExceptionReason(storedReason); sanitized != nil {
		storedReason = *sanitized
	}
	var rawLastError any
	if lastError.Valid {
		rawLastError = lastError.String
	}
	err = tx.QueryRowContext(ctx, `UPDATE carpool_billing_requests SET resolution=$1,resolved_at=clock_timestamp(),resolved_by=$2,resolution_reason=$3,last_error=$4,updated_at=clock_timestamp() WHERE id=$5 AND status='settled' RETURNING resolved_at,updated_at`, resolution, actorID, storedReason, rawLastError, billingRequestID).Scan(&item.ResolvedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Status = "settled"
	item.Resolution = &resolution
	item.ResolvedBy = &actorID
	item.Reason = &storedReason
	item.ActualCostUSD = &resolvedCost
	if err = recordOperationTx(ctx, tx, operation, "billing_request", billingRequestID, item); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *CarpoolResetRepository) ListResetObservations(ctx context.Context, filters domain.CarpoolResetObservationFilters) ([]domain.CarpoolResetObservation, int64, error) {
	normalizeResetPagination(&filters.Page, &filters.PageSize)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_reset_account_states`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.upstream_identity_hash,s.baseline_complete,s.last_complete_at,s.health_status,s.known_credit_count,
		       (SELECT COUNT(*) FROM carpool_reset_credits c WHERE c.account_state_id=s.id AND c.assignment_status IN ('pending','needs_review'))
		FROM carpool_reset_account_states s
		ORDER BY s.last_observed_at DESC NULLS LAST,s.id
		LIMIT $1 OFFSET $2`, filters.PageSize, (filters.Page-1)*filters.PageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.CarpoolResetObservation, 0)
	for rows.Next() {
		var item domain.CarpoolResetObservation
		var lastComplete sql.NullTime
		if err := rows.Scan(&item.UpstreamIdentityHash, &item.BaselineComplete, &lastComplete, &item.HealthStatus, &item.KnownCreditCount, &item.UnassignedCreditCount); err != nil {
			return nil, 0, err
		}
		item.LastCompleteAt = nullableTime(lastComplete)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *CarpoolResetRepository) RecordResetObservation(ctx context.Context, input domain.CarpoolResetObservationInput) (_ *domain.CarpoolResetObservationResult, err error) {
	if len(strings.TrimSpace(input.UpstreamIdentityHash)) != 64 || input.RepresentativeAccountID <= 0 || input.ObservedAt.IsZero() {
		return nil, service.ErrCarpoolResetInvalid
	}
	for _, credit := range input.Credits {
		if len(strings.TrimSpace(credit.CreditHash)) != 64 {
			return nil, service.ErrCarpoolResetInvalid
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_reset_account_states(upstream_identity_hash,representative_account_id,last_observed_at,health_status)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(upstream_identity_hash) DO UPDATE SET
			representative_account_id=LEAST(carpool_reset_account_states.representative_account_id,EXCLUDED.representative_account_id),
			last_observed_at=EXCLUDED.last_observed_at,
			health_status=EXCLUDED.health_status,
			updated_at=EXCLUDED.last_observed_at`, input.UpstreamIdentityHash, input.RepresentativeAccountID, input.ObservedAt, resetObservationHealth(input.Complete)); err != nil {
		return nil, err
	}
	var stateID int64
	var baselineComplete bool
	if err = tx.QueryRowContext(ctx, `SELECT id,baseline_complete FROM carpool_reset_account_states WHERE upstream_identity_hash=$1 FOR UPDATE`, input.UpstreamIdentityHash).Scan(&stateID, &baselineComplete); err != nil {
		return nil, err
	}
	if !input.Complete {
		reason := strings.TrimSpace(input.IncompleteReason)
		if reason == "" {
			reason = "incomplete_snapshot"
		}
		health := input.HealthStatus
		if health != "error" {
			health = "incomplete"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_account_states SET last_observed_at=$1,health_status=$2,incomplete_reason=$3,revision=revision+1,updated_at=$1 WHERE id=$4`, input.ObservedAt, health, reason, stateID); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &domain.CarpoolResetObservationResult{Complete: false}, nil
	}

	credits := deduplicateResetCredits(input.Credits)
	if !baselineComplete {
		for _, credit := range credits {
			if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_credits(account_state_id,upstream_identity_hash,credit_hash,first_seen_at,last_seen_at,expires_at,initial_stock,assignment_status) VALUES($1,$2,$3,$4,$4,$5,TRUE,'baseline') ON CONFLICT(upstream_identity_hash,credit_hash) DO UPDATE SET last_seen_at=EXCLUDED.last_seen_at,expires_at=COALESCE(EXCLUDED.expires_at,carpool_reset_credits.expires_at)`, stateID, input.UpstreamIdentityHash, credit.CreditHash, input.ObservedAt, credit.ExpiresAt); err != nil {
				return nil, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_account_states SET baseline_complete=TRUE,last_observed_at=$1,last_complete_at=$1,health_status='healthy',known_credit_count=$2,incomplete_reason=NULL,revision=revision+1,updated_at=$1 WHERE id=$3`, input.ObservedAt, len(credits), stateID); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &domain.CarpoolResetObservationResult{Complete: true}, nil
	}

	newCredits := make([]domain.CarpoolResetCreditEvidence, 0)
	for _, credit := range credits {
		var creditID int64
		insertErr := tx.QueryRowContext(ctx, `INSERT INTO carpool_reset_credits(account_state_id,upstream_identity_hash,credit_hash,first_seen_at,last_seen_at,expires_at,initial_stock,assignment_status) VALUES($1,$2,$3,$4,$4,$5,FALSE,'pending') ON CONFLICT(upstream_identity_hash,credit_hash) DO NOTHING RETURNING id`, stateID, input.UpstreamIdentityHash, credit.CreditHash, input.ObservedAt, credit.ExpiresAt).Scan(&creditID)
		switch {
		case insertErr == nil:
			newCredits = append(newCredits, credit)
		case errors.Is(insertErr, sql.ErrNoRows):
			if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_credits SET last_seen_at=$1,expires_at=COALESCE($2,expires_at),updated_at=$1 WHERE upstream_identity_hash=$3 AND credit_hash=$4`, input.ObservedAt, credit.ExpiresAt, input.UpstreamIdentityHash, credit.CreditHash); err != nil {
				return nil, err
			}
		default:
			return nil, insertErr
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_account_states SET last_observed_at=$1,last_complete_at=$1,health_status='healthy',known_credit_count=$2,incomplete_reason=NULL,revision=revision+1,updated_at=$1 WHERE id=$3`, input.ObservedAt, len(credits), stateID); err != nil {
		return nil, err
	}
	result := &domain.CarpoolResetObservationResult{Complete: true, NewCandidates: len(newCredits)}
	if len(newCredits) > 0 {
		batch, assignErr := r.assignObservedResetCreditsTx(ctx, tx, input.UpstreamIdentityHash, newCredits)
		if assignErr != nil {
			return nil, assignErr
		}
		result.Batch = batch
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *CarpoolResetRepository) assignObservedResetCreditsTx(ctx context.Context, tx *sql.Tx, identityHash string, credits []domain.CarpoolResetCreditEvidence) (*domain.CarpoolResetBatch, error) {
	lastSuccessful, pendingBatchID, err := lockResetScopeTx(ctx, tx, domain.CarpoolGlobalScopeID)
	if err != nil {
		return nil, err
	}
	var batch *domain.CarpoolResetBatch
	if pendingBatchID != nil {
		batch, err = getResetBatchTx(ctx, tx, *pendingBatchID, true)
		if err != nil {
			return nil, err
		}
		if !isPendingResetStatus(batch.Status) {
			batch = nil
		}
	}
	now, err := r.resetDatabaseNowTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, credit := range credits {
		if batch == nil {
			batch, err = createScheduledResetBatchTx(ctx, tx, domain.CarpoolGlobalScopeID, "automatic", credit.CreditHash, now, lastSuccessful)
			if err != nil {
				return nil, err
			}
		}
		reason := "complete local reset-credit observation; official campaign identity unavailable"
		if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_reset_qualifications(scope_id,batch_id,source,source_event_key_hash,reason,confirmed_at) VALUES($1,$2,'automatic',$3,$4,$5) ON CONFLICT(scope_id,source_event_key_hash) DO NOTHING`, domain.CarpoolGlobalScopeID, batch.ID, credit.CreditHash, reason, now); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carpool_reset_credits SET assignment_status='assigned',reset_batch_id=$1,updated_at=$2 WHERE upstream_identity_hash=$3 AND credit_hash=$4 AND assignment_status='pending'`, batch.ID, now, identityHash, credit.CreditHash); err != nil {
			return nil, err
		}
	}
	return batch, nil
}

func deduplicateResetCredits(credits []domain.CarpoolResetCreditEvidence) []domain.CarpoolResetCreditEvidence {
	seen := make(map[string]struct{}, len(credits))
	result := make([]domain.CarpoolResetCreditEvidence, 0, len(credits))
	for _, credit := range credits {
		credit.CreditHash = strings.TrimSpace(credit.CreditHash)
		if _, exists := seen[credit.CreditHash]; exists {
			continue
		}
		seen[credit.CreditHash] = struct{}{}
		result = append(result, credit)
	}
	return result
}

func resetObservationHealth(complete bool) string {
	if complete {
		return "healthy"
	}
	return "incomplete"
}

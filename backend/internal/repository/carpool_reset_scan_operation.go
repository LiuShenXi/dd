package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const resetObservationScanLockIdentity = "carpool:reset-observation-scan"

func (r *CarpoolResetRepository) RunResetObservationScanOperation(ctx context.Context, operation domain.CarpoolOperation, scan func(context.Context) (domain.CarpoolResetScanResult, error)) (domain.CarpoolResetScanResult, error) {
	var result domain.CarpoolResetScanResult
	if err := validateOperation(operation, "reset_observation_scan"); err != nil {
		return result, err
	}
	if scan == nil {
		return result, service.ErrCarpoolResetInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, resetObservationScanLockIdentity); err != nil {
		return result, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return result, lookupErr
	} else if replayed {
		if err = replayOperationResponse(response, &result); err != nil {
			return result, err
		}
		return result, nil
	}

	result, err = scan(ctx)
	if err != nil {
		return result, err
	}
	if err = recordOperationTx(ctx, tx, operation, "reset_observation_scan", domain.CarpoolGlobalScopeID, result); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

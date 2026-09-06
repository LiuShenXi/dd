//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolRecoveryRepository_SkipsLockedStaleAdmissionAndPromotesKnownReceipt(t *testing.T) {
	ctx := context.Background()
	cost := decimal.RequireFromString("12.34567890")
	payload := json.RawMessage(`{"version":1,"marker":"known-recovery"}`)
	fixture := newCarpoolBillingReconciliationFixture(t, &cost, payload)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		UPDATE carpool_billing_requests
		SET status='usage_known',retry_count=0,last_error=NULL,updated_at=NOW()
		WHERE id=$1
		RETURNING id
	`, fixture.billingRequestID).Scan(&fixture.billingRequestID))

	var staleID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_billing_requests(
			request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,
			status,request_fingerprint,created_at,updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,'admitted',$8,NOW()-INTERVAL '2 hours',NOW()-INTERVAL '2 hours')
		RETURNING id
	`, "carpool:stale-locked:"+uuid.NewString(), fixture.apiKeyID, fixture.userID, fixture.groupID, fixture.termID, fixture.cycleID, fixture.admittedAt, uuid.NewString()).Scan(&staleID))

	blocker, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var lockedID int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM carpool_billing_requests WHERE id=$1 FOR UPDATE`, staleID).Scan(&lockedID))
	require.Equal(t, staleID, lockedID)

	recoveryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	startedAt := time.Now()
	receipts, err := fixture.repo.RecoverPendingReceipts(recoveryCtx, 10)
	elapsed := time.Since(startedAt)
	cancel()
	require.NoError(t, err)
	require.Less(t, elapsed, time.Second, "SKIP LOCKED recovery should not wait for the stale admitted row lock")

	var recovered *domain.CarpoolKnownUsage
	for i := range receipts {
		if receipts[i].Snapshot.BillingRequestID == fixture.billingRequestID {
			recovered = &receipts[i]
			break
		}
	}
	require.NotNil(t, recovered)
	require.True(t, cost.Equal(recovered.ActualCostUSD))
	require.JSONEq(t, string(payload), string(recovered.BillingPayload))
	require.Equal(t, 1, recovered.RetryCount)

	var staleStatus string
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT status FROM carpool_billing_requests WHERE id=$1`, staleID).Scan(&staleStatus))
	require.Equal(t, "admitted", staleStatus)

	require.NoError(t, fixture.repo.MarkKnownUsageReconcileRequired(ctx, recovered.Snapshot, "automatic_settlement_retry_exhausted"))
	var status, storedCost, storedPayload, diagnostic string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,actual_cost_usd::text,billing_payload::text,last_error
		FROM carpool_billing_requests WHERE id=$1
	`, fixture.billingRequestID).Scan(&status, &storedCost, &storedPayload, &diagnostic))
	require.Equal(t, "reconcile_required", status)
	require.Equal(t, cost.StringFixed(8), storedCost)
	require.JSONEq(t, string(payload), storedPayload)
	require.Equal(t, "automatic_settlement_retry_exhausted", diagnostic)

	userID := fixture.userID
	exceptions, total, err := fixture.repo.ListBillingExceptions(ctx, domain.CarpoolBillingExceptionFilters{
		Page: 1, PageSize: 20, UserID: &userID,
	})
	require.NoError(t, err)
	require.Positive(t, total)
	var manual *domain.CarpoolBillingException
	for i := range exceptions {
		if exceptions[i].ID == fixture.billingRequestID {
			manual = &exceptions[i]
			break
		}
	}
	require.NotNil(t, manual)
	require.Equal(t, "reconcile_required", manual.Status)
	require.Nil(t, manual.Resolution)
	require.NotNil(t, manual.KnownCostUSD)
	require.True(t, cost.Equal(*manual.KnownCostUSD))
	require.Nil(t, manual.ActualCostUSD)
}

func TestCarpoolBillingExceptionProjection_PreservesCanonicalDiagnostics(t *testing.T) {
	for _, diagnostic := range []string{
		"usage_receipt_missing",
		"usage_persistence_or_settlement_failed",
	} {
		t.Run(diagnostic, func(t *testing.T) {
			ctx := context.Background()
			fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
			_, err := integrationDB.ExecContext(ctx, `
				UPDATE carpool_billing_requests SET last_error=$1 WHERE id=$2
			`, diagnostic, fixture.billingRequestID)
			require.NoError(t, err)

			userID := fixture.userID
			exceptions, total, err := fixture.repo.ListBillingExceptions(ctx, domain.CarpoolBillingExceptionFilters{
				Page: 1, PageSize: 20, UserID: &userID,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Len(t, exceptions, 1)
			require.NotNil(t, exceptions[0].SanitizedError)
			require.Equal(t, diagnostic, *exceptions[0].SanitizedError)
		})
	}
}

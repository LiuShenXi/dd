//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type carpoolBillingReconciliationFixture struct {
	repo             *CarpoolRepository
	service          *service.CarpoolService
	userID           int64
	apiKeyID         int64
	groupID          int64
	termID           int64
	cycleID          int64
	billingRequestID int64
	requestID        string
	admittedAt       time.Time
}

func newCarpoolBillingReconciliationFixture(
	t *testing.T,
	knownCost *decimal.Decimal,
	billingPayload json.RawMessage,
) *carpoolBillingReconciliationFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewCarpoolRepository(integrationDB)
	carpoolService := service.NewCarpoolService(repo)

	user := mustCreateUser(t, client, &service.User{
		Email:        "carpool-reconcile-" + uuid.NewString() + "@example.com",
		PasswordHash: "synthetic-hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "carpool-reconcile-" + uuid.NewString(),
		SubscriptionType: service.SubscriptionTypeCarpool,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:      user.ID,
		GroupID:     &group.ID,
		Key:         "sk-carpool-reconcile-" + uuid.NewString(),
		Name:        "synthetic-carpool-reconciliation",
		Quota:       50,
		QuotaUsed:   3,
		RateLimit5h: 20,
		RateLimit1d: 30,
		RateLimit7d: 40,
		Usage5h:     4,
		Usage1d:     5,
		Usage7d:     6,
	})

	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now))
	plans, err := repo.ListPlans(ctx, true)
	require.NoError(t, err)
	var plan *domain.CarpoolPlan
	for i := range plans {
		if plans[i].Code == "four_seat" {
			plan = &plans[i]
			break
		}
	}
	require.NotNil(t, plan)
	snapshot := plan.Snapshot()
	snapshotJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)

	startsAt := now.Add(-time.Hour)
	expiresAt := startsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	var termID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_terms(
			user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,
			status,source_mode,history_complete,statistics_since,created_by
		) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,'active','new',TRUE,$6,$1)
		RETURNING id
	`, user.ID, domain.CarpoolGlobalScopeID, group.ID, plan.ID, string(snapshotJSON), startsAt, expiresAt).Scan(&termID))

	cycleEndsAt := startsAt.Add(7 * 24 * time.Hour)
	var cycleID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_cycles(
			term_id,cycle_no,starts_at,ends_at,base_quota_usd,
			base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at
		) VALUES($1,1,$2,$3,550.00000000,100.00000000,7.00000000,9.00000000,'active',$2)
		RETURNING id
	`, termID, startsAt, cycleEndsAt).Scan(&cycleID))

	requestID := "carpool:reconcile:" + uuid.NewString()
	admittedAt := now
	var persistedCost any
	if knownCost != nil {
		persistedCost = knownCost.Round(8).StringFixed(8)
	}
	var persistedPayload any
	if len(billingPayload) > 0 {
		persistedPayload = string(billingPayload)
	}
	var billingRequestID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_billing_requests(
			request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,
			status,request_fingerprint,billing_payload,actual_cost_usd,last_error
		) VALUES($1,$2,$3,$4,$5,$6,$7,'reconcile_required',$8,$9::jsonb,$10,'receipt_missing_timeout')
		RETURNING id,admitted_at
	`, requestID, apiKey.ID, user.ID, group.ID, termID, cycleID, admittedAt, uuid.NewString(), persistedPayload, persistedCost).Scan(&billingRequestID, &admittedAt))

	return &carpoolBillingReconciliationFixture{
		repo:             repo,
		service:          carpoolService,
		userID:           user.ID,
		apiKeyID:         apiKey.ID,
		groupID:          group.ID,
		termID:           termID,
		cycleID:          cycleID,
		billingRequestID: billingRequestID,
		requestID:        requestID,
		admittedAt:       admittedAt,
	}
}

func (f *carpoolBillingReconciliationFixture) reconcile(
	ctx context.Context,
	key, resolution, reason string,
	cost *decimal.Decimal,
) (*domain.CarpoolBillingException, error) {
	return f.service.ReconcileBillingException(ctx, f.billingRequestID, f.userID, service.CarpoolBillingReconcileInput{
		Confirmed:     true,
		Resolution:    resolution,
		ActualCostUSD: cost,
		Reason:        reason,
	}, key)
}

func TestCarpoolBillingReconciliation_ActualCostIsAtomicAndAuditable(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	cost := decimal.RequireFromString("12.345678129")

	result, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "verified provider cost", &cost)
	require.NoError(t, err)
	require.Equal(t, "settled", result.Status)
	require.NotNil(t, result.Resolution)
	require.Equal(t, domain.CarpoolBillingResolutionActualCost, *result.Resolution)
	require.NotNil(t, result.ResolvedAt)
	require.NotNil(t, result.ResolvedBy)
	require.Equal(t, fixture.userID, *result.ResolvedBy)
	require.NotNil(t, result.Reason)
	require.Equal(t, "verified provider cost", *result.Reason)
	require.NotNil(t, result.ActualCostUSD)
	require.True(t, decimal.RequireFromString("12.34567813").Equal(*result.ActualCostUSD))
	require.False(t, result.AuxiliaryUsageReconstructed)
	require.NotNil(t, result.SanitizedError)
	require.Equal(t, "receipt_missing_timeout", *result.SanitizedError)

	var baseBalance, quotaUsed, usage5h, usage1d, usage7d string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&baseBalance))
	require.Equal(t, "87.65432187", baseBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT quota_used::text,usage_5h::text,usage_1d::text,usage_7d::text FROM api_keys WHERE id=$1`, fixture.apiKeyID).Scan(&quotaUsed, &usage5h, &usage1d, &usage7d))
	require.Equal(t, "3.00000000", quotaUsed)
	require.Equal(t, "4.00000000", usage5h)
	require.Equal(t, "5.00000000", usage1d)
	require.Equal(t, "6.00000000", usage7d)

	var status, persistedCost, diagnostic, resolution, reason string
	var resolvedAt time.Time
	var resolvedBy int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,actual_cost_usd::text,last_error,resolution,resolved_at,resolved_by,resolution_reason
		FROM carpool_billing_requests WHERE id=$1
	`, fixture.billingRequestID).Scan(&status, &persistedCost, &diagnostic, &resolution, &resolvedAt, &resolvedBy, &reason))
	require.Equal(t, "settled", status)
	require.Equal(t, "12.34567813", persistedCost)
	require.Equal(t, "receipt_missing_timeout", diagnostic)
	require.Equal(t, domain.CarpoolBillingResolutionActualCost, resolution)
	require.Equal(t, fixture.userID, resolvedBy)
	require.Equal(t, "verified provider cost", reason)
	require.Equal(t, result.ResolvedAt.UTC(), resolvedAt.UTC())

	userID := fixture.userID
	unresolved, unresolvedTotal, err := fixture.repo.ListBillingExceptions(ctx, domain.CarpoolBillingExceptionFilters{Page: 1, PageSize: 20, UserID: &userID})
	require.NoError(t, err)
	require.Zero(t, unresolvedTotal)
	require.Empty(t, unresolved)

	settled, settledTotal, err := fixture.repo.ListBillingExceptions(ctx, domain.CarpoolBillingExceptionFilters{Page: 1, PageSize: 20, UserID: &userID, Status: "settled"})
	require.NoError(t, err)
	require.EqualValues(t, 1, settledTotal)
	require.Len(t, settled, 1)
	require.NotNil(t, settled[0].ResolvedAt)
	require.Equal(t, result.ResolvedAt.UTC(), settled[0].ResolvedAt.UTC())
	require.Equal(t, result.Reason, settled[0].Reason)
	require.False(t, settled[0].AuxiliaryUsageReconstructed)
}

func TestCarpoolBillingReconciliation_NoCostCreatesNoUsageLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)

	result, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionNoCost, "confirmed no billable usage", nil)
	require.NoError(t, err)
	require.NotNil(t, result.ActualCostUSD)
	require.True(t, result.ActualCostUSD.IsZero())

	var baseBalance string
	var ledgerCount, dedupCount, operationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&baseBalance))
	require.Equal(t, "100.00000000", baseBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_operations WHERE resource_type='billing_request' AND resource_id=$1`, fixture.billingRequestID).Scan(&operationCount))
	require.Equal(t, 1, operationCount)
}

func TestCarpoolBillingReconciliation_ReplayAndResolvedConflicts(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	cost := decimal.RequireFromString("3.125")
	key := uuid.NewString()

	first, err := fixture.reconcile(ctx, key, domain.CarpoolBillingResolutionActualCost, "provider statement", &cost)
	require.NoError(t, err)
	replayed, err := fixture.reconcile(ctx, key, domain.CarpoolBillingResolutionActualCost, "provider statement", &cost)
	require.NoError(t, err)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	replayedJSON, err := json.Marshal(replayed)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(replayedJSON))
	require.Equal(t, string(firstJSON), string(replayedJSON), "replay must preserve the exact response representation")

	_, err = fixture.reconcile(ctx, key, domain.CarpoolBillingResolutionActualCost, "different payload", &cost)
	require.ErrorIs(t, err, service.ErrCarpoolIdempotencyConflict)
	_, err = fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "provider statement", &cost)
	require.ErrorIs(t, err, service.ErrCarpoolIdempotencyConflict)

	var ledgerCount, dedupCount, operationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_operations WHERE resource_type='billing_request' AND resource_id=$1`, fixture.billingRequestID).Scan(&operationCount))
	require.Equal(t, 1, operationCount)
}

func TestCarpoolBillingReconciliation_RollsBackDedupDebitAndOperation(t *testing.T) {
	ctx := context.Background()
	persistedCost := decimal.RequireFromString("2")
	fixture := newCarpoolBillingReconciliationFixture(t, &persistedCost, nil)
	conflictingCost := decimal.RequireFromString("3")

	_, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "conflicting evidence", &conflictingCost)
	require.ErrorIs(t, err, service.ErrCarpoolIdempotencyConflict)

	var status, baseBalance string
	var ledgerCount, dedupCount, operationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM carpool_billing_requests WHERE id=$1`, fixture.billingRequestID).Scan(&status))
	require.Equal(t, "reconcile_required", status)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&baseBalance))
	require.Equal(t, "100.00000000", baseBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Zero(t, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_operations WHERE resource_type='billing_request' AND resource_id=$1`, fixture.billingRequestID).Scan(&operationCount))
	require.Zero(t, operationCount)
}

func TestCarpoolBillingReconciliation_ParallelResolutionsApplyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	cost := decimal.RequireFromString("8.25")
	type outcome struct {
		result *domain.CarpoolBillingException
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "parallel verification", &cost)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	successes, conflicts := 0, 0
	for result := range outcomes {
		if result.err == nil {
			successes++
			require.NotNil(t, result.result)
			continue
		}
		require.ErrorIs(t, result.err, service.ErrCarpoolIdempotencyConflict)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	var baseBalance string
	var ledgerCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&baseBalance))
	require.Equal(t, "91.75000000", baseBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
}

func TestCarpoolBillingReconciliation_ParallelSameKeyReplaysExactResponse(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	cost := decimal.RequireFromString("8.25")
	key := uuid.NewString()
	type outcome struct {
		result *domain.CarpoolBillingException
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := fixture.reconcile(ctx, key, domain.CarpoolBillingResolutionActualCost, "parallel exact replay", &cost)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	encoded := make([]string, 0, 2)
	for result := range outcomes {
		require.NoError(t, result.err)
		require.NotNil(t, result.result)
		body, err := json.Marshal(result.result)
		require.NoError(t, err)
		encoded = append(encoded, string(body))
	}
	require.Len(t, encoded, 2)
	require.Equal(t, encoded[0], encoded[1])

	var ledgerCount, dedupCount, operationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_operations WHERE resource_type='billing_request' AND resource_id=$1`, fixture.billingRequestID).Scan(&operationCount))
	require.Equal(t, 1, operationCount)
}

func TestCarpoolBillingReconciliation_FinalOperationFailureRollsBackAllEffects(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	cost := decimal.RequireFromString("6.75")
	suffix := "t" + strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "fail_carpool_operation_" + suffix
	triggerName := "fail_carpool_operation_trigger_" + suffix
	require.NoError(t, execSyntheticReconciliationFailureTrigger(ctx, functionName, triggerName, fixture.billingRequestID))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON carpool_operations`, triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	_, err := fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "verified but forced audit failure", &cost)
	require.Error(t, err)
	require.Contains(t, err.Error(), "synthetic final operation failure")

	var status, baseBalance string
	var actualCost, resolution, resolvedAt, resolvedBy, reason any
	var ledgerCount, dedupCount, operationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,actual_cost_usd,resolution,resolved_at,resolved_by,resolution_reason
		FROM carpool_billing_requests WHERE id=$1
	`, fixture.billingRequestID).Scan(&status, &actualCost, &resolution, &resolvedAt, &resolvedBy, &reason))
	require.Equal(t, "reconcile_required", status)
	require.Nil(t, actualCost)
	require.Nil(t, resolution)
	require.Nil(t, resolvedAt)
	require.Nil(t, resolvedBy)
	require.Nil(t, reason)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, fixture.cycleID).Scan(&baseBalance))
	require.Equal(t, "100.00000000", baseBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1`, fixture.requestID).Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Zero(t, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_operations WHERE resource_type='billing_request' AND resource_id=$1`, fixture.billingRequestID).Scan(&operationCount))
	require.Zero(t, operationCount)
}

func execSyntheticReconciliationFailureTrigger(ctx context.Context, functionName, triggerName string, billingRequestID int64) error {
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.resource_type = 'billing_request' AND NEW.resource_id = %d THEN
				RAISE EXCEPTION 'synthetic final operation failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON carpool_operations
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, functionName, billingRequestID, triggerName, functionName))
	return err
}

func TestCarpoolBillingReconciliation_DelayedKnownReceiptCannotOverwriteManualAmount(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	manualCost := decimal.RequireFromString("4.5")
	knownCost := decimal.RequireFromString("7.25")
	snapshot := domain.CarpoolBillingSnapshot{
		BillingRequestID: fixture.billingRequestID,
		RequestID:        fixture.requestID,
		UserID:           fixture.userID,
		APIKeyID:         fixture.apiKeyID,
		GroupID:          fixture.groupID,
		TermID:           fixture.termID,
		CycleID:          fixture.cycleID,
		AdmittedAt:       fixture.admittedAt,
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var reconcileErr, knownUsageErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, reconcileErr = fixture.reconcile(ctx, uuid.NewString(), domain.CarpoolBillingResolutionActualCost, "manual provider verification", &manualCost)
	}()
	go func() {
		defer wg.Done()
		<-start
		knownUsageErr = fixture.repo.PersistKnownUsage(ctx, snapshot, knownCost, json.RawMessage(`{"version":1,"synthetic":true}`))
	}()
	close(start)
	wg.Wait()

	require.NotEqual(t, reconcileErr == nil, knownUsageErr == nil, "exactly one competing resolution path must win")
	var status, finalCost string
	var resolution *string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status,actual_cost_usd::text,resolution FROM carpool_billing_requests WHERE id=$1`, fixture.billingRequestID).Scan(&status, &finalCost, &resolution))
	if reconcileErr == nil {
		require.ErrorIs(t, knownUsageErr, service.ErrCarpoolIdempotencyConflict)
		require.Equal(t, "settled", status)
		require.Equal(t, "4.50000000", finalCost)
		require.NotNil(t, resolution)
		require.Equal(t, domain.CarpoolBillingResolutionActualCost, *resolution)
	} else {
		require.ErrorIs(t, reconcileErr, service.ErrCarpoolIdempotencyConflict)
		require.NoError(t, knownUsageErr)
		require.Equal(t, "usage_known", status)
		require.Equal(t, "7.25000000", finalCost)
		require.Nil(t, resolution)
	}
}

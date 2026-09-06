//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolResetScanOperation_ReplaysOriginalResponse(t *testing.T) {
	resetGlobalResetState(t)
	ctx := context.Background()
	repo := NewCarpoolResetRepository(integrationDB)
	operation := carpoolTestOperation("reset_observation_scan", createResetIntegrationActor(t))
	want := domain.CarpoolResetScanResult{Scanned: 4, Complete: 2, Incomplete: 2, NewCandidates: 1}
	calls := 0
	scan := func(context.Context) (domain.CarpoolResetScanResult, error) {
		calls++
		return want, nil
	}

	first, err := repo.RunResetObservationScanOperation(ctx, operation, scan)
	require.NoError(t, err)
	require.Equal(t, want, first)
	replayed, err := repo.RunResetObservationScanOperation(ctx, operation, scan)
	require.NoError(t, err)
	require.Equal(t, first, replayed)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_operations WHERE kind='reset_observation_scan' AND actor_id=$1`, operation.ActorID))
}

func TestCarpoolResetScanOperation_CancellationDoesNotWriteFinalResponse(t *testing.T) {
	resetGlobalResetState(t)
	ctx, cancel := context.WithCancel(context.Background())
	repo := NewCarpoolResetRepository(integrationDB)
	operation := carpoolTestOperation("reset_observation_scan", createResetIntegrationActor(t))
	calls := 0

	_, err := repo.RunResetObservationScanOperation(ctx, operation, func(context.Context) (domain.CarpoolResetScanResult, error) {
		calls++
		cancel()
		return domain.CarpoolResetScanResult{Scanned: 1}, context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_operations WHERE kind='reset_observation_scan' AND actor_id=$1`, operation.ActorID))
}

func TestCarpoolResetScanOperation_FinalResponseFailureRetriesWithoutDuplicateQualification(t *testing.T) {
	resetGlobalResetState(t)
	ctx := context.Background()
	account := mustCreateAccount(t, testEntClient(t), &service.Account{
		Name: "reset-scan-account-" + uuid.NewString(), Type: service.AccountTypeOAuth, Platform: service.PlatformOpenAI,
	})
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	identityHash := resetIntegrationHash("scan-identity-" + uuid.NewString())
	creditHash := resetIntegrationHash("scan-credit-" + uuid.NewString())
	_, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityHash, RepresentativeAccountID: account.ID,
		ObservedAt: slotAt.Add(-2 * time.Hour), Complete: true,
	})
	require.NoError(t, err)

	functionName := "fail_reset_scan_operation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	triggerName := functionName + "_trigger"
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.kind = 'reset_observation_scan' THEN
				RAISE EXCEPTION 'forced scan response write failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON carpool_operations
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, functionName, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON carpool_operations`, triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	operation := carpoolTestOperation("reset_observation_scan", createResetIntegrationActor(t))
	calls := 0
	scan := func(context.Context) (domain.CarpoolResetScanResult, error) {
		calls++
		observation, observationErr := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
			UpstreamIdentityHash: identityHash, RepresentativeAccountID: account.ID,
			ObservedAt: slotAt.Add(-time.Hour), Complete: true,
			Credits: []domain.CarpoolResetCreditEvidence{{CreditHash: creditHash}},
		})
		if observationErr != nil {
			return domain.CarpoolResetScanResult{}, observationErr
		}
		return domain.CarpoolResetScanResult{
			Scanned: 1, Complete: 1, NewCandidates: observation.NewCandidates,
		}, nil
	}

	first, err := repo.RunResetObservationScanOperation(ctx, operation, scan)
	require.ErrorContains(t, err, "forced scan response write failure")
	require.Equal(t, 1, first.NewCandidates)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_operations WHERE kind='reset_observation_scan' AND actor_id=$1`, operation.ActorID))

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER %s ON carpool_operations`, triggerName))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`DROP FUNCTION %s()`, functionName))
	require.NoError(t, err)

	retried, err := repo.RunResetObservationScanOperation(ctx, operation, scan)
	require.NoError(t, err)
	require.Zero(t, retried.NewCandidates)
	require.Equal(t, 2, calls)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))

	replayed, err := repo.RunResetObservationScanOperation(ctx, operation, scan)
	require.NoError(t, err)
	require.Equal(t, retried, replayed)
	require.Equal(t, 2, calls)
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_operations WHERE kind='reset_observation_scan' AND actor_id=$1`, operation.ActorID))
}

//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolResetObservation_BaselinesReplacementDedupAndBatchMerge(t *testing.T) {
	resetGlobalResetState(t)
	ctx := context.Background()
	accountA := mustCreateAccount(t, testEntClient(t), &service.Account{
		Name: "reset-observation-a-" + uuid.NewString(), Type: service.AccountTypeOAuth, Platform: service.PlatformOpenAI,
	})
	accountB := mustCreateAccount(t, testEntClient(t), &service.Account{
		Name: "reset-observation-b-" + uuid.NewString(), Type: service.AccountTypeOAuth, Platform: service.PlatformOpenAI,
	})
	databaseNow := carpoolDatabaseNow(t, ctx)
	slotAt, _ := service.NextCarpoolResetSchedule(databaseNow, nil)
	repo := resetRepositoryAt(slotAt.Add(-time.Hour))
	identityA := resetIntegrationHash("identity-a-" + uuid.NewString())
	identityB := resetIntegrationHash("identity-b-" + uuid.NewString())
	creditA := resetIntegrationHash("credit-a-" + uuid.NewString())
	creditBInitial := resetIntegrationHash("credit-b-initial-" + uuid.NewString())
	creditBReplacement := resetIntegrationHash("credit-b-replacement-" + uuid.NewString())

	incomplete, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityB, RepresentativeAccountID: accountB.ID,
		ObservedAt: slotAt.Add(-3 * time.Hour), Complete: false,
		IncompleteReason: "credit_identity_incomplete",
		Credits:          []domain.CarpoolResetCreditEvidence{{CreditHash: creditBInitial}},
	})
	require.NoError(t, err)
	require.False(t, incomplete.Complete)
	require.Zero(t, incomplete.NewCandidates)
	requireObservationState(t, identityB, false, 0, "incomplete")
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_credits WHERE upstream_identity_hash=$1`, identityB))
	require.Zero(t, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))

	emptyBaseline, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityA, RepresentativeAccountID: accountA.ID,
		ObservedAt: slotAt.Add(-2 * time.Hour), Complete: true,
	})
	require.NoError(t, err)
	require.True(t, emptyBaseline.Complete)
	require.Zero(t, emptyBaseline.NewCandidates)
	require.Nil(t, emptyBaseline.Batch)
	requireObservationState(t, identityA, true, 0, "healthy")

	firstCandidate, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityA, RepresentativeAccountID: accountA.ID,
		ObservedAt: slotAt.Add(-90 * time.Minute), Complete: true,
		Credits: []domain.CarpoolResetCreditEvidence{
			{CreditHash: creditA},
			{CreditHash: creditA},
		},
	})
	require.NoError(t, err)
	require.True(t, firstCandidate.Complete)
	require.Equal(t, 1, firstCandidate.NewCandidates)
	require.NotNil(t, firstCandidate.Batch)
	firstBatchID := firstCandidate.Batch.ID
	firstScheduledAt := *firstCandidate.Batch.ScheduledAt

	replayedCandidate, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityA, RepresentativeAccountID: accountA.ID,
		ObservedAt: slotAt.Add(-80 * time.Minute), Complete: true,
		Credits: []domain.CarpoolResetCreditEvidence{{CreditHash: creditA}},
	})
	require.NoError(t, err)
	require.Zero(t, replayedCandidate.NewCandidates)
	require.Nil(t, replayedCandidate.Batch)

	initialStock, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityB, RepresentativeAccountID: accountB.ID,
		ObservedAt: slotAt.Add(-70 * time.Minute), Complete: true,
		Credits: []domain.CarpoolResetCreditEvidence{{CreditHash: creditBInitial}},
	})
	require.NoError(t, err)
	require.True(t, initialStock.Complete)
	require.Zero(t, initialStock.NewCandidates)
	require.Nil(t, initialStock.Batch)

	replacement, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityB, RepresentativeAccountID: accountB.ID,
		ObservedAt: slotAt.Add(-60 * time.Minute), Complete: true,
		Credits: []domain.CarpoolResetCreditEvidence{{CreditHash: creditBReplacement}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, replacement.NewCandidates)
	require.NotNil(t, replacement.Batch)
	require.Equal(t, firstBatchID, replacement.Batch.ID)
	require.Equal(t, firstScheduledAt, *replacement.Batch.ScheduledAt, "new evidence must join without delaying the existing promise")

	finalIncomplete, err := repo.RecordResetObservation(ctx, domain.CarpoolResetObservationInput{
		UpstreamIdentityHash: identityB, RepresentativeAccountID: accountB.ID,
		ObservedAt: slotAt.Add(-50 * time.Minute), Complete: false,
		IncompleteReason: "partial_response",
	})
	require.NoError(t, err)
	require.False(t, finalIncomplete.Complete)
	requireObservationState(t, identityB, true, 1, "incomplete")

	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_account_states WHERE upstream_identity_hash IN ($1,$2)`, identityA, identityB))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_batches WHERE scope_id=$1`, domain.CarpoolGlobalScopeID))
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE batch_id=$1`, firstBatchID))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_credits WHERE upstream_identity_hash=$1`, identityA))
	require.Equal(t, 2, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_credits WHERE upstream_identity_hash=$1`, identityB))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_credits WHERE upstream_identity_hash=$1 AND credit_hash=$2 AND initial_stock=TRUE AND assignment_status='baseline'`, identityB, creditBInitial))
	require.Equal(t, 1, resetRowCount(t, `SELECT COUNT(*) FROM carpool_reset_credits WHERE upstream_identity_hash=$1 AND credit_hash=$2 AND initial_stock=FALSE AND assignment_status='assigned' AND reset_batch_id=$3`, identityB, creditBReplacement, firstBatchID))
}

func resetIntegrationHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func requireObservationState(t *testing.T, identityHash string, baselineComplete bool, knownCreditCount int, health string) {
	t.Helper()
	var actualBaseline bool
	var actualKnown int
	var actualHealth string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
		SELECT baseline_complete,known_credit_count,health_status
		FROM carpool_reset_account_states WHERE upstream_identity_hash=$1
	`, identityHash).Scan(&actualBaseline, &actualKnown, &actualHealth))
	require.Equal(t, baselineComplete, actualBaseline)
	require.Equal(t, knownCreditCount, actualKnown)
	require.Equal(t, health, actualHealth)
}

func resetGlobalResetState(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE carpool_reset_scope_states SET pending_batch_id=NULL WHERE scope_id=1;
		DELETE FROM carpool_reset_announcement_outbox WHERE scope_id=1;
		DELETE FROM announcements WHERE source_type='carpool_reset'
		  AND source_id IN (SELECT id FROM carpool_reset_batches WHERE scope_id=1);
		DELETE FROM carpool_reset_qualifications WHERE scope_id=1;
		DELETE FROM carpool_reset_targets
		  WHERE batch_id IN (SELECT id FROM carpool_reset_batches WHERE scope_id=1);
		DELETE FROM carpool_cycle_carryovers
		  WHERE reset_batch_id IN (SELECT id FROM carpool_reset_batches WHERE scope_id=1);
		DELETE FROM carpool_reset_credits;
		DELETE FROM carpool_reset_account_states;
		DELETE FROM carpool_reset_batches WHERE scope_id=1;
		DELETE FROM carpool_reset_scope_states WHERE scope_id=1;
		DELETE FROM carpool_operations
		  WHERE resource_type IN ('reset_batch','reset_observation_scan');
	`)
	require.NoError(t, err)
}

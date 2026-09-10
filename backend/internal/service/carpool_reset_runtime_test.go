package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type resetRuntimeRepository struct {
	publishCalls  atomic.Int32
	officialScope int64
	officialOp    domain.CarpoolOperation
}

type resetRuntimeAccountSource struct{ accounts []Account }

func (s resetRuntimeAccountSource) ListByPlatform(context.Context, string) ([]Account, error) {
	return s.accounts, nil
}

type blockingResetQuotaReader struct{ started chan struct{} }

func (r *blockingResetQuotaReader) QueryUsage(ctx context.Context, _ int64) (*OpenAIQuotaUsage, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *resetRuntimeRepository) ListResetBatches(context.Context, domain.CarpoolResetBatchFilters) ([]domain.CarpoolResetBatch, int64, error) {
	return nil, 0, nil
}
func (r *resetRuntimeRepository) RegisterResetQualification(context.Context, int64, string, string, domain.CarpoolOperation) (*domain.CarpoolResetBatch, error) {
	return nil, nil
}
func (r *resetRuntimeRepository) ScheduleResetBatch(context.Context, int64, string, domain.CarpoolOperation) (*domain.CarpoolResetBatch, error) {
	return nil, nil
}
func (r *resetRuntimeRepository) ExecuteResetBatch(context.Context, int64, *domain.CarpoolOperation) (*domain.CarpoolResetBatch, error) {
	return nil, nil
}
func (r *resetRuntimeRepository) ExecuteOfficialReset(_ context.Context, scopeID int64, operation domain.CarpoolOperation) (*domain.CarpoolResetBatch, error) {
	r.officialScope, r.officialOp = scopeID, operation
	return &domain.CarpoolResetBatch{ScopeID: scopeID, TriggerKind: "official"}, nil
}
func (r *resetRuntimeRepository) ListDueResetBatchIDs(context.Context, int) ([]int64, error) {
	return nil, nil
}
func (r *resetRuntimeRepository) ListResetObservations(context.Context, domain.CarpoolResetObservationFilters) ([]domain.CarpoolResetObservation, int64, error) {
	return nil, 0, nil
}
func (r *resetRuntimeRepository) RecordResetObservation(context.Context, domain.CarpoolResetObservationInput) (*domain.CarpoolResetObservationResult, error) {
	return &domain.CarpoolResetObservationResult{}, nil
}
func (r *resetRuntimeRepository) ListAnnouncementCarpoolScopes(context.Context, int64, time.Time) (map[int64]struct{}, error) {
	return nil, nil
}
func (r *resetRuntimeRepository) UserResetWindow(context.Context, int64, time.Time) (domain.CarpoolUserResetWindow, error) {
	return domain.CarpoolUserResetWindow{}, nil
}
func (r *resetRuntimeRepository) PublishResetAnnouncements(context.Context, int) (int, error) {
	r.publishCalls.Add(1)
	return 0, nil
}
func (r *resetRuntimeRepository) RunResetObservationScanOperation(ctx context.Context, _ domain.CarpoolOperation, scan func(context.Context) (domain.CarpoolResetScanResult, error)) (domain.CarpoolResetScanResult, error) {
	return scan(ctx)
}

func TestCarpoolResetServiceImmediateStopAndRestart(t *testing.T) {
	repo := &resetRuntimeRepository{}
	svc := NewCarpoolResetService(repo)

	svc.Start(context.Background())
	require.Eventually(t, func() bool { return repo.publishCalls.Load() >= 1 }, time.Second, 5*time.Millisecond)
	svc.Stop()
	require.Nil(t, svc.done)
	require.Nil(t, svc.cancel)

	svc.Start(context.Background())
	require.Eventually(t, func() bool { return repo.publishCalls.Load() >= 2 }, time.Second, 5*time.Millisecond)
	svc.Stop()
}

func TestProvideCarpoolResetServiceWiresReadersAndHooks(t *testing.T) {
	repo := &resetRuntimeRepository{}
	quota := &OpenAIQuotaService{}
	announcements := &AnnouncementService{}
	carpool := &CarpoolService{}

	reset := ProvideCarpoolResetService(repo, nil, quota, announcements, carpool)
	reset.Stop()

	require.Same(t, reset, carpool.resetReader)
	require.Same(t, reset, announcements.carpoolAudience)
	quota.observationMu.RLock()
	hook := quota.observationHook
	quota.observationMu.RUnlock()
	require.Same(t, reset, hook)
}

func TestCarpoolResetService_OfficialRequiresConfirmationAndBuildsOperation(t *testing.T) {
	repo := &resetRuntimeRepository{}
	svc := NewCarpoolResetService(repo)
	_, err := svc.OfficialReset(context.Background(), 7, CarpoolOfficialResetInput{ScopeID: domain.CarpoolGlobalScopeID}, "key")
	require.ErrorIs(t, err, ErrCarpoolResetInvalid)
	_, err = svc.OfficialReset(context.Background(), 7, CarpoolOfficialResetInput{ScopeID: 2, Confirmed: true}, "key")
	require.ErrorIs(t, err, ErrCarpoolResetInvalid)

	batch, err := svc.OfficialReset(context.Background(), 7, CarpoolOfficialResetInput{ScopeID: domain.CarpoolGlobalScopeID, Confirmed: true}, "official-key")
	require.NoError(t, err)
	require.EqualValues(t, domain.CarpoolGlobalScopeID, repo.officialScope)
	require.Equal(t, "reset_official", repo.officialOp.Kind)
	require.Equal(t, "official-key", repo.officialOp.Key)
	require.NotEmpty(t, repo.officialOp.Fingerprint)
	require.Equal(t, "official", batch.TriggerKind)
}

func TestCarpoolResetServiceSlowObservationDoesNotBlockDueLoop(t *testing.T) {
	repo := &resetRuntimeRepository{}
	quota := &blockingResetQuotaReader{started: make(chan struct{}, 1)}
	svc := NewCarpoolResetService(repo)
	svc.processInterval = 5 * time.Millisecond
	svc.scanInitialDelay = 0
	svc.scanInterval = time.Hour
	svc.SetObservationScanner(resetRuntimeAccountSource{accounts: []Account{{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic"},
	}}}, quota)
	svc.Start(context.Background())
	t.Cleanup(svc.Stop)

	select {
	case <-quota.started:
	case <-time.After(time.Second):
		require.Fail(t, "observation scan did not start")
	}
	before := repo.publishCalls.Load()
	require.Eventually(t, func() bool { return repo.publishCalls.Load() > before }, time.Second, 5*time.Millisecond)
	svc.Stop()
}

func TestCarpoolResetServiceManualObservationHasOverallTimeout(t *testing.T) {
	repo := &resetRuntimeRepository{}
	quota := &blockingResetQuotaReader{started: make(chan struct{}, 1)}
	svc := NewCarpoolResetService(repo)
	svc.manualScanTimeout = 20 * time.Millisecond
	svc.SetObservationScanner(resetRuntimeAccountSource{accounts: []Account{{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "synthetic"},
	}}}, quota)

	startedAt := time.Now()
	_, err := svc.ScanObservations(context.Background(), 7, "synthetic-key")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(startedAt), time.Second)
}

func TestBuildOpenAIQuotaResetObservationCompletenessAndHashing(t *testing.T) {
	now := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	empty := &openAIRateLimitResetCreditDetails{CreditListPresent: true, IdentityListComplete: true}
	input := buildOpenAIQuotaResetObservation(8, "upstream-raw-id", now, empty)
	require.True(t, input.Complete)
	require.Empty(t, input.Credits)
	require.Len(t, input.UpstreamIdentityHash, 64)
	require.NotContains(t, input.UpstreamIdentityHash, "upstream-raw-id")

	incomplete := buildOpenAIQuotaResetObservation(8, "upstream-raw-id", now, &openAIRateLimitResetCreditDetails{
		CreditListPresent: true, IdentityListComplete: false,
	})
	require.False(t, incomplete.Complete)
	require.Equal(t, "credit_identity_incomplete", incomplete.IncompleteReason)

	complete := buildOpenAIQuotaResetObservation(8, "upstream-raw-id", now, &openAIRateLimitResetCreditDetails{
		CreditListPresent:    true,
		IdentityListComplete: true,
		AutoResetCandidates:  []openAIAutoResetCreditCandidate{{ID: "card-raw-id"}, {ID: "card-raw-id"}},
	})
	require.True(t, complete.Complete)
	require.Len(t, complete.Credits, 1)
	require.Len(t, complete.Credits[0].CreditHash, 64)
	require.NotContains(t, complete.Credits[0].CreditHash, "card-raw-id")
}

func TestParseOpenAIRateLimitResetCreditDetailsRejectsNullMemberAsComplete(t *testing.T) {
	details, err := parseOpenAIRateLimitResetCreditDetails([]byte(`[null]`))
	require.NoError(t, err)
	require.True(t, details.CreditListPresent)
	require.False(t, details.IdentityListComplete)
}

func TestNormalizeResetObservationAccountsDeduplicatesImportsAndShadows(t *testing.T) {
	parentID := int64(7)
	accounts := []Account{
		{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "same"}},
		{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "same"}},
		{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, Credentials: map[string]any{"chatgpt_account_id": "same"}},
		{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"chatgpt_account_id": "other"}},
	}

	got := normalizeResetObservationAccounts(accounts)
	require.Len(t, got, 1)
	require.Equal(t, int64(7), got[0].ID)
}

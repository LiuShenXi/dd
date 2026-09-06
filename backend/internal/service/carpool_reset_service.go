package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrCarpoolResetNotDue      = infraerrors.Conflict("CARPOOL_RESET_NOT_DUE", "carpool reset batch is not due")
	ErrCarpoolResetInvalid     = infraerrors.BadRequest("CARPOOL_RESET_INVALID", "carpool reset request is invalid")
	ErrCarpoolResetUnavailable = infraerrors.Conflict("CARPOOL_RESET_UNAVAILABLE", "carpool reset batch is unavailable")
)

type CarpoolResetRepositoryAPI interface {
	ListResetBatches(context.Context, domain.CarpoolResetBatchFilters) ([]domain.CarpoolResetBatch, int64, error)
	RegisterResetQualification(context.Context, int64, string, string, domain.CarpoolOperation) (*domain.CarpoolResetBatch, error)
	ScheduleResetBatch(context.Context, int64, string, domain.CarpoolOperation) (*domain.CarpoolResetBatch, error)
	ExecuteResetBatch(context.Context, int64, *domain.CarpoolOperation) (*domain.CarpoolResetBatch, error)
	ListDueResetBatchIDs(context.Context, int) ([]int64, error)
	ListResetObservations(context.Context, domain.CarpoolResetObservationFilters) ([]domain.CarpoolResetObservation, int64, error)
	RecordResetObservation(context.Context, domain.CarpoolResetObservationInput) (*domain.CarpoolResetObservationResult, error)
	ListAnnouncementCarpoolScopes(context.Context, int64, time.Time) (map[int64]struct{}, error)
	UserResetWindow(context.Context, int64, time.Time) (domain.CarpoolUserResetWindow, error)
	PublishResetAnnouncements(context.Context, int) (int, error)
}

type CarpoolResetService struct {
	repo     CarpoolResetRepositoryAPI
	accounts CarpoolResetAccountSource
	quota    CarpoolResetQuotaReader

	mu                sync.Mutex
	cancel            context.CancelFunc
	done              chan struct{}
	processInterval   time.Duration
	scanInitialDelay  time.Duration
	scanInterval      time.Duration
	manualScanTimeout time.Duration
}

type CarpoolResetAccountSource interface {
	ListByPlatform(context.Context, string) ([]Account, error)
}

type CarpoolResetQuotaReader interface {
	QueryUsage(context.Context, int64) (*OpenAIQuotaUsage, error)
}

type CarpoolResetScanOperationRepository interface {
	RunResetObservationScanOperation(context.Context, domain.CarpoolOperation, func(context.Context) (domain.CarpoolResetScanResult, error)) (domain.CarpoolResetScanResult, error)
}

func NewCarpoolResetService(repo CarpoolResetRepositoryAPI) *CarpoolResetService {
	return &CarpoolResetService{
		repo:              repo,
		processInterval:   5 * time.Second,
		scanInitialDelay:  time.Minute,
		scanInterval:      time.Hour,
		manualScanTimeout: 2 * time.Minute,
	}
}

func (s *CarpoolResetService) SetObservationScanner(accounts CarpoolResetAccountSource, quota CarpoolResetQuotaReader) {
	s.mu.Lock()
	s.accounts = accounts
	s.quota = quota
	s.mu.Unlock()
}

type CarpoolResetQualificationInput struct {
	ScopeID        int64  `json:"scope_id"`
	Confirmed      bool   `json:"confirmed"`
	SourceEventKey string `json:"source_event_key"`
	Reason         string `json:"reason"`
}

type CarpoolResetScheduleInput struct {
	Reason string `json:"reason"`
}

func (s *CarpoolResetService) ListBatches(ctx context.Context, filters domain.CarpoolResetBatchFilters) ([]domain.CarpoolResetBatch, int64, error) {
	return s.repo.ListResetBatches(ctx, filters)
}

func (s *CarpoolResetService) RegisterQualification(ctx context.Context, actorID int64, input CarpoolResetQualificationInput, key string) (*domain.CarpoolResetBatch, error) {
	input.SourceEventKey = strings.TrimSpace(input.SourceEventKey)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ScopeID == 0 {
		input.ScopeID = domain.CarpoolGlobalScopeID
	}
	if actorID <= 0 || input.ScopeID != domain.CarpoolGlobalScopeID || !input.Confirmed || input.SourceEventKey == "" || input.Reason == "" || strings.TrimSpace(key) == "" {
		return nil, ErrCarpoolResetInvalid
	}
	fingerprint, err := carpoolFingerprint("reset_qualification", actorID, input.ScopeID, input)
	if err != nil {
		return nil, err
	}
	return s.repo.RegisterResetQualification(ctx, input.ScopeID, input.SourceEventKey, input.Reason, domain.CarpoolOperation{Kind: "reset_qualification", ActorID: actorID, Key: key, Fingerprint: fingerprint})
}

func (s *CarpoolResetService) Schedule(ctx context.Context, batchID, actorID int64, input CarpoolResetScheduleInput, key string) (*domain.CarpoolResetBatch, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if batchID <= 0 || actorID <= 0 || input.Reason == "" || strings.TrimSpace(key) == "" {
		return nil, ErrCarpoolResetInvalid
	}
	fingerprint, err := carpoolFingerprint("reset_schedule", actorID, batchID, input)
	if err != nil {
		return nil, err
	}
	return s.repo.ScheduleResetBatch(ctx, batchID, input.Reason, domain.CarpoolOperation{Kind: "reset_schedule", ActorID: actorID, Key: key, Fingerprint: fingerprint})
}

func (s *CarpoolResetService) Execute(ctx context.Context, batchID, actorID int64, key string) (*domain.CarpoolResetBatch, error) {
	if batchID <= 0 || actorID <= 0 || strings.TrimSpace(key) == "" {
		return nil, ErrCarpoolResetInvalid
	}
	fingerprint, err := carpoolFingerprint("reset_execute", actorID, batchID, struct{}{})
	if err != nil {
		return nil, err
	}
	operation := &domain.CarpoolOperation{Kind: "reset_execute", ActorID: actorID, Key: key, Fingerprint: fingerprint}
	return s.repo.ExecuteResetBatch(ctx, batchID, operation)
}

func (s *CarpoolResetService) UserResetWindow(ctx context.Context, userID int64, now time.Time) (domain.CarpoolUserResetWindow, error) {
	return s.repo.UserResetWindow(ctx, userID, now)
}

func (s *CarpoolResetService) ListAnnouncementCarpoolScopes(ctx context.Context, userID int64, now time.Time) (map[int64]struct{}, error) {
	return s.repo.ListAnnouncementCarpoolScopes(ctx, userID, now)
}

func (s *CarpoolResetService) ListObservations(ctx context.Context, filters domain.CarpoolResetObservationFilters) ([]domain.CarpoolResetObservation, int64, error) {
	return s.repo.ListResetObservations(ctx, filters)
}

func (s *CarpoolResetService) RecordOpenAIQuotaResetObservation(ctx context.Context, input domain.CarpoolResetObservationInput) (*domain.CarpoolResetObservationResult, error) {
	return s.repo.RecordResetObservation(ctx, input)
}

func (s *CarpoolResetService) ScanObservations(ctx context.Context, actorID int64, key string) (domain.CarpoolResetScanResult, error) {
	if actorID <= 0 || strings.TrimSpace(key) == "" {
		return domain.CarpoolResetScanResult{}, ErrCarpoolResetInvalid
	}
	fingerprint, err := carpoolFingerprint("reset_observation_scan", actorID, domain.CarpoolGlobalScopeID, struct{}{})
	if err != nil {
		return domain.CarpoolResetScanResult{}, err
	}
	operationRepo, ok := s.repo.(CarpoolResetScanOperationRepository)
	if !ok {
		return domain.CarpoolResetScanResult{}, ErrCarpoolResetUnavailable
	}
	timeout := s.manualScanTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return operationRepo.RunResetObservationScanOperation(scanCtx, domain.CarpoolOperation{
		Kind: "reset_observation_scan", ActorID: actorID, Key: key, Fingerprint: fingerprint,
	}, s.scanObservations)
}

func (s *CarpoolResetService) scanObservations(ctx context.Context) (domain.CarpoolResetScanResult, error) {
	s.mu.Lock()
	accountsSource, quota := s.accounts, s.quota
	s.mu.Unlock()
	if accountsSource == nil || quota == nil {
		return domain.CarpoolResetScanResult{}, ErrCarpoolResetUnavailable
	}
	accounts, err := accountsSource.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return domain.CarpoolResetScanResult{}, err
	}
	parents := normalizeResetObservationAccounts(accounts)
	result := domain.CarpoolResetScanResult{Scanned: len(parents)}
	jobs := make(chan Account)
	var resultMu sync.Mutex
	var workers sync.WaitGroup
	workerCount := 4
	if len(parents) < workerCount {
		workerCount = len(parents)
	}
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for account := range jobs {
				complete, candidates := s.scanResetObservationAccount(ctx, quota, account)
				resultMu.Lock()
				if complete {
					result.Complete++
				} else {
					result.Incomplete++
				}
				result.NewCandidates += candidates
				resultMu.Unlock()
			}
		}()
	}
	for _, account := range parents {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return result, ctx.Err()
		case jobs <- account:
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *CarpoolResetService) scanResetObservationAccount(ctx context.Context, quota CarpoolResetQuotaReader, account Account) (bool, int) {
	var usage *OpenAIQuotaUsage
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		usage, err = quota.QueryUsage(callCtx, account.ID)
		cancel()
		if err == nil {
			break
		}
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return false, 0
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if err != nil || usage == nil {
		if failure, ok := resetObservationFailureInput(account, time.Now().UTC(), "query_failed"); ok {
			_, _ = s.repo.RecordResetObservation(ctx, failure)
		}
		return false, 0
	}
	if usage.resetObservation == nil {
		return false, 0
	}
	observationResult := usage.resetObservationResult
	observationErr := usage.resetObservationErr
	if observationResult == nil && observationErr == nil {
		observationResult, observationErr = s.repo.RecordResetObservation(ctx, *usage.resetObservation)
	}
	if observationErr != nil || observationResult == nil {
		return false, 0
	}
	return observationResult.Complete, observationResult.NewCandidates
}

func normalizeResetObservationAccounts(accounts []Account) []Account {
	byIdentity := make(map[string]Account)
	for _, account := range accounts {
		if account.Type != AccountTypeOAuth || account.IsShadow() {
			continue
		}
		identity := strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
		if identity == "" {
			identity = strings.TrimSpace(account.GetCredential("organization_id"))
		}
		if identity == "" {
			continue
		}
		identityHash := hashCarpoolResetValue("openai:chatgpt-account-id", identity)
		if existing, ok := byIdentity[identityHash]; !ok || account.ID < existing.ID {
			byIdentity[identityHash] = account
		}
	}
	result := make([]Account, 0, len(byIdentity))
	for _, account := range byIdentity {
		result = append(result, account)
	}
	return result
}

func resetObservationFailureInput(account Account, observedAt time.Time, reason string) (domain.CarpoolResetObservationInput, bool) {
	identity := strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
	if identity == "" {
		identity = strings.TrimSpace(account.GetCredential("organization_id"))
	}
	if identity == "" {
		return domain.CarpoolResetObservationInput{}, false
	}
	return domain.CarpoolResetObservationInput{
		UpstreamIdentityHash:    hashCarpoolResetValue("openai:chatgpt-account-id", identity),
		RepresentativeAccountID: account.ID,
		ObservedAt:              observedAt,
		HealthStatus:            "error",
		IncompleteReason:        reason,
	}, true
}

func (s *CarpoolResetService) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	go s.run(ctx, done)
}

func (s *CarpoolResetService) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *CarpoolResetService) run(ctx context.Context, done chan struct{}) {
	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		s.runObservationScanner(ctx)
	}()
	defer func() {
		background.Wait()
		close(done)
	}()
	processInterval := s.processInterval
	if processInterval <= 0 {
		processInterval = 5 * time.Second
	}
	ticker := time.NewTicker(processInterval)
	defer ticker.Stop()
	for {
		s.processBackground(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *CarpoolResetService) runObservationScanner(ctx context.Context) {
	initialDelay := s.scanInitialDelay
	if initialDelay < 0 {
		initialDelay = time.Minute
	}
	timer := time.NewTimer(initialDelay + resetObservationJitterForWindow(time.Now(), initialDelay))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_, _ = s.scanObservations(scanCtx)
			cancel()
			scanInterval := s.scanInterval
			if scanInterval <= 0 {
				scanInterval = time.Hour
			}
			timer.Reset(scanInterval + resetObservationJitterForWindow(time.Now(), scanInterval))
		}
	}
}

func (s *CarpoolResetService) processBackground(ctx context.Context) {
	publishCtx, publishCancel := context.WithTimeout(ctx, 10*time.Second)
	_, _ = s.repo.PublishResetAnnouncements(publishCtx, 20)
	publishCancel()
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	ids, err := s.repo.ListDueResetBatchIDs(listCtx, 20)
	listCancel()
	if err != nil {
		return
	}
	for _, id := range ids {
		executeCtx, executeCancel := context.WithTimeout(ctx, 15*time.Second)
		_, _ = s.repo.ExecuteResetBatch(executeCtx, id, nil)
		executeCancel()
	}
}

func resetObservationJitter(now time.Time) time.Duration {
	return time.Duration(now.UnixNano() % int64(5*time.Minute))
}

func resetObservationJitterForWindow(now time.Time, window time.Duration) time.Duration {
	if window < time.Second {
		return 0
	}
	return resetObservationJitter(now)
}

func hashCarpoolResetValue(namespace, value string) string {
	sum := sha256.Sum256([]byte(namespace + "\n" + strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func validateCarpoolResetBatchStatus(status string) bool {
	switch status {
	case "", domain.CarpoolResetStatusQualified, domain.CarpoolResetStatusScheduled, domain.CarpoolResetStatusRunning, domain.CarpoolResetStatusCompleted, domain.CarpoolResetStatusCancelled, domain.CarpoolResetStatusNeedsReview:
		return true
	default:
		return false
	}
}

func normalizeResetPage(page, pageSize *int) {
	if *page < 1 {
		*page = 1
	}
	if *pageSize < 1 {
		*pageSize = 20
	}
	if *pageSize > 200 {
		*pageSize = 200
	}
}

func resetSourceEventHash(scopeID int64, sourceEventKey string) string {
	return hashCarpoolResetValue(fmt.Sprintf("carpool-reset-scope:%d", scopeID), sourceEventKey)
}

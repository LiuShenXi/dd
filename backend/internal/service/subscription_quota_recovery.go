package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

type SubscriptionQuotaRecoveryService struct {
	repo                         SubscriptionQuotaBillingRepository
	billing                      UsageBillingRepository
	logs                         UsageLogRepository
	owner                        string
	batch                        int
	interval, itemTimeout, grace time.Duration
	enabled                      bool
	startOnce, stopOnce          sync.Once
	cancel                       context.CancelFunc
	wg                           sync.WaitGroup
	postSettlement               func(context.Context, domain.SubscriptionQuotaKnownUsage) error
}

func NewSubscriptionQuotaRecoveryService(repo SubscriptionQuotaBillingRepository, billing UsageBillingRepository, logs UsageLogRepository, cfg *config.Config) *SubscriptionQuotaRecoveryService {
	s := &SubscriptionQuotaRecoveryService{repo: repo, billing: billing, logs: logs, owner: uuid.NewString(), batch: 100, interval: 30 * time.Second, itemTimeout: 5 * time.Second, grace: time.Minute, enabled: true}
	if cfg != nil {
		q := cfg.SubscriptionQuota
		s.enabled = q.RecoveryEnabled
		if q.RecoveryBatchSize > 0 {
			s.batch = q.RecoveryBatchSize
		}
		if q.RecoveryInterval > 0 {
			s.interval = q.RecoveryInterval
		}
		if q.RecoveryItemTimeout > 0 {
			s.itemTimeout = q.RecoveryItemTimeout
		}
		if q.RequestStaleGrace > 0 {
			s.grace = q.RequestStaleGrace
		}
	}
	if s.batch > 1000 {
		s.batch = 1000
	}
	return s
}
func (s *SubscriptionQuotaRecoveryService) Start() {
	if s == nil || !s.enabled {
		return
	}
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()
			for {
				if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
					logger.LegacyPrintf("service.subscription_quota", "recovery batch incomplete: %v", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
func (s *SubscriptionQuotaRecoveryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.startOnce.Do(func() {})
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

// RunOnce is also the bounded operational/test seam. Each item gets a new child
// of the worker root, so one timeout cannot poison later items and Stop cancels
// ongoing work. It is independent of issuance and new-admission flags.
func (s *SubscriptionQuotaRecoveryService) RunOnce(ctx context.Context) (runErr error) {
	if s == nil || s.repo == nil || s.billing == nil || s.logs == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	defer func() { s.observeBatch(ctx, runErr) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	scanCtx, cancel := context.WithTimeout(ctx, s.itemTimeout)
	batch, err := s.repo.ClaimRecoverable(scanCtx, domain.SubscriptionQuotaRecoveryClaim{Owner: s.owner, Limit: s.batch, Lease: 30 * time.Second})
	cancel()
	if err != nil {
		return err
	}
	var firstErr error
	remember := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, known := range batch {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		itemCtx, cancel := context.WithTimeout(ctx, s.itemTimeout)
		command, decodeErr := DecodeSubscriptionQuotaUsageBillingReceipt(known.BillingPayload)
		if decodeErr == nil && (known.ReceiptVersion != SubscriptionQuotaUsageBillingReceiptVersion || known.RequestFingerprint != command.RequestFingerprint || known.ActualCostUSD != command.SubscriptionTermCost.StringFixed(8) || !domain.SubscriptionQuotaSnapshotsEqual(known.Snapshot, *command.SubscriptionTermSnapshot)) {
			decodeErr = ErrSubscriptionQuotaReceiptConflict
		}
		var applyErr error
		if decodeErr == nil {
			applyErr = ValidateSubscriptionQuotaUsageLogPayload(known.UsageLogPayload, command)
			if applyErr == nil {
				_, applyErr = s.billing.Apply(itemCtx, command)
			}
		} else {
			applyErr = decodeErr
		}
		cancel()
		if applyErr != nil {
			observeSubscriptionQuotaRecoveryRetry()
			remember(applyErr)
			category := "transient_settlement_error"
			if errors.Is(applyErr, ErrSubscriptionQuotaReceiptConflict) || errors.Is(applyErr, ErrUsageBillingRequestConflict) || errors.Is(applyErr, ErrUsageBillingSourceConflict) {
				category = "receipt_conflict"
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, s.itemTimeout)
			remember(s.repo.FinishRecoveryAttempt(cleanupCtx, known.Snapshot.RequestRowID, s.owner, category, subscriptionQuotaRecoveryDelay(known.RecoveryAttempts)))
			cleanupCancel()
			continue
		}
		logCtx, logCancel := context.WithTimeout(ctx, s.itemTimeout)
		remember(s.writeUsageLog(logCtx, known))
		logCancel()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	logScanCtx, logScanCancel := context.WithTimeout(ctx, s.itemTimeout)
	pending, logErr := s.repo.ListPendingUsageLogs(logScanCtx, s.batch)
	logScanCancel()
	remember(logErr)
	for _, known := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		itemCtx, cancel := context.WithTimeout(ctx, s.itemTimeout)
		remember(s.writeUsageLog(itemCtx, known))
		cancel()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	remember(s.repo.MarkStaleAdmissions(ctx, s.grace, s.batch))
	if maintenance, ok := s.repo.(interface {
		MaintainTermLifecycles(context.Context, int) error
	}); ok {
		remember(maintenance.MaintainTermLifecycles(ctx, s.batch))
	}
	return firstErr
}

func subscriptionQuotaRecoveryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 7 {
		return 5 * time.Minute
	}
	delay := 5 * time.Second * time.Duration(1<<uint(attempts-1))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
func (s *SubscriptionQuotaRecoveryService) writeUsageLog(ctx context.Context, known domain.SubscriptionQuotaKnownUsage) (writeErr error) {
	defer func() {
		if writeErr != nil {
			observeSubscriptionQuotaRecoveryRetry()
		}
	}()
	command, err := DecodeSubscriptionQuotaUsageBillingReceipt(known.BillingPayload)
	if err != nil {
		return err
	}
	if err = ValidateSubscriptionQuotaUsageLogPayload(known.UsageLogPayload, command); err != nil {
		return err
	}
	log, err := DecodeSubscriptionQuotaUsageLogPayload(known.UsageLogPayload)
	if err != nil {
		return err
	}
	if _, err = s.logs.Create(ctx, log); err != nil {
		observeSubscriptionQuotaPublicationFailure()
		return fmt.Errorf("quota usage statistics write: %w", err)
	}
	if s.postSettlement != nil {
		if err = s.postSettlement(ctx, known); err != nil {
			return err
		}
	}
	return s.repo.MarkUsageLogRecorded(ctx, known.Snapshot.RequestRowID, known.RequestFingerprint)
}

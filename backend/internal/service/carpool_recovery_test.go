package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type carpoolRecoveryRepositoryStub struct {
	CarpoolRepositoryAPI

	databaseNowFn func(context.Context) (time.Time, error)
	maintainFn    func(context.Context, time.Time, int) error
	recoverFn     func(context.Context, int) ([]domain.CarpoolKnownUsage, error)
	markFn        func(context.Context, domain.CarpoolBillingSnapshot, string) error

	mu          sync.Mutex
	reconciled  []domain.CarpoolBillingSnapshot
	diagnostics []string
}

func (s *carpoolRecoveryRepositoryStub) DatabaseNow(ctx context.Context) (time.Time, error) {
	if s.databaseNowFn != nil {
		return s.databaseNowFn(ctx)
	}
	return time.Now().UTC(), nil
}

func (s *carpoolRecoveryRepositoryStub) MaintainCycles(ctx context.Context, now time.Time, limit int) error {
	if s.maintainFn != nil {
		return s.maintainFn(ctx, now, limit)
	}
	return nil
}

func (s *carpoolRecoveryRepositoryStub) RecoverPendingReceipts(ctx context.Context, limit int) ([]domain.CarpoolKnownUsage, error) {
	if s.recoverFn != nil {
		return s.recoverFn(ctx, limit)
	}
	return nil, nil
}

func (s *carpoolRecoveryRepositoryStub) MarkKnownUsageReconcileRequired(ctx context.Context, snapshot domain.CarpoolBillingSnapshot, diagnostic string) error {
	if s.markFn != nil {
		if err := s.markFn(ctx, snapshot, diagnostic); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconciled = append(s.reconciled, snapshot)
	s.diagnostics = append(s.diagnostics, diagnostic)
	return nil
}

func (s *carpoolRecoveryRepositoryStub) reconciliationState() ([]domain.CarpoolBillingSnapshot, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.CarpoolBillingSnapshot(nil), s.reconciled...), append([]string(nil), s.diagnostics...)
}

type carpoolRecoveryBillingStub struct {
	UsageBillingRepository
	applyFn func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error)
}

func (s *carpoolRecoveryBillingStub) Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	return s.applyFn(ctx, cmd)
}

func TestCarpoolKnownUsageRecoveryRetainsBoundedTransientRetries(t *testing.T) {
	for _, retryCount := range []int{1, carpoolKnownUsageMaxRetryCount - 1} {
		t.Run(strconv.Itoa(retryCount), func(t *testing.T) {
			receipt := carpoolRecoveryReceipt(t, retryCount, 1)
			repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
				return []domain.CarpoolKnownUsage{receipt}, nil
			}}
			svc := NewCarpoolService(repo)
			svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
				return nil, errors.New("temporary database failure")
			}})

			svc.recoverKnownUsage(context.Background(), 10)

			reconciled, diagnostics := repo.reconciliationState()
			require.Empty(t, reconciled)
			require.Empty(t, diagnostics)
		})
	}

	receipt := carpoolRecoveryReceipt(t, carpoolKnownUsageMaxRetryCount, 1)
	repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
		return []domain.CarpoolKnownUsage{receipt}, nil
	}}
	svc := NewCarpoolService(repo)
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		return nil, errors.New("temporary database failure")
	}})

	svc.recoverKnownUsage(context.Background(), 10)

	reconciled, diagnostics := repo.reconciliationState()
	require.Equal(t, []domain.CarpoolBillingSnapshot{receipt.Snapshot}, reconciled)
	require.Equal(t, []string{"automatic_settlement_retry_exhausted"}, diagnostics)
}

func TestCarpoolKnownUsageRecoveryPromotesPermanentFailuresImmediately(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "missing user", err: ErrUserNotFound},
		{name: "missing api key", err: ErrAPIKeyNotFound},
		{name: "missing account", err: ErrAccountNotFound},
		{name: "missing group", err: ErrGroupNotFound},
		{name: "immutable request conflict", err: ErrUsageBillingRequestConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt := carpoolRecoveryReceipt(t, 1, 1)
			repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
				return []domain.CarpoolKnownUsage{receipt}, nil
			}}
			svc := NewCarpoolService(repo)
			svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
				return nil, tc.err
			}})

			svc.recoverKnownUsage(context.Background(), 10)

			reconciled, diagnostics := repo.reconciliationState()
			require.Equal(t, []domain.CarpoolBillingSnapshot{receipt.Snapshot}, reconciled)
			require.Equal(t, []string{"automatic_settlement_permanent_failure"}, diagnostics)
		})
	}
}

func TestCarpoolKnownUsageRecoveryContinuesAfterMalformedReceipt(t *testing.T) {
	malformed := carpoolRecoveryReceipt(t, 1, 1)
	malformed.BillingPayload = []byte(`{"version":1`)
	valid := carpoolRecoveryReceipt(t, 1, 2)
	repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
		return []domain.CarpoolKnownUsage{malformed, valid}, nil
	}}
	var applied []string
	svc := NewCarpoolService(repo)
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		applied = append(applied, cmd.RequestID)
		return &UsageBillingApplyResult{Applied: true}, nil
	}})

	svc.recoverKnownUsage(context.Background(), 10)

	reconciled, diagnostics := repo.reconciliationState()
	require.Equal(t, []domain.CarpoolBillingSnapshot{malformed.Snapshot}, reconciled)
	require.Equal(t, []string{"invalid_durable_billing_command"}, diagnostics)
	require.Equal(t, []string{valid.Snapshot.RequestID}, applied)
}

func TestCarpoolKnownUsageRecoveryAcceptsStaleDedupConvergence(t *testing.T) {
	receipt := carpoolRecoveryReceipt(t, carpoolKnownUsageMaxRetryCount, 1)
	repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
		return []domain.CarpoolKnownUsage{receipt}, nil
	}}
	svc := NewCarpoolService(repo)
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		return &UsageBillingApplyResult{Applied: false}, nil
	}})

	svc.recoverKnownUsage(context.Background(), 10)

	reconciled, diagnostics := repo.reconciliationState()
	require.Empty(t, reconciled)
	require.Empty(t, diagnostics)
}

func TestCarpoolKnownUsageRecoveryBlockedReceiptDoesNotStarveFollowingReceipt(t *testing.T) {
	blocked := carpoolRecoveryReceipt(t, carpoolKnownUsageMaxRetryCount, 1)
	following := carpoolRecoveryReceipt(t, 1, 2)
	markContextActive := false
	repo := &carpoolRecoveryRepositoryStub{
		recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
			return []domain.CarpoolKnownUsage{blocked, following}, nil
		},
		markFn: func(ctx context.Context, _ domain.CarpoolBillingSnapshot, _ string) error {
			markContextActive = ctx.Err() == nil
			return nil
		},
	}
	var settled []string
	svc := NewCarpoolService(repo)
	svc.recoveryApplyTimeout = 20 * time.Millisecond
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		if cmd.RequestID == blocked.Snapshot.RequestID {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		settled = append(settled, cmd.RequestID)
		return &UsageBillingApplyResult{Applied: true}, nil
	}})

	svc.recoverKnownUsage(context.Background(), 10)

	reconciled, diagnostics := repo.reconciliationState()
	require.Equal(t, []domain.CarpoolBillingSnapshot{blocked.Snapshot}, reconciled)
	require.Equal(t, []string{"automatic_settlement_retry_exhausted"}, diagnostics)
	require.True(t, markContextActive)
	require.Equal(t, []string{following.Snapshot.RequestID}, settled)
}

func TestCarpoolKnownUsageRecoveryDoesNotPromoteAfterParentCancellation(t *testing.T) {
	receipt := carpoolRecoveryReceipt(t, carpoolKnownUsageMaxRetryCount, 1)
	repo := &carpoolRecoveryRepositoryStub{recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
		return []domain.CarpoolKnownUsage{receipt}, nil
	}}
	svc := NewCarpoolService(repo)
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		require.Fail(t, "Apply should not run after parent cancellation")
		return nil, context.Canceled
	}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc.recoverKnownUsage(ctx, 10)

	reconciled, diagnostics := repo.reconciliationState()
	require.Empty(t, reconciled)
	require.Empty(t, diagnostics)
}

func TestCarpoolMaintenanceDoesNotStarveKnownUsageRecovery(t *testing.T) {
	maintenanceStarted := make(chan struct{}, 1)
	recoveryStarted := make(chan struct{}, 1)
	repo := &carpoolRecoveryRepositoryStub{
		databaseNowFn: func(ctx context.Context) (time.Time, error) {
			maintenanceStarted <- struct{}{}
			<-ctx.Done()
			return time.Time{}, ctx.Err()
		},
		recoverFn: func(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
			recoveryStarted <- struct{}{}
			return nil, nil
		},
	}
	svc := NewCarpoolService(repo)
	svc.maintenanceTimeout = time.Hour
	svc.recoveryScanTimeout = time.Hour
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		return &UsageBillingApplyResult{}, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.maintain(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-maintenanceStarted:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-recoveryStarted:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
}

func TestCarpoolServiceStopCancelsBlockedMaintenanceAndRecovery(t *testing.T) {
	started := make(chan struct{}, 2)
	block := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	repo := &carpoolRecoveryRepositoryStub{
		databaseNowFn: func(ctx context.Context) (time.Time, error) {
			return time.Time{}, block(ctx)
		},
		recoverFn: func(ctx context.Context, _ int) ([]domain.CarpoolKnownUsage, error) {
			return nil, block(ctx)
		},
	}
	svc := NewCarpoolService(repo)
	svc.maintenanceInterval = time.Hour
	svc.maintenanceTimeout = time.Hour
	svc.recoveryScanTimeout = time.Hour
	svc.SetUsageBillingApplier(&carpoolRecoveryBillingStub{applyFn: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
		return &UsageBillingApplyResult{}, nil
	}})
	svc.Start(context.Background())

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			require.FailNow(t, "background work did not start")
		}
	}
	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		require.FailNow(t, "Stop did not cancel blocked work")
	}
}

func carpoolRecoveryReceipt(t *testing.T, retryCount, sequence int) domain.CarpoolKnownUsage {
	t.Helper()
	snapshot := domain.CarpoolBillingSnapshot{
		BillingRequestID: int64(100 + sequence),
		RequestID:        "recovery-request-" + strconv.Itoa(sequence),
		UserID:           10,
		APIKeyID:         20,
		GroupID:          30,
		TermID:           40,
		CycleID:          50,
		AdmittedAt:       time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	cost := decimal.RequireFromString("1.25000000")
	payload, err := MarshalCarpoolUsageBillingReceipt(&UsageBillingCommand{
		RequestID:       snapshot.RequestID,
		APIKeyID:        snapshot.APIKeyID,
		UserID:          snapshot.UserID,
		CarpoolCost:     cost,
		CarpoolSnapshot: &snapshot,
	})
	require.NoError(t, err)
	return domain.CarpoolKnownUsage{
		Snapshot:       snapshot,
		ActualCostUSD:  cost,
		BillingPayload: payload,
		RetryCount:     retryCount,
	}
}

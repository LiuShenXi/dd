package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

type SubscriptionQuotaBacklog struct {
	Admitted           int64   `json:"admitted"`
	Known              int64   `json:"pending_known"`
	Reconcile          int64   `json:"reconcile_required"`
	PendingLogs        int64   `json:"pending_statistics"`
	OldestKnownSeconds float64 `json:"oldest_known_seconds"`
}
type SubscriptionQuotaMetricsSnapshot struct {
	SubscriptionQuotaBacklog
	RecoveryRuns         uint64            `json:"recovery_runs"`
	RecoveryFailures     uint64            `json:"recovery_failures"`
	KnownPersisted       uint64            `json:"known_persisted"`
	UnknownTransitions   uint64            `json:"unknown_transitions"`
	Admission            map[string]uint64 `json:"admission_total"`
	Settlement           map[string]uint64 `json:"settlement_total"`
	LedgerMismatch       uint64            `json:"ledger_mismatch_total"`
	UsageLogMissing      uint64            `json:"usage_log_missing_total"`
	RecoveryRetries      uint64            `json:"recovery_retries_total"`
	OverageUSD           string            `json:"overage_usd"`
	ScopeLockWaitCount   uint64            `json:"scope_lock_wait_count"`
	ScopeLockWaitSeconds float64           `json:"scope_lock_wait_seconds"`
}

var subscriptionQuotaMetrics struct {
	runs, failures, known, unknown               atomic.Uint64
	backlog                                      atomic.Pointer[SubscriptionQuotaBacklog]
	mu                                           sync.Mutex
	admission, settlement                        map[string]uint64
	ledgerMismatch, missing, retries, scopeCount uint64
	scopeWait                                    time.Duration
	overage                                      decimal.Decimal
}

func GetSubscriptionQuotaMetricsSnapshot() SubscriptionQuotaMetricsSnapshot {
	s := SubscriptionQuotaMetricsSnapshot{RecoveryRuns: subscriptionQuotaMetrics.runs.Load(), RecoveryFailures: subscriptionQuotaMetrics.failures.Load(), KnownPersisted: subscriptionQuotaMetrics.known.Load(), UnknownTransitions: subscriptionQuotaMetrics.unknown.Load()}
	if backlog := subscriptionQuotaMetrics.backlog.Load(); backlog != nil {
		s.SubscriptionQuotaBacklog = *backlog
	}
	subscriptionQuotaMetrics.mu.Lock()
	defer subscriptionQuotaMetrics.mu.Unlock()
	s.Admission = map[string]uint64{}
	for k, v := range subscriptionQuotaMetrics.admission {
		s.Admission[k] = v
	}
	s.Settlement = map[string]uint64{}
	for k, v := range subscriptionQuotaMetrics.settlement {
		s.Settlement[k] = v
	}
	s.LedgerMismatch = subscriptionQuotaMetrics.ledgerMismatch
	s.UsageLogMissing = subscriptionQuotaMetrics.missing
	s.RecoveryRetries = subscriptionQuotaMetrics.retries
	s.OverageUSD = subscriptionQuotaMetrics.overage.StringFixed(8)
	s.ScopeLockWaitCount = subscriptionQuotaMetrics.scopeCount
	s.ScopeLockWaitSeconds = subscriptionQuotaMetrics.scopeWait.Seconds()
	return s
}

var ErrSubscriptionQuotaLedgerMismatch = errors.New("subscription quota ledger and used amount disagree")

func ObserveSubscriptionQuotaAdmission(err error) {
	result := "admitted"
	switch {
	case err == nil:
	case errors.Is(err, ErrSubscriptionQuotaExpired):
		result = "expired"
	case errors.Is(err, ErrSubscriptionQuotaExhausted):
		result = "exhausted"
	case errors.Is(err, ErrSubscriptionQuotaCancelled):
		result = "cancelled"
	case errors.Is(err, ErrSubscriptionQuotaNotActive):
		result = "inactive"
	case errors.Is(err, ErrSubscriptionQuotaReconciliation):
		result = "reconcile"
	default:
		result = "unavailable"
	}
	subscriptionQuotaMetrics.mu.Lock()
	if subscriptionQuotaMetrics.admission == nil {
		subscriptionQuotaMetrics.admission = map[string]uint64{}
	}
	subscriptionQuotaMetrics.admission[result]++
	subscriptionQuotaMetrics.mu.Unlock()
	slog.Info("subscription_quota_admission_total", "result", result)
}
func ObserveSubscriptionQuotaSettlement(applied bool, err error) {
	result := "replay"
	if applied {
		result = "applied"
	}
	if err != nil {
		result = "failed"
		if errors.Is(err, ErrSubscriptionQuotaReceiptConflict) || errors.Is(err, ErrUsageBillingRequestConflict) || errors.Is(err, ErrUsageBillingSourceConflict) {
			result = "conflict"
		}
	}
	subscriptionQuotaMetrics.mu.Lock()
	if subscriptionQuotaMetrics.settlement == nil {
		subscriptionQuotaMetrics.settlement = map[string]uint64{}
	}
	subscriptionQuotaMetrics.settlement[result]++
	subscriptionQuotaMetrics.mu.Unlock()
	slog.Info("subscription_quota_settlement_total", "result", result)
}
func ObserveSubscriptionQuotaScopeWait(wait time.Duration) {
	subscriptionQuotaMetrics.mu.Lock()
	subscriptionQuotaMetrics.scopeCount++
	subscriptionQuotaMetrics.scopeWait += wait
	subscriptionQuotaMetrics.mu.Unlock()
}
func ObserveSubscriptionQuotaLedgerMismatch() {
	subscriptionQuotaMetrics.mu.Lock()
	subscriptionQuotaMetrics.ledgerMismatch++
	subscriptionQuotaMetrics.mu.Unlock()
	slog.Error("subscription_quota_ledger_mismatch_total", "action", "card admission held")
}
func ObserveSubscriptionQuotaOverage(amount decimal.Decimal) {
	if !amount.IsPositive() {
		return
	}
	subscriptionQuotaMetrics.mu.Lock()
	subscriptionQuotaMetrics.overage = subscriptionQuotaMetrics.overage.Add(amount)
	subscriptionQuotaMetrics.mu.Unlock()
	slog.Warn("subscription_quota_overage_usd", "delta_usd", amount.StringFixed(8))
}
func observeSubscriptionQuotaPublicationFailure() {
	subscriptionQuotaMetrics.mu.Lock()
	subscriptionQuotaMetrics.missing++
	subscriptionQuotaMetrics.mu.Unlock()
}
func observeSubscriptionQuotaRecoveryRetry() {
	subscriptionQuotaMetrics.mu.Lock()
	subscriptionQuotaMetrics.retries++
	subscriptionQuotaMetrics.mu.Unlock()
}
func (s *SubscriptionQuotaRecoveryService) observeBatch(ctx context.Context, runErr error) {
	subscriptionQuotaMetrics.runs.Add(1)
	if runErr != nil {
		subscriptionQuotaMetrics.failures.Add(1)
	}
	if reader, ok := s.repo.(interface {
		BillingBacklog(context.Context) (SubscriptionQuotaBacklog, error)
	}); ok {
		probe, cancel := context.WithTimeout(ctx, s.itemTimeout)
		defer cancel()
		if backlog, err := reader.BillingBacklog(probe); err == nil {
			subscriptionQuotaMetrics.backlog.Store(&backlog)
		}
	}
	metrics := GetSubscriptionQuotaMetricsSnapshot()
	// Same process snapshot + structured log convention as idempotency metrics.
	slog.Info("subscription quota recovery metrics", "pending_known", metrics.Known, "reconcile_required", metrics.Reconcile, "admitted", metrics.Admitted, "pending_statistics", metrics.PendingLogs, "oldest_known_seconds", metrics.OldestKnownSeconds, "recovery_runs", metrics.RecoveryRuns, "recovery_failures", metrics.RecoveryFailures, "ledger_mismatch_total", metrics.LedgerMismatch, "usage_log_missing_total", metrics.UsageLogMissing, "recovery_retries_total", metrics.RecoveryRetries, "overage_usd", metrics.OverageUSD, "scope_lock_wait_seconds", metrics.ScopeLockWaitSeconds, "scope_lock_wait_count", metrics.ScopeLockWaitCount)
}

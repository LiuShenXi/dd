package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type subscriptionQuotaReadinessTestRepo struct {
	SubscriptionQuotaRepository
	SubscriptionQuotaBillingRepository
	err    error
	checks int
}

type subscriptionQuotaPublicationTestRepo struct {
	SubscriptionQuotaBillingRepository
	acknowledged int
}

func (r *subscriptionQuotaPublicationTestRepo) MarkUsageLogRecorded(context.Context, int64, string) error {
	r.acknowledged++
	return nil
}

type subscriptionQuotaPublicationTestLogs struct {
	UsageLogRepository
	created int
}

func (r *subscriptionQuotaPublicationTestLogs) Create(context.Context, *UsageLog) (bool, error) {
	r.created++
	return true, nil
}

func TestSubscriptionQuotaStatisticsCacheFailureLeavesAcknowledgementPending(t *testing.T) {
	at := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	pricing, _ := json.Marshal(subscriptionQuotaPricingSnapshot{Version: 1, RequestedModel: "synthetic", UpstreamModel: "synthetic", UserMultiplier: 1, PeakMultiplier: 1})
	snapshot := domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestRowID: 1, RequestID: NewSubscriptionQuotaAdmissionRequestID(), UserID: 1, APIKeyID: 1, GroupID: 1, SubscriptionID: 1, TermID: 1, AccountID: 1, AccountType: AccountTypeAPIKey, AdmittedAt: at, PricingAt: at, PricingSnapshot: pricing}
	_, snapshot.PricingSnapshotHash, _ = domain.MarshalSubscriptionQuotaPricingEnvelope(snapshot)
	_, known, err := PrepareSubscriptionQuotaKnownUsage(&snapshot, &SubscriptionQuotaCalculatedCost{Model: "synthetic", Cost: &CostBreakdown{ActualCost: 1, TotalCost: 1}}, nil)
	require.NoError(t, err)
	repo := &subscriptionQuotaPublicationTestRepo{}
	logs := &subscriptionQuotaPublicationTestLogs{}
	worker := NewSubscriptionQuotaRecoveryService(repo, nil, logs, &config.Config{})
	failed := true
	attempts := 0
	worker.postSettlement = func(context.Context, domain.SubscriptionQuotaKnownUsage) error {
		attempts++
		if failed {
			return errors.New("synthetic Redis unavailable")
		}
		return nil
	}
	require.ErrorContains(t, worker.writeUsageLog(context.Background(), known), "Redis unavailable")
	require.Equal(t, 0, repo.acknowledged)
	require.Equal(t, 1, attempts)
	failed = false
	require.NoError(t, worker.writeUsageLog(context.Background(), known))
	require.Equal(t, 1, repo.acknowledged)
	require.Equal(t, 2, attempts)
}

func (r *subscriptionQuotaReadinessTestRepo) CheckBillingReadiness(context.Context) error {
	r.checks++
	return r.err
}

func TestSubscriptionQuotaRuntimeStartupCapabilities(t *testing.T) {
	cfg := &config.Config{SubscriptionQuota: config.SubscriptionQuotaConfig{AdmissionEnabled: true}}
	_, err := ProvideSubscriptionQuotaService(nil, cfg)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
	repo := &subscriptionQuotaReadinessTestRepo{err: errors.New("missing 250")}
	_, err = ProvideSubscriptionQuotaService(repo, cfg)
	require.ErrorContains(t, err, "missing 250")
	require.Equal(t, 1, repo.checks)
	repo.err = nil
	svc, err := ProvideSubscriptionQuotaService(repo, cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Equal(t, 2, repo.checks)
	// R1 still recognizes and can inspect term data without granting admission.
	_, err = ProvideSubscriptionQuotaService(nil, &config.Config{})
	require.NoError(t, err)
	require.NoError(t, subscriptionQuotaCodecReadiness())
}

func TestSubscriptionQuotaRecoveryBackoffBounded(t *testing.T) {
	for attempts, expected := range map[int]time.Duration{0: 5 * time.Second, 1: 5 * time.Second, 2: 10 * time.Second, 6: 160 * time.Second, 7: 300 * time.Second, 1000000: 300 * time.Second} {
		require.Equal(t, expected, subscriptionQuotaRecoveryDelay(attempts))
	}
}

func TestSubscriptionQuotaRecordUsageMissingSnapshotCannotFallback(t *testing.T) {
	gateway := &GatewayService{}
	input := &RecordUsageInput{APIKey: &APIKey{Group: &Group{SubscriptionQuotaMode: "term"}}}
	require.ErrorIs(t, gateway.RecordUsage(context.Background(), input), ErrSubscriptionQuotaBillingUnavailable)
}

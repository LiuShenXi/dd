package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

func ProvideSubscriptionQuotaService(repo SubscriptionQuotaRepository, cfg *config.Config) (*SubscriptionQuotaService, error) {
	svc := NewSubscriptionQuotaService(repo, cfg)
	if cfg != nil && cfg.SubscriptionQuota.AdmissionEnabled {
		if _, err := svc.billingRepository(); err != nil {
			return nil, err
		}
		ready, ok := repo.(interface{ CheckBillingReadiness(context.Context) error })
		if !ok {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ready.CheckBillingReadiness(ctx); err != nil {
			return nil, fmt.Errorf("term admission capability check: %w", err)
		}
		if err := subscriptionQuotaCodecReadiness(); err != nil {
			return nil, ErrSubscriptionQuotaBillingUnavailable.WithCause(err)
		}
	}
	return svc, nil
}

func subscriptionQuotaCodecReadiness() error {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pricing, _ := json.Marshal(subscriptionQuotaPricingSnapshot{Version: 1, RequestedModel: "capability", UpstreamModel: "capability", UserMultiplier: 1, PeakMultiplier: 1})
	snapshot := domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestRowID: 1, RequestID: NewSubscriptionQuotaAdmissionRequestID(), UserID: 1, APIKeyID: 1, GroupID: 1, SubscriptionID: 1, TermID: 1, AccountID: 1, AccountType: AccountTypeAPIKey, AdmittedAt: at, PricingAt: at, PricingSnapshot: pricing}
	_, snapshot.PricingSnapshotHash, _ = domain.MarshalSubscriptionQuotaPricingEnvelope(snapshot)
	_, known, err := PrepareSubscriptionQuotaKnownUsage(&snapshot, &SubscriptionQuotaCalculatedCost{Model: "capability", Cost: &CostBreakdown{}}, nil)
	if err != nil {
		return err
	}
	command, err := DecodeSubscriptionQuotaUsageBillingReceipt(known.BillingPayload)
	if err != nil {
		return err
	}
	return ValidateSubscriptionQuotaUsageLogPayload(known.UsageLogPayload, command)
}

func ProvideSubscriptionQuotaRecoveryService(quota *SubscriptionQuotaService, billing UsageBillingRepository, logs UsageLogRepository, cache *BillingCacheService, cfg *config.Config) (*SubscriptionQuotaRecoveryService, error) {
	repo, err := quota.billingRepository()
	if err != nil {
		return nil, err
	}
	if billing == nil || logs == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if cfg != nil && cfg.SubscriptionQuota.AdmissionEnabled {
		capability, ok := billing.(interface{ SubscriptionQuotaBillingReady() bool })
		if !ok || !capability.SubscriptionQuotaBillingReady() {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
	}
	svc := NewSubscriptionQuotaRecoveryService(repo, billing, logs, cfg)
	svc.postSettlement = func(ctx context.Context, known domain.SubscriptionQuotaKnownUsage) error {
		return cache.InvalidateAPIKeyRateLimit(ctx, known.Snapshot.APIKeyID)
	}
	svc.Start()
	return svc, nil
}

package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func (s *SubscriptionQuotaService) billingRepository() (SubscriptionQuotaBillingRepository, error) {
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	repo, ok := s.repo.(SubscriptionQuotaBillingRepository)
	if !ok || repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	return repo, nil
}
func (s *SubscriptionQuotaService) Admit(ctx context.Context, in domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	repo, err := s.billingRepository()
	if err != nil {
		return nil, err
	}
	enabled := false
	if s.cfg != nil && s.cfg.RunMode != config.RunModeSimple && s.cfg.SubscriptionQuota.AdmissionEnabled {
		for _, id := range s.cfg.SubscriptionQuota.EnabledGroupIDs {
			if id == in.GroupID {
				enabled = true
				break
			}
		}
	}
	if !enabled {
		ObserveSubscriptionQuotaAdmission(ErrSubscriptionQuotaBillingUnavailable)
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	in.DefaultClaudeConcurrency = s.cfg.Default.ClaudeSubscriptionConcurrency
	snapshot, err := repo.AdmitRequest(ctx, in)
	if err != nil && infraerrors.Code(err) >= 500 && !strings.HasPrefix(infraerrors.Reason(err), "TERM_") {
		return nil, ErrSubscriptionQuotaBillingUnavailable.WithCause(err)
	}
	return snapshot, err
}
func (s *SubscriptionQuotaService) Heartbeat(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot) error {
	repo, err := s.billingRepository()
	if err != nil {
		return err
	}
	return repo.Heartbeat(ctx, snapshot)
}
func (s *SubscriptionQuotaService) MarkUsageLogRecorded(ctx context.Context, rowID int64, fingerprint string) error {
	repo, err := s.billingRepository()
	if err != nil {
		return err
	}
	return repo.MarkUsageLogRecorded(ctx, rowID, fingerprint)
}
func (s *SubscriptionQuotaService) PersistKnownUsage(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost decimal.Decimal, billing, usage json.RawMessage) error {
	if snapshot == nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	command, err := DecodeSubscriptionQuotaUsageBillingReceipt(billing)
	if err != nil {
		return err
	}
	if !cost.Equal(command.SubscriptionTermCost) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	repo, err := s.billingRepository()
	if err != nil {
		return err
	}
	err = repo.PersistKnownUsage(ctx, domain.SubscriptionQuotaKnownUsage{Snapshot: snapshot.Clone(), ActualCostUSD: cost.StringFixed(8), ReceiptVersion: SubscriptionQuotaUsageBillingReceiptVersion, RequestFingerprint: command.RequestFingerprint, BillingPayload: append(json.RawMessage(nil), billing...), UsageLogPayload: append(json.RawMessage(nil), usage...)})
	if err == nil {
		subscriptionQuotaMetrics.known.Add(1)
	}
	return err
}
func (s *SubscriptionQuotaService) MarkNoCharge(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, evidence domain.SubscriptionQuotaNoChargeEvidence) error {
	repo, err := s.billingRepository()
	if err != nil {
		return err
	}
	return repo.MarkNoCharge(ctx, snapshot, evidence)
}
func (s *SubscriptionQuotaService) MarkReconcileRequired(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, reason string) error {
	repo, err := s.billingRepository()
	if err != nil {
		return err
	}
	err = repo.MarkReconcileRequired(ctx, snapshot, reason)
	if err == nil {
		subscriptionQuotaMetrics.unknown.Add(1)
	}
	return err
}

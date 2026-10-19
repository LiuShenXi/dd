package service

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
)

// PrepareSubscriptionQuotaKnownUsage is shared by live usage and audited
// verification. Optional log data supplies display metadata, never ownership or
// financial inputs. Every monetary input has already used the frozen rules.
func PrepareSubscriptionQuotaKnownUsage(snapshot *domain.SubscriptionQuotaBillingSnapshot, calculated *SubscriptionQuotaCalculatedCost, display *UsageLog) (*UsageBillingCommand, domain.SubscriptionQuotaKnownUsage, error) {
	var known domain.SubscriptionQuotaKnownUsage
	if snapshot == nil || calculated == nil || calculated.Cost == nil || calculated.Model == "" {
		return nil, known, ErrSubscriptionQuotaReceiptConflict
	}
	cost := calculated.Cost
	for _, value := range []float64{cost.ActualCost, cost.InputCost, cost.OutputCost, cost.CacheCreationCost, cost.CacheReadCost, cost.TotalCost, calculated.AccountQuotaCost, calculated.AccountRateMultiplier} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, known, ErrSubscriptionQuotaReceiptConflict
		}
	}
	money := func(value float64) decimal.Decimal {
		return decimal.NewFromFloat(value).Round(UsageBillingMonetaryScale)
	}
	actual := money(cost.ActualCost)
	var frozen subscriptionQuotaPricingSnapshot
	if err := json.Unmarshal(snapshot.PricingSnapshot, &frozen); err != nil {
		return nil, known, ErrSubscriptionQuotaReceiptConflict
	}
	usage := calculated.NormalizedUsage
	s := snapshot.Clone()
	counters := &domain.SubscriptionQuotaCounterCosts{}
	if s.KeyQuotaTracked {
		counters.APIKeyQuotaCost = actual
	}
	if s.KeyRateLimitTracked {
		counters.APIKeyRateLimitCost = actual
	}
	if s.AccountQuotaTracked {
		counters.AccountQuotaCost = money(calculated.AccountQuotaCost)
	}
	cmd := &UsageBillingCommand{RequestID: s.RequestID, APIKeyID: s.APIKeyID, UserID: s.UserID, AccountID: s.AccountID, AccountType: s.AccountType, Model: calculated.Model, ServiceTier: calculated.ServiceTier, ReasoningEffort: frozen.ReasoningEffort, BillingType: BillingTypeSubscription, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheCreationTokens: usage.CacheCreationInputTokens, CacheReadTokens: usage.CacheReadInputTokens, SubscriptionTermSnapshot: &s, SubscriptionTermCost: actual, SubscriptionTermCounters: counters}
	if err := cmd.Validate(); err != nil {
		return nil, known, err
	}
	cmd.Normalize()
	log := &UsageLog{}
	if display != nil {
		*log = *display
	}
	log.RequestID, log.UserID, log.APIKeyID, log.AccountID = s.RequestID, s.UserID, s.APIKeyID, s.AccountID
	log.GroupID, log.SubscriptionID, log.SubscriptionTermID, log.SubscriptionQuotaRequestID = &s.GroupID, &s.SubscriptionID, &s.TermID, &s.RequestRowID
	log.Model, log.RequestedModel = calculated.Model, frozen.RequestedModel
	log.UpstreamModel = &frozen.UpstreamModel
	log.InputTokens, log.OutputTokens, log.CacheCreationTokens, log.CacheReadTokens = usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens
	log.CacheCreation5mTokens, log.CacheCreation1hTokens = usage.CacheCreation5mTokens, usage.CacheCreation1hTokens
	log.InputCost, log.OutputCost, log.CacheCreationCost, log.CacheReadCost, log.TotalCost, log.ActualCost = cost.InputCost, cost.OutputCost, cost.CacheCreationCost, cost.CacheReadCost, cost.TotalCost, cost.ActualCost
	log.RateMultiplier = frozen.UserMultiplier * frozen.PeakMultiplier
	log.AccountRateMultiplier = &calculated.AccountRateMultiplier
	log.AccountStatsCost = calculated.AccountStatsCost
	log.SubscriptionQuotaAccountStatsCost = nil
	if calculated.AccountStatsCost != nil {
		value := *calculated.AccountStatsCost
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, known, ErrSubscriptionQuotaReceiptConflict
		}
		exact := money(value)
		log.SubscriptionQuotaAccountStatsCost = &exact
	}
	log.LongContextBillingApplied = cost.LongContextBillingApplied
	log.BillingType = BillingTypeSubscription
	log.CreatedAt = s.AdmittedAt
	mode := cost.BillingMode
	log.BillingMode = &mode
	log.ServiceTier, log.ReasoningEffort = &cmd.ServiceTier, &cmd.ReasoningEffort
	payload, err := MarshalSubscriptionQuotaUsageBillingReceipt(cmd)
	if err != nil {
		return nil, known, err
	}
	usagePayload, err := MarshalSubscriptionQuotaUsageLogPayloadForCommand(log, cmd)
	if err != nil {
		return nil, known, err
	}
	known = domain.SubscriptionQuotaKnownUsage{Snapshot: s.Clone(), ActualCostUSD: actual.StringFixed(8), ReceiptVersion: SubscriptionQuotaUsageBillingReceiptVersion, RequestFingerprint: cmd.RequestFingerprint, BillingPayload: payload, UsageLogPayload: usagePayload}
	return cmd, known, nil
}

func (s *GatewayService) recordSubscriptionQuotaUsage(ctx context.Context, input *RecordUsageInput, snapshot *domain.SubscriptionQuotaBillingSnapshot) error {
	if s == nil || s.billingCacheService == nil || s.billingCacheService.SubscriptionQuotaGatewayBilling() == nil || s.usageBillingRepo == nil || s.usageLogRepo == nil || snapshot == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	port := s.billingCacheService.SubscriptionQuotaGatewayBilling()
	// Mandatory persistence outlives a disconnected HTTP context, but remains bounded.
	billingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reconcile := func(reason string, err error) error {
		_ = port.MarkReconcileRequired(billingCtx, snapshot, reason)
		return err
	}
	if input.Result == nil || !input.Result.UsageObserved || !input.Result.UsageFinal {
		return reconcile("usage_incomplete", ErrSubscriptionQuotaUsageUnknown)
	}
	if input.APIKey == nil || input.User == nil || input.Account == nil || input.APIKey.ID != snapshot.APIKeyID || input.User.ID != snapshot.UserID || input.Account.ID != snapshot.AccountID {
		return reconcile("receipt_conflict", ErrSubscriptionQuotaReceiptConflict)
	}
	calculated, err := s.CalculateSubscriptionQuotaCost(snapshot.PricingSnapshot, input.Result, snapshot.PricingAt, input.ForceCacheBilling)
	if err != nil {
		return reconcile("pricing_failed", err)
	}
	result := input.Result
	duration := int(result.Duration.Milliseconds())
	display := &UsageLog{Stream: result.Stream, DurationMs: &duration, FirstTokenMs: result.FirstTokenMs}
	if input.ChannelID > 0 {
		display.ChannelID = &input.ChannelID
	}
	if input.ModelMappingChain != "" {
		display.ModelMappingChain = &input.ModelMappingChain
	}
	if input.InboundEndpoint != "" {
		display.InboundEndpoint = &input.InboundEndpoint
	}
	if input.UpstreamEndpoint != "" {
		display.UpstreamEndpoint = &input.UpstreamEndpoint
	}
	if result.UpstreamResponseModel != "" {
		display.UpstreamResponseModel = &result.UpstreamResponseModel
	}
	cmd, known, err := PrepareSubscriptionQuotaKnownUsage(snapshot, calculated, display)
	if err != nil {
		return reconcile("receipt_conflict", err)
	}
	if err = port.PersistKnownUsage(billingCtx, snapshot, cmd.SubscriptionTermCost, known.BillingPayload, known.UsageLogPayload); err != nil {
		return err
	}
	if _, err = s.usageBillingRepo.Apply(billingCtx, cmd); err != nil {
		return err
	}
	// Publication uses the durable payload and a separate acknowledgement. A log
	// failure leaves the settled money event available to the recovery worker.
	log, err := DecodeSubscriptionQuotaUsageLogPayload(known.UsageLogPayload)
	if err != nil {
		return err
	}
	if _, err = s.usageLogRepo.Create(billingCtx, log); err != nil {
		observeSubscriptionQuotaPublicationFailure()
		return err
	}
	if err = s.billingCacheService.InvalidateAPIKeyRateLimit(billingCtx, snapshot.APIKeyID); err != nil {
		return err
	}
	if ack, ok := port.(interface {
		MarkUsageLogRecorded(context.Context, int64, string) error
	}); ok {
		return ack.MarkUsageLogRecorded(billingCtx, snapshot.RequestRowID, cmd.RequestFingerprint)
	}
	return nil
}

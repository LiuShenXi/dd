package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const subscriptionQuotaPricingVersion = 1

type SubscriptionQuotaPricingInput struct {
	APIKey            *APIKey
	Account           *Account
	RequestedModel    string
	UpstreamModel     string
	ChannelFields     ChannelUsageFields
	ServiceTier       string
	ReasoningEffort   string
	ForceCacheBilling bool
}

type SubscriptionQuotaCalculatedCost struct {
	Cost                  *CostBreakdown
	Model                 string
	NormalizedUsage       ClaudeUsage
	AccountRateMultiplier float64
	AccountQuotaCost      float64
	AccountStatsCost      *float64
	ServiceTier           string
}

// The two private ResolvedPricing fields are part of the billing rules. A
// direct json.Marshal(ResolvedPricing) would silently omit both.
type subscriptionQuotaFrozenPrice struct {
	Mode                   BillingMode          `json:"mode"`
	Base                   *ModelPricing        `json:"base"`
	Intervals              []PricingInterval    `json:"intervals"`
	RequestTiers           []PricingInterval    `json:"request_tiers"`
	DefaultPerRequestPrice float64              `json:"default_per_request_price"`
	Source                 string               `json:"source"`
	SupportsCacheBreakdown bool                 `json:"supports_cache_breakdown"`
	Channel                *ChannelModelPricing `json:"channel"`
	LongContext            bool                 `json:"long_context"`
}

func freezeSubscriptionQuotaPrice(p *ResolvedPricing) (subscriptionQuotaFrozenPrice, error) {
	if p == nil || (p.Mode != BillingModeToken && p.Mode != BillingModePerRequest) || (p.Mode == BillingModeToken && p.BasePricing == nil && len(p.Intervals) == 0) {
		return subscriptionQuotaFrozenPrice{}, ErrSubscriptionQuotaBillingUnavailable
	}
	f := subscriptionQuotaFrozenPrice{Mode: p.Mode, Base: p.BasePricing, Intervals: p.Intervals, RequestTiers: p.RequestTiers, DefaultPerRequestPrice: p.DefaultPerRequestPrice, Source: p.Source, SupportsCacheBreakdown: p.SupportsCacheBreakdown, Channel: p.channelPricing, LongContext: p.longContextPricingEnabled}
	// Round-trip now: detach every nested pointer, including interval prices.
	b, err := json.Marshal(f)
	if err != nil {
		return f, fmt.Errorf("freeze term pricing: %w", err)
	}
	var copy subscriptionQuotaFrozenPrice
	err = json.Unmarshal(b, &copy)
	return copy, err
}

func (p subscriptionQuotaFrozenPrice) resolved() *ResolvedPricing {
	return &ResolvedPricing{Mode: p.Mode, BasePricing: p.Base, Intervals: p.Intervals, RequestTiers: p.RequestTiers, DefaultPerRequestPrice: p.DefaultPerRequestPrice, Source: p.Source, SupportsCacheBreakdown: p.SupportsCacheBreakdown, channelPricing: p.Channel, longContextPricingEnabled: p.LongContext}
}

func (p subscriptionQuotaFrozenPrice) channelPriced() bool {
	return p.Source == PricingSourceGroup || p.Source == PricingSourceChannel
}

func (s *GatewayService) resolveSubscriptionQuotaPrice(ctx context.Context, resolver *ModelPricingResolver, group *Group, model string) *ResolvedPricing {
	p := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &group.ID, Group: group})
	if p != nil && p.Mode == BillingModeToken && p.channelPricing != nil {
		ch := p.channelPricing
		if _, err := s.billingService.GetModelPricing(model); err != nil && ch.InputPrice == nil && ch.OutputPrice == nil && ch.CacheWritePrice == nil && ch.CacheWrite1hPrice == nil && ch.CacheReadPrice == nil {
			copy := *p
			copy.BasePricing = nil
			p = &copy
		}
	}
	return p
}

type subscriptionQuotaPricingSnapshot struct {
	Version               int                                     `json:"version"`
	PricingAt             time.Time                               `json:"pricing_at"`
	RequestedModel        string                                  `json:"requested_model"`
	UpstreamModel         string                                  `json:"upstream_model"`
	BaselineModel         string                                  `json:"baseline_model"`
	BillingModelSource    string                                  `json:"billing_model_source"`
	Baseline              subscriptionQuotaFrozenPrice            `json:"baseline"`
	ResponseCandidates    map[string]subscriptionQuotaFrozenPrice `json:"response_candidates"`
	UserMultiplier        float64                                 `json:"user_multiplier"`
	PeakMultiplier        float64                                 `json:"peak_multiplier"`
	PeakTimezone          string                                  `json:"peak_timezone"`
	PeakRules             subscriptionQuotaPeakRules              `json:"peak_rules"`
	ServiceTier           string                                  `json:"service_tier"`
	ReasoningEffort       string                                  `json:"reasoning_effort"`
	CacheTTLTarget        string                                  `json:"cache_ttl_target"`
	ForceCacheBilling     bool                                    `json:"force_cache_billing"`
	AccountRateMultiplier float64                                 `json:"account_rate_multiplier"`
	AccountStats          *subscriptionQuotaFrozenAccountStats    `json:"account_stats,omitempty"`
}

type subscriptionQuotaPeakRules struct {
	Enabled    bool    `json:"enabled"`
	Start      string  `json:"start"`
	End        string  `json:"end"`
	Multiplier float64 `json:"multiplier"`
}

// PrepareSubscriptionQuotaPricing reads mutable price sources exactly once.
// Its returned factory captures only detached values and an immutable timezone;
// the repository invokes it with the final database admission clock.
func (s *GatewayService) PrepareSubscriptionQuotaPricing(ctx context.Context, in SubscriptionQuotaPricingInput) (func(time.Time) (json.RawMessage, error), error) {
	if s == nil || s.billingService == nil || in.APIKey == nil || in.APIKey.Group == nil || in.Account == nil || !in.APIKey.Group.IsTermQuota() || in.Account.Platform != PlatformAnthropic {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	apiKey := in.APIKey
	group := *apiKey.Group
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	concrete := forwardResultBillingModel(in.RequestedModel, in.UpstreamModel)
	model := concrete
	switch in.ChannelFields.BillingModelSource {
	case BillingModelSourceRequested:
		if in.ChannelFields.OriginalModel != "" {
			model = in.ChannelFields.OriginalModel
		}
	case BillingModelSourceChannelMapped:
		if in.ChannelFields.ChannelMappedModel != "" {
			model = in.ChannelFields.ChannelMappedModel
		}
	}
	var baseline subscriptionQuotaFrozenPrice
	var resolvedModel string
	for _, candidate := range []string{model, in.UpstreamModel, in.RequestedModel} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		p := s.resolveSubscriptionQuotaPrice(ctx, resolver, &group, candidate)
		f, err := freezeSubscriptionQuotaPrice(p)
		if err == nil {
			baseline = f
			resolvedModel = candidate
			break
		}
	}
	if resolvedModel == "" {
		return nil, fmt.Errorf("term baseline pricing unavailable: %w", ErrSubscriptionQuotaBillingUnavailable)
	}
	base := s.ResolveUserGroupRateMultiplier(ctx, apiKey.UserID, group.ID, group.RateMultiplier)
	if math.IsNaN(base) || math.IsInf(base, 0) || base < 0 {
		return nil, fmt.Errorf("invalid term multiplier: %w", ErrSubscriptionQuotaBillingUnavailable)
	}
	ttl, _ := s.resolveCacheTTLUsageOverrideTarget(ctx, in.Account)
	accountRate := in.Account.BillingRateMultiplier()
	if math.IsNaN(accountRate) || math.IsInf(accountRate, 0) || accountRate < 0 {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	frozen := subscriptionQuotaPricingSnapshot{Version: subscriptionQuotaPricingVersion, RequestedModel: in.RequestedModel, UpstreamModel: in.UpstreamModel, BaselineModel: resolvedModel, BillingModelSource: in.ChannelFields.BillingModelSource, Baseline: baseline, ResponseCandidates: map[string]subscriptionQuotaFrozenPrice{}, UserMultiplier: base, ServiceTier: in.ServiceTier, ReasoningEffort: in.ReasoningEffort, CacheTTLTarget: ttl, ForceCacheBilling: in.ForceCacheBilling}
	frozen.AccountRateMultiplier = accountRate
	frozen.PeakRules = subscriptionQuotaPeakRules{Enabled: group.PeakRateEnabled, Start: group.PeakStart, End: group.PeakEnd, Multiplier: group.PeakRateMultiplier}
	cs := s.channelService
	if cs == nil {
		cs = resolver.channelService
	}
	var channel *Channel
	if cs != nil {
		channel, _ = cs.GetChannelForGroup(ctx, group.ID)
	}
	if channel != nil {
		var err error
		frozen.AccountStats, err = s.freezeSubscriptionQuotaAccountStats(ctx, channel, cs.GetGroupPlatform(ctx, group.ID), in.Account.ID, group.ID, firstNonEmpty(in.UpstreamModel, in.RequestedModel))
		if err != nil {
			return nil, err
		}
	}
	if in.ChannelFields.BillingModelSource == BillingModelSourceResponse {
		models := append([]string{resolvedModel, in.RequestedModel, in.UpstreamModel}, s.billingService.ListSupportedModels()...)
		if s.billingService.pricingService != nil {
			models = append(models, s.billingService.pricingService.ListModelNamesByProvider("anthropic")...)
		}
		for _, entry := range group.ModelPricing {
			models = append(models, entry.Models...)
		}
		if channel != nil {
			for _, entry := range channel.ModelPricing {
				models = append(models, entry.Models...)
			}
		}
		for from, to := range in.Account.GetModelMapping() {
			models = append(models, from, to)
		}
		seen := map[string]bool{}
		for _, candidate := range models {
			key := strings.ToLower(strings.TrimSpace(candidate))
			if key == "" || seen[key] || strings.ContainsAny(key, "*?") {
				continue
			}
			seen[key] = true
			p := s.resolveSubscriptionQuotaPrice(ctx, resolver, &group, candidate)
			if p == nil || p.Mode != BillingModeToken {
				continue
			}
			if p.Source != PricingSourceGroup && p.Source != PricingSourceChannel && !s.billingService.HasIdentifiedTokenPricing(candidate) {
				continue
			}
			f, err := freezeSubscriptionQuotaPrice(p)
			if err != nil {
				continue
			}
			frozen.ResponseCandidates[key] = f
		}
	}
	// Detached group and location preserve the existing peak rule without
	// reading request-time globals when the transaction calls the factory.
	location := timezone.Location()
	frozen.PeakTimezone = location.String()
	return func(at time.Time) (json.RawMessage, error) {
		if at.IsZero() {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
		copy := frozen
		copy.PricingAt = at.UTC()
		copy.PeakMultiplier = group.peakMultiplierAtLocation(at, location)
		if math.IsNaN(copy.PeakMultiplier) || math.IsInf(copy.PeakMultiplier, 0) || copy.PeakMultiplier < 0 {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
		b, err := json.Marshal(copy)
		return json.RawMessage(b), err
	}, nil
}

func validSubscriptionQuotaMetering(usage ClaudeUsage) bool {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheCreationInputTokens < 0 || usage.CacheReadInputTokens < 0 || usage.CacheCreation5mTokens < 0 || usage.CacheCreation1hTokens < 0 || usage.ImageOutputTokens < 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	return usage.InputTokens <= maxInt-usage.CacheCreationInputTokens && usage.InputTokens+usage.CacheCreationInputTokens <= maxInt-usage.CacheReadInputTokens && usage.InputTokens+usage.CacheCreationInputTokens+usage.CacheReadInputTokens <= maxInt-usage.OutputTokens && usage.CacheCreation5mTokens <= maxInt-usage.CacheCreation1hTokens
}

// CalculateSubscriptionQuotaCost never resolves current model/group/account
// settings. Both baseline and optional response prices come from admission.
func (s *GatewayService) CalculateSubscriptionQuotaCost(data json.RawMessage, result *ForwardResult, pricingAt time.Time, forceCacheBilling bool) (*SubscriptionQuotaCalculatedCost, error) {
	var snapshot subscriptionQuotaPricingSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode term pricing: %w", err)
	}
	if snapshot.Version != subscriptionQuotaPricingVersion || snapshot.BaselineModel == "" || !snapshot.PricingAt.Equal(pricingAt) || snapshot.ForceCacheBilling != forceCacheBilling || result == nil || !result.UsageObserved || !result.UsageFinal || !validSubscriptionQuotaMetering(result.Usage) {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	for _, value := range []float64{snapshot.UserMultiplier, snapshot.PeakMultiplier, snapshot.AccountRateMultiplier} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
	}
	if result.ImageCount > 0 || result.AudioUsage != nil || result.SearchCount > 0 {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	usage := result.Usage
	if snapshot.ForceCacheBilling && usage.InputTokens > 0 {
		usage.CacheReadInputTokens += usage.InputTokens
		usage.InputTokens = 0
	}
	if snapshot.CacheTTLTarget != "" {
		applyCacheTTLOverride(&usage, snapshot.CacheTTLTarget)
	}
	if !validSubscriptionQuotaMetering(usage) {
		return nil, ErrSubscriptionQuotaUsageUnknown
	}
	tier := ResolveBillingServiceTier(snapshot.ServiceTier, result.UpstreamResponseServiceTier).Billing
	calculate := func(model string, price subscriptionQuotaFrozenPrice) (*CostBreakdown, error) {
		billing := &BillingService{}
		return billing.CalculateCostUnified(CostInput{Model: model, Tokens: UsageTokens{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheCreationTokens: usage.CacheCreationInputTokens, CacheReadTokens: usage.CacheReadInputTokens, CacheCreation5mTokens: usage.CacheCreation5mTokens, CacheCreation1hTokens: usage.CacheCreation1hTokens, ImageOutputTokens: usage.ImageOutputTokens}, RequestCount: 1, RateMultiplier: snapshot.UserMultiplier * snapshot.PeakMultiplier, PricingAt: snapshot.PricingAt, ServiceTier: tier, ReasoningEffort: snapshot.ReasoningEffort, Resolver: &ModelPricingResolver{}, Resolved: price.resolved()})
	}
	cost, err := calculate(snapshot.BaselineModel, snapshot.Baseline)
	if err != nil {
		return nil, err
	}
	model := snapshot.BaselineModel
	if candidate := responseModelBillingDeclaration(snapshot.BillingModelSource, result.UpstreamResponseModel, result.UpstreamResponseModelConflict, false); candidate != "" {
		if price, ok := snapshot.ResponseCandidates[strings.ToLower(strings.TrimSpace(candidate))]; ok {
			responseCost, err := calculate(candidate, price)
			if err != nil {
				return nil, err
			}
			if responseModelBillingAdoptable(cost, responseCost, snapshot.Baseline.channelPriced(), price.channelPriced()) {
				cost = responseCost
				model = candidate
			}
		}
	}
	if cost == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	for _, amount := range []float64{cost.InputCost, cost.ImageInputCost, cost.OutputCost, cost.ImageOutputCost, cost.CacheCreationCost, cost.CacheReadCost, cost.TotalCost, cost.ActualCost} {
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
	}
	accountCost := cost.TotalCost * snapshot.AccountRateMultiplier
	if math.IsNaN(accountCost) || math.IsInf(accountCost, 0) || accountCost < 0 {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	stats, err := snapshot.AccountStats.calculate(UsageTokens{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheCreationTokens: usage.CacheCreationInputTokens, CacheReadTokens: usage.CacheReadInputTokens, CacheCreation5mTokens: usage.CacheCreation5mTokens, CacheCreation1hTokens: usage.CacheCreation1hTokens}, cost.TotalCost, tier, snapshot.ReasoningEffort, snapshot.PricingAt)
	if err != nil {
		return nil, err
	}
	return &SubscriptionQuotaCalculatedCost{Cost: cost, Model: model, NormalizedUsage: usage, AccountRateMultiplier: snapshot.AccountRateMultiplier, AccountQuotaCost: QuantizeUsageBillingAmount(accountCost), AccountStatsCost: stats, ServiceTier: tier}, nil
}

// Verified usage is provider evidence supplied by the audited resolve workflow.
// It can fill counters, never replace admission identity or mutable price rules.
func (s *GatewayService) CalculateSubscriptionQuotaVerifiedCost(data json.RawMessage, in SubscriptionQuotaVerifiedUsage, pricingAt time.Time) (*SubscriptionQuotaCalculatedCost, error) {
	var snapshot subscriptionQuotaPricingSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if in.RequestedModel != "" && in.RequestedModel != snapshot.RequestedModel || in.UpstreamModel != "" && in.UpstreamModel != snapshot.UpstreamModel || in.SearchCallCount != 0 {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	maxInt := int64(^uint(0) >> 1)
	for _, value := range []int64{in.InputTokens, in.OutputTokens, in.CacheCreation5mTokens, in.CacheCreation1hTokens, in.CacheReadTokens} {
		if value < 0 || value > maxInt {
			return nil, ErrSubscriptionQuotaUsageUnknown
		}
	}
	if in.CacheCreation5mTokens > maxInt-in.CacheCreation1hTokens {
		return nil, ErrSubscriptionQuotaUsageUnknown
	}
	result := &ForwardResult{Model: snapshot.RequestedModel, UpstreamModel: snapshot.UpstreamModel, UpstreamResponseModel: in.ResponseModel, UpstreamResponseServiceTier: in.ServiceTier, UsageObserved: true, UsageFinal: true, UsageEvidenceSource: "audited_verified_usage", Usage: ClaudeUsage{InputTokens: int(in.InputTokens), OutputTokens: int(in.OutputTokens), CacheCreation5mTokens: int(in.CacheCreation5mTokens), CacheCreation1hTokens: int(in.CacheCreation1hTokens), CacheCreationInputTokens: int(in.CacheCreation5mTokens + in.CacheCreation1hTokens), CacheReadInputTokens: int(in.CacheReadTokens)}}
	return s.CalculateSubscriptionQuotaCost(data, result, pricingAt, snapshot.ForceCacheBilling)
}

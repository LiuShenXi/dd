//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func termPricingKey() *APIKey {
	gid := int64(100)
	return &APIKey{ID: 21, UserID: 11, GroupID: &gid, Group: &Group{ID: gid, Platform: PlatformAnthropic, SubscriptionType: "subscription", SubscriptionQuotaMode: domain.SubscriptionQuotaModeTerm, RateMultiplier: 0.8, LongContextPricingEnabled: true}}
}

func TestSubscriptionQuotaPricingIntervalOnly(t *testing.T) {
	model := "private-interval-only-model"
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{model}, BillingMode: BillingModeToken, Intervals: []PricingInterval{{MinTokens: 100, MaxTokens: testPtrInt(1000), InputPrice: testPtrFloat64(0.001), OutputPrice: testPtrFloat64(0.002), CacheWritePrice: testPtrFloat64(0.003), CacheWrite1hPrice: testPtrFloat64(0.006)}}}})
	svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}
	factory, err := svc.PrepareSubscriptionQuotaPricing(context.Background(), SubscriptionQuotaPricingInput{APIKey: termPricingKey(), Account: newAnthropicAPIKeyAccountForTest(), RequestedModel: model, UpstreamModel: model})
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	raw, err := factory(at)
	require.NoError(t, err)
	var frozen subscriptionQuotaPricingSnapshot
	require.NoError(t, json.Unmarshal(raw, &frozen))
	require.Nil(t, frozen.Baseline.Base)
	require.True(t, frozen.Baseline.SupportsCacheBreakdown)
	result := &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: ClaudeUsage{InputTokens: 200, OutputTokens: 10, CacheCreationInputTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 20}}
	got, err := svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
	require.NoError(t, err)
	require.InDelta(t, 0.2+0.02+0.03+0.12, got.Cost.TotalCost, 1e-12)
	require.InDelta(t, (0.2+0.02+0.03+0.12)*0.8, got.Cost.ActualCost, 1e-12)
	for _, tokens := range []int{0, 100, 1001} {
		result.Usage = ClaudeUsage{InputTokens: tokens, OutputTokens: 1}
		_, err = svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
		require.Error(t, err, "no interval covers %d; cannot invent a zero base", tokens)
	}
	result.Usage = ClaudeUsage{InputTokens: 1000, OutputTokens: 1}
	_, err = svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
	require.NoError(t, err, "existing interval convention includes max")
}

func TestSubscriptionQuotaPricingFrozenRulesAndClock(t *testing.T) {
	key := termPricingKey()
	key.Group.PeakRateEnabled = true
	key.Group.PeakStart = "14:00"
	key.Group.PeakEnd = "18:00"
	key.Group.PeakRateMultiplier = 3
	billing := NewBillingService(&config.Config{}, nil)
	svc := &GatewayService{billingService: billing}
	at := time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC)
	factory, err := svc.PrepareSubscriptionQuotaPricing(context.Background(), SubscriptionQuotaPricingInput{APIKey: key, Account: newAnthropicAPIKeyAccountForTest(), RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4"})
	require.NoError(t, err)
	// Mutable inputs may change before final admission invokes the pure factory.
	key.Group.PeakRateMultiplier = 99
	key.Group.RateMultiplier = 99
	raw, err := factory(at)
	require.NoError(t, err)
	result := &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: ClaudeUsage{InputTokens: 250001, OutputTokens: 10, CacheReadInputTokens: 20, CacheCreationInputTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 20}}
	got, err := svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
	require.NoError(t, err)
	var snap subscriptionQuotaPricingSnapshot
	require.NoError(t, json.Unmarshal(raw, &snap))
	require.Equal(t, 3.0, snap.PeakMultiplier)
	require.Equal(t, 0.8, snap.UserMultiplier)
	require.True(t, snap.Baseline.LongContext)
	expected, err := billing.CalculateCostUnified(CostInput{Model: "claude-sonnet-4", Tokens: UsageTokens{InputTokens: 250001, OutputTokens: 10, CacheReadTokens: 20, CacheCreationTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 20}, RateMultiplier: 2.4, PricingAt: at, Resolver: &ModelPricingResolver{}, Resolved: snap.Baseline.resolved()})
	require.NoError(t, err)
	require.InDelta(t, expected.ActualCost, got.Cost.ActualCost, 1e-12)
	billing.fallbackPrices = map[string]*ModelPricing{"claude-sonnet-4": {InputPricePerToken: 99, OutputPricePerToken: 99}}
	later, err := svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
	require.NoError(t, err)
	require.Equal(t, got.Cost, later.Cost)
	_, err = svc.CalculateSubscriptionQuotaCost(raw, result, at.Add(time.Second), false)
	require.Error(t, err)
	result.UsageFinal = false
	_, err = svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
	require.Error(t, err)
}

func TestSubscriptionQuotaPricingPrivateChannelTimeRule(t *testing.T) {
	at := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	resolved := channelTimeResolvedForTest(&ModelPricing{InputPricePerToken: 0.001}, nil)
	price, err := freezeSubscriptionQuotaPrice(resolved)
	require.NoError(t, err)
	resolved.channelPricing.TimePricing.Periods[0].Multiplier = 99
	snapshot := subscriptionQuotaPricingSnapshot{Version: 1, PricingAt: at, BaselineModel: "private-model", Baseline: price, UserMultiplier: 0.8, PeakMultiplier: 1}
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	got, err := (&GatewayService{}).CalculateSubscriptionQuotaCost(raw, &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: ClaudeUsage{InputTokens: 1000}}, at, false)
	require.NoError(t, err)
	require.InDelta(t, 2.0, got.Cost.TotalCost, 1e-12)
	require.InDelta(t, 1.6, got.Cost.ActualCost, 1e-12)
}

func TestSubscriptionQuotaPricingResponseCandidateSafety(t *testing.T) {
	at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	price := func(rate float64) subscriptionQuotaFrozenPrice {
		return subscriptionQuotaFrozenPrice{Mode: BillingModeToken, Base: &ModelPricing{InputPricePerToken: rate}, Source: PricingSourceLiteLLM}
	}
	snap := subscriptionQuotaPricingSnapshot{Version: 1, PricingAt: at, BaselineModel: "baseline", BillingModelSource: BillingModelSourceResponse, Baseline: price(0.002), ResponseCandidates: map[string]subscriptionQuotaFrozenPrice{"cheap": price(0.001), "expensive": price(0.003), "free": price(0)}, UserMultiplier: 1, PeakMultiplier: 1}
	raw, err := json.Marshal(snap)
	require.NoError(t, err)
	for _, tc := range []struct {
		model    string
		conflict bool
		want     string
	}{{"cheap", false, "cheap"}, {"expensive", false, "baseline"}, {"free", false, "baseline"}, {"unfrozen", false, "baseline"}, {"cheap", true, "baseline"}} {
		got, err := (&GatewayService{}).CalculateSubscriptionQuotaCost(raw, &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: ClaudeUsage{InputTokens: 100}, UpstreamResponseModel: tc.model, UpstreamResponseModelConflict: tc.conflict}, at, false)
		require.NoError(t, err)
		require.Equal(t, tc.want, got.Model)
	}
}

func TestSubscriptionQuotaPricingVerifiedScopeAndAccountFreeze(t *testing.T) {
	at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	account := newAnthropicAPIKeyAccountForTest()
	rate := 0.25
	account.RateMultiplier = &rate
	factory, err := svc.PrepareSubscriptionQuotaPricing(context.Background(), SubscriptionQuotaPricingInput{APIKey: termPricingKey(), Account: account, RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4", ForceCacheBilling: true})
	require.NoError(t, err)
	rate = 99
	raw, err := factory(at)
	require.NoError(t, err)
	verified := SubscriptionQuotaVerifiedUsage{RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4", InputTokens: 100, OutputTokens: 10, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4, CacheReadTokens: 5}
	got, err := svc.CalculateSubscriptionQuotaVerifiedCost(raw, verified, at)
	require.NoError(t, err)
	require.Zero(t, got.NormalizedUsage.InputTokens)
	require.Equal(t, 105, got.NormalizedUsage.CacheReadInputTokens)
	require.Equal(t, 7, got.NormalizedUsage.CacheCreationInputTokens)
	require.Equal(t, 0.25, got.AccountRateMultiplier)
	require.Equal(t, QuantizeUsageBillingAmount(got.Cost.TotalCost*0.25), got.AccountQuotaCost)
	for _, mutate := range []func(*SubscriptionQuotaVerifiedUsage){func(in *SubscriptionQuotaVerifiedUsage) { in.RequestedModel = "other" }, func(in *SubscriptionQuotaVerifiedUsage) { in.UpstreamModel = "other" }, func(in *SubscriptionQuotaVerifiedUsage) { in.SearchCallCount = 1 }, func(in *SubscriptionQuotaVerifiedUsage) { in.OutputTokens = -1 }, func(in *SubscriptionQuotaVerifiedUsage) {
		in.CacheCreation5mTokens = 1<<63 - 1
		in.CacheCreation1hTokens = 1
	}} {
		invalid := verified
		mutate(&invalid)
		_, err = svc.CalculateSubscriptionQuotaVerifiedCost(raw, invalid, at)
		require.Error(t, err)
	}
}

func TestSubscriptionQuotaPricingAccountStatsFrozenPrecedence(t *testing.T) {
	for _, mode := range []string{"custom", "customer", "model_default", "no_channel"} {
		t.Run(mode, func(t *testing.T) {
			billing := newTestBillingServiceForResolver()
			cs := &ChannelService{}
			customRate := 0.004
			channel := Channel{ID: 9, Status: StatusActive, GroupIDs: []int64{100}, ApplyPricingToAccountStats: mode == "customer"}
			if mode == "custom" {
				channel.ApplyPricingToAccountStats = true
				channel.AccountStatsPricingRules = []AccountStatsPricingRule{{AccountIDs: []int64{201}, Pricing: []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, InputPrice: &customRate}}}}
			}
			channels := []Channel{channel}
			if mode == "no_channel" {
				channels = nil
			}
			cs.cache.Store(populateChannelCache(channels, map[int64]string{100: PlatformAnthropic}))
			svc := &GatewayService{billingService: billing, channelService: cs, resolver: NewModelPricingResolver(cs, billing)}
			at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
			factory, err := svc.PrepareSubscriptionQuotaPricing(context.Background(), SubscriptionQuotaPricingInput{APIKey: termPricingKey(), Account: newAnthropicAPIKeyAccountForTest(), RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4"})
			require.NoError(t, err)
			raw, err := factory(at)
			require.NoError(t, err)
			usage := ClaudeUsage{InputTokens: 100, OutputTokens: 2}
			result := &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: usage}
			got, err := svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
			require.NoError(t, err)
			expected := resolveAccountStatsCost(context.Background(), cs, billing, 201, 100, "claude-sonnet-4", UsageTokens{InputTokens: 100, OutputTokens: 2}, 1, got.Cost.TotalCost, "", at)
			if expected == nil {
				require.Nil(t, got.AccountStatsCost)
			} else {
				require.NotNil(t, got.AccountStatsCost)
				require.InDelta(t, *expected, *got.AccountStatsCost, 1e-12)
			}
			customRate = 99
			svc.channelService = nil
			svc.billingService = nil
			later, err := svc.CalculateSubscriptionQuotaCost(raw, result, at, false)
			require.NoError(t, err)
			require.Equal(t, got.AccountStatsCost, later.AccountStatsCost)
		})
	}
}

func TestSubscriptionQuotaPricingPrivateChannelResponseCandidate(t *testing.T) {
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, InputPrice: testPtrFloat64(0.002)}, {Platform: PlatformAnthropic, Models: []string{"private-response-id"}, InputPrice: testPtrFloat64(0.001)}})
	svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}
	at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	factory, err := svc.PrepareSubscriptionQuotaPricing(context.Background(), SubscriptionQuotaPricingInput{APIKey: termPricingKey(), Account: newAnthropicAPIKeyAccountForTest(), RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4", ChannelFields: ChannelUsageFields{BillingModelSource: BillingModelSourceResponse}})
	require.NoError(t, err)
	raw, err := factory(at)
	require.NoError(t, err)
	got, err := svc.CalculateSubscriptionQuotaCost(raw, &ForwardResult{UsageObserved: true, UsageFinal: true, Usage: ClaudeUsage{InputTokens: 100}, UpstreamResponseModel: "private-response-id"}, at, false)
	require.NoError(t, err)
	require.Equal(t, "private-response-id", got.Model)
	require.InDelta(t, 0.1, got.Cost.TotalCost, 1e-12)
}

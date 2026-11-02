package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// Freeze the already-selected account's four-level statistics policy. These
// amounts remain separate from the user's allowance and account quota debit.
type subscriptionQuotaFrozenAccountStats struct {
	Model                string                        `json:"model"`
	Custom               *ChannelModelPricing          `json:"custom,omitempty"`
	ApplyCustomerPricing bool                          `json:"apply_customer_pricing"`
	ModelDefault         *subscriptionQuotaFrozenPrice `json:"model_default,omitempty"`
}

func (s *GatewayService) freezeSubscriptionQuotaAccountStats(ctx context.Context, channel *Channel, platform string, accountID, groupID int64, model string) (*subscriptionQuotaFrozenAccountStats, error) {
	if channel == nil || model == "" {
		return nil, nil
	}
	frozen := &subscriptionQuotaFrozenAccountStats{Model: model, ApplyCustomerPricing: channel.ApplyPricingToAccountStats}
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		if pricing := findPricingForModel(rule.Pricing, platform, strings.ToLower(model)); pricing != nil {
			raw, err := json.Marshal(pricing)
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(raw, &frozen.Custom); err != nil {
				return nil, err
			}
			break
		}
	}
	resolved := NewModelPricingResolver(nil, s.billingService).Resolve(ctx, PricingInput{Model: model})
	if price, err := freezeSubscriptionQuotaPrice(resolved); err == nil {
		frozen.ModelDefault = &price
	}
	return frozen, nil
}

func (s *subscriptionQuotaFrozenAccountStats) calculate(tokens UsageTokens, totalCost float64, tier, effort string, pricingAt time.Time) (*float64, error) {
	if s == nil {
		return nil, nil
	}
	var cost *float64
	if s.Custom != nil {
		cost = calculateStatsCost(s.Custom, tokens, 1)
		if cost != nil {
			*cost *= maxReasoningEffortBillingMultiplier(s.Model, effort, nil)
		}
	}
	if cost == nil && s.ApplyCustomerPricing {
		if totalCost > 0 {
			value := totalCost
			cost = &value
		}
		return validateSubscriptionQuotaStatsCost(cost)
	}
	if cost == nil && s.ModelDefault != nil {
		value, err := (&BillingService{}).CalculateCostUnified(CostInput{Model: s.Model, Tokens: tokens, RequestCount: 1, RateMultiplier: 1, ServiceTier: normalizeBillingServiceTier(tier), ReasoningEffort: effort, PricingAt: pricingAt, Resolver: &ModelPricingResolver{}, Resolved: s.ModelDefault.resolved()})
		if err == nil && value != nil && value.TotalCost > 0 {
			cost = &value.TotalCost
		}
	}
	return validateSubscriptionQuotaStatsCost(cost)
}

func validateSubscriptionQuotaStatsCost(cost *float64) (*float64, error) {
	if cost != nil && (math.IsNaN(*cost) || math.IsInf(*cost, 0) || *cost < 0) {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	return cost, nil
}

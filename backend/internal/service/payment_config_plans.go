package service

import (
	"context"
	"entgo.io/ent/dialect"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if req.GroupID != nil && *req.GroupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	QuotaMode          string   `json:"quota_mode"`
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		if !seen[p.GroupID] {
			seen[p.GroupID] = true
			ids = append(ids, p.GroupID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			QuotaMode:          domain.NormalizeSubscriptionQuotaMode(g.SubscriptionQuotaMode),
			Name:               g.Name,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	plans, err := s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return []*dbent.SubscriptionPlan{}, nil
	}
	ids := make([]int64, 0, len(plans))
	for _, plan := range plans {
		ids = append(ids, plan.GroupID)
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*dbent.Group, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	visible := make([]*dbent.SubscriptionPlan, 0, len(plans))
	for _, plan := range plans {
		g := byID[plan.GroupID]
		if g == nil {
			continue
		}
		if domain.NormalizeSubscriptionQuotaMode(g.SubscriptionQuotaMode) != domain.SubscriptionQuotaModeWindows {
			if _, err := validatePlanQuota(g, plan.ValidityDays, plan.ValidityUnit, true, plan.TermQuotaUsd); err != nil || g.Status != "active" || g.DeletedAt != nil {
				continue
			}
		}
		visible = append(visible, plan)
	}
	return visible, nil
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanRequired(req.Name, req.GroupID, req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice); err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	groupQuery := tx.Group.Query().Where(group.IDEQ(req.GroupID))
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		groupQuery.ForShare()
	}
	g, err := groupQuery.Only(ctx)
	if err != nil {
		return nil, err
	}
	quota, err := validatePlanQuota(g, req.ValidityDays, req.ValidityUnit, req.ForSale, req.DefaultQuotaUSD)
	if err != nil {
		return nil, err
	}
	b := tx.SubscriptionPlan.Create().
		SetGroupID(req.GroupID).SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder)
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	if quota != nil {
		b.SetTermQuotaUsd(*quota)
	}
	plan, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	plan.Unwrap()
	return plan, nil
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	planQuery := tx.SubscriptionPlan.Query().Where(subscriptionplan.IDEQ(id))
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		planQuery.ForUpdate()
	}
	current, err := planQuery.Only(ctx)
	if err != nil {
		return nil, err
	}
	groupID, days, unit, sale, quota := current.GroupID, current.ValidityDays, current.ValidityUnit, current.ForSale, current.TermQuotaUsd
	if req.GroupID != nil {
		groupID = *req.GroupID
	}
	if req.ValidityDays != nil {
		days = *req.ValidityDays
	}
	if req.ValidityUnit != nil {
		unit = *req.ValidityUnit
	}
	if req.ForSale != nil {
		sale = *req.ForSale
	}
	if req.DefaultQuotaUSD != nil {
		quota = req.DefaultQuotaUSD
	}
	groupQuery := tx.Group.Query().Where(group.IDEQ(groupID))
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		groupQuery.ForShare()
	}
	g, err := groupQuery.Only(ctx)
	if err != nil {
		return nil, err
	}
	quota, err = validatePlanQuota(g, days, unit, sale, quota)
	if err != nil {
		return nil, err
	}
	u := tx.SubscriptionPlan.UpdateOneID(id)
	if quota != nil {
		u.SetTermQuotaUsd(*quota)
	}
	if req.GroupID != nil {
		u.SetGroupID(*req.GroupID)
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	plan, err := u.Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	plan.Unwrap()
	return plan, nil
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}

// validateTermPlanDuration accepts the UI's canonical unit and existing plans'
// singular spelling; both describe the same fixed 168-hour entitlement.
func validateTermPlanDuration(days int, unit string) error {
	if days != 7 || (unit != "days" && unit != "day") {
		return infraerrors.BadRequest("TERM_PLAN_DURATION_INVALID", "Claude quota plans must have exactly seven days")
	}
	return nil
}

// validatePlanQuota keeps sale price and the exact USD allowance independent.
func validatePlanQuota(g *dbent.Group, days int, unit string, forSale bool, quota *string) (*string, error) {
	var normalized *string
	if quota != nil {
		value, err := domain.NormalizeSubscriptionQuotaMoney(*quota)
		if err != nil {
			return nil, ErrSubscriptionQuotaInvalid
		}
		normalized = &value
	}
	if g == nil {
		return nil, ErrSubscriptionQuotaGroupInvalid
	}
	mode := domain.NormalizeSubscriptionQuotaMode(g.SubscriptionQuotaMode)
	if mode == domain.SubscriptionQuotaModeWindows {
		return normalized, nil
	}
	if mode != domain.SubscriptionQuotaModeTerm || g.Platform != PlatformAnthropic || g.SubscriptionType != SubscriptionTypeSubscription || g.FallbackGroupID != nil || g.FallbackGroupIDOnInvalidRequest != nil {
		return nil, ErrSubscriptionQuotaGroupInvalid
	}
	if err := validateTermPlanDuration(days, unit); err != nil {
		return nil, err
	}
	if forSale && (g.Status != "active" || g.DeletedAt != nil) {
		return nil, ErrSubscriptionQuotaGroupInvalid
	}
	if forSale && normalized == nil {
		return nil, ErrSubscriptionQuotaRequired
	}
	return normalized, nil
}

// PlanDefaultQuotaUSD projects NUMERIC as canonical text for all plan APIs.
func PlanDefaultQuotaUSD(plan *dbent.SubscriptionPlan) *string {
	if plan == nil || plan.TermQuotaUsd == nil {
		return nil
	}
	value, err := domain.NormalizeSubscriptionQuotaMoney(*plan.TermQuotaUsd)
	if err != nil {
		return nil
	}
	return &value
}

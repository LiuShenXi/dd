package service

import (
	"context"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	entgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	entuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionQuotaOrderSnapshot = infraerrors.Conflict("SUBSCRIPTION_QUOTA_ORDER_REVIEW_REQUIRED", "subscription entitlement snapshot is missing or invalid; manual review is required")
	ErrSubscriptionQuotaPaymentLease  = infraerrors.Conflict("SUBSCRIPTION_QUOTA_PAYMENT_LEASE_LOST", "paid subscription fulfillment lease is no longer current")
)

type SubscriptionQuotaPaymentRepository interface {
	FulfillPaidTerm(context.Context, domain.SubscriptionQuotaPaidTermParams) (*domain.SubscriptionQuotaMutationResult, error)
}

func (s *PaymentService) SetSubscriptionQuotaPayment(repo SubscriptionQuotaPaymentRepository, cfg *config.Config) {
	s.subscriptionQuotaPayment = repo
	s.subscriptionQuotaConfig = cfg
}

func (s *PaymentService) quotaOrderIssueAllowed(groupID int64) bool {
	if s == nil || s.subscriptionQuotaPayment == nil || s.subscriptionQuotaConfig == nil || s.subscriptionQuotaConfig.RunMode == config.RunModeSimple || !s.subscriptionQuotaConfig.SubscriptionQuota.IssueEnabled {
		return false
	}
	for _, id := range s.subscriptionQuotaConfig.SubscriptionQuota.EnabledGroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

// PaymentOrderQuotaSnapshot only returns a validated allowlisted DTO. Malformed
// stored objects remain unavailable for display and are rejected by fulfillment.
func PaymentOrderQuotaSnapshot(order *dbent.PaymentOrder) *domain.SubscriptionQuotaOrderSnapshot {
	if order == nil {
		return nil
	}
	snapshot, err := domain.ParseSubscriptionQuotaOrderSnapshot(order.SubscriptionQuotaSnapshot)
	if err != nil {
		return nil
	}
	return snapshot
}

func (s *PaymentService) freezeQuotaOrderPlan(ctx context.Context, tx *dbent.Tx, userID int64, plan *dbent.SubscriptionPlan) (*dbent.SubscriptionPlan, *domain.SubscriptionQuotaOrderSnapshot, error) {
	if plan == nil {
		return nil, nil, nil
	}
	planQuery := tx.SubscriptionPlan.Query().Where(subscriptionplan.IDEQ(plan.ID))
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		planQuery.ForShare()
	}
	current, err := planQuery.Only(ctx)
	if err != nil || !current.ForSale {
		return nil, nil, infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan not found or not for sale")
	}
	// A provider amount was computed from the preflight price. Refuse a racing
	// price edit rather than charge that old amount for a different current price.
	if current.Price != plan.Price {
		return nil, nil, infraerrors.Conflict("PLAN_CHANGED", "plan changed; refresh checkout before purchasing")
	}
	userQuery := tx.User.Query().Where(entuser.IDEQ(userID), entuser.DeletedAtIsNil())
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		userQuery.ForShare()
	}
	user, err := userQuery.Only(ctx)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != "active" {
		return nil, nil, infraerrors.Forbidden("USER_INACTIVE", "user account is disabled")
	}
	groupQuery := tx.Group.Query().Where(entgroup.IDEQ(current.GroupID), entgroup.DeletedAtIsNil())
	if s.entClient.Driver().Dialect() != dialect.SQLite {
		groupQuery.ForShare()
	}
	g, err := groupQuery.Only(ctx)
	if err != nil {
		return nil, nil, err
	}
	if g.Status != "active" || g.SubscriptionType != SubscriptionTypeSubscription {
		return nil, nil, ErrSubscriptionQuotaGroupInvalid
	}
	quota, err := validatePlanQuota(g, current.ValidityDays, current.ValidityUnit, current.ForSale, current.TermQuotaUsd)
	if err != nil {
		return nil, nil, err
	}
	mode := domain.NormalizeSubscriptionQuotaMode(g.SubscriptionQuotaMode)
	if mode == domain.SubscriptionQuotaModeWindows {
		return current, nil, nil
	}
	if !s.quotaOrderIssueAllowed(current.GroupID) {
		return nil, nil, ErrSubscriptionQuotaBillingUnavailable
	}
	concurrency := s.subscriptionQuotaConfig.Default.ClaudeSubscriptionConcurrency
	if user.ClaudeSubscriptionConcurrency != nil {
		concurrency = *user.ClaudeSubscriptionConcurrency
	}
	if !ValidClaudeSubscriptionConcurrency(concurrency) {
		return nil, nil, ErrTermConcurrencyNotConfigured
	}
	snapshot := &domain.SubscriptionQuotaOrderSnapshot{SchemaVersion: 1, Mode: domain.SubscriptionQuotaModeTerm, GroupID: g.ID, Platform: g.Platform, DurationSeconds: int64(domain.SubscriptionQuotaDuration / time.Second), QuotaUSD: *quota, SourcePlanID: current.ID, PlanName: current.Name, ProductName: current.ProductName, GroupName: g.Name}
	if err = snapshot.Validate(); err != nil {
		return nil, nil, ErrSubscriptionQuotaOrderSnapshot
	}
	return current, snapshot, nil
}

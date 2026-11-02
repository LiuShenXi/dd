package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaModeValidationAndNoBillingFallback(t *testing.T) {
	valid := Group{ID: 6, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: domain.SubscriptionQuotaModeTerm}
	for _, mode := range []string{"", "windows"} {
		require.NoError(t, ValidateSubscriptionQuotaMode(&Group{SubscriptionQuotaMode: mode}))
	}
	require.NoError(t, ValidateSubscriptionQuotaMode(&valid))
	for _, mutate := range []func(*Group){
		func(g *Group) { g.Platform = PlatformOpenAI },
		func(g *Group) { g.SubscriptionType = SubscriptionTypeStandard },
		func(g *Group) { g.SubscriptionQuotaMode = "unknown" },
		func(g *Group) { id := int64(2); g.FallbackGroupID = &id },
		func(g *Group) { id := int64(2); g.FallbackGroupIDOnInvalidRequest = &id },
	} {
		group := valid
		mutate(&group)
		require.ErrorIs(t, ValidateSubscriptionQuotaMode(&group), ErrSubscriptionQuotaGroupInvalid)
	}
	for _, runMode := range []string{config.RunModeStandard, config.RunModeSimple} {
		// Deliberately no balance cache, subscription service or quota repository.
		billing := &BillingCacheService{cfg: &config.Config{RunMode: runMode}}
		require.ErrorIs(t, billing.CheckBillingEligibility(context.Background(), &User{Balance: 1000}, nil, &valid, nil, PlatformAnthropic), ErrSubscriptionQuotaBillingUnavailable)
	}
}

type termQuotaGuardSubscriptionRepo struct {
	userSubRepoNoop
	sub *UserSubscription
}

func (r *termQuotaGuardSubscriptionRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	return r.sub, nil
}
func (r *termQuotaGuardSubscriptionRepo) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	return r.sub, nil
}

func TestSubscriptionQuotaLegacyWritesRejectBeforeParentLock(t *testing.T) {
	group := &Group{ID: 6, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}
	sub := &UserSubscription{ID: 1, UserID: 7, GroupID: 6, Group: group, ExpiresAt: time.Now().Add(time.Hour), WeeklyUsageUSD: 12}
	svc := &SubscriptionService{groupRepo: &subscriptionGroupRepoStub{group: group}, userSubRepo: &termQuotaGuardSubscriptionRepo{sub: sub}}
	for name, action := range map[string]func() error{
		"assign": func() error {
			_, _, e := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{UserID: 7, GroupID: 6})
			return e
		},
		"bulk assign": func() error {
			_, _, e := svc.assignSubscriptionWithReuse(context.Background(), &AssignSubscriptionInput{UserID: 7, GroupID: 6})
			return e
		},
		"extend":      func() error { _, e := svc.ExtendSubscription(context.Background(), 1, 7); return e },
		"reset":       func() error { _, e := svc.AdminResetQuota(context.Background(), 1, true, true, true); return e },
		"revoke":      func() error { return svc.RevokeSubscription(context.Background(), 1) },
		"restore":     func() error { _, e := svc.RestoreSubscription(context.Background(), 1); return e },
		"activate":    func() error { return svc.CheckAndActivateWindow(context.Background(), sub) },
		"maintenance": func() error { return svc.CheckAndResetWindows(context.Background(), sub) },
	} {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, action(), ErrLegacySubscriptionOperationUnsupported) })
	}
	require.Equal(t, 12.0, sub.WeeklyUsageUSD)
}

func TestSubscriptionQuotaRefundSnapshotsRejectEveryLegacyPayload(t *testing.T) {
	svc := &PaymentService{}
	for _, orderType := range []string{payment.OrderTypeSubscription, payment.OrderTypeBalance} {
		require.ErrorIs(t, svc.rejectLegacyQuotaOrder(context.Background(), &dbent.PaymentOrder{OrderType: orderType, SubscriptionQuotaSnapshot: map[string]any{"quota_mode": "term"}}), ErrLegacySubscriptionOperationUnsupported)
	}
	require.NoError(t, svc.rejectLegacyQuotaOrder(context.Background(), &dbent.PaymentOrder{OrderType: payment.OrderTypeBalance}))
}

func TestSubscriptionQuotaAuthSnapshotRetainsModeAndInvalidatesOld(t *testing.T) {
	svc := &APIKeyService{}
	groupID := int64(6)
	key := &APIKey{ID: 1, UserID: 7, GroupID: &groupID, User: &User{ID: 7}, Group: &Group{ID: 6, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}}
	entry := &APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)}
	for i := 0; i < 2; i++ {
		got, used, err := svc.applyAuthCacheEntry("synthetic", entry)
		require.NoError(t, err)
		require.True(t, used)
		require.True(t, got.Group.IsTermQuota())
		got.Group.SubscriptionQuotaMode = "windows"
	}
	entry.Snapshot.Version--
	_, used, err := svc.applyAuthCacheEntry("synthetic", entry)
	require.NoError(t, err)
	require.False(t, used)
}

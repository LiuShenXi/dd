//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTermPlanDefaultAndDurationValidation(t *testing.T) {
	g := &dbent.Group{Status: "active", Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}
	value := "100.12345678"
	for _, unit := range []string{"days", "day"} {
		normalized, err := validatePlanQuota(g, 7, unit, true, &value)
		require.NoError(t, err)
		require.Equal(t, value, *normalized)
	}
	short := "100"
	normalized, err := validatePlanQuota(g, 7, "days", true, &short)
	require.NoError(t, err)
	require.Equal(t, "100.00000000", *normalized)
	_, err = validatePlanQuota(g, 7, "day", true, nil)
	require.ErrorIs(t, err, ErrSubscriptionQuotaRequired)
	_, err = validatePlanQuota(g, 7, "day", false, nil)
	require.NoError(t, err)
	for _, duration := range []struct {
		days int
		unit string
	}{{1, "week"}, {7, "month"}, {7, "months"}, {7, "year"}, {7, "years"}, {6, "day"}, {8, "day"}, {6, "days"}, {8, "days"}, {7, ""}, {7, " days"}} {
		_, err = validatePlanQuota(g, duration.days, duration.unit, true, &value)
		require.Error(t, err)
	}
	for _, bad := range []string{"0", "-1", "1e2", "1.000000001", "NaN", " 1"} {
		_, err = validatePlanQuota(g, 7, "day", false, &bad)
		require.ErrorIs(t, err, ErrSubscriptionQuotaInvalid)
	}
	g.SubscriptionQuotaMode = "windows"
	_, err = validatePlanQuota(g, 2, "month", true, nil)
	require.NoError(t, err)
}

type quotaPaidStub struct{ calls int }

func (s *quotaPaidStub) FulfillPaidTerm(context.Context, domain.SubscriptionQuotaPaidTermParams) (*domain.SubscriptionQuotaMutationResult, error) {
	s.calls++
	return nil, ErrSubscriptionQuotaPaymentLease
}

func TestPaidTermDispatchDoesNotDependOnSalesOrPlan(t *testing.T) {
	snapshot := domain.SubscriptionQuotaOrderSnapshot{SchemaVersion: 1, Mode: "term", GroupID: 1, Platform: "anthropic", DurationSeconds: 604800, QuotaUSD: "10.00000000", SourcePlanID: 1, PlanName: "Deleted plan", GroupName: "Claude"}
	repo := &quotaPaidStub{}
	svc := &PaymentService{}
	svc.SetSubscriptionQuotaPayment(repo, &config.Config{})
	err := svc.doSub(context.Background(), &dbent.PaymentOrder{ID: 1, SubscriptionQuotaSnapshot: snapshot.Map()}, &paymentFulfillmentLease{version: time.Now()})
	require.ErrorIs(t, err, ErrSubscriptionQuotaPaymentLease)
	require.Equal(t, 1, repo.calls)
	require.False(t, svc.quotaOrderIssueAllowed(1))
	// A malformed non-nil snapshot must never fall through to the old service.
	err = svc.doSub(context.Background(), &dbent.PaymentOrder{ID: 1, SubscriptionQuotaSnapshot: map[string]any{}}, &paymentFulfillmentLease{version: time.Now()})
	require.ErrorIs(t, err, ErrSubscriptionQuotaOrderSnapshot)
	require.Equal(t, 1, repo.calls)
	require.Nil(t, PaymentOrderQuotaSnapshot(&dbent.PaymentOrder{SubscriptionQuotaSnapshot: map[string]any{"api_key": "secret"}}))
}

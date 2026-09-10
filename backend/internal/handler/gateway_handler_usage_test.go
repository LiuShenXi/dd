package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type carpoolUsageDetailsRepo struct {
	service.CarpoolRepositoryAPI
	details *domain.CarpoolUserDetails
	err     error
	userID  int64
}

func (r *carpoolUsageDetailsRepo) GetUserDetails(_ context.Context, userID int64) (*domain.CarpoolUserDetails, error) {
	r.userID = userID
	return r.details, r.err
}

func TestUsageCarpoolNeverReturnsRetiredBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                   string
		change                 func(*domain.CarpoolUserDetails)
		err                    error
		nilDetails, nilService bool
		status                 string
		remaining              float64
		wantHTTP               int
	}{
		{name: "active", status: "active", remaining: 123.456789},
		{name: "exhausted", change: func(d *domain.CarpoolUserDetails) { d.Quota.AvailableUSD = decimal.Zero }, status: "quota_exhausted"},
		{name: "negative quota", change: func(d *domain.CarpoolUserDetails) { d.Quota.AvailableUSD = decimal.NewFromInt(-5) }, status: "quota_exhausted"},
		{name: "expired", change: func(d *domain.CarpoolUserDetails) { d.Term.Status = domain.CarpoolTermExpired }, status: "expired"},
		{name: "expiry boundary", change: func(d *domain.CarpoolUserDetails) { d.Term.ExpiresAt = now }, status: "expired"},
		{name: "not started", change: func(d *domain.CarpoolUserDetails) { d.Term.StartsAt = now.Add(time.Hour) }, status: "scheduled"},
		{name: "terminated", change: func(d *domain.CarpoolUserDetails) { d.Term.Status = domain.CarpoolTermTerminated }, status: "terminated"},
		{name: "missing term", change: func(d *domain.CarpoolUserDetails) { d.Term = nil }, status: "not_opened"},
		{name: "missing quota", change: func(d *domain.CarpoolUserDetails) { d.Quota = nil }, status: "unavailable"},
		{name: "repository failure", err: errors.New("database failure"), wantHTTP: http.StatusServiceUnavailable},
		{name: "nil details", nilDetails: true, wantHTTP: http.StatusServiceUnavailable},
		{name: "missing service", nilService: true, wantHTTP: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			details := &domain.CarpoolUserDetails{
				ServerNow: now,
				Term:      &domain.CarpoolUserTerm{Status: domain.CarpoolTermActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
				Quota:     &domain.CarpoolUserQuota{AvailableUSD: decimal.RequireFromString("123.456789")},
			}
			if tc.change != nil {
				tc.change(details)
			}
			if tc.nilDetails {
				details = nil
			}
			repo := &carpoolUsageDetailsRepo{details: details, err: tc.err}
			h := &GatewayHandler{carpoolService: service.NewCarpoolService(repo)}
			if tc.nilService {
				h.carpoolService = nil
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
			h.usageUnrestricted(c, c.Request.Context(), &service.APIKey{
				User:  &service.User{Balance: 9999},
				Group: &service.Group{Name: "Original OpenAI", SubscriptionType: service.SubscriptionTypeCarpool},
			}, middleware.AuthSubject{UserID: 42}, gin.H{"today": 1}, []int{2}, []int{3})
			wantHTTP := tc.wantHTTP
			if wantHTTP == 0 {
				wantHTTP = http.StatusOK
			}
			require.Equal(t, wantHTTP, recorder.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.NotContains(t, body, "balance")
			require.NotContains(t, recorder.Body.String(), "9999")
			if !tc.nilService {
				require.EqualValues(t, 42, repo.userID)
			}
			if wantHTTP != http.StatusOK {
				return
			}
			require.Equal(t, "unrestricted", body["mode"])
			require.Equal(t, "carpool", body["billing_type"])
			require.Equal(t, tc.status, body["status"])
			require.Equal(t, tc.status == "active", body["isValid"])
			require.Equal(t, tc.remaining, body["remaining"])
			require.Contains(t, body, "usage")
			require.Contains(t, body, "daily_usage")
			require.Contains(t, body, "model_stats")
		})
	}
}

func TestUsageQuotaLimitedCarpoolReturnsOnlyKeyQuota(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	h := &GatewayHandler{}
	h.usageQuotaLimited(c, c.Request.Context(), &service.APIKey{
		Status: service.StatusAPIKeyActive, Quota: 10, QuotaUsed: 4,
		User: &service.User{Balance: 9999}, Group: &service.Group{SubscriptionType: service.SubscriptionTypeCarpool},
	}, nil, nil, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "quota_limited", body["mode"])
	require.EqualValues(t, 6, body["remaining"])
	require.NotContains(t, body, "balance")
	require.NotContains(t, recorder.Body.String(), "9999")
}

func TestUsageUnrestrictedIncludesWeeklyWindowStart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

	weeklyWindowStart := time.Date(2026, time.July, 13, 0, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{
		WeeklyWindowStart: &weeklyWindowStart,
	})

	handler := &GatewayHandler{}
	handler.usageUnrestricted(
		c,
		context.Background(),
		&service.APIKey{Group: &service.Group{
			Name:             "Weekly plan",
			SubscriptionType: service.SubscriptionTypeSubscription,
		}},
		middleware.AuthSubject{},
		nil,
		nil,
		nil,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Subscription struct {
			WeeklyWindowStart *time.Time `json:"weekly_window_start"`
		} `json:"subscription"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Subscription.WeeklyWindowStart)
	require.True(t, weeklyWindowStart.Equal(*response.Subscription.WeeklyWindowStart))
}

//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaLegacyI4Repo struct {
	service.UserSubscriptionRepository
	row      service.UserSubscription
	userRead int64
}

func (r *quotaLegacyI4Repo) GetByID(context.Context, int64) (*service.UserSubscription, error) {
	row := r.row
	return &row, nil
}
func (r *quotaLegacyI4Repo) ListByUserID(_ context.Context, userID int64) ([]service.UserSubscription, error) {
	r.userRead = userID
	return []service.UserSubscription{r.row}, nil
}
func (r *quotaLegacyI4Repo) ListActiveByUserID(_ context.Context, userID int64) ([]service.UserSubscription, error) {
	r.userRead = userID
	return []service.UserSubscription{r.row}, nil
}
func (r *quotaLegacyI4Repo) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	return []service.UserSubscription{r.row}, &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1}, nil
}
func (r *quotaLegacyI4Repo) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	return []service.UserSubscription{r.row}, &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1}, nil
}

func quotaLegacyI4Router(repo *quotaLegacyI4Repo, reader service.SubscriptionQuotaSummaryReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewSubscriptionService(nil, repo, nil, nil, nil)
	svc.SetSubscriptionQuotaSummaryReader(reader)
	h := &handler.Handlers{Subscription: handler.NewSubscriptionHandler(svc), Admin: &handler.AdminHandlers{Subscription: adminhandler.NewSubscriptionHandler(svc)}}
	auth := func(c *gin.Context) {
		role := c.GetHeader("X-Test-Role")
		if role == "" {
			c.AbortWithStatus(401)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), role)
		c.Next()
	}
	audit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	r := gin.New()
	RegisterUserRoutes(r.Group("/api/v1"), h, middleware.JWTAuthMiddleware(auth), audit, nil, nil)
	RegisterAdminRoutes(r.Group("/api/v1"), h, middleware.AdminAuthMiddleware(auth), audit, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	return r
}

func TestSubscriptionQuotaI4LegacyHTTPAllSurfacesReadCanonicalSummary(t *testing.T) {
	now := time.Now()
	limit := float64(9999)
	repo := &quotaLegacyI4Repo{row: service.UserSubscription{ID: 3, UserID: 42, GroupID: 6, Status: "active", ExpiresAt: now.Add(1000 * time.Hour), DailyUsageUSD: 999, WeeklyUsageUSD: 999, MonthlyUsageUSD: 999, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, Notes: "synthetic private administrator note", Group: &service.Group{ID: 6, Name: "Claude", Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term", DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit}}}
	reader := &quotaUsageI4Reader{summary: &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: now, CurrentTerm: &domain.SubscriptionQuotaTerm{ID: 40, Status: "active", QuotaUSD: "100.00000000", UsedUSD: "30.00000000", PendingUSD: "5.00000000", RemainingUSD: "65.00000000", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}, NextTerms: []domain.SubscriptionQuotaTerm{}}}
	r := quotaLegacyI4Router(repo, reader)
	for _, path := range []string{"/api/v1/subscriptions", "/api/v1/subscriptions/active", "/api/v1/subscriptions/progress", "/api/v1/subscriptions/summary", "/api/v1/admin/subscriptions", "/api/v1/admin/subscriptions/3", "/api/v1/admin/subscriptions/3/progress", "/api/v1/admin/groups/6/subscriptions", "/api/v1/admin/users/42/subscriptions"} {
		t.Run(path, func(t *testing.T) {
			w := quotaRouteRequest(r, "GET", path, "", service.RoleAdmin, "")
			require.Equal(t, 200, w.Code, w.Body.String())
			body := string(quotaRouteData(t, w))
			require.Contains(t, body, `"quota_mode":"term"`)
			require.Contains(t, body, `"used_usd":"30.00000000"`)
			require.Contains(t, body, `"pending_usd":"5.00000000"`)
			require.NotContains(t, body, `"daily_usage_usd"`)
			require.NotContains(t, body, `"weekly_usage_usd"`)
			require.NotContains(t, body, `"monthly_usage_usd"`)
			require.NotContains(t, body, `"daily_used_usd"`)
			require.NotContains(t, body, `"daily_limit_usd":9999`)
			if !strings.Contains(path, "/admin/") {
				require.NotContains(t, body, "private administrator note")
			}
		})
	}
	require.Equal(t, int64(42), reader.user)
	require.Equal(t, int64(6), reader.group)
	require.Equal(t, float64(999), repo.row.DailyUsageUSD)
	reader.summary.CurrentTerm = nil
	reader.summary.NextTerms = []domain.SubscriptionQuotaTerm{{ID: 41, StartsAt: now.Add(time.Hour), ExpiresAt: now.Add(169 * time.Hour)}}
	w := quotaRouteRequest(r, "GET", "/api/v1/subscriptions/active", "", service.RoleUser, "")
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `[]`, string(quotaRouteData(t, w)))
	w = quotaRouteRequest(r, "GET", "/api/v1/subscriptions/summary", "", service.RoleUser, "")
	var out map[string]any
	require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &out))
	require.Equal(t, float64(0), out["active_count"])
}

func TestSubscriptionQuotaI4LegacyHTTPWindowsDoesNotCallTermReader(t *testing.T) {
	now := time.Now()
	limit := float64(100)
	repo := &quotaLegacyI4Repo{row: service.UserSubscription{ID: 3, UserID: 42, GroupID: 7, Status: "active", ExpiresAt: now.Add(time.Hour), WeeklyWindowStart: &now, WeeklyUsageUSD: 5, Group: &service.Group{ID: 7, SubscriptionType: service.SubscriptionTypeSubscription, WeeklyLimitUSD: &limit}}}
	reader := &quotaUsageI4Reader{err: errors.New("must not read terms")}
	r := quotaLegacyI4Router(repo, reader)
	for _, path := range []string{"/api/v1/subscriptions", "/api/v1/subscriptions/active", "/api/v1/subscriptions/progress", "/api/v1/subscriptions/summary", "/api/v1/admin/subscriptions", "/api/v1/admin/subscriptions/3", "/api/v1/admin/subscriptions/3/progress", "/api/v1/admin/groups/7/subscriptions", "/api/v1/admin/users/42/subscriptions"} {
		w := quotaRouteRequest(r, "GET", path, "", service.RoleAdmin, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"quota_mode":"windows"`)
	}
	require.Zero(t, reader.calls)
}

//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaUsageI4Reader struct {
	service.SubscriptionQuotaRepository
	summary     *domain.SubscriptionQuotaSummary
	err         error
	calls       int
	user, group int64
}

func (r *quotaUsageI4Reader) GetSummary(_ context.Context, user, group int64) (*domain.SubscriptionQuotaSummary, error) {
	r.calls++
	r.user, r.group = user, group
	return r.summary, r.err
}

type quotaUsageI4KeyRepo struct{ termI3RouteKeyRepo }

func (quotaUsageI4KeyRepo) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	now := time.Now()
	return &service.APIKeyRateLimitData{Usage5h: 2, Window5hStart: &now}, nil
}

func quotaUsageI4Router(t *testing.T, key *service.APIKey, reader *quotaUsageI4Reader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(quotaUsageI4KeyRepo{termI3RouteKeyRepo{key: key}}, nil, nil, nil, nil, nil, cfg)
	h := handler.NewGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, keys, nil, nil, nil, nil, cfg, nil)
	if reader != nil {
		h.SetSubscriptionQuotaSummaryReader(service.NewSubscriptionQuotaService(reader, nil))
	}
	r := gin.New()
	RegisterGatewayRoutes(r, &handler.Handlers{Gateway: h, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
	return r
}

func quotaUsageI4Key() *service.APIKey {
	gid := int64(6)
	return &service.APIKey{ID: 21, UserID: 11, GroupID: &gid, Status: service.StatusAPIKeyActive, User: &service.User{ID: 11, Status: service.StatusActive, Balance: -100}, Group: &service.Group{ID: gid, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}}
}
func quotaUsageI4Get(r *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/v1/usage?user_id=999", nil)
	req.Header.Set("x-api-key", "synthetic-term-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSubscriptionQuotaI4UsageActualRouterSeparatesCardFromKeyConstraints(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			key := quotaUsageI4Key()
			if limited {
				key.Quota = 5
				key.QuotaUsed = 1
				key.RateLimit5h = 10
			}
			now := time.Now()
			reader := &quotaUsageI4Reader{summary: &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: now, CurrentTerm: &domain.SubscriptionQuotaTerm{ID: 40, Status: "active", QuotaUSD: "100.00000000", UsedUSD: "30.00000000", PendingUSD: "5.00000000", RemainingUSD: "65.00000000", ExpiresAt: now.Add(time.Hour)}, NextTerms: []domain.SubscriptionQuotaTerm{}}}
			w := quotaUsageI4Get(quotaUsageI4Router(t, key, reader))
			require.Equal(t, 200, w.Code, w.Body.String())
			var out map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			require.Equal(t, "claude_subscription", out["billing_scope"])
			require.Equal(t, "term", out["quota_mode"])
			require.Equal(t, "65.00000000", out["remaining"])
			require.NotContains(t, out, "balance")
			require.NotContains(t, out, "subscription")
			require.Equal(t, int64(11), reader.user)
			require.Equal(t, int64(6), reader.group)
			require.Equal(t, 1, reader.calls)
			if limited {
				require.Equal(t, "quota_limited", out["mode"])
				require.Equal(t, float64(4), out["quota"].(map[string]any)["remaining"])
				require.Equal(t, float64(8), out["rate_limits"].([]any)[0].(map[string]any)["remaining"])
			} else {
				require.Equal(t, "unrestricted", out["mode"])
				require.NotContains(t, out, "quota")
			}
		})
	}
}

func TestSubscriptionQuotaI4UsageUnavailableAndNoCurrentNeverFallsBack(t *testing.T) {
	for _, status := range []string{"not_active", "expired", "cancelled", "exhausted", "reconciling"} {
		t.Run(status, func(t *testing.T) {
			summary := &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: time.Now(), NextTerms: []domain.SubscriptionQuotaTerm{}}
			if status == "exhausted" || status == "reconciling" {
				summary.CurrentTerm = &domain.SubscriptionQuotaTerm{Status: status, RemainingUSD: "0.00000000"}
			} else if status != "not_active" {
				summary.LastTerm = &domain.SubscriptionQuotaTerm{Status: status}
			}
			w := quotaUsageI4Get(quotaUsageI4Router(t, quotaUsageI4Key(), &quotaUsageI4Reader{summary: summary}))
			require.Equal(t, 200, w.Code, w.Body.String())
			var out map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			require.Equal(t, status, out["status"])
			require.Equal(t, false, out["isValid"])
			require.Equal(t, "0.00000000", out["remaining"])
			require.NotContains(t, out, "balance")
		})
	}
	for _, reader := range []*quotaUsageI4Reader{nil, {err: errors.New("synthetic DB unavailable")}, {summary: &domain.SubscriptionQuotaSummary{GroupID: 99, QuotaMode: "term"}}} {
		w := quotaUsageI4Get(quotaUsageI4Router(t, quotaUsageI4Key(), reader))
		require.Equal(t, 503, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "TERM_BILLING_UNAVAILABLE")
		require.NotContains(t, w.Body.String(), "-100")
	}
}

func TestSubscriptionQuotaI4UsageWindowsAndGPTNeverQueryTermReader(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformOpenAI} {
		key := quotaUsageI4Key()
		key.Group.SubscriptionQuotaMode = "windows"
		key.Group.Platform = platform
		key.Group.SubscriptionType = service.SubscriptionTypeStandard
		key.Quota = 5
		key.QuotaUsed = 1
		reader := &quotaUsageI4Reader{err: errors.New("term reader must not be called")}
		w := quotaUsageI4Get(quotaUsageI4Router(t, key, reader))
		require.Equal(t, 200, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Equal(t, float64(4), out["remaining"])
		require.NotContains(t, out, "quota_summary")
		require.Zero(t, reader.calls)
	}
}

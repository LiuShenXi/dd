//go:build unit

package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type termI3RouteKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r termI3RouteKeyRepo) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	return r.key, nil
}
func (termI3RouteKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

// Any invocation of this port in malformed ingress tests is a test failure:
// authentication must not admit or read term data before a real inference send.
type termI3RouteBilling struct {
	service.SubscriptionQuotaGatewayBilling
}

type termI3MetadataBilling struct {
	service.SubscriptionQuotaGatewayBilling
	calls   int
	failure error
}

func (b *termI3MetadataBilling) Admit(context.Context, domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	b.calls++
	return nil, b.failure
}

type termI3MetadataScheduler struct {
	service.SchedulerCache
	account *service.Account
}

func (s *termI3MetadataScheduler) GetSnapshot(context.Context, service.SchedulerBucket) ([]*service.Account, bool, error) {
	return []*service.Account{s.account}, true, nil
}
func (s *termI3MetadataScheduler) GetAccount(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}
func (*termI3MetadataScheduler) UpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

type termI3MetadataUpstream struct{ calls int }

func (u *termI3MetadataUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"input_tokens":7}`))}, nil
}
func (u *termI3MetadataUpstream) DoWithTLS(req *http.Request, proxy string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, n)
}

func termI3ActualRouter(t *testing.T, enabled, dependency bool, mode string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gid := int64(6)
	cfg := &config.Config{RunMode: mode, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}, SubscriptionQuota: config.SubscriptionQuotaConfig{AdmissionEnabled: enabled, EnabledGroupIDs: []int64{gid}}}
	key := &service.APIKey{ID: 21, UserID: 11, GroupID: &gid, Status: service.StatusActive, User: &service.User{ID: 11, Status: service.StatusActive, Balance: -100}, Group: &service.Group{ID: gid, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}}
	keys := service.NewAPIKeyService(termI3RouteKeyRepo{key: key}, nil, nil, nil, nil, nil, cfg)
	if dependency {
		keys.SetSubscriptionQuotaGatewayBilling(&termI3RouteBilling{})
	}
	router := gin.New()
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
	return router
}

func TestSubscriptionQuotaI3ActualRouterAliasesAndUnsupported(t *testing.T) {
	router := termI3ActualRouter(t, true, true, config.RunModeStandard)
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest("POST", path, strings.NewReader("{"))
			request.Header.Set("x-api-key", "synthetic-term-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 400, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "parse")
			require.NotContains(t, response.Body.String(), "INSUFFICIENT_BALANCE")
		})
	}
	for _, route := range []struct{ method, path string }{{"POST", "/v1/responses/compact"}, {"POST", "/responses/compact"}, {"POST", "/backend-api/codex/responses/compact"}, {"POST", "/v1/responses/input_tokens"}, {"GET", "/v1/responses"}, {"GET", "/responses"}, {"GET", "/backend-api/codex/responses"}, {"POST", "/v1/images/generations"}, {"POST", "/v1/embeddings"}, {"POST", "/v1/web_search"}, {"POST", "/antigravity/v1/messages"}, {"POST", "/v1beta/models/claude:generateContent"}} {
		t.Run(route.method+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"model":"claude-sonnet-4"}`))
			request.Header.Set("x-api-key", "synthetic-term-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 403, response.Code, response.Body.String())
			if strings.HasPrefix(route.path, "/v1beta/") {
				require.Contains(t, response.Body.String(), "PERMISSION_DENIED")
			} else {
				require.Contains(t, response.Body.String(), "TERM_")
			}
		})
	}
}

func TestSubscriptionQuotaI3ActualRouterFlagsAndMissingDependency(t *testing.T) {
	for _, tc := range []struct {
		name                string
		enabled, dependency bool
		mode                string
	}{{"disabled", false, true, config.RunModeStandard}, {"missing port", true, false, config.RunModeStandard}, {"simple", true, true, config.RunModeSimple}} {
		t.Run(tc.name, func(t *testing.T) {
			router := termI3ActualRouter(t, tc.enabled, tc.dependency, tc.mode)
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{"))
			request.Header.Set("x-api-key", "synthetic-term-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 503, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "TERM_BILLING_UNAVAILABLE")
		})
	}
}

func TestSubscriptionQuotaI3ActualRouterCountTokensWithoutGenerationRights(t *testing.T) {
	for _, failure := range []error{service.ErrSubscriptionQuotaNotActive, service.ErrSubscriptionQuotaExpired} {
		for _, path := range []string{"/v1/messages/count_tokens", "/messages/count_tokens"} {
			t.Run(failure.Error()+path, func(t *testing.T) {
				gid := int64(6)
				cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
				key := &service.APIKey{ID: 21, UserID: 11, GroupID: &gid, Status: service.StatusActive, User: &service.User{ID: 11, Status: service.StatusActive, Balance: -100}, Group: &service.Group{ID: gid, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}}
				billing := &termI3MetadataBilling{failure: failure}
				keys := service.NewAPIKeyService(termI3RouteKeyRepo{key: key}, nil, nil, nil, nil, nil, cfg)
				keys.SetSubscriptionQuotaGatewayBilling(billing)
				account := &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}, AccountGroups: []service.AccountGroup{{AccountID: 51, GroupID: gid}}}
				scheduler := service.NewSchedulerSnapshotService(&termI3MetadataScheduler{account: account}, nil, nil, nil, nil)
				upstream := &termI3MetadataUpstream{}
				gateway := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg, scheduler, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := handler.NewGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, nil, keys, nil, nil, nil, nil, cfg, nil)
				router := gin.New()
				RegisterGatewayRoutes(router, &handler.Handlers{Gateway: h, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
				request := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`))
				request.Header.Set("x-api-key", "synthetic-term-key")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, 200, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), `"input_tokens":7`)
				require.Equal(t, 1, upstream.calls)
				require.Zero(t, billing.calls)
				request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4"}`))
				request.Header.Set("x-api-key", "synthetic-term-key")
				response = httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, 503, response.Code, response.Body.String())
				require.Equal(t, 1, upstream.calls)
				require.Zero(t, billing.calls)
			})
		}
	}
}

//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaCompatibilityKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r quotaCompatibilityKeyRepo) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	return r.key, nil
}

func TestSubscriptionQuotaR1ActualRouterRejectsEveryGenerationAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		cfg := &config.Config{RunMode: mode, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
		groupID := int64(6)
		key := &service.APIKey{ID: 11, UserID: 7, GroupID: &groupID, Status: service.StatusActive,
			User:  &service.User{ID: 7, Status: service.StatusActive, Balance: 1000},
			Group: &service.Group{ID: groupID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}}
		keys := service.NewAPIKeyService(quotaCompatibilityKeyRepo{key: key}, nil, nil, nil, nil, nil, cfg)
		router := gin.New()
		RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
		for _, route := range []struct{ method, path string }{
			{"POST", "/v1/messages"}, {"POST", "/v1/chat/completions"}, {"POST", "/chat/completions"},
			{"POST", "/v1/responses"}, {"POST", "/responses"}, {"POST", "/backend-api/codex/responses"},
			{"POST", "/v1/responses/compact"}, {"POST", "/responses/compact"}, {"POST", "/backend-api/codex/responses/compact"},
			{"GET", "/v1/responses"}, {"GET", "/responses"}, {"GET", "/backend-api/codex/responses"},
			{"POST", "/v1/images/generations"}, {"POST", "/images/edits/async"}, {"POST", "/v1/images/batches"},
			{"POST", "/v1/live"}, {"POST", "/v1/alpha/search"}, {"POST", "/v1/embeddings"},
			{"POST", "/v1/videos"}, {"POST", "/v1/web_search"}, {"POST", "/contents/generations/tasks"},
			{"POST", "/v1beta/models/claude:generateContent"}, {"POST", "/antigravity/v1/messages"},
			{"POST", "/antigravity/v1beta/models/claude:generateContent"},
		} {
			t.Run(mode+route.path+route.method, func(t *testing.T) {
				request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"model":"claude-sonnet"}`))
				request.Header.Set("x-api-key", "synthetic-quota-key")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				expected := http.StatusServiceUnavailable
				if strings.HasPrefix(route.path, "/v1beta/") || strings.HasPrefix(route.path, "/antigravity/") {
					expected = http.StatusForbidden
				}
				require.Equal(t, expected, response.Code, response.Body.String())
			})
		}
	}
}

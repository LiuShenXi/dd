//go:build embed

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sentinelEmbedSettings struct{}

func (sentinelEmbedSettings) GetPublicSettingsForInjection(context.Context) (any, error) {
	return map[string]any{}, nil
}

// These are the production middleware and route registration, in production
// order. Testing the handler alone misses SPA interception in embedded builds.
func TestCodexSentinelEmbeddedGatewayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.True(t, web.HasEmbeddedFrontend(), "this regression must run with embedded frontend assets")
	const token = "test-only-sentinel-0123456789abcdef0123456789abcdef"
	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte(token), 0600))
	for _, legacy := range []bool{false, true} {
		name := "injected"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			for _, configured := range []bool{false, true} {
				name := "unconfigured"
				if configured {
					name = "configured"
				}
				t.Run(name, func(t *testing.T) {
					t.Setenv("CODEX_SENTINEL_TOKEN_FILE", "")
					if configured {
						t.Setenv("CODEX_SENTINEL_TOKEN_FILE", tokenPath)
					}
					t.Setenv("CODEX_SENTINEL_ACCOUNT_IDS", "2")
					t.Setenv("CODEX_SENTINEL_MODELS", "gpt-6-astra")
					cfg := &config.Config{RunMode: config.RunModeSimple}
					svc := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
					t.Cleanup(svc.StopOpenAICodexTicketHarvester)
					h := &handler.Handlers{
						Gateway:       handler.NewGatewayHandler(nil, svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
						OpenAIGateway: handler.NewOpenAIGatewayHandler(svc, nil, nil, nil, nil, nil, nil, nil, cfg),
						AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
					}
					router := gin.New()
					if legacy {
						router.Use(web.ServeEmbeddedFrontend())
					} else {
						frontend, err := web.NewFrontendServer(sentinelEmbedSettings{})
						require.NoError(t, err)
						router.Use(frontend.Middleware())
					}
					RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusTeapot) }), nil, nil, nil, nil, nil, cfg)
					// Prove the SPA middleware is active, rather than merely testing API-only routing.
					page := httptest.NewRecorder()
					router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/sentinel-spa-control", nil))
					require.Equal(t, http.StatusOK, page.Code)
					require.Contains(t, page.Header().Get("Content-Type"), "text/html")
					for _, auth := range []string{"", "wrong-token", token} {
						for _, method := range []string{http.MethodGet, http.MethodPost} {
							path := "/internal/codex-sentinel/status?after=0"
							body := ""
							if method == http.MethodPost {
								path = "/internal/codex-sentinel/refresh"
								body = `{"account_id":2,"model":"gpt-6-astra","reason":"missing","expected_ticket_version":"","event_id":""}`
							}
							request := httptest.NewRequest(method, path, strings.NewReader(body))
							if auth != "" {
								request.Header.Set("Authorization", "Bearer "+auth)
							}
							response := httptest.NewRecorder()
							router.ServeHTTP(response, request)
							expected := http.StatusNotFound
							if configured {
								expected = http.StatusUnauthorized
								if auth == token {
									expected = http.StatusOK
								}
							}
							require.Equal(t, expected, response.Code, "%s %s", method, path)
							require.NotContains(t, response.Header().Get("Content-Type"), "text/html")
							if expected == http.StatusOK {
								require.Contains(t, response.Header().Get("Content-Type"), "application/json")
								var decoded map[string]any
								require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
								if method == http.MethodGet {
									require.Equal(t, float64(1), decoded["protocol_version"])
									require.Equal(t, false, decoded["enabled"])
								} else {
									require.Equal(t, "disabled", decoded["status"])
								}
							}
						}
					}
					unknown := httptest.NewRecorder()
					router.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/internal/codex-sentinel/unknown", nil))
					require.Equal(t, http.StatusNotFound, unknown.Code)
				})
			}
		})
	}
}

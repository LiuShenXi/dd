//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuth_UngroupedRestriction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, tc := range []struct {
			name       string
			role       string
			restricted bool
		}{
			{name: "restricted member", role: service.RoleUser, restricted: true},
			{name: "ordinary user", role: service.RoleUser},
			{name: "administrator", role: service.RoleAdmin},
		} {
			name := "main/" + tc.name
			if google {
				name = "google/" + tc.name
			}
			t.Run(name, func(t *testing.T) {
				key := &service.APIKey{ID: 100, UserID: 7, Key: "test-key", Status: service.StatusActive,
					User: &service.User{ID: 7, Role: tc.role, Status: service.StatusActive, Balance: 550, RestrictPublicGroups: tc.restricted}}
				repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				if google {
					router.Use(APIKeyAuthGoogle(svc, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
				}
				router.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/t", nil)
				request.Header.Set("x-api-key", key.Key)
				request.Header.Set("x-goog-api-key", key.Key)
				router.ServeHTTP(response, request)
				if tc.restricted {
					require.Equal(t, http.StatusForbidden, response.Code)
				} else {
					require.Equal(t, http.StatusOK, response.Code)
				}
			})
		}
	}
}

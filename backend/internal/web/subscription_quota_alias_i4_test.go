//go:build embed

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaRegisteredRootAliasesBypassEmbeddedSPA(t *testing.T) {
	server, err := NewFrontendServer(&mockSettingsProvider{settings: map[string]string{"site_name": "synthetic"}})
	require.NoError(t, err)
	for _, route := range []struct{ method, path string }{{"POST", "/chat/completions"}, {"POST", "/messages/count_tokens"}, {"GET", "/models/claude-sonnet"}} {
		r := gin.New()
		r.Use(server.Middleware())
		called := false
		r.Handle(route.method, route.path, func(c *gin.Context) { called = true; c.JSON(200, gin.H{"api_alias": true}) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		require.True(t, called, route.path)
		require.JSONEq(t, `{"api_alias":true}`, w.Body.String())
	}
	for _, path := range []string{"/chat/unknown", "/messages/unknown", "/models/a/b"} {
		r := gin.New()
		r.Use(server.Middleware())
		called := false
		r.GET(path, func(c *gin.Context) { called = true; c.Status(http.StatusTeapot) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.False(t, called, path)
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Header().Get("Content-Type"), "text/html")
	}
}

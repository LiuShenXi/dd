package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/releasedrain"
	"github.com/gin-gonic/gin"
)

func ReleaseDrain(controller *releasedrain.Controller, queueTimeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ReleaseDrainBypass(c.Request.Method, c.FullPath()) {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), queueTimeout)
		done, err := controller.Enter(ctx)
		cancel()
		if err != nil {
			c.Header("Retry-After", "5")
			AbortWithError(c, http.StatusServiceUnavailable, "RELEASE_IN_PROGRESS", "Service update in progress; retry shortly")
			return
		}
		defer done()
		c.Next()
	}
}

// Match only registered control routes. The routes still execute administrator
// authentication and audit middleware; no header can bypass the request gate.
func ReleaseDrainBypass(method, route string) bool {
	if method == http.MethodGet && (route == "/health" || route == "/api/v1/admin/release/status") {
		return true
	}
	if method != http.MethodPost {
		return false
	}
	switch route {
	case "/api/v1/admin/release/drain", "/api/v1/admin/release/lock", "/api/v1/admin/release/cancel", "/api/v1/admin/release/resume":
		return true
	default:
		return false
	}
}

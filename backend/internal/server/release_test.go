package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/releasedrain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type releaseInvalidatorStub struct {
	err   error
	calls int
}

func (s *releaseInvalidatorStub) InvalidateReleaseAuthCache(context.Context, []int64, []int64) error {
	s.calls++
	return s.err
}

func TestReleaseRoutesRemainAuthenticatedAndHoldBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	controller := releasedrain.New(true, 4, nil)
	r.Use(middleware.ReleaseDrain(controller, 5*time.Millisecond))
	var audited atomic.Int64
	auth := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "test-admin" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	})
	audit := middleware.AuditLogMiddleware(func(c *gin.Context) { audited.Add(1); c.Next() })
	invalidator := &releaseInvalidatorStub{err: errors.New("redis down")}
	registerReleaseRoutes(r, controller, auth, audit, invalidator)
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	var userAuthCalls atomic.Int64
	r.GET("/api/v1/user", func(c *gin.Context) { userAuthCalls.Add(1); c.Status(http.StatusOK) })
	r.GET("/api/v1/admin/release/other", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := func(method, path, body string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authorized {
			req.Header.Set("Authorization", "test-admin")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, 200, request("GET", "/health", "", false).Code)
	require.Equal(t, 401, request("GET", "/api/v1/admin/release/status", "", false).Code)
	require.Equal(t, 200, request("GET", "/api/v1/admin/release/status", "", true).Code)
	require.Equal(t, 503, request("GET", "/api/v1/user", "", true).Code)
	require.Zero(t, userAuthCalls.Load())
	require.Equal(t, 503, request("GET", "/api/v1/admin/release/other", "", true).Code)
	body := fmt.Sprintf(`{"operation_id":%q,"user_ids":[1],"group_ids":[2,3]}`, controller.Status().OperationID)
	require.Equal(t, 401, request("POST", "/api/v1/admin/release/resume", body, false).Code)
	require.Zero(t, invalidator.calls)
	require.Equal(t, 503, request("POST", "/api/v1/admin/release/resume", body, true).Code)
	require.Equal(t, "migrating", controller.Status().State)
	invalidator.err = nil
	require.Equal(t, 200, request("POST", "/api/v1/admin/release/resume", body, true).Code)
	require.Equal(t, 200, request("GET", "/api/v1/user", "", true).Code)
	require.Equal(t, int64(3), audited.Load())
}

func TestReleaseDrainTracksWholeHandlerAndAsyncUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var pending atomic.Int64
	controller := releasedrain.New(false, 2, pending.Load)
	r := gin.New()
	r.Use(middleware.ReleaseDrain(controller, time.Second))
	started, finish, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.GET("/v1/responses", func(c *gin.Context) { close(started); <-finish; pending.Add(1); c.Status(200) })
	go func() {
		defer close(returned)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/responses", nil))
	}()
	<-started
	status, err := controller.Drain()
	require.NoError(t, err)
	require.Equal(t, int64(1), controller.Status().ActiveHTTP)
	require.ErrorIs(t, controller.LockMigration(status.OperationID), releasedrain.ErrBusy)
	close(finish)
	<-returned
	require.Zero(t, controller.Status().ActiveHTTP)
	require.ErrorIs(t, controller.LockMigration(status.OperationID), releasedrain.ErrBusy)
	pending.Add(-1)
	require.NoError(t, controller.LockMigration(status.OperationID))
}

func TestReleaseDrainKeepsUpgradedWebSocketActiveUntilClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := releasedrain.New(false, 2, nil)
	r := gin.New()
	r.Use(middleware.ReleaseDrain(controller, time.Second))
	returned := make(chan struct{})
	r.GET("/v1/responses", func(c *gin.Context) {
		defer close(returned)
		conn, err := coderws.Accept(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, _ = conn.Read(c.Request.Context())
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	status, err := controller.Drain()
	require.NoError(t, err)
	require.Equal(t, int64(1), controller.Status().ActiveHTTP)
	require.ErrorIs(t, controller.LockMigration(status.OperationID), releasedrain.ErrBusy)
	require.NoError(t, conn.CloseNow())
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("WebSocket handler did not finish")
	}
	require.Eventually(t, func() bool { return controller.Status().ActiveHTTP == 0 }, time.Second, time.Millisecond)
	require.NoError(t, controller.LockMigration(status.OperationID))
}

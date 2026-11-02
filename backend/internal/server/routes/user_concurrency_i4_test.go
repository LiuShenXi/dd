//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type concurrencyI4Cache struct {
	service.ConcurrencyCache
	service.UserScopedConcurrencyCache
	legacyFail, claudeFail bool
}

func (c *concurrencyI4Cache) GetLegacyUserConcurrencyBounded(context.Context, int64) (int, error) {
	if c.legacyFail {
		return 0, errors.New("legacy unavailable")
	}
	return 5, nil
}
func (c *concurrencyI4Cache) GetScopeStats(context.Context, service.UserConcurrencyScope, int64) (service.UserScopeStats, error) {
	if c.claudeFail {
		return service.UserScopeStats{}, errors.New("Claude unavailable")
	}
	return service.UserScopeStats{Active: 2, Waiting: 1}, nil
}

type concurrencyI4Users struct {
	service.UserRepository
	user service.User
}

func (r *concurrencyI4Users) GetByID(context.Context, int64) (*service.User, error) {
	user := r.user
	return &user, nil
}
func (r *concurrencyI4Users) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func (r *concurrencyI4Users) ListUserAuthIdentities(context.Context, int64) ([]service.UserAuthIdentityRecord, error) {
	return nil, nil
}

type concurrencyI4Admin struct {
	service.AdminService
	user    service.User
	updates int
	input   *service.UpdateUserInput
}

func (s *concurrencyI4Admin) GetUser(context.Context, int64) (*service.User, error) {
	user := s.user
	return &user, nil
}
func (s *concurrencyI4Admin) ListUsers(context.Context, int, int, service.UserListFilters, string, string) ([]service.User, int64, error) {
	return []service.User{s.user}, 1, nil
}
func (s *concurrencyI4Admin) UpdateUser(_ context.Context, _ int64, in *service.UpdateUserInput) (*service.User, error) {
	s.updates++
	s.input = in
	if in.ClaudeSubscriptionConcurrency != nil {
		s.user.ClaudeSubscriptionConcurrency = in.ClaudeSubscriptionConcurrency
	}
	user := s.user
	return &user, nil
}

func concurrencyI4Router() (*gin.Engine, *concurrencyI4Cache, *concurrencyI4Admin, *concurrencyI4Users) {
	cache := &concurrencyI4Cache{}
	router, admin, users := concurrencyI4RouterWithCache(cache)
	return router, cache, admin, users
}

func concurrencyI4RouterWithCache(cache service.ConcurrencyCache) (*gin.Engine, *concurrencyI4Admin, *concurrencyI4Users) {
	gin.SetMode(gin.TestMode)
	value := 2
	user := service.User{ID: 42, Concurrency: 5, ClaudeSubscriptionConcurrency: &value, Status: service.StatusActive, Role: service.RoleUser, Email: "synthetic@example.test"}
	scope := service.NewConcurrencyService(cache)
	scope.ConfigureClaudeSubscriptionConcurrency(nil, 3)
	userRepo := &concurrencyI4Users{user: user}
	users := service.ProvideUserService(userRepo, nil, nil, nil, scope)
	admin := &concurrencyI4Admin{user: user}
	h := &handler.Handlers{User: handler.NewUserHandler(users, nil, nil, nil, nil, nil), Admin: &handler.AdminHandlers{User: adminhandler.NewUserHandler(admin, scope, nil, nil, nil, users, nil)}}
	h.Auth = handler.NewAuthHandler(nil, nil, users, nil, nil, nil, nil, nil)
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
	RegisterAuthRoutes(r.Group("/api/v1"), h, middleware.JWTAuthMiddleware(auth), audit, nil, nil, nil)
	return r, admin, userRepo
}

func TestClaudeSubscriptionConcurrencyI4ActualAdminListUsesBoundedProjection(t *testing.T) {
	router, cache, _, _ := concurrencyI4Router()
	for _, unavailable := range []bool{false, true} {
		cache.legacyFail, cache.claudeFail = unavailable, unavailable
		w := quotaRouteRequest(router, "GET", "/api/v1/admin/users", "", service.RoleAdmin, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var data struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
		require.Len(t, data.Items, 1)
		user := data.Items[0]
		if unavailable {
			require.Equal(t, float64(0), user["current_concurrency"])
			require.Nil(t, user["legacy_current_concurrency"])
			require.Nil(t, user["claude_current_concurrency"])
			require.Nil(t, user["claude_waiting_count"])
			require.Equal(t, "unavailable", user["legacy_concurrency_status"])
			require.Equal(t, "unavailable", user["claude_concurrency_status"])
		} else {
			require.Equal(t, float64(5), user["current_concurrency"])
			require.Equal(t, float64(5), user["legacy_current_concurrency"])
			require.Equal(t, float64(2), user["claude_current_concurrency"])
			require.Equal(t, float64(1), user["claude_waiting_count"])
		}
		require.Equal(t, float64(5), user["concurrency"])
		require.Equal(t, float64(2), user["effective_claude_subscription_concurrency"])
	}
}

func TestClaudeSubscriptionConcurrencyI4ActualAdminListBlackholeReturnsUnknownWithinDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var connections sync.WaitGroup
	var socketsMu sync.Mutex
	var sockets []net.Conn
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			socket, err := listener.Accept()
			if err != nil {
				return
			}
			socketsMu.Lock()
			sockets = append(sockets, socket)
			socketsMu.Unlock()
			connections.Add(1)
			go func() { defer connections.Done(); defer socket.Close(); _, _ = io.Copy(io.Discard, socket) }()
		}
	}()
	legacyClient := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, PoolTimeout: 30 * time.Second, MaxRetries: 20})
	cache := repository.NewConcurrencyCache(legacyClient, 15, 30)
	t.Cleanup(func() {
		require.NoError(t, cache.(interface{ CloseScopeCache() error }).CloseScopeCache())
		_ = legacyClient.Close()
		_ = listener.Close()
		<-acceptDone
		socketsMu.Lock()
		for _, socket := range sockets {
			_ = socket.Close()
		}
		socketsMu.Unlock()
		connections.Wait()
	})
	router, _, _ := concurrencyI4RouterWithCache(cache)
	done := make(chan *httptest.ResponseRecorder, 1)
	started := time.Now()
	go func() { done <- quotaRouteRequest(router, "GET", "/api/v1/admin/users", "", service.RoleAdmin, "") }()
	select {
	case w := <-done:
		t.Logf("actual admin list returned in %s", time.Since(started))
		require.Equal(t, 200, w.Code, w.Body.String())
		var data struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
		require.Len(t, data.Items, 1)
		user := data.Items[0]
		require.Equal(t, float64(0), user["current_concurrency"], "old wrapper compatibility")
		require.Nil(t, user["legacy_current_concurrency"])
		require.Nil(t, user["claude_current_concurrency"])
		require.Nil(t, user["claude_waiting_count"])
		require.Equal(t, "unavailable", user["legacy_concurrency_status"])
		require.Equal(t, "unavailable", user["claude_concurrency_status"])
	case <-time.After(4 * time.Second):
		t.Fatal("admin list waited on the original 30s Redis client instead of the bounded display projection")
	}
	require.Equal(t, 30*time.Second, legacyClient.Options().ReadTimeout)
	require.Equal(t, 20, legacyClient.Options().MaxRetries)
	require.False(t, legacyClient.Options().ContextTimeoutEnabled)
}

func TestClaudeSubscriptionConcurrencyI4ActualSelfAndAdminProjections(t *testing.T) {
	r, cache, _, users := concurrencyI4Router()
	for _, path := range []string{"/api/v1/user/profile", "/api/v1/auth/me", "/api/v1/admin/users/42"} {
		role := service.RoleUser
		if path == "/api/v1/admin/users/42" {
			role = service.RoleAdmin
		}
		w := quotaRouteRequest(r, "GET", path, "", role, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var data map[string]any
		require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
		require.Equal(t, float64(5), data["concurrency"])
		require.Equal(t, float64(5), data["legacy_current_concurrency"])
		require.Equal(t, "available", data["legacy_concurrency_status"])
		require.Equal(t, float64(2), data["claude_subscription_concurrency"])
		require.Equal(t, float64(3), data["default_claude_subscription_concurrency"])
		require.Equal(t, float64(2), data["effective_claude_subscription_concurrency"])
		require.Equal(t, float64(2), data["claude_current_concurrency"])
		require.Equal(t, float64(1), data["claude_waiting_count"])
	}
	cache.claudeFail = true
	w := quotaRouteRequest(r, "GET", "/api/v1/user/profile", "", service.RoleUser, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
	require.Nil(t, data["claude_current_concurrency"])
	require.Nil(t, data["claude_waiting_count"])
	require.Equal(t, "unavailable", data["claude_concurrency_status"])
	require.Equal(t, float64(5), data["legacy_current_concurrency"])
	cache.claudeFail = false
	cache.legacyFail = true
	w = quotaRouteRequest(r, "GET", "/api/v1/user/profile", "", service.RoleUser, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
	require.Nil(t, data["legacy_current_concurrency"])
	require.Equal(t, "unavailable", data["legacy_concurrency_status"])
	require.Equal(t, float64(2), data["claude_current_concurrency"])
	users.user.ClaudeSubscriptionConcurrency = nil
	w = quotaRouteRequest(r, "GET", "/api/v1/auth/me", "", service.RoleUser, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
	require.Nil(t, data["claude_subscription_concurrency"])
	require.Equal(t, float64(3), data["effective_claude_subscription_concurrency"])
	cache.claudeFail = true
	w = quotaRouteRequest(r, "GET", "/api/v1/auth/me", "", service.RoleUser, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(quotaRouteData(t, w), &data))
	require.Nil(t, data["claude_current_concurrency"])
	require.Nil(t, data["legacy_current_concurrency"])
	require.Equal(t, "unavailable", data["claude_concurrency_status"])
	require.Equal(t, "unavailable", data["legacy_concurrency_status"])
}

func TestClaudeSubscriptionConcurrencyI4ActualAdminUpdatePresenceAndLegacyCompatibility(t *testing.T) {
	r, _, admin, _ := concurrencyI4Router()
	for _, raw := range []string{`null`, `0`, `-1`, `1.5`, `2147483648`} {
		w := quotaRouteRequest(r, "PUT", "/api/v1/admin/users/42", `{"claude_subscription_concurrency":`+raw+`}`, service.RoleAdmin, "")
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Zero(t, admin.updates)
	}
	w := quotaRouteRequest(r, "PUT", "/api/v1/admin/users/42", `{"claude_subscription_concurrency":3}`, service.RoleAdmin, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 3, *admin.input.ClaudeSubscriptionConcurrency)
	require.Nil(t, admin.input.Concurrency)
	require.Equal(t, 5, admin.user.Concurrency)
	w = quotaRouteRequest(r, "PUT", "/api/v1/admin/users/42", `{"notes":"old-client-edit"}`, service.RoleAdmin, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Nil(t, admin.input.ClaudeSubscriptionConcurrency)
	require.Equal(t, 3, *admin.user.ClaudeSubscriptionConcurrency)
}

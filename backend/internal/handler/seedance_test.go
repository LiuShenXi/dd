//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSeedancePromptAuditBlocksBeforeTaskCreation(t *testing.T) {
	for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
		t.Run(prefix, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
			engine := blockingHandlerPromptEngine()
			h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
			router := gin.New()
			router.Use(securityAuditMediaTestMiddleware)
			path := prefix + "/contents/generations/tasks"
			router.POST(path, h.SeedanceTasks)
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"doubao-seedance","content":[{"type":"text","text":"first blocked scene"},{"type":"image_url","image_url":{"url":"https://example.test/reference.png"}},{"type":"text","text":"second blocked scene"}]}`))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, request)

			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), securityaudit.ErrorCodeBlocked)
			evaluated, enqueued, requests := engine.snapshot()
			require.Equal(t, 1, evaluated)
			require.Zero(t, enqueued)
			require.Len(t, requests, 1)
			require.Equal(t, "doubao-seedance", requests[0].Model)
			snapshot, err := securityaudit.ExtractPromptSnapshot(requests[0])
			require.NoError(t, err)
			require.Contains(t, snapshot.FullPrompt, "first blocked scene")
			require.Contains(t, snapshot.FullPrompt, "second blocked scene")
			require.Zero(t, upstream.calls)
			require.Zero(t, slots.acquired)
			require.Zero(t, slots.userAcquired)
			require.Zero(t, bindings.writes)
			require.Empty(t, bindings.pending)
			require.Empty(t, bindings.billed)
		})
	}
}

func TestSeedanceHandlerRejectsCarpoolBeforeUpstreamOrBilling(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
			t.Run(platform+"/"+method, func(t *testing.T) {
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
				c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
				key, ok := middleware.GetAPIKeyFromContext(c)
				require.True(t, ok)
				key.Group.Platform = platform
				key.Group.SubscriptionType = service.SubscriptionTypeCarpool
				c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks", strings.NewReader(`{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Params = gin.Params{{Key: "task_id", Value: "task-ark"}}

				h.SeedanceTasks(c)

				require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "CARPOOL_SEEDANCE_UNSUPPORTED")
				require.Zero(t, upstream.calls, "unsupported billing must fail before creating or accessing an upstream task")
				require.Zero(t, slots.acquired)
				require.Zero(t, slots.userAcquired)
				require.Zero(t, bindings.writes)
				require.Empty(t, bindings.pending)
				require.Empty(t, bindings.billed)
			})
		}
	}
}

func TestSeedanceHandlerLifecycleAndOwnership(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	var owner int64
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		body := `{"id":"task-ark","status":"queued"}`
		if req.Method == http.MethodPost {
			owner = id
		} else {
			require.Equal(t, owner, id)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	newContext := func(method string) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformOpenAI
		body := ""
		if method == http.MethodPost {
			body = `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`
		}
		c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks", strings.NewReader(body))
		c.Params = gin.Params{{Key: "task_id", Value: "task-ark"}}
		return c, w
	}
	c, w := newContext(http.MethodPost)
	h.SeedanceTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Positive(t, owner)
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		c, w = newContext(method)
		h.SeedanceTasks(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		slots.assertReleased(t)
	}
	for _, other := range []string{"user", "key", "group", "task", "provider"} {
		c, w = newContext(http.MethodGet)
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11, Concurrency: 5})
		case "key":
			key.ID = 21
		case "group":
			group := int64(25)
			key.GroupID = &group
		case "task":
			c.Params = gin.Params{{Key: "task_id", Value: "other"}}
		case "provider":
			c.Params = gin.Params{{Key: "request_id", Value: "task-ark"}}
		}
		before := upstream.calls
		if other == "provider" {
			h.GrokVideoStatus(c)
		} else {
			h.SeedanceTasks(c)
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, before, upstream.calls)
		slots.assertReleased(t)
	}
	c, _ = newContext(http.MethodGet)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 12345}, ResponseID: "seedance:task-ark"}
	for i := range 20 {
		billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
		if i == 0 {
			require.NotNil(t, billed)
			require.Equal(t, "doubao-seedance", billed.BillingModel)
			require.Equal(t, 12345, billed.Usage.OutputTokens)
			require.Zero(t, billed.VideoCount)
		} else {
			require.Nil(t, billed)
		}
	}
	require.Len(t, bindings.billed, 1)
}

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type subscriptionQuotaRouteRepo struct {
	service.SubscriptionQuotaRepository
	calls                   []string
	userID, groupID, termID int64
	open                    domain.SubscriptionQuotaOpenParams
	cancel                  domain.SubscriptionQuotaCancelParams
	page                    domain.SubscriptionQuotaPageParams
	replayed                bool
}

func quotaRouteSummary() *domain.SubscriptionQuotaSummary {
	return &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
}

func (r *subscriptionQuotaRouteRepo) GetSummary(_ context.Context, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error) {
	r.calls = append(r.calls, "summary")
	r.userID, r.groupID = userID, groupID
	return quotaRouteSummary(), nil
}
func (r *subscriptionQuotaRouteRepo) ListSummaries(_ context.Context, userID int64) ([]domain.SubscriptionQuotaSummary, error) {
	r.calls = append(r.calls, "list")
	r.userID = userID
	return nil, nil
}
func (r *subscriptionQuotaRouteRepo) ListTerms(_ context.Context, userID, groupID int64, p domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaTermPage, error) {
	r.calls = append(r.calls, "terms")
	r.userID, r.groupID, r.page = userID, groupID, p
	return &domain.SubscriptionQuotaTermPage{}, nil
}
func (r *subscriptionQuotaRouteRepo) ListUsage(_ context.Context, userID, termID int64, p domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaUsagePage, error) {
	r.calls = append(r.calls, "usage")
	r.userID, r.termID, r.page = userID, termID, p
	if userID != 42 || termID != 50 {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	return &domain.SubscriptionQuotaUsagePage{}, nil
}
func (r *subscriptionQuotaRouteRepo) PreviewTerm(_ context.Context, p domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaPreview, error) {
	r.calls = append(r.calls, "preview")
	r.open = p
	return &domain.SubscriptionQuotaPreview{GroupID: p.GroupID, EffectiveQuotaUSD: "100.00000000"}, nil
}
func (r *subscriptionQuotaRouteRepo) OpenTerm(_ context.Context, p domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaMutationResult, error) {
	r.calls = append(r.calls, "open")
	r.open = p
	return &domain.SubscriptionQuotaMutationResult{Summary: *quotaRouteSummary(), Replayed: r.replayed}, nil
}
func (r *subscriptionQuotaRouteRepo) CancelTerm(_ context.Context, p domain.SubscriptionQuotaCancelParams) (*domain.SubscriptionQuotaMutationResult, error) {
	r.calls = append(r.calls, "cancel")
	r.cancel = p
	return &domain.SubscriptionQuotaMutationResult{Summary: *quotaRouteSummary()}, nil
}

func newSubscriptionQuotaRouteTest(t *testing.T) (*gin.Engine, *subscriptionQuotaRouteRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &subscriptionQuotaRouteRepo{}
	svc := service.NewSubscriptionQuotaService(repo, nil)
	h := &handler.Handlers{SubscriptionQuota: handler.NewSubscriptionQuotaHandler(svc), Admin: &handler.AdminHandlers{SubscriptionQuota: adminhandler.NewSubscriptionQuotaHandler(svc)}}
	auth := func(c *gin.Context) {
		role := c.GetHeader("X-Test-Role")
		if role == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Set(string(servermiddleware.ContextKeyUserRole), role)
		c.Next()
	}
	audit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	router := gin.New()
	RegisterUserRoutes(router.Group("/api/v1"), h, servermiddleware.JWTAuthMiddleware(auth), audit, nil, nil)
	RegisterAdminRoutes(router.Group("/api/v1"), h, servermiddleware.AdminAuthMiddleware(auth), audit, servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	return router, repo
}

func quotaRouteRequest(router http.Handler, method, path, body, role, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func quotaRouteData(t *testing.T, w *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, "success", envelope.Message)
	return envelope.Data
}

func TestSubscriptionQuotaRoutesAuthenticationAndRegistration(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		admin              bool
	}{
		{"GET", "/api/v1/subscription-quota", "", false},
		{"GET", "/api/v1/subscription-quota/groups/6", "", false},
		{"GET", "/api/v1/subscription-quota/groups/6/terms", "", false},
		{"GET", "/api/v1/subscription-quota/terms/50/usage", "", false},
		{"GET", "/api/v1/admin/users/77/subscription-quota?group_id=6", "", true},
		{"GET", "/api/v1/admin/users/77/subscription-quota/terms?group_id=6", "", true},
		{"POST", "/api/v1/admin/users/77/subscription-quota/terms/preview", `{"group_id":6,"operation":"open","quota_usd":"100"}`, true},
		{"POST", "/api/v1/admin/users/77/subscription-quota/terms", `{"group_id":6,"operation":"open","quota_usd":"100","expected_revision":0}`, true},
		{"POST", "/api/v1/admin/subscription-quota/terms/50/cancel", `{"expected_revision":0,"reason":"cancel this card"}`, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router, repo := newSubscriptionQuotaRouteTest(t)
			w := quotaRouteRequest(router, tc.method, tc.path, tc.body, "", "test-operation-key-01")
			require.Equal(t, 401, w.Code)
			require.Empty(t, repo.calls)
			role := "user"
			if tc.admin {
				w = quotaRouteRequest(router, tc.method, tc.path, tc.body, role, "test-operation-key-01")
				require.Equal(t, 403, w.Code)
				require.Empty(t, repo.calls)
				role = "admin"
			}
			w = quotaRouteRequest(router, tc.method, tc.path, tc.body, role, "test-operation-key-01")
			require.Contains(t, []int{200, 201}, w.Code, w.Body.String())
			require.Len(t, repo.calls, 1)
			quotaRouteData(t, w)
		})
	}
}

func TestSubscriptionQuotaRoutesStrictMutationJSON(t *testing.T) {
	router, repo := newSubscriptionQuotaRouteTest(t)
	valid := `"group_id":6,"operation":"open","quota_usd":"1","expected_revision":0`
	for _, body := range []string{
		`{` + valid + `,"user_id":999}`, `{` + valid + `,"platform":"openai"}`, `{` + valid + `,"validity_days":1}`,
		`{` + valid + `,"wallet":"balance"}`, `{` + valid + `,"notes":null}`, `{` + valid + `,"group_id":6}`,
		`{` + valid + `,"expected_revision":null}`, `{` + valid + `,"quota_usd":null,"plan_id":1}`,
		`{"group_id":6,"operation":"open","plan_id":null,"expected_revision":0}`,
		`{"group_id":6,"operation":"open","quota_usd":"1","plan_id":null,"expected_revision":0}`,
		`{"group_id":6,"operation":"open","quota_usd":null,"plan_id":1,"expected_revision":0}`,
		`{"group_id":6,"operation":"open","quota_usd":"1"}`, `{` + valid + `} {}`, `null`, `[]`,
		`{"group_id":6,"operation":"open","quota_usd":1,"expected_revision":0}`,
		`{"group_id":6,"operation":"open","quota_usd":"1","expected_revision":-1}`,
		`{"group_id":6,"operation":"open","quota_usd":"1","expected_revision":1.2}`,
		`{"group_id":6,"operation":"open","quota_usd":"1","Expected_Revision":0}`,
	} {
		t.Run(body, func(t *testing.T) {
			w := quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms", body, "admin", "test-operation-key-01")
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Empty(t, repo.calls)
		})
	}
	for _, quota := range []string{"", "0", "01", "-1", "1e2", "+1", "NaN", "Infinity", "1.123456789", "1000000000000"} {
		body := `{"group_id":6,"operation":"open","quota_usd":"` + quota + `","expected_revision":0}`
		w := quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms", body, "admin", "test-operation-key-01")
		require.Equal(t, 400, w.Code, body)
		require.Empty(t, repo.calls)
	}
}

func TestSubscriptionQuotaRoutesIdentityPreviewReplayAndDTO(t *testing.T) {
	router, repo := newSubscriptionQuotaRouteTest(t)
	previewPath := "/api/v1/admin/users/77/subscription-quota/terms/preview"
	w := quotaRouteRequest(router, "POST", previewPath, `{"group_id":6,"operation":"renew","quota_usd":"1","expected_revision":0}`, "admin", "")
	require.Equal(t, 400, w.Code)
	require.Empty(t, repo.calls)
	w = quotaRouteRequest(router, "POST", previewPath, `{"group_id":6,"operation":"renew","quota_usd":"1"}`, "admin", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, []string{"preview"}, repo.calls)
	require.EqualValues(t, 77, repo.open.UserID)
	require.EqualValues(t, 42, repo.open.ActorID)
	require.Empty(t, repo.open.IdempotencyKey)
	data := quotaRouteData(t, w)
	require.Contains(t, string(data), `"warnings":[]`)
	require.Contains(t, string(data), `"next_terms":[]`)
	body := `{"group_id":6,"operation":"open","quota_usd":"1","expected_revision":0}`
	for _, key := range []string{"", "too-short", strings.Repeat("x", 129)} {
		before := len(repo.calls)
		w = quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms", body, "admin", key)
		require.Equal(t, 400, w.Code)
		require.Len(t, repo.calls, before)
	}
	w = quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms?user_id=999", body, "admin", "test-operation-key-01")
	require.Equal(t, 201, w.Code, w.Body.String())
	require.EqualValues(t, 77, repo.open.UserID)
	require.EqualValues(t, 42, repo.open.ActorID)
	require.Equal(t, "1.00000000", *repo.open.QuotaUSD)
	require.Zero(t, repo.open.ExpectedRevision)
	repo.replayed = true
	w = quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms", body, "admin", "test-operation-key-01")
	require.Equal(t, 200, w.Code)
	require.Contains(t, string(quotaRouteData(t, w)), `"replayed":true`)
	w = quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/groups/6?user_id=999", "", "user", "")
	require.Equal(t, 200, w.Code)
	require.EqualValues(t, 42, repo.userID)
	require.EqualValues(t, 6, repo.groupID)
	data = quotaRouteData(t, w)
	require.Contains(t, string(data), `"current_term":null`)
	require.Contains(t, string(data), `"last_term":null`)
	require.Contains(t, string(data), `"next_terms":[]`)
	w = quotaRouteRequest(router, "GET", "/api/v1/subscription-quota", "", "user", "")
	require.Equal(t, "[]", string(quotaRouteData(t, w)))
	w = quotaRouteRequest(router, "POST", "/api/v1/admin/subscription-quota/terms/50/cancel", `{"expected_revision":0,"reason":"manual stop"}`, "admin", "test-cancel-key-0001")
	require.Equal(t, 200, w.Code)
	require.EqualValues(t, 50, repo.cancel.TermID)
	require.EqualValues(t, 42, repo.cancel.ActorID)
}

func TestSubscriptionQuotaRoutesPaginationAndT24Ownership(t *testing.T) {
	router, repo := newSubscriptionQuotaRouteTest(t)
	for _, suffix := range []string{"?limit=0", "?limit=-1", "?limit=101", "?limit=", "?limit=no", "?limit=1&limit=2", "?cursor=invalid"} {
		w := quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/groups/6/terms"+suffix, "", "user", "")
		require.Equal(t, 400, w.Code, suffix)
		require.Empty(t, repo.calls)
	}
	w := quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/groups/6/terms", "", "user", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, 20, repo.page.Limit)
	require.JSONEq(t, `{"items":[],"next_cursor":null,"has_more":false}`, string(quotaRouteData(t, w)))
	w = quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/groups/6/terms?limit=100&cursor="+domain.EncodeSubscriptionQuotaCursor(99), "", "user", "")
	require.Equal(t, 200, w.Code)
	require.EqualValues(t, 99, repo.page.BeforeID)
	require.Equal(t, 100, repo.page.Limit)
	w = quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/terms/50/usage?user_id=999", "", "user", "")
	require.Equal(t, 200, w.Code)
	require.EqualValues(t, 42, repo.userID)
	require.JSONEq(t, `{"items":[],"next_cursor":null,"has_more":false}`, string(quotaRouteData(t, w)))
	other := quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/terms/51/usage?user_id=999", "", "user", "")
	missing := quotaRouteRequest(router, "GET", "/api/v1/subscription-quota/terms/999/usage", "", "user", "")
	require.Equal(t, 404, other.Code)
	require.Equal(t, missing.Code, other.Code)
	require.JSONEq(t, missing.Body.String(), other.Body.String())
	for _, path := range []string{"/api/v1/admin/users/0/subscription-quota?group_id=6", "/api/v1/admin/users/77/subscription-quota", "/api/v1/subscription-quota/groups/0"} {
		before := len(repo.calls)
		w = quotaRouteRequest(router, "GET", path, "", "admin", "")
		require.Equal(t, 400, w.Code)
		require.Len(t, repo.calls, before)
	}
}

func TestSubscriptionQuotaRoutesCancelRejectsIdentityAndInvalidReason(t *testing.T) {
	router, repo := newSubscriptionQuotaRouteTest(t)
	for _, body := range []string{
		`{"expected_revision":0,"reason":"stop","user_id":999}`,
		`{"expected_revision":0,"reason":"stop","term_id":999}`,
		`{"expected_revision":null,"reason":"stop"}`,
		`{"expected_revision":0,"reason":null}`,
		`{"expected_revision":0,"reason":"   "}`,
		`{"expected_revision":0,"reason":"` + strings.Repeat("字", 501) + `"}`,
		`{"reason":"stop"}`,
	} {
		w := quotaRouteRequest(router, "POST", "/api/v1/admin/subscription-quota/terms/50/cancel", body, "admin", "test-cancel-key-0001")
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Empty(t, repo.calls)
	}
}

func TestSubscriptionQuotaRoutesServiceValidationReasons(t *testing.T) {
	router, repo := newSubscriptionQuotaRouteTest(t)
	for _, tc := range []struct{ body, reason string }{
		{`{"group_id":6,"operation":"open","expected_revision":0}`, "QUOTA_REQUIRED"},
		{`{"group_id":6,"operation":"open","quota_usd":"1e2","expected_revision":0}`, "INVALID_QUOTA"},
	} {
		w := quotaRouteRequest(router, "POST", "/api/v1/admin/users/77/subscription-quota/terms", tc.body, "admin", "test-operation-key-01")
		require.Equal(t, 400, w.Code)
		var envelope struct {
			Reason string `json:"reason"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		require.Equal(t, tc.reason, envelope.Reason)
		require.Empty(t, repo.calls)
	}
}

//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaAdministrationI4Repo struct {
	service.SubscriptionQuotaRepository
	query         domain.SubscriptionQuotaRequestQueryParams
	resolution    domain.SubscriptionQuotaResolutionParams
	reads, writes int
	replay        bool
}

func (r *quotaAdministrationI4Repo) ListRequests(_ context.Context, p domain.SubscriptionQuotaRequestQueryParams) (*domain.SubscriptionQuotaRequestPage, error) {
	r.reads++
	r.query = p
	return &domain.SubscriptionQuotaRequestPage{Items: []domain.SubscriptionQuotaRequest{{ID: 31, UserID: 11, GroupID: 6, APIKeyID: 21, TermID: 40, RequestID: "subquota:synthetic", Status: "reconcile_required"}}}, nil
}
func (r *quotaAdministrationI4Repo) ResolveRequest(_ context.Context, p domain.SubscriptionQuotaResolutionParams) (*domain.SubscriptionQuotaResolveResult, error) {
	r.writes++
	r.resolution = p
	return &domain.SubscriptionQuotaResolveResult{Request: domain.SubscriptionQuotaRequest{ID: p.RequestRowID, UserID: 11, GroupID: 6, APIKeyID: 21, TermID: 40, Status: "no_charge"}, Replayed: r.replay}, nil
}

type quotaAdministrationI4Calculator struct{}

func (quotaAdministrationI4Calculator) CalculateSubscriptionQuotaVerifiedCost(json.RawMessage, service.SubscriptionQuotaVerifiedUsage, time.Time) (*service.SubscriptionQuotaCalculatedCost, error) {
	panic("HTTP validation must not invoke pricing outside the repository scope")
}

func newQuotaAdministrationI4Router() (*gin.Engine, *quotaAdministrationI4Repo) {
	gin.SetMode(gin.TestMode)
	repo := &quotaAdministrationI4Repo{}
	svc := service.NewSubscriptionQuotaService(repo, nil)
	resolution := service.NewSubscriptionQuotaResolutionService(repo, quotaAdministrationI4Calculator{})
	admin := adminhandler.NewSubscriptionQuotaHandler(svc)
	admin.SetRequestAdministration(svc, resolution)
	h := &handler.Handlers{Admin: &handler.AdminHandlers{SubscriptionQuota: admin}}
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
	r := gin.New()
	RegisterAdminRoutes(r.Group("/api/v1"), h, middleware.AdminAuthMiddleware(auth), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	return r, repo
}

func TestSubscriptionQuotaI4RequestQueryActualRouterAndSanitizedDTO(t *testing.T) {
	r, repo := newQuotaAdministrationI4Router()
	path := "/api/v1/admin/subscription-quota/requests"
	for _, role := range []string{"", service.RoleUser} {
		w := quotaRouteRequest(r, "GET", path, "", role, "")
		require.Contains(t, []int{401, 403}, w.Code)
		require.Zero(t, repo.reads)
	}
	w := quotaRouteRequest(r, "GET", path+"?limit=1&status=reconcile_required&user_id=11&group_id=6&cursor="+domain.EncodeSubscriptionQuotaCursor(100), "", service.RoleAdmin, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(100), repo.query.BeforeID)
	require.Equal(t, 1, repo.query.Limit)
	require.Equal(t, int64(11), repo.query.UserID)
	require.Equal(t, int64(6), repo.query.GroupID)
	data := quotaRouteData(t, w)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Contains(t, out, "next_cursor")
	require.Equal(t, false, out["has_more"])
	items := out["items"].([]any)
	item := items[0].(map[string]any)
	require.Nil(t, item["actual_cost_usd"])
	require.Len(t, item, 13)
	for _, hidden := range []string{"pricing_snapshot", "billing_payload", "usage_log_payload", "resolution_evidence", "account_id"} {
		require.NotContains(t, item, hidden)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=1&limit=2", "cursor=x", "cursor=&cursor=x", "status=settling", "user_id=-1", "group_id=0", "user_id=11&user_id=12", "unknown=1"} {
		before := repo.reads
		w = quotaRouteRequest(r, "GET", path+"?"+query, "", service.RoleAdmin, "")
		require.Equal(t, 400, w.Code, query+" "+w.Body.String())
		require.Equal(t, before, repo.reads)
	}
}

func TestSubscriptionQuotaI4ResolveActualServiceStrictPresenceAndReplay(t *testing.T) {
	r, repo := newQuotaAdministrationI4Router()
	path := "/api/v1/admin/subscription-quota/requests/31/resolve"
	key := "synthetic-resolve-key-0001"
	base := `{"expected_request_revision":0,"resolution":"no_charge","evidence_reference":"synthetic upstream ticket","reason":"verified no consumption"}`
	w := quotaRouteRequest(r, "POST", path, base, service.RoleAdmin, key)
	require.Equal(t, 202, w.Code, w.Body.String())
	quotaRouteData(t, w)
	require.Equal(t, int64(42), repo.resolution.ActorID)
	require.Equal(t, int64(31), repo.resolution.RequestRowID)
	require.Zero(t, repo.resolution.ExpectedRequestRevision)
	require.Equal(t, key, repo.resolution.IdempotencyKey)
	repo.replay = true
	w = quotaRouteRequest(r, "POST", path, base, service.RoleAdmin, key)
	require.Equal(t, 200, w.Code)
	require.Contains(t, string(quotaRouteData(t, w)), `"replayed":true`)
	verified := `{"expected_request_revision":0,"resolution":"verified_usage","evidence_reference":"ticket","reason":"verified complete usage","verified_usage":{"input_tokens":0,"output_tokens":0,"cache_creation_5m_tokens":0,"cache_creation_1h_tokens":0,"cache_read_tokens":0}}`
	w = quotaRouteRequest(r, "POST", path, verified, service.RoleAdmin, key)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotNil(t, repo.resolution.PrepareKnown)
	for _, body := range []string{
		`{"resolution":"no_charge","evidence_reference":"t","reason":"r"}`,
		`{"expected_request_revision":null,"resolution":"no_charge","evidence_reference":"t","reason":"r"}`,
		`{"expected_request_revision":0,"resolution":"no_charge","evidence_reference":"t","reason":"r","cost":"1"}`,
		`{"expected_request_revision":0,"resolution":"no_charge","evidence_reference":"t","reason":"r","verified_usage":{}}`,
		`{"expected_request_revision":0,"resolution":"verified_usage","evidence_reference":"t","reason":"r","verified_usage":{"input_tokens":0}}`,
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":null`, 1),
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":-1`, 1),
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":1.5`, 1),
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":9223372036854775808`, 1),
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":0,"input_tokens":1`, 1),
		strings.Replace(verified, `"input_tokens":0`, `"input_tokens":0,"actual_cost_usd":"1"`, 1),
	} {
		before := repo.writes
		w = quotaRouteRequest(r, "POST", path, body, service.RoleAdmin, key)
		require.Equal(t, http.StatusBadRequest, w.Code, body+" "+w.Body.String())
		require.Equal(t, before, repo.writes)
	}
	before := repo.writes
	w = quotaRouteRequest(r, "POST", path, base, service.RoleUser, key)
	require.Equal(t, 403, w.Code)
	require.Equal(t, before, repo.writes)
}

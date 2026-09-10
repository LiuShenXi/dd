package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type renewalHandlerRepository struct{ service.CarpoolRepositoryAPI }

func (*renewalHandlerRepository) GetTerm(context.Context, int64) (*domain.CarpoolTerm, error) {
	return &domain.CarpoolTerm{ID: 4, UserID: 8, ScopeID: 1, ExpiresAt: time.Date(2026, 9, 11, 13, 16, 25, 123456000, time.UTC), PlanSnapshot: domain.CarpoolPlanSnapshot{Code: "four_seat", WeeklyQuotaUSD: decimal.NewFromInt(450), DurationDays: 7, WeeklyQuotaCustomized: true, DurationCustomized: true}}, nil
}
func (*renewalHandlerRepository) ListPlans(context.Context, bool) ([]domain.CarpoolPlan, error) {
	return []domain.CarpoolPlan{{ID: 2, Code: "four_seat"}}, nil
}
func (*renewalHandlerRepository) GetPreviewPlan(context.Context, int64, int64) (*domain.CarpoolPlan, error) {
	return &domain.CarpoolPlan{ID: 2, Code: "four_seat", Enabled: true, DurationDays: 28, CycleDays: 7, WeeklyQuotaUSD: decimal.NewFromInt(550)}, nil
}
func (*renewalHandlerRepository) DatabaseNow(context.Context) (time.Time, error) {
	return time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC), nil
}

func TestCarpoolRenewalPreviewHTTPBodyAndOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewCarpoolHandler(service.NewCarpoolService(&renewalHandlerRepository{}))
	router.POST("/api/v1/admin/users/:id/carpool/preview", handler.Preview)
	for _, tc := range []struct {
		name, user, body string
		status           int
	}{
		{"default renewal", "8", `{"plan_id":0,"renew_from_term_id":4,"starts_at":"2030-01-01T00:00:00Z","mode":"new","takeover":null}`, http.StatusOK},
		{"wrong owner", "9", `{"plan_id":0,"renew_from_term_id":4}`, http.StatusForbidden},
		{"opening still requires plan", "8", `{"plan_id":0}`, http.StatusBadRequest},
		{"renewal forbids takeover", "8", `{"plan_id":0,"renew_from_term_id":4,"mode":"takeover","takeover":{}}`, http.StatusBadRequest},
		{"negative source", "8", `{"plan_id":0,"renew_from_term_id":-1}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+tc.user+"/carpool/preview", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.status == http.StatusOK {
				var envelope struct {
					Data domain.CarpoolTermPreview `json:"data"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				require.Equal(t, 7, envelope.Data.Plan.DurationDays)
				require.True(t, decimal.NewFromInt(450).Equal(envelope.Data.Plan.WeeklyQuotaUSD))
				require.Equal(t, time.Date(2026, 9, 11, 13, 16, 25, 123456000, time.UTC), envelope.Data.StartsAt)
			}
		})
	}
}

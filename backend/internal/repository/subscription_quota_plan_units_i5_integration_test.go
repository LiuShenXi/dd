//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaPaymentI5_PlanDayUnitsThroughAdminAndCheckout(t *testing.T) {
	for _, unit := range []string{"days", "day"} {
		t.Run(unit, func(t *testing.T) {
			f := newQuotaPaymentI5Fixture(t)
			guard := f.moneyGuard(t)
			admin := adminhandler.NewPaymentHandler(f.pay, f.plans)
			f.router.POST("/api/v1/admin/payment/plans", admin.CreatePlan)
			f.router.PUT("/api/v1/admin/payment/plans/:id", admin.UpdatePlan)
			request := func(method, path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, r)
				return w
			}
			body := fmt.Sprintf(`{"group_id":%d,"name":"UI seven-day quota","price":9.87,"currency":"CNY","validity_days":7,"validity_unit":%q,"for_sale":true,"default_quota_usd":"125.12345678"}`, f.group, unit)
			created := request(http.MethodPost, "/api/v1/admin/payment/plans", body)
			require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
			var result struct {
				Data struct {
					ID           int64  `json:"id"`
					ValidityUnit string `json:"validity_unit"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &result))
			require.Positive(t, result.Data.ID)
			require.Equal(t, unit, result.Data.ValidityUnit)
			path := fmt.Sprintf("/api/v1/admin/payment/plans/%d", result.Data.ID)
			updated := request(http.MethodPut, path, fmt.Sprintf(`{"name":"Updated UI quota","validity_days":7,"validity_unit":%q}`, unit))
			require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
			for _, invalid := range []string{`{"validity_days":7,"validity_unit":"months"}`, `{"validity_days":7,"validity_unit":"years"}`, `{"validity_days":6,"validity_unit":"days"}`, `{"validity_days":8,"validity_unit":"day"}`} {
				rejected := request(http.MethodPut, path, invalid)
				require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
				require.Contains(t, rejected.Body.String(), "TERM_PLAN_DURATION_INVALID")
			}
			stored, err := f.plans.GetPlan(context.Background(), result.Data.ID)
			require.NoError(t, err)
			require.Equal(t, unit, stored.ValidityUnit)
			require.Equal(t, 7, stored.ValidityDays)
			listed, err := f.plans.ListPlansForSale(context.Background())
			require.NoError(t, err)
			found := false
			for _, plan := range listed {
				found = found || plan.ID == result.Data.ID
			}
			require.True(t, found, "valid UI plans must remain visible for checkout")
			order := f.create(t, result.Data.ID)
			snapshot, err := domain.ParseSubscriptionQuotaOrderSnapshot(order.SubscriptionQuotaSnapshot)
			require.NoError(t, err)
			require.Equal(t, int64(604800), snapshot.DurationSeconds)
			require.Equal(t, "Updated UI quota", snapshot.PlanName)
			require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
			f.requireCard(t, order, payment.OrderStatusCompleted)
			require.Equal(t, int32(1), f.creates.Load())
			guard()
		})
	}
}

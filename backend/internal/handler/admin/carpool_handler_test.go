package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type emptyCarpoolListRepository struct {
	service.CarpoolRepositoryAPI
}

func (*emptyCarpoolListRepository) ListAdminTerms(context.Context, domain.CarpoolTermFilters) ([]domain.CarpoolAdminTerm, int64, error) {
	return nil, 0, nil
}

func (*emptyCarpoolListRepository) ListAdminCycles(context.Context, domain.CarpoolCycleFilters) ([]domain.CarpoolAdminCycle, int64, error) {
	return nil, 0, nil
}

func (*emptyCarpoolListRepository) ListAdminLedger(context.Context, domain.CarpoolLedgerFilters) ([]domain.CarpoolLedgerEntry, int64, error) {
	return nil, 0, nil
}

func (*emptyCarpoolListRepository) ListPayments(context.Context, int64, int, int) ([]domain.CarpoolPayment, int64, error) {
	return nil, 0, nil
}

func TestCarpoolPreviewRejectsInvalidPathUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/users/-1/carpool/preview", strings.NewReader(`{"plan_id":1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "-1"}}

	NewCarpoolHandler(nil).Preview(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Invalid id")
}

func TestCarpoolListEndpointsEncodeEmptyItemsAsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewCarpoolHandler(service.NewCarpoolService(&emptyCarpoolListRepository{}))
	tests := []struct {
		name   string
		path   string
		params gin.Params
		handle func(*gin.Context)
	}{
		{name: "terms", path: "/admin/carpool/terms?user_id=26", handle: handler.Terms},
		{name: "cycles", path: "/admin/carpool/cycles", handle: handler.Cycles},
		{name: "ledger", path: "/admin/carpool/ledger", handle: handler.Ledger},
		{name: "payments", path: "/admin/carpool/terms/1/payments", params: gin.Params{{Key: "id", Value: "1"}}, handle: handler.Payments},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			ctx.Params = tc.params

			tc.handle(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var envelope struct {
				Data struct {
					Items json.RawMessage `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.JSONEq(t, `[]`, string(envelope.Data.Items))
		})
	}
}

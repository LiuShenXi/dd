//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type homeProductsCarpoolRepo struct {
	service.CarpoolRepositoryAPI
	plans []domain.CarpoolPlan
}

func (r *homeProductsCarpoolRepo) ListPlans(context.Context, bool) ([]domain.CarpoolPlan, error) {
	return r.plans, nil
}

type homeProductsSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *homeProductsSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func TestGetHomeProductsReadsRealPlans(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("sqlite", "file:home_products_handler?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	termGroup, err := client.Group.Create().SetName("Claude 周卡").SetPlatform(service.PlatformAnthropic).
		SetSubscriptionType(service.SubscriptionTypeSubscription).SetSubscriptionQuotaMode(domain.SubscriptionQuotaModeTerm).
		SetRateMultiplier(0.2).Save(ctx)
	require.NoError(t, err)
	windowsGroup, err := client.Group.Create().SetName("普通").SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	quota := "150"
	termPlan, err := client.SubscriptionPlan.Create().SetGroupID(termGroup.ID).SetName("周卡").SetPrice(100).
		SetValidityDays(7).SetValidityUnit("days").SetTermQuotaUsd(quota).Save(ctx)
	require.NoError(t, err)
	windowsPlan, err := client.SubscriptionPlan.Create().SetGroupID(windowsGroup.ID).SetName("普通").SetPrice(9).
		SetValidityDays(30).SetValidityUnit("days").Save(ctx)
	require.NoError(t, err)

	carpool := service.NewCarpoolService(&homeProductsCarpoolRepo{plans: []domain.CarpoolPlan{
		{ID: 16, Code: "four_seat", Name: "Four-seat", ListPriceCNY: decimal.NewFromInt(330), WeeklyQuotaUSD: decimal.NewFromInt(500), DurationDays: 28, CycleDays: 7, BoostRatio: decimal.RequireFromString("0.1"), BoostCount: 2, Enabled: true},
		{ID: 19, Code: "solo", Name: "Solo", ListPriceCNY: decimal.NewFromInt(999), WeeklyQuotaUSD: decimal.NewFromInt(2000), Enabled: false},
	}})
	payments := service.NewPaymentConfigService(client, nil, []byte("0123456789abcdef0123456789abcdef"))

	call := func(planID int64) map[string]any {
		repo := &homeProductsSettingRepo{values: map[string]string{
			service.SettingKeyHomeProductsSettings: `{"gpt_carpool":{"enabled":true,"status":"available","rules":["r1"]},"claude_weekly":{"enabled":true,"status":"pilot","plan_id":` + jsonInt(planID) + `,"rules":[]},"contact":"微信 x"}`,
		}}
		h := NewHomeProductsHandler(service.NewSettingService(repo, nil), carpool, payments)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/home/products", nil)
		h.GetHomeProducts(c)
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Data
	}

	data := call(termPlan.ID)
	gpt := data["gpt_carpool"].(map[string]any)
	tiers := gpt["tiers"].([]any)
	require.Len(t, tiers, 1, "disabled carpool plans are hidden")
	tier := tiers[0].(map[string]any)
	require.Equal(t, float64(4), tier["seats"])
	require.Equal(t, "330", tier["price_cny"])
	require.Equal(t, "500", tier["weekly_quota_usd"])

	weekly := data["claude_weekly"].(map[string]any)
	require.Equal(t, float64(100), weekly["price_cny"])
	require.Equal(t, "150.00000000", weekly["quota_usd"])
	require.Equal(t, 0.2, weekly["rate_multiplier"])
	require.Equal(t, "微信 x", data["contact"])

	// Non-term plans and missing plans never leak a price onto the card.
	for _, planID := range []int64{windowsPlan.ID, 999999} {
		weekly = call(planID)["claude_weekly"].(map[string]any)
		require.Nil(t, weekly["price_cny"])
		require.Nil(t, weekly["quota_usd"])
	}
}

func jsonInt(v int64) string {
	data, _ := json.Marshal(v)
	return string(data)
}

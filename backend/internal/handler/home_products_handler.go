package handler

import (
	"context"
	"log/slog"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// HomeProductsHandler 首页车次卡片（公开、无需认证）。
// 价格与额度全部读取真实套餐，展示配置只决定是否显示、状态与规则文案。
type HomeProductsHandler struct {
	settingService       *service.SettingService
	carpoolService       *service.CarpoolService
	paymentConfigService *service.PaymentConfigService
}

func NewHomeProductsHandler(settingService *service.SettingService, carpoolService *service.CarpoolService, paymentConfigService *service.PaymentConfigService) *HomeProductsHandler {
	return &HomeProductsHandler{settingService: settingService, carpoolService: carpoolService, paymentConfigService: paymentConfigService}
}

// carpoolPlanSeats 套餐编码 → 座位数，未知编码返回 0（前端改用套餐名展示）。
var carpoolPlanSeats = map[string]int{
	"two_seat":   2,
	"three_seat": 3,
	"four_seat":  4,
}

type homeCarpoolTier struct {
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	Seats          int             `json:"seats"`
	PriceCNY       decimal.Decimal `json:"price_cny"`
	WeeklyQuotaUSD decimal.Decimal `json:"weekly_quota_usd"`
	DurationDays   int             `json:"duration_days"`
	CycleDays      int             `json:"cycle_days"`
	BoostRatio     decimal.Decimal `json:"boost_ratio"`
	BoostCount     int             `json:"boost_count"`
}

type homeGPTCarpool struct {
	Enabled bool              `json:"enabled"`
	Status  string            `json:"status"`
	Rules   []string          `json:"rules"`
	Tiers   []homeCarpoolTier `json:"tiers"`
}

// homeClaudeWeekly 价格/额度为 nil 表示未关联有效套餐，前端显示「待定」。
type homeClaudeWeekly struct {
	Enabled        bool     `json:"enabled"`
	Status         string   `json:"status"`
	Rules          []string `json:"rules"`
	PlanID         int64    `json:"plan_id"`
	PlanName       string   `json:"plan_name"`
	PriceCNY       *float64 `json:"price_cny"`
	OriginalPrice  *float64 `json:"original_price"`
	QuotaUSD       *string  `json:"quota_usd"`
	RateMultiplier *float64 `json:"rate_multiplier"`
	DurationDays   int      `json:"duration_days"`
}

type homeProductsResponse struct {
	GPTCarpool   homeGPTCarpool   `json:"gpt_carpool"`
	ClaudeWeekly homeClaudeWeekly `json:"claude_weekly"`
	Contact      string           `json:"contact"`
}

// GetHomeProducts 返回首页车次卡片数据。
// GET /api/v1/home/products
func (h *HomeProductsHandler) GetHomeProducts(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.settingService.GetHomeProductsSettings(ctx)
	if response.ErrorFrom(c, err) {
		return
	}
	plans, err := h.carpoolService.Plans(ctx)
	if response.ErrorFrom(c, err) {
		return
	}

	tiers := make([]homeCarpoolTier, 0, len(plans))
	for _, plan := range plans {
		if !plan.Enabled {
			continue
		}
		tiers = append(tiers, homeCarpoolTier{
			Code:           plan.Code,
			Name:           plan.Name,
			Seats:          carpoolPlanSeats[plan.Code],
			PriceCNY:       plan.ListPriceCNY,
			WeeklyQuotaUSD: plan.WeeklyQuotaUSD,
			DurationDays:   plan.DurationDays,
			CycleDays:      plan.CycleDays,
			BoostRatio:     plan.BoostRatio,
			BoostCount:     plan.BoostCount,
		})
	}

	response.Success(c, homeProductsResponse{
		GPTCarpool: homeGPTCarpool{
			Enabled: settings.GPTCarpool.Enabled,
			Status:  settings.GPTCarpool.Status,
			Rules:   settings.GPTCarpool.Rules,
			Tiers:   tiers,
		},
		ClaudeWeekly: h.claudeWeekly(ctx, settings.ClaudeWeekly),
		Contact:      settings.Contact,
	})
}

// claudeWeekly 读取关联的 Claude 周卡套餐；套餐缺失或不是 term 周卡时只返回展示配置。
func (h *HomeProductsHandler) claudeWeekly(ctx context.Context, cfg service.HomeProductClaudeWeekly) homeClaudeWeekly {
	out := homeClaudeWeekly{Enabled: cfg.Enabled, Status: cfg.Status, Rules: cfg.Rules, PlanID: cfg.PlanID}
	if cfg.PlanID <= 0 || h.paymentConfigService == nil {
		return out
	}
	plan, err := h.paymentConfigService.GetPlan(ctx, cfg.PlanID)
	if err != nil {
		slog.Warn("home products: claude weekly plan unavailable", "plan_id", cfg.PlanID, "error", err)
		return out
	}
	info := h.paymentConfigService.GetGroupInfoMap(ctx, []*dbent.SubscriptionPlan{plan})[plan.GroupID]
	if info.QuotaMode != domain.SubscriptionQuotaModeTerm {
		slog.Warn("home products: claude weekly plan is not a term plan", "plan_id", cfg.PlanID)
		return out
	}
	price := plan.Price
	rate := info.RateMultiplier
	out.PlanName = plan.Name
	out.PriceCNY = &price
	out.OriginalPrice = plan.OriginalPrice
	out.QuotaUSD = service.PlanDefaultQuotaUSD(plan)
	out.RateMultiplier = &rate
	out.DurationDays = plan.ValidityDays
	return out
}

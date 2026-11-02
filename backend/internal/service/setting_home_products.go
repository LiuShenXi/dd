package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

// HomeProductsSettings 首页车次卡片的展示配置（JSON 存于 settings 表）。
// 价格与额度不在这里：GPT 拼车取 carpool_plans 最新版本，Claude 周卡取
// PlanID 指向的 subscription_plans（price / term_quota_usd）及其分组倍率，
// 与开通会员时读取的是同一份数据。
type HomeProductsSettings struct {
	GPTCarpool   HomeProductGPTCarpool   `json:"gpt_carpool"`
	ClaudeWeekly HomeProductClaudeWeekly `json:"claude_weekly"`
	// Contact 卡片下方的开通联系方式文案，空则前端使用默认文案
	Contact string `json:"contact"`
}

// HomeProductGPTCarpool GPT 拼车卡片展示配置。
type HomeProductGPTCarpool struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
	// Rules 卡片规则要点，空则前端使用默认文案
	Rules []string `json:"rules"`
}

// HomeProductClaudeWeekly Claude 周卡卡片展示配置。
type HomeProductClaudeWeekly struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
	// PlanID 关联的 Claude 周卡订阅套餐（term 模式），0 表示未关联
	PlanID int64    `json:"plan_id"`
	Rules  []string `json:"rules"`
}

const (
	HomeProductStatusAvailable = "available"
	HomeProductStatusPilot     = "pilot"
	HomeProductStatusSoon      = "soon"
	HomeProductStatusSoldOut   = "sold_out"

	homeProductRulesMax     = 10
	homeProductRuleMaxRunes = 120
	homeProductContactMax   = 200
)

var homeProductStatuses = map[string]struct{}{
	HomeProductStatusAvailable: {},
	HomeProductStatusPilot:     {},
	HomeProductStatusSoon:      {},
	HomeProductStatusSoldOut:   {},
}

// DefaultHomeProductsSettings 返回默认首页车次展示配置。
func DefaultHomeProductsSettings() *HomeProductsSettings {
	return &HomeProductsSettings{
		GPTCarpool: HomeProductGPTCarpool{
			Enabled: true,
			Status:  HomeProductStatusAvailable,
			Rules:   []string{},
		},
		ClaudeWeekly: HomeProductClaudeWeekly{
			Enabled: true,
			Status:  HomeProductStatusPilot,
			Rules:   []string{},
		},
	}
}

// normalizeHomeProductRules 去除首尾空白并丢弃空行。
func normalizeHomeProductRules(rules []string) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		if trimmed := strings.TrimSpace(rule); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ValidateHomeProductsSettings 规范化并校验配置，非法时返回错误。
func ValidateHomeProductsSettings(s *HomeProductsSettings) error {
	if s == nil {
		return errors.New("settings cannot be nil")
	}
	s.Contact = strings.TrimSpace(s.Contact)
	if utf8.RuneCountInString(s.Contact) > homeProductContactMax {
		return fmt.Errorf("contact must be at most %d characters", homeProductContactMax)
	}

	s.GPTCarpool.Rules = normalizeHomeProductRules(s.GPTCarpool.Rules)
	s.ClaudeWeekly.Rules = normalizeHomeProductRules(s.ClaudeWeekly.Rules)
	for _, rules := range [][]string{s.GPTCarpool.Rules, s.ClaudeWeekly.Rules} {
		if len(rules) > homeProductRulesMax {
			return fmt.Errorf("at most %d rules per product", homeProductRulesMax)
		}
		for _, rule := range rules {
			if utf8.RuneCountInString(rule) > homeProductRuleMaxRunes {
				return fmt.Errorf("each rule must be at most %d characters", homeProductRuleMaxRunes)
			}
		}
	}

	for _, status := range []string{s.GPTCarpool.Status, s.ClaudeWeekly.Status} {
		if _, ok := homeProductStatuses[status]; !ok {
			return fmt.Errorf("invalid product status %q", status)
		}
	}
	if s.ClaudeWeekly.PlanID < 0 {
		return errors.New("claude weekly plan id cannot be negative")
	}
	return nil
}

// GetHomeProductsSettings 读取首页车次展示配置。缺失/空/解析失败 → 默认配置。
func (s *SettingService) GetHomeProductsSettings(ctx context.Context) (*HomeProductsSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyHomeProductsSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultHomeProductsSettings(), nil
		}
		return nil, fmt.Errorf("get home products settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return DefaultHomeProductsSettings(), nil
	}

	settings := DefaultHomeProductsSettings()
	if err := json.Unmarshal([]byte(value), settings); err != nil {
		slog.Warn("failed to unmarshal home products settings, falling back to defaults",
			"error", err, "key", SettingKeyHomeProductsSettings)
		return DefaultHomeProductsSettings(), nil
	}
	if err := ValidateHomeProductsSettings(settings); err != nil {
		slog.Warn("stored home products settings are invalid, falling back to defaults",
			"error", err, "key", SettingKeyHomeProductsSettings)
		return DefaultHomeProductsSettings(), nil
	}
	return settings, nil
}

// SetHomeProductsSettings 校验并保存首页车次展示配置。
func (s *SettingService) SetHomeProductsSettings(ctx context.Context, settings *HomeProductsSettings) error {
	if err := ValidateHomeProductsSettings(settings); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal home products settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyHomeProductsSettings, string(data))
}

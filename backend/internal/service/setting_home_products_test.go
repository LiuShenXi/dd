package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHomeProductsSettingsDefaultsWhenMissing(t *testing.T) {
	svc := NewSettingService(&panelRateLimitSettingRepo{}, nil)

	settings, err := svc.GetHomeProductsSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultHomeProductsSettings(), settings)
}

func TestHomeProductsSettingsRoundTrip(t *testing.T) {
	repo := &panelRateLimitSettingRepo{}
	svc := NewSettingService(repo, nil)

	in := &HomeProductsSettings{
		GPTCarpool:   HomeProductGPTCarpool{Enabled: true, Status: HomeProductStatusAvailable, Rules: []string{"  每 7 天补满  ", "", " "}},
		ClaudeWeekly: HomeProductClaudeWeekly{Enabled: false, Status: HomeProductStatusSoldOut, PlanID: 12, Rules: []string{"7×24 小时"}},
		Contact:      "  微信 abc  ",
	}
	require.NoError(t, svc.SetHomeProductsSettings(context.Background(), in))

	out, err := svc.GetHomeProductsSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"每 7 天补满"}, out.GPTCarpool.Rules)
	require.Equal(t, int64(12), out.ClaudeWeekly.PlanID)
	require.False(t, out.ClaudeWeekly.Enabled)
	require.Equal(t, HomeProductStatusSoldOut, out.ClaudeWeekly.Status)
	require.Equal(t, "微信 abc", out.Contact)
}

func TestHomeProductsSettingsRejectsInvalid(t *testing.T) {
	svc := NewSettingService(&panelRateLimitSettingRepo{}, nil)
	valid := func() *HomeProductsSettings { return DefaultHomeProductsSettings() }

	cases := map[string]func(*HomeProductsSettings){
		"status":  func(s *HomeProductsSettings) { s.GPTCarpool.Status = "free" },
		"plan id": func(s *HomeProductsSettings) { s.ClaudeWeekly.PlanID = -1 },
		"contact": func(s *HomeProductsSettings) { s.Contact = strings.Repeat("长", homeProductContactMax+1) },
		"rule len": func(s *HomeProductsSettings) {
			s.ClaudeWeekly.Rules = []string{strings.Repeat("长", homeProductRuleMaxRunes+1)}
		},
		"rule count": func(s *HomeProductsSettings) {
			s.GPTCarpool.Rules = make([]string, homeProductRulesMax+1)
			for i := range s.GPTCarpool.Rules {
				s.GPTCarpool.Rules[i] = "x"
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid()
			mutate(s)
			require.Error(t, svc.SetHomeProductsSettings(context.Background(), s))
		})
	}
}

func TestHomeProductsSettingsFallsBackOnCorruptValue(t *testing.T) {
	repo := &panelRateLimitSettingRepo{values: map[string]string{SettingKeyHomeProductsSettings: `{"gpt_carpool":{"status":"bogus"}}`}}
	svc := NewSettingService(repo, nil)

	settings, err := svc.GetHomeProductsSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultHomeProductsSettings(), settings)
}

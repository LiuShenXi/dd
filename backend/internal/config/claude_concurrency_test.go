package config

import (
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClaudeSubscriptionConcurrencyIndependentDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	require.Equal(t, 3, viper.GetInt("default.claude_subscription_concurrency"))
	viper.Set("default.user_concurrency", 17)
	require.Equal(t, 3, viper.GetInt("default.claude_subscription_concurrency"))
	viper.Set("default.claude_subscription_concurrency", 0)
	require.Zero(t, viper.GetInt("default.claude_subscription_concurrency"), "explicit invalid config is preserved for term fail-closed checks, never inherited from GPT")
}

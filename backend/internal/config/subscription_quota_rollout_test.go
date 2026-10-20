//go:build unit

package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionQuotaRolloutEnvironmentReplicaRoster(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID", "app-1")
	t.Setenv("SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS", "app-1,app-2")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "app-1", cfg.SubscriptionQuota.RolloutReplicaID)
	require.Equal(t, []string{"app-1", "app-2"}, cfg.SubscriptionQuota.RolloutExpectedReplicaIDs)
}

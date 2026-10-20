//go:build unit

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionQuotaRolloutReplicaRosterAndCapabilities(t *testing.T) {
	policy := domain.SubscriptionQuotaRolloutPolicy{ExpectedReplicaIDs: []string{"app-2", "app-1"}}
	a := domain.SubscriptionQuotaReplica{ReplicaID: "app-1", CapabilityVersion: 1, ReceiptVersion: 1, LeaseProtocolVersion: 1, ClaudeDefaultConcurrency: 3, IssueEnabled: true, AdmissionEnabled: true, RecoveryEnabled: true, ProtectedGroupIDs: []int64{6}, EnabledGroupIDs: []int64{6}, ExpectedReplicaIDs: []string{"app-1", "app-2"}}
	b := a
	b.ReplicaID = "app-2"
	require.Empty(t, rolloutReplicaBlockers([]domain.SubscriptionQuotaReplica{a, b}, 6, policy))
	require.Contains(t, rolloutReplicaBlockers([]domain.SubscriptionQuotaReplica{a}, 6, policy), "REPLICA_MISSING_OR_STALE")
	require.Contains(t, rolloutReplicaBlockers([]domain.SubscriptionQuotaReplica{a, a, b}, 6, policy), "REPLICA_DUPLICATE")
	b.EnabledGroupIDs = []int64{6, 7}
	require.Contains(t, rolloutReplicaBlockers([]domain.SubscriptionQuotaReplica{a, b}, 6, policy), "REPLICA_CONFIG_MISMATCH")
	require.Equal(t, []string{"REPLICA_ROSTER_UNCONFIGURED"}, rolloutReplicaBlockers(nil, 6, domain.SubscriptionQuotaRolloutPolicy{}))
}

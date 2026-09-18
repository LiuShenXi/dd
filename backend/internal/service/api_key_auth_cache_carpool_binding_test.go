package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotPreservesPerUserBillingIsolation(t *testing.T) {
	svc := &APIKeyService{}
	groupID := int64(2)
	var entries []*APIKeyAuthCacheEntry
	for _, billing := range []string{SubscriptionTypeCarpool, SubscriptionTypeStandard} {
		key := &APIKey{ID: int64(len(entries) + 1), UserID: int64(len(entries) + 1), GroupID: &groupID,
			User: &User{ID: int64(len(entries) + 1), Balance: 550},
			Group: &Group{ID: groupID, SubscriptionType: billing, Platform: PlatformOpenAI, RateMultiplier: 0.8,
				ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.1"}}}}
		encoded, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
		require.NoError(t, err)
		var decoded APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		entries = append(entries, &decoded)
	}
	for _, index := range []int{0, 1, 0, 1} {
		key, used, err := svc.applyAuthCacheEntry("test", entries[index])
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, index == 0, key.Group.IsCarpoolType())
		require.Equal(t, groupID, *key.GroupID)
		require.Equal(t, 0.8, key.Group.RateMultiplier)
		require.True(t, key.Group.ModelAllowlistEnabled(), "v25 must retain allowlist enforcement alongside carpool projection")
		require.True(t, key.Group.ModelAllowlist.Allows("gpt-5.1"))
		require.False(t, key.Group.ModelAllowlist.Allows("gpt-4.1"))
		key.Group.SubscriptionType = "mutated-request-copy"
	}
	entries[0].Snapshot.Version = apiKeyAuthSnapshotVersion - 1
	_, used, err := svc.applyAuthCacheEntry("test", entries[0])
	require.NoError(t, err)
	require.False(t, used, "both upstream and custom v24 snapshots must be reloaded")
}

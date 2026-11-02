//go:build integration

package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/backgroundjobs"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBackgroundJobLeadershipRealPostgresHandoffAndLoss(t *testing.T) {
	db := newUpgrade026Database(t)
	ctx := context.Background()
	blue := backgroundjobs.New("coordinated", backgroundjobs.PostgresLocker{DB: db})
	green := backgroundjobs.New("coordinated", backgroundjobs.PostgresLocker{DB: db})
	defer blue.Hold()
	defer green.Hold()
	require.NoError(t, blue.Activate(ctx, blue.Status().InstanceID))
	require.ErrorIs(t, green.Activate(ctx, green.Status().InstanceID), backgroundjobs.ErrLeadership)
	job, finish, ok := blue.Enter(ctx)
	require.True(t, ok)
	blue.Hold()
	require.NoError(t, job.Err())
	require.ErrorIs(t, green.Activate(ctx, green.Status().InstanceID), backgroundjobs.ErrLeadership)
	finish()
	require.NoError(t, green.Activate(ctx, green.Status().InstanceID))
	job, finish, ok = green.Enter(ctx)
	require.True(t, ok)
	defer finish()
	// Only this test's separate synthetic database is queried/terminated.
	var killed bool
	require.NoError(t, db.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND pid<>pg_backend_pid() LIMIT 1`).Scan(&killed))
	require.True(t, killed)
	require.Eventually(t, func() bool { return job.Err() != nil }, 7*time.Second, 10*time.Millisecond)
	require.Equal(t, "held", green.Status().State)
	_, _, ok = green.Enter(ctx)
	require.False(t, ok)
	finish()
	require.False(t, green.Status().Leader)
	require.NoError(t, blue.Activate(ctx, blue.Status().InstanceID))
}

func (s *ConcurrencyCacheSuite) TestBlueGreenPreserveStartupKeepsLiveOwnersAndWaits() {
	t := s.T()
	const accountID, userID, apiKeyID int64 = 9801, 9802, 9803
	ok, err := s.cache.AcquireAccountSlot(s.ctx, accountID, 1, "blue-process-account")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.cache.AcquireUserSlot(s.ctx, userID, 1, "blue-process-user")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.rawCache.AcquireOpenAIWSIngressLease(s.ctx, apiKeyID, 1, "blue-process-ws")
	require.NoError(t, err)
	require.True(t, ok)
	waitKeys := []string{fmt.Sprint(accountWaitKeyPrefix, accountID), fmt.Sprint(waitQueueKeyPrefix, userID), fmt.Sprint(accountWaitKeyPrefix, 9988)}
	for _, key := range waitKeys {
		require.NoError(t, s.rdb.Set(s.ctx, key, "2", time.Minute).Err())
	}
	// The missing one-time marker is deliberate: preserve must skip that global
	// legacy sweep as well as the per-account/user cleanup Lua scripts.
	require.Zero(t, s.rdb.Exists(s.ctx, legacyWaitSweepMarkerKey).Val())
	cfg := &config.Config{}
	cfg.Gateway.ConcurrencyStartupCleanupMode = "preserve"
	service.ProvideConcurrencyService(s.cache, nil, nil, cfg)
	require.Zero(t, s.rdb.Exists(s.ctx, legacyWaitSweepMarkerKey).Val())
	for _, key := range waitKeys {
		require.Equal(t, "2", s.rdb.Get(s.ctx, key).Val())
	}
	require.Equal(t, []string{"blue-process-account"}, s.rdb.ZRange(s.ctx, fmt.Sprint(accountSlotKeyPrefix, accountID), 0, -1).Val())
	require.Equal(t, []string{"blue-process-user"}, s.rdb.ZRange(s.ctx, fmt.Sprint(userSlotKeyPrefix, userID), 0, -1).Val())
	require.Equal(t, []string{"blue-process-ws"}, s.rdb.ZRange(s.ctx, openAIWSIngressLeaseKey(apiKeyID), 0, -1).Val())
	ok, err = s.cache.AcquireAccountSlot(s.ctx, accountID, 1, "green-process-account")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.cache.AcquireUserSlot(s.ctx, userID, 1, "green-process-user")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, s.cache.CleanupExpiredAccountSlotKeys(s.ctx))
	require.Equal(t, []string{"blue-process-account"}, s.rdb.ZRange(s.ctx, fmt.Sprint(accountSlotKeyPrefix, accountID), 0, -1).Val())
}

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/backgroundjobs"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// The held candidate must reach no repositories/providers. Nil dependencies
// intentionally fail loudly if a worker bypasses its claim gate.
func TestAutonomousWorkersDoNotClaimWhileCandidateHeld(t *testing.T) {
	gate := backgroundjobs.New("coordinated", nil)
	t.Run("backup_cron", func(t *testing.T) {
		(&BackupService{backgroundJobs: gate, bgCtx: context.Background()}).runScheduledBackup()
	})
	t.Run("scheduled", func(t *testing.T) { (&ScheduledTestRunnerService{backgroundJobs: gate}).runDue() })
	t.Run("channel", func(t *testing.T) {
		(&ChannelMonitorRunner{backgroundJobs: gate}).fire(context.Background(), &scheduledMonitor{id: 1})
	})
	t.Run("cn", func(t *testing.T) { (&CNProviderBalanceCheckService{backgroundJobs: gate}).runOnce() })
	t.Run("batch_cleanup", func(t *testing.T) {
		svc := &BatchImageCleanupService{backgroundJobs: gate, Repo: &heldBatchRepo{}}
		result, err := svc.RunOnce(context.Background(), time.Now())
		require.NoError(t, err)
		require.Equal(t, BatchImageCleanupRunResult{}, result)
	})
	require.Zero(t, gate.Status().ActiveTasks)
}

type heldBatchRepo struct{ BatchImageRepository }

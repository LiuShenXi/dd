package releasedrain

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/backgroundjobs"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseDrainWaitsForBackgroundAndCancelRestoresPriorState(t *testing.T) {
	gate := backgroundjobs.New("legacy", nil)
	c := New(false, 4, nil)
	c.SetBackgroundGate(gate)
	ctx, done, ok := gate.Enter(context.Background())
	require.True(t, ok)
	status, err := c.Drain()
	require.NoError(t, err)
	require.Equal(t, "held", status.BackgroundJobs.State)
	require.Equal(t, int64(1), status.BackgroundJobs.ActiveTasks)
	require.NoError(t, ctx.Err())
	require.ErrorIs(t, c.LockMigration(status.OperationID), ErrBusy)
	require.ErrorIs(t, c.ActivateBackground(context.Background(), gate.Status().InstanceID), ErrState)
	require.NoError(t, c.Cancel(status.OperationID))
	require.Equal(t, "active", gate.Status().State)
	done()
	// A candidate that was already held must not activate just because its HTTP
	// drain was cancelled (the legacy predecessor can still be alive).
	gate.Hold()
	status, err = c.Drain()
	require.NoError(t, err)
	require.NoError(t, c.Cancel(status.OperationID))
	require.Equal(t, "held", gate.Status().State)
}

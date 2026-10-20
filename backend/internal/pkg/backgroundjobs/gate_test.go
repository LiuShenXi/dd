package backgroundjobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testLocker struct {
	mu      sync.Mutex
	current *testLease
	err     error
}
type testLease struct {
	owner *testLocker
	lost  chan struct{}
	once  sync.Once
}

func (l *testLocker) Acquire(context.Context) (Lease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	if l.current != nil {
		return nil, ErrLeadership
	}
	p := &testLease{owner: l, lost: make(chan struct{})}
	l.current = p
	return p, nil
}
func (l *testLease) Lost() <-chan struct{} { return l.lost }
func (l *testLease) Release() {
	l.once.Do(func() { l.owner.mu.Lock(); l.owner.current = nil; l.owner.mu.Unlock() })
}

func TestGateHandoffKeepsLeadershipUntilLastTaskCompletes(t *testing.T) {
	lock := &testLocker{}
	blue, green := New("coordinated", lock), New("coordinated", lock)
	ctx := context.Background()
	_, _, ok := green.Enter(ctx)
	require.False(t, ok)
	require.NoError(t, blue.Activate(ctx, blue.Status().InstanceID))
	job, finish, ok := blue.Enter(ctx)
	require.True(t, ok)
	// Counts a pool-queued task too; completion cannot be double-counted.
	_, finishQueued, ok := blue.Enter(ctx)
	require.True(t, ok)
	require.ErrorIs(t, green.Activate(ctx, green.Status().InstanceID), ErrLeadership)
	require.False(t, blue.Hold())
	require.NoError(t, job.Err(), "normal drain must not cancel active work")
	_, _, ok = blue.Enter(ctx)
	require.False(t, ok)
	require.Equal(t, int64(2), blue.Status().ActiveTasks)
	finish()
	finish()
	require.ErrorIs(t, green.Activate(ctx, green.Status().InstanceID), ErrLeadership)
	finishQueued()
	require.False(t, blue.Status().Leader)
	require.NoError(t, green.Activate(ctx, green.Status().InstanceID))
	require.True(t, green.Status().Leader)
	green.Hold()
}

func TestGateFailureIdentityAndLossRemainHeld(t *testing.T) {
	lock := &testLocker{err: errors.New("database unavailable")}
	gate := New("coordinated", lock)
	ctx := context.Background()
	require.ErrorIs(t, gate.Activate(ctx, "other process"), ErrIdentity)
	require.ErrorIs(t, gate.Activate(ctx, gate.Status().InstanceID), ErrLeadership)
	require.Equal(t, "held", gate.Status().State)
	lock.err = nil
	require.NoError(t, gate.Activate(ctx, gate.Status().InstanceID))
	job, finish, ok := gate.Enter(ctx)
	require.True(t, ok)
	close(lock.current.lost)
	require.Eventually(t, func() bool { return job.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, "held", gate.Status().State)
	require.False(t, gate.Status().Leader)
	require.ErrorIs(t, gate.Activate(ctx, gate.Status().InstanceID), ErrLeadership)
	_, _, ok = gate.Enter(ctx)
	require.False(t, ok)
	finish()
	require.Zero(t, gate.Status().ActiveTasks)
	require.False(t, gate.Status().Leader)
}

func TestGateLegacyAndCancelRetainExistingWork(t *testing.T) {
	for _, mode := range []string{"legacy", "coordinated"} {
		t.Run(mode, func(t *testing.T) {
			gate := New(mode, &testLocker{})
			require.NoError(t, gate.Activate(context.Background(), gate.Status().InstanceID))
			job, done, ok := gate.Enter(context.Background())
			require.True(t, ok)
			gate.Hold()
			require.NoError(t, gate.Activate(context.Background(), gate.Status().InstanceID))
			require.Equal(t, "active", gate.Status().State)
			require.NoError(t, job.Err())
			done()
			gate.Hold()
		})
	}
}

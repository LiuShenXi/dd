package releasedrain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDrainWaitsHTTPAndUsageThenFailsClosed(t *testing.T) {
	var usage atomic.Int64
	c := New(false, 2, usage.Load)
	finish, err := c.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.Drain()
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(c.LockMigration(status.OperationID), ErrBusy) {
		t.Fatal("locked active request")
	}
	usage.Add(1)
	finish()
	finish()
	if !errors.Is(c.LockMigration(status.OperationID), ErrBusy) {
		t.Fatal("locked pending usage")
	}
	usage.Add(-1)
	if err := c.LockMigration(status.OperationID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(c.Cancel(status.OperationID), ErrState) {
		t.Fatal("cancel reopened migration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Enter(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if c.Status().State != "migrating" || c.Status().QueuedHTTP != 0 {
		t.Fatal(c.Status())
	}
	if err := c.Resume(context.Background(), status.OperationID, func(context.Context) error { return errors.New("redis down") }); err == nil {
		t.Fatal("refresh failure ignored")
	}
	if c.Status().State != "migrating" {
		t.Fatal("refresh failure opened gate")
	}
	if err := c.Resume(context.Background(), status.OperationID, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	finish, err = c.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finish()
	if c.Status().ActiveHTTP != 0 {
		t.Fatal(c.Status())
	}
}

func TestQueueBoundCancelAndStaleOperation(t *testing.T) {
	c := New(false, 1, nil)
	first, _ := c.Drain()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan error, 1)
	go func() {
		done, err := c.Enter(ctx)
		if done != nil {
			done()
		}
		entered <- err
	}()
	deadline := time.Now().Add(time.Second)
	for c.Status().QueuedHTTP != 1 {
		if time.Now().After(deadline) {
			t.Fatal("request not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Enter(context.Background()); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	if err := c.Cancel(first.OperationID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-entered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not release queued request")
	}
	second, _ := c.Drain()
	if second.OperationID == first.OperationID {
		t.Fatal("operation id reused")
	}
	if !errors.Is(c.Cancel(first.OperationID), ErrState) {
		t.Fatal("stale cancel accepted")
	}
}

func TestRestartAndExpiredResumeRemainHeld(t *testing.T) {
	c := New(true, 1, nil)
	status := c.Status()
	if status.State != "migrating" || status.OperationID == "" {
		t.Fatal(status)
	}
	if !errors.Is(c.Cancel(status.OperationID), ErrState) {
		t.Fatal("startup hold canceled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Resume(ctx, status.OperationID, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if c.Status().State != "migrating" {
		t.Fatal(c.Status())
	}
}

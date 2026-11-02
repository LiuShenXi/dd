// Package backgroundjobs coordinates autonomous external jobs during releases.
// Durable billing recovery and outbox consumers retain their own claim protocols.
package backgroundjobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
)

var (
	ErrBusy       = errors.New("background jobs are still active")
	ErrIdentity   = errors.New("background instance does not match")
	ErrLeadership = errors.New("background leadership is unavailable")
)

// Lease owns one database session for its entire tenure, including draining work.
// Lost closes if that session can no longer be confirmed alive.
type Lease interface {
	Lost() <-chan struct{}
	Release()
}
type Locker interface {
	Acquire(context.Context) (Lease, error)
}

type Status struct {
	Mode        string `json:"mode"`
	State       string `json:"state"`
	ActiveTasks int64  `json:"active_tasks"`
	InstanceID  string `json:"instance_id"`
	Leader      bool   `json:"leader"`
}

type Gate struct {
	mu               sync.Mutex
	mode, instanceID string
	held             bool
	active           int64
	locker           Locker
	lease            Lease
	leadership       context.Context
	cancel           context.CancelFunc
}

func New(mode string, locker Locker) *Gate {
	if mode == "" {
		mode = "legacy"
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return &Gate{mode: mode, instanceID: hex.EncodeToString(id[:]), held: mode == "coordinated", locker: locker}
}

func (g *Gate) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := "active"
	if g.held {
		state = "held"
	}
	leader := g.lease != nil && g.leadership != nil && g.leadership.Err() == nil
	return Status{g.mode, state, g.active, g.instanceID, leader}
}

// Activate is explicit: in particular a new binary must not compete with an old
// binary that predates this protocol. Operators retire that process first.
func (g *Gate) Activate(ctx context.Context, instanceID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if instanceID != g.instanceID {
		return ErrIdentity
	}
	if !g.held {
		return nil
	}
	if g.mode == "coordinated" {
		if g.lease != nil {
			if g.leadership.Err() != nil {
				return ErrLeadership
			}
			g.held = false
			return nil
		}
		if g.active != 0 {
			return ErrBusy
		}
		if g.locker == nil {
			return ErrLeadership
		}
		lease, err := g.locker.Acquire(ctx)
		if err != nil {
			return errors.Join(ErrLeadership, err)
		}
		g.lease = lease
		g.leadership, g.cancel = context.WithCancel(context.Background())
		go g.watch(lease, g.leadership)
	}
	g.held = false
	return nil
}

func (g *Gate) watch(lease Lease, ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-lease.Lost():
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lease != lease {
		return
	}
	// Loss is fail-closed. Existing external calls receive cancellation; a failed
	// response is never treated as proof that a remote side effect did not occur.
	g.held = true
	g.cancel()
	slog.Error("background job leadership lost; claims held")
	if g.active == 0 {
		g.releaseLocked()
	}
}

// Hold stops new claims and lets existing work finish under the same leader lock.
// It returns the previous held state so release cancellation can restore it.
func (g *Gate) Hold() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	wasHeld := g.held
	g.held = true
	if g.active == 0 {
		g.releaseLocked()
	}
	return wasHeld
}

// Enter accounts queued as well as running work. Call it before submitting to a
// pool, and always finish on submission failure. Nil preserves isolated tests.
func (g *Gate) Enter(ctx context.Context) (context.Context, func(), bool) {
	if g == nil {
		return ctx, func() {}, true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held || ctx.Err() != nil {
		return ctx, nil, false
	}
	if g.mode == "coordinated" && (g.lease == nil || g.leadership.Err() != nil) {
		return ctx, nil, false
	}
	jobCtx, cancel := context.WithCancel(ctx)
	var stop func() bool
	if g.leadership != nil {
		stop = context.AfterFunc(g.leadership, cancel)
	}
	g.active++
	var once sync.Once
	return jobCtx, func() {
		once.Do(func() {
			if stop != nil {
				stop()
			}
			cancel()
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			if g.held && g.active == 0 {
				g.releaseLocked()
			}
		})
	}, true
}

func (g *Gate) releaseLocked() {
	lease := g.lease
	g.lease = nil
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
		g.leadership = nil
	}
	if lease != nil {
		lease.Release()
	}
}

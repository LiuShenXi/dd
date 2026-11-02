// Package releasedrain coordinates a single serving process during a ledger takeover.
package releasedrain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/backgroundjobs"
)

var (
	ErrState     = errors.New("release operation or state does not match")
	ErrBusy      = errors.New("requests or usage tasks are still active")
	ErrQueueFull = errors.New("release request queue is full")
)

type Status struct {
	OperationID    string                 `json:"operation_id"`
	State          string                 `json:"state"`
	ActiveHTTP     int64                  `json:"active_http"`
	PendingUsage   int64                  `json:"pending_usage"`
	QueuedHTTP     int64                  `json:"queued_http"`
	BackgroundJobs *backgroundjobs.Status `json:"background_jobs,omitempty"`
}

type Controller struct {
	mu                sync.Mutex
	operationID       string
	state             string
	active            int64
	queued            int64
	queueLimit        int64
	changed           chan struct{}
	pendingUsage      func() int64
	background        *backgroundjobs.Gate
	backgroundWasHeld bool
}

func New(startHeld bool, queueLimit int64, pendingUsage func() int64) *Controller {
	c := &Controller{state: "open", queueLimit: queueLimit, changed: make(chan struct{}), pendingUsage: pendingUsage}
	if startHeld {
		// A restarted takeover process cannot know whether the external commit succeeded.
		c.state = "migrating"
		c.operationID = newOperationID()
	}
	return c
}

// SetBackgroundGate attaches the process-wide autonomous job controller before serving.
func (c *Controller) SetBackgroundGate(g *backgroundjobs.Gate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.background = g
	if g != nil {
		c.backgroundWasHeld = g.Status().State == "held"
		if c.state != "open" {
			g.Hold()
		}
	}
}

func (c *Controller) ActivateBackground(ctx context.Context, instanceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != "open" || c.background == nil {
		return ErrState
	}
	return c.background.Activate(ctx, instanceID)
}

func (c *Controller) backgroundActive() int64 {
	if c.background == nil {
		return 0
	}
	return c.background.Status().ActiveTasks
}

func (c *Controller) restoreBackground(ctx context.Context) error {
	if c.background == nil || c.backgroundWasHeld {
		return nil
	}
	return c.background.Activate(ctx, c.background.Status().InstanceID)
}

func newOperationID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

func (c *Controller) usage() int64 {
	if c.pendingUsage == nil {
		return 0
	}
	return c.pendingUsage()
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := Status{OperationID: c.operationID, State: c.state, ActiveHTTP: c.active, PendingUsage: c.usage(), QueuedHTTP: c.queued}
	if c.background != nil {
		bg := c.background.Status()
		status.BackgroundJobs = &bg
	}
	return status
}

// Enter counts the complete handler lifetime, including upgraded WebSocket sessions.
// The serving process must be the only ingress writer; external job producers must
// be disabled and drained separately before LockMigration.
func (c *Controller) Enter(ctx context.Context) (func(), error) {
	c.mu.Lock()
	waiting := false
	defer func() {
		if waiting {
			c.queued--
		}
		c.mu.Unlock()
	}()
	for c.state != "open" {
		if !waiting {
			if c.queued >= c.queueLimit {
				return nil, ErrQueueFull
			}
			c.queued++
			waiting = true
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.mu.Lock()
			return nil, ctx.Err()
		case <-changed:
			c.mu.Lock()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.active++
	var once sync.Once
	return func() { once.Do(func() { c.mu.Lock(); c.active--; c.mu.Unlock() }) }, nil
}

func (c *Controller) Drain() (Status, error) {
	c.mu.Lock()
	if c.state != "open" {
		c.mu.Unlock()
		return c.Status(), ErrState
	}
	c.state = "draining"
	if c.background != nil {
		c.backgroundWasHeld = c.background.Hold()
	}
	c.operationID = newOperationID()
	c.mu.Unlock()
	return c.Status(), nil
}

func (c *Controller) LockMigration(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || id != c.operationID || c.state != "draining" {
		return ErrState
	}
	if c.active != 0 || c.usage() != 0 || c.backgroundActive() != 0 {
		return ErrBusy
	}
	c.state = "migrating"
	return nil
}

func (c *Controller) Cancel(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || id != c.operationID || c.state != "draining" {
		return ErrState
	}
	if err := c.restoreBackground(context.Background()); err != nil {
		return err
	}
	c.open()
	return nil
}

// Resume never opens the gate on callback failure, timeout or an uncertain commit.
// The operator must first verify the external migration outcome and refresh caches.
func (c *Controller) Resume(ctx context.Context, id string, refresh func(context.Context) error) error {
	c.mu.Lock()
	if id == "" || id != c.operationID || c.state != "migrating" || refresh == nil {
		c.mu.Unlock()
		return ErrState
	}
	if c.active != 0 || c.usage() != 0 || c.backgroundActive() != 0 {
		c.mu.Unlock()
		return ErrBusy
	}
	c.state = "refreshing"
	c.mu.Unlock()
	err := refresh(ctx)
	if err == nil {
		err = ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.state = "migrating"
		return err
	}
	if err := c.restoreBackground(ctx); err != nil {
		c.state = "migrating"
		return err
	}
	c.open()
	return nil
}

func (c *Controller) open() {
	c.state = "open"
	c.operationID = ""
	close(c.changed)
	c.changed = make(chan struct{})
}

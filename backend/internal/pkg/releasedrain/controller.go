// Package releasedrain coordinates a single serving process during a ledger takeover.
package releasedrain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

var (
	ErrState     = errors.New("release operation or state does not match")
	ErrBusy      = errors.New("requests or usage tasks are still active")
	ErrQueueFull = errors.New("release request queue is full")
)

type Status struct {
	OperationID  string `json:"operation_id"`
	State        string `json:"state"`
	ActiveHTTP   int64  `json:"active_http"`
	PendingUsage int64  `json:"pending_usage"`
	QueuedHTTP   int64  `json:"queued_http"`
}

type Controller struct {
	mu           sync.Mutex
	operationID  string
	state        string
	active       int64
	queued       int64
	queueLimit   int64
	changed      chan struct{}
	pendingUsage func() int64
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
	return Status{c.operationID, c.state, c.active, c.usage(), c.queued}
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
	if c.active != 0 || c.usage() != 0 {
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
	if c.active != 0 || c.usage() != 0 {
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
	c.open()
	return nil
}

func (c *Controller) open() {
	c.state = "open"
	c.operationID = ""
	close(c.changed)
	c.changed = make(chan struct{})
}

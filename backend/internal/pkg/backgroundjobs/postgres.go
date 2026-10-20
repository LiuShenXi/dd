package backgroundjobs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"log/slog"
	"sync"
	"time"
)

// A single authoritative backend: do not fall back to a different lock service.
// The lock covers all autonomous jobs, not just each callback, so two timers
// cannot take turns performing duplicate probes within the same interval.
const advisoryKey int64 = 0x42474a4f42533031
const lockTimeout = 3 * time.Second

type PostgresLocker struct{ DB *sql.DB }

func (p PostgresLocker) Acquire(ctx context.Context) (Lease, error) {
	if p.DB == nil {
		return nil, ErrLeadership
	}
	ctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	conn, err := p.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryKey).Scan(&acquired); err != nil || !acquired {
		// A query error can hide successful acquisition. Discard the session rather
		// than returning a potentially locked connection to the pool.
		discard(conn)
		if err != nil {
			return nil, err
		}
		return nil, ErrLeadership
	}
	monitorCtx, stop := context.WithCancel(context.Background())
	lease := &postgresLease{conn: conn, lost: make(chan struct{}), stop: stop}
	go lease.monitor(monitorCtx)
	return lease, nil
}

type postgresLease struct {
	conn *sql.Conn
	lost chan struct{}
	stop context.CancelFunc
	once sync.Once
}

func (l *postgresLease) Lost() <-chan struct{} { return l.lost }
func (l *postgresLease) monitor(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := l.conn.PingContext(checkCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			close(l.lost)
			return
		}
	}
}
func (l *postgresLease) Release() {
	l.once.Do(func() {
		l.stop()
		ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
		defer cancel()
		var unlocked bool
		err := l.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryKey).Scan(&unlocked)
		if err != nil || !unlocked {
			slog.Error("background leadership release failed; discarding session", "error", err)
			discard(l.conn)
			return
		}
		_ = l.conn.Close()
	})
}

func discard(conn *sql.Conn) {
	// driver.ErrBadConn tells database/sql never to reuse this physical session.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

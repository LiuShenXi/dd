package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// No general session-setting escape hatch is added to the migration runner.
// These are the complete, bounded statements in this one online migration.
func subscriptionQuotaOnlineIndexTimeoutStatement(name, statement string) bool {
	if name != "247_subscription_quota_usage_index_notx.sql" {
		return false
	}
	switch statement {
	case "SET LOCK_TIMEOUT = '5S'", "SET STATEMENT_TIMEOUT = '5MIN'", "RESET LOCK_TIMEOUT", "RESET STATEMENT_TIMEOUT":
		return true
	default:
		return false
	}
}

func applySubscriptionQuotaOnlineIndexMigration(ctx context.Context, db migrationConnection, name, content, checksum string) (err error) {
	var oldLock, oldStatement string
	if err := db.QueryRowContext(ctx, "SELECT current_setting('lock_timeout'), current_setting('statement_timeout')").Scan(&oldLock, &oldStatement); err != nil {
		return fmt.Errorf("read quota migration timeouts: %w", err)
	}
	// A cancelled CREATE INDEX must not return modified session settings to the
	// pool. Restore through an independent bounded context on the same connection.
	defer func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, restoreErr := db.ExecContext(restoreCtx, "SELECT set_config('lock_timeout',$1,false), set_config('statement_timeout',$2,false)", oldLock, oldStatement)
		if restoreErr != nil {
			if conn, ok := db.(*sql.Conn); ok {
				// Never return a session with unknown settings to the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
			err = errors.Join(err, fmt.Errorf("restore quota migration timeouts: %w", restoreErr))
		}
	}()
	if _, err := db.ExecContext(ctx, "SELECT set_config('lock_timeout','5s',false), set_config('statement_timeout','5min',false)"); err != nil {
		return fmt.Errorf("set quota migration timeouts: %w", err)
	}
	if err := prepareNonTransactionalMigration(ctx, db, name); err != nil {
		return fmt.Errorf("prepare migration %s: %w", name, err)
	}
	for i, stmt := range splitSQLStatements(content) {
		statement := strings.TrimSpace(stmt)
		if statement == "" || stripSQLLineComment(statement) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply migration %s (non-tx statement %d): %w", name, i+1, err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", name, checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

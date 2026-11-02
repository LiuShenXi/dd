//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaHistoryIndexesI6_PendingCountsRetainMeaning(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	first := f.admit(t, f.rights.userA, f.keyA)
	_, command := f.persist(t, first, "0.2", "0.01")
	_, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	known := f.admit(t, f.rights.userA, f.keyA)
	f.persist(t, known, "0.3", "0.01")
	f.admit(t, f.rights.userA, f.keyA)
	var original service.SubscriptionQuotaBacklog
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FILTER(WHERE status='admitted'),count(*) FILTER(WHERE status='usage_known'),count(*) FILTER(WHERE status='reconcile_required'),count(*) FILTER(WHERE status='settled' AND usage_log_recorded_at IS NULL),COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-MIN(updated_at) FILTER(WHERE status='usage_known')),0) FROM subscription_quota_requests`).Scan(&original.Admitted, &original.Known, &original.Reconcile, &original.PendingLogs, &original.OldestKnownSeconds))
	actual, err := (&subscriptionQuotaRepository{db: f.db}).BillingBacklog(context.Background())
	require.NoError(t, err)
	require.Equal(t, original.Admitted, actual.Admitted)
	require.Equal(t, original.Known, actual.Known)
	require.Equal(t, original.Reconcile, actual.Reconcile)
	require.Equal(t, original.PendingLogs, actual.PendingLogs)
	require.InDelta(t, original.OldestKnownSeconds, actual.OldestKnownSeconds, 1)
	require.Positive(t, actual.Admitted)
	require.Positive(t, actual.Known)
	require.Positive(t, actual.PendingLogs)
}

func quotaHistoryIndexMigration(t *testing.T) (string, string, string) {
	t.Helper()
	name := "254_subscription_quota_history_indexes_notx.sql"
	raw, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	content := strings.TrimSpace(string(raw))
	digest := sha256.Sum256([]byte(content))
	return name, content, hex.EncodeToString(digest[:])
}

func TestSubscriptionQuotaHistoryIndexesI6_RecoversRealInvalidIndex(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	f.admit(t, f.rights.userA, f.keyA)
	f.admit(t, f.rights.userA, f.keyA)
	name, _, checksum := quotaHistoryIndexMigration(t)
	_, err := f.db.Exec(`DELETE FROM schema_migrations WHERE filename=$1`, name)
	require.NoError(t, err)
	_, err = f.db.Exec(`DROP INDEX subscription_quota_requests_reconciliation_hold`)
	require.NoError(t, err)
	_, err = f.db.Exec(`CREATE UNIQUE INDEX CONCURRENTLY subscription_quota_requests_reconciliation_hold ON subscription_quota_requests(term_id)`)
	requireSubscriptionQuotaSQLState(t, err, "23505")
	var before int64
	var valid bool
	require.NoError(t, f.db.QueryRow(`SELECT indexrelid::bigint,indisvalid FROM pg_index WHERE indexrelid='subscription_quota_requests_reconciliation_hold'::regclass`).Scan(&before, &valid))
	require.False(t, valid)
	require.NoError(t, ApplyMigrations(context.Background(), f.db))
	var after int64
	var stored string
	require.NoError(t, f.db.QueryRow(`SELECT indexrelid::bigint,indisvalid FROM pg_index WHERE indexrelid='subscription_quota_requests_reconciliation_hold'::regclass`).Scan(&after, &valid))
	require.True(t, valid)
	require.NotEqual(t, before, after)
	require.NoError(t, f.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE filename=$1`, name).Scan(&stored))
	require.Equal(t, checksum, stored)
	var requests int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_requests`).Scan(&requests))
	require.Equal(t, 2, requests)
}

func TestSubscriptionQuotaHistoryIndexesI6_FailureRestoresSessionTimeouts(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	name, content, checksum := quotaHistoryIndexMigration(t)
	_, err := f.db.Exec(`DELETE FROM schema_migrations WHERE filename=$1`, name)
	require.NoError(t, err)
	_, err = f.db.Exec(`DROP INDEX subscription_quota_requests_reconciliation_hold`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := f.db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(ctx, `SELECT set_config('lock_timeout','321ms',false),set_config('statement_timeout','17s',false)`)
	require.NoError(t, err)
	blocker, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback()
	_, err = blocker.ExecContext(ctx, `LOCK TABLE subscription_quota_requests IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	err = applySubscriptionQuotaOnlineIndexMigration(ctx, conn, name, content, checksum)
	requireSubscriptionQuotaSQLState(t, err, "55P03")
	var lock, statement string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT current_setting('lock_timeout'),current_setting('statement_timeout')`).Scan(&lock, &statement))
	require.Equal(t, "321ms", lock)
	require.Equal(t, "17s", statement)
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&count))
	require.Zero(t, count)
}

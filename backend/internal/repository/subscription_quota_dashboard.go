package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Only dashboard writers and durable term publication use this lock. Acquire it
// before reading source rows, and before the usage_logs INSERT trigger takes the
// group-rollup lock. Ordinary usage publication never takes this lock.
func lockDashboardAggregationWriter(ctx context.Context, q sqlExecutor) error {
	_, err := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1936811121, 1)`)
	return err
}

// The caller owns the dashboard writer lock and calls this only after a FIRST
// successful term-log INSERT, in that same transaction. Replays never add twice.
// Published history may outlive its raw rows: append the known contribution
// instead of rebuilding a bucket and erasing its retained historical base.
func subscriptionQuotaPublishHistoricalDashboardTx(ctx context.Context, tx *sql.Tx, log *service.UsageLog, _ json.RawMessage) error {
	if tx == nil || log == nil || log.SubscriptionQuotaRequestID == nil || log.SubscriptionTermID == nil {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	var published bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM usage_logs l
		WHERE l.id=$1 AND l.created_at=$2
		AND l.subscription_quota_request_id=$3 AND l.subscription_term_id=$4
		AND (l.created_at < COALESCE((SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark WHERE id=1), '-infinity'::timestamptz)
			OR EXISTS (SELECT 1 FROM usage_dashboard_hourly h WHERE h.bucket_start=date_trunc('hour',l.created_at AT TIME ZONE $5) AT TIME ZONE $5)
			OR EXISTS (SELECT 1 FROM usage_dashboard_daily d WHERE d.bucket_date=(l.created_at AT TIME ZONE $5)::date))
	)`, log.ID, log.CreatedAt, *log.SubscriptionQuotaRequestID, *log.SubscriptionTermID, timezone.Name()).Scan(&published)
	if err != nil || !published {
		return err
	}
	// Use the inserted NUMERIC values, including the independent account-stats
	// base and multiplier; never round-trip these amounts through float64 here.
	const contribution = `SELECT
		date_trunc('hour',created_at AT TIME ZONE $3) AT TIME ZONE $3 AS bucket_start,
		(created_at AT TIME ZONE $3)::date AS bucket_date,
		user_id,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,
		total_cost,actual_cost,
		COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1) AS account_cost,
		COALESCE(duration_ms,0) AS total_duration_ms
		FROM usage_logs WHERE id=$1 AND created_at=$2`
	for _, bucket := range []struct{ table, users, key string }{
		{"usage_dashboard_hourly", "usage_dashboard_hourly_users", "bucket_start"},
		{"usage_dashboard_daily", "usage_dashboard_daily_users", "bucket_date"},
	} {
		query := fmt.Sprintf(`WITH contribution AS (%s), new_user AS (
			INSERT INTO %s (%s,user_id) SELECT %s,user_id FROM contribution
			ON CONFLICT DO NOTHING RETURNING 1
		)
		INSERT INTO %s (%s,total_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,account_cost,total_duration_ms,active_users,computed_at)
		SELECT %s,1,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,account_cost,total_duration_ms,(SELECT count(*) FROM new_user),NOW() FROM contribution
		ON CONFLICT (%s) DO UPDATE SET
			total_requests=%s.total_requests+EXCLUDED.total_requests,
			input_tokens=%s.input_tokens+EXCLUDED.input_tokens,
			output_tokens=%s.output_tokens+EXCLUDED.output_tokens,
			cache_creation_tokens=%s.cache_creation_tokens+EXCLUDED.cache_creation_tokens,
			cache_read_tokens=%s.cache_read_tokens+EXCLUDED.cache_read_tokens,
			total_cost=%s.total_cost+EXCLUDED.total_cost,
			actual_cost=%s.actual_cost+EXCLUDED.actual_cost,
			account_cost=%s.account_cost+EXCLUDED.account_cost,
			total_duration_ms=%s.total_duration_ms+EXCLUDED.total_duration_ms,
			active_users=%s.active_users+EXCLUDED.active_users,
			computed_at=EXCLUDED.computed_at`, contribution,
			bucket.users, bucket.key, bucket.key,
			bucket.table, bucket.key, bucket.key, bucket.key,
			bucket.table, bucket.table, bucket.table, bucket.table, bucket.table,
			bucket.table, bucket.table, bucket.table, bucket.table, bucket.table)
		if _, err := tx.ExecContext(ctx, query, log.ID, log.CreatedAt, timezone.Name()); err != nil {
			return err
		}
	}
	return nil
}

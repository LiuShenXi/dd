package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/migrations"
)

// This is a capability check only. Startup migrations remain owned by the
// existing migration runner, and this method never repairs or alters a schema.
func (r *subscriptionQuotaRepository) CheckBillingReadiness(ctx context.Context) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("term billing database unavailable")
	}
	for _, name := range []string{"245_subscription_quota_structure.sql", "246_subscription_quota_usage_links.sql", "247_subscription_quota_usage_index_notx.sql", "248_subscription_quota_usage_validate.sql", "249_subscription_quota_protected_groups.sql", "250_subscription_quota_usage_cost_width.sql", "251_claude_subscription_concurrency.sql"} {
		raw, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(raw))))
		var stored string
		if err = r.db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE filename=$1`, name).Scan(&stored); err != nil {
			return fmt.Errorf("term billing migration %s unavailable: %w", name, err)
		}
		if stored != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("term billing migration %s checksum mismatch", name)
		}
	}
	var ready bool
	err := r.db.QueryRowContext(ctx, `SELECT
 to_regclass('subscription_quota_terms') IS NOT NULL AND to_regclass('subscription_quota_requests') IS NOT NULL AND to_regclass('subscription_quota_ledger') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('usage_logs_subscription_term_time') AND i.indrelid=to_regclass('usage_logs') AND i.indisvalid AND i.indisready)
 AND (SELECT count(*)=7 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='usage_logs' AND column_name IN ('input_cost','output_cost','cache_creation_cost','cache_read_cost','total_cost','actual_cost','account_stats_cost') AND numeric_precision=22 AND numeric_scale=10)
 AND (SELECT count(*)=2 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='usage_logs' AND column_name IN ('subscription_term_id','subscription_quota_request_id'))
 AND (SELECT count(*)=7 FROM information_schema.columns WHERE table_schema=current_schema() AND ((table_name='usage_group_daily_rollups' AND column_name='actual_cost') OR (table_name IN ('usage_dashboard_hourly','usage_dashboard_daily') AND column_name IN ('total_cost','actual_cost','account_cost'))) AND numeric_precision=30 AND numeric_scale=10)`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("term billing schema capability missing")
	}
	return nil
}

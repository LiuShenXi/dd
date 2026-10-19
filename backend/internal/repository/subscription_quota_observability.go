package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *subscriptionQuotaRepository) BillingBacklog(ctx context.Context) (service.SubscriptionQuotaBacklog, error) {
	var out service.SubscriptionQuotaBacklog
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='admitted'),count(*) FILTER(WHERE status='usage_known'),count(*) FILTER(WHERE status='reconcile_required'),count(*) FILTER(WHERE status='settled' AND usage_log_recorded_at IS NULL),COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-MIN(updated_at) FILTER(WHERE status='usage_known')),0) FROM subscription_quota_requests`).Scan(&out.Admitted, &out.Known, &out.Reconcile, &out.PendingLogs, &out.OldestKnownSeconds)
	return out, err
}

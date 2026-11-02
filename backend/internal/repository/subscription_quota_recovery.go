package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *subscriptionQuotaRepository) ClaimRecoverable(ctx context.Context, in domain.SubscriptionQuotaRecoveryClaim) ([]domain.SubscriptionQuotaKnownUsage, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if in.Owner == "" || len(in.Owner) > 64 || strings.ContainsAny(in.Owner, "\r\n\x00") || in.Limit < 1 || in.Limit > 1000 || in.Lease <= 0 || in.Lease > 5*time.Minute {
		return nil, service.ErrSubscriptionQuotaInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `WITH picked AS (
 SELECT id AS picked_id FROM subscription_quota_requests WHERE status='usage_known' AND (next_retry_at IS NULL OR next_retry_at<=clock_timestamp()) AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED)
 UPDATE subscription_quota_requests r SET lease_owner=$2,lease_until=clock_timestamp()+($3*INTERVAL '1 millisecond'),recovery_attempts=recovery_attempts+1 FROM picked WHERE r.id=picked.picked_id RETURNING `+subscriptionQuotaRequestColumns, in.Limit, in.Owner, in.Lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	batch := []domain.SubscriptionQuotaKnownUsage{}
	for rows.Next() {
		row, scanErr := subscriptionQuotaScanRequest(rows)
		if scanErr != nil && row.known.Snapshot.RequestRowID == 0 {
			_ = rows.Close()
			return nil, scanErr
		}
		batch = append(batch, row.known)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return batch, nil
}
func (r *subscriptionQuotaRepository) FinishRecoveryAttempt(ctx context.Context, id int64, owner, category string, retry time.Duration) error {
	if r == nil || r.db == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	switch category {
	case "receipt_conflict", "receipt_invalid":
		return r.markKnownRecoveryConflict(ctx, id, owner, category)
	case "":
		category = "transient_settlement_error"
	default:
		category = "transient_settlement_error"
	}
	if retry < time.Second {
		retry = time.Second
	}
	if retry > 5*time.Minute {
		retry = 5 * time.Minute
	}
	_, err := r.db.ExecContext(ctx, `UPDATE subscription_quota_requests SET lease_owner=NULL,lease_until=NULL,next_retry_at=clock_timestamp()+($3*INTERVAL '1 millisecond'),last_error_category=$4 WHERE id=$1 AND lease_owner=$2 AND status='usage_known'`, id, owner, retry.Milliseconds(), category)
	return err
}
func (r *subscriptionQuotaRepository) markKnownRecoveryConflict(ctx context.Context, id int64, owner, category string) error {
	// Obtain immutable routing identities without holding a request lock, then
	// acquire the normal scope->parent->term->request order in a new transaction.
	var s domain.SubscriptionQuotaBillingSnapshot
	err := r.db.QueryRowContext(ctx, `SELECT user_id,group_id,subscription_id,term_id FROM subscription_quota_requests WHERE id=$1`, id).Scan(&s.UserID, &s.GroupID, &s.SubscriptionID, &s.TermID)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, s.UserID, s.GroupID); err != nil {
		return err
	}
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM user_subscriptions WHERE id=$1 FOR UPDATE`, s.SubscriptionID).Scan(&locked); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM subscription_quota_terms WHERE id=$1 FOR UPDATE`, s.TermID).Scan(&locked); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='reconcile_required',revision=revision+1,last_error_category=$3,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_owner=$2 AND status='usage_known'`, id, owner, category)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *subscriptionQuotaRepository) MarkStaleAdmissions(ctx context.Context, grace time.Duration, limit int) error {
	if r == nil || r.db == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	if grace <= 0 || limit < 1 || limit > 1000 {
		return service.ErrSubscriptionQuotaInput
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE status='admitted' AND updated_at<clock_timestamp()-($1*INTERVAL '1 millisecond') AND (lease_until IS NULL OR lease_until<clock_timestamp()) ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`, grace.Milliseconds(), limit)
	if err != nil {
		return err
	}
	snapshots := []domain.SubscriptionQuotaBillingSnapshot{}
	for rows.Next() {
		row, err := subscriptionQuotaScanRequest(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		snapshots = append(snapshots, row.known.Snapshot)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	var firstErr error
	for _, snapshot := range snapshots {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		itemCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = r.markAdmissionStale(itemCtx, snapshot, grace)
		cancel()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
func (r *subscriptionQuotaRepository) markAdmissionStale(ctx context.Context, snapshot domain.SubscriptionQuotaBillingSnapshot, grace time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := subscriptionQuotaLockRequest(ctx, tx, snapshot)
	if err != nil {
		return err
	}
	if row.status != "admitted" {
		return nil
	}
	// Recheck the lease after obtaining locks. A scan that raced a heartbeat must
	// not turn a healthy long-running stream into an unknown consumption event.
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='reconcile_required',revision=revision+1,last_error_category='admission_stale',updated_at=clock_timestamp() WHERE id=$1 AND status='admitted' AND updated_at<clock_timestamp()-($2*INTERVAL '1 millisecond') AND (lease_until IS NULL OR lease_until<clock_timestamp())`, snapshot.RequestRowID, grace.Milliseconds())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *subscriptionQuotaRepository) ListPendingUsageLogs(ctx context.Context, limit int) ([]domain.SubscriptionQuotaKnownUsage, error) {
	if limit < 1 || limit > 1000 {
		return nil, service.ErrSubscriptionQuotaInput
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE status='settled' AND usage_log_recorded_at IS NULL ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	batch := []domain.SubscriptionQuotaKnownUsage{}
	for rows.Next() {
		row, err := subscriptionQuotaScanRequest(rows)
		if err != nil && row.known.Snapshot.RequestRowID == 0 {
			return nil, err
		}
		batch = append(batch, row.known)
	}
	return batch, rows.Err()
}
func (r *subscriptionQuotaRepository) MarkUsageLogRecorded(ctx context.Context, id int64, fingerprint string) error {
	var saved int64
	err := r.db.QueryRowContext(ctx, `UPDATE subscription_quota_requests r SET usage_log_recorded_at=COALESCE(usage_log_recorded_at,clock_timestamp()) WHERE r.id=$1 AND r.request_fingerprint=$2 AND r.status='settled' AND (r.usage_log_recorded_at IS NOT NULL OR EXISTS(
 SELECT 1 FROM usage_logs l WHERE l.request_id=r.request_id AND l.api_key_id=r.api_key_id AND l.user_id=r.user_id AND l.group_id=r.group_id AND l.subscription_id=r.subscription_id AND l.subscription_term_id=r.term_id AND l.subscription_quota_request_id=r.id AND l.actual_cost=r.actual_cost_usd)) RETURNING id`, id, fingerprint).Scan(&saved)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: usage log not verified", service.ErrSubscriptionQuotaReceiptConflict)
	}
	return err
}

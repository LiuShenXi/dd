package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type subscriptionQuotaProtectedGroupRepository struct{ db *sql.DB }

func NewSubscriptionQuotaProtectedGroupRepository(db *sql.DB) service.SubscriptionQuotaProtectedGroupRepository {
	return &subscriptionQuotaProtectedGroupRepository{db: db}
}

func (r *subscriptionQuotaProtectedGroupRepository) AdmitProtectedGroup(ctx context.Context, in service.SubscriptionQuotaProtectedAdmission) error {
	if r == nil || r.db == nil || in.RequestID == "" || in.UserID <= 0 || in.APIKeyID <= 0 || in.GroupID <= 0 {
		return service.ErrSubscriptionQuotaProtectedUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Match the shared counter-row order: user -> group -> api_key. In particular,
	// never hold the Claude group lock while waiting for a user held by GPT open.
	var userStatus string
	var restrictPublic bool
	if err := tx.QueryRowContext(ctx, `SELECT status,restrict_public_groups FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, in.UserID).Scan(&userStatus, &restrictPublic); err != nil {
		return err
	}
	var status, platform, subscriptionType, mode string
	var exclusive bool
	err = tx.QueryRowContext(ctx, `SELECT status,platform,subscription_type,subscription_quota_mode,is_exclusive FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, in.GroupID).
		Scan(&status, &platform, &subscriptionType, &mode, &exclusive)
	if err != nil {
		return err
	}
	if status != service.StatusActive || platform != service.PlatformAnthropic || mode != "windows" ||
		(subscriptionType != service.SubscriptionTypeStandard && subscriptionType != service.SubscriptionTypeSubscription) || subscriptionType != in.SubscriptionType {
		return service.ErrSubscriptionQuotaProtectedUnavailable
	}
	var userID, groupID int64
	var keyStatus string
	var expiresAt, window5h, window1d, window7d sql.NullTime
	var quota, used, limit5h, limit1d, limit7d, used5h, used1d, used7d float64
	err = tx.QueryRowContext(ctx, `SELECT user_id,group_id,status,expires_at,quota,quota_used,
        rate_limit_5h,rate_limit_1d,rate_limit_7d,usage_5h,usage_1d,usage_7d,window_5h_start,window_1d_start,window_7d_start
        FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, in.APIKeyID).
		Scan(&userID, &groupID, &keyStatus, &expiresAt, &quota, &used, &limit5h, &limit1d, &limit7d, &used5h, &used1d, &used7d, &window5h, &window1d, &window7d)
	if err != nil {
		return err
	}
	// Database time is refreshed only after all locks. A timestamp projected by a
	// SELECT FOR SHARE could otherwise have been evaluated before its lock wait.
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	windowExceeded := func(limit, usage float64, start sql.NullTime, duration time.Duration) bool {
		return limit > 0 && start.Valid && start.Time.Add(duration).After(now) && usage >= limit
	}
	if userID != in.UserID || groupID != in.GroupID || keyStatus != service.StatusActive || userStatus != service.StatusActive ||
		(expiresAt.Valid && !expiresAt.Time.After(now)) || (quota > 0 && used >= quota) ||
		windowExceeded(limit5h, used5h, window5h, 5*time.Hour) || windowExceeded(limit1d, used1d, window1d, 24*time.Hour) || windowExceeded(limit7d, used7d, window7d, 7*24*time.Hour) {
		return service.ErrSubscriptionQuotaProtectedUnavailable
	}
	if subscriptionType == service.SubscriptionTypeStandard && (exclusive || restrictPublic) {
		var allowed bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_allowed_groups WHERE user_id=$1 AND group_id=$2)`, in.UserID, in.GroupID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return service.ErrSubscriptionQuotaProtectedUnavailable
		}
	}
	if subscriptionType == service.SubscriptionTypeSubscription {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_subscriptions WHERE user_id=$1 AND group_id=$2 AND deleted_at IS NULL AND status='active' AND expires_at>clock_timestamp())`, in.UserID, in.GroupID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return service.ErrSubscriptionQuotaProtectedUnavailable
		}
	}
	var admittedID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO subscription_quota_protected_requests(request_id,user_id,api_key_id,group_id,subscription_type,state) VALUES($1,$2,$3,$4,$5,'admitted')
        ON CONFLICT(request_id) DO UPDATE SET request_id=EXCLUDED.request_id
        WHERE subscription_quota_protected_requests.user_id=EXCLUDED.user_id
          AND subscription_quota_protected_requests.api_key_id=EXCLUDED.api_key_id
          AND subscription_quota_protected_requests.group_id=EXCLUDED.group_id
          AND subscription_quota_protected_requests.subscription_type=EXCLUDED.subscription_type
          AND subscription_quota_protected_requests.state='admitted'
        RETURNING request_id`, in.RequestID, in.UserID, in.APIKeyID, in.GroupID, in.SubscriptionType).Scan(&admittedID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *subscriptionQuotaProtectedGroupRepository) RecordProtectedGroupUsage(ctx context.Context, requestID string, payload json.RawMessage) error {
	if r == nil || r.db == nil || len(payload) == 0 || len(payload) > 65536 || !json.Valid(payload) {
		return service.ErrSubscriptionQuotaProtectedUnavailable
	}
	var saved string
	err := r.db.QueryRowContext(ctx, `UPDATE subscription_quota_protected_requests SET state='usage_pending',usage_payload=$2::jsonb,updated_at=clock_timestamp()
        WHERE request_id=$1 AND state='admitted' AND usage_payload IS NULL RETURNING request_id`, requestID, string(payload)).Scan(&saved)
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var matches bool
	err = r.db.QueryRowContext(ctx, `SELECT usage_payload=$2::jsonb FROM subscription_quota_protected_requests WHERE request_id=$1 AND state IN ('usage_pending','completed','reconcile_required')`, requestID, string(payload)).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("protected request usage facts conflict")
	}
	return nil
}

func (r *subscriptionQuotaProtectedGroupRepository) FinishProtectedGroup(ctx context.Context, requestID, state, reason string) error {
	if r == nil || r.db == nil {
		return service.ErrSubscriptionQuotaProtectedUnavailable
	}
	if state != "completed" && state != "no_charge" && state != "reconcile_required" {
		return fmt.Errorf("unsupported protected request outcome")
	}
	// Only a prepared usage task can complete. Reconciliation is deliberately not
	// an automatic release state; evidence-based repair is a separate operation.
	result, err := r.db.ExecContext(ctx, `UPDATE subscription_quota_protected_requests SET state=$2,reason=$3,updated_at=clock_timestamp(),
        finished_at=CASE WHEN $2 IN ('completed','no_charge') THEN clock_timestamp() ELSE NULL END
        WHERE request_id=$1 AND ((state='usage_pending' AND $2='completed' AND usage_payload IS NOT NULL)
          OR (state='admitted' AND $2='no_charge')
          OR (state IN ('admitted','usage_pending','reconcile_required') AND $2='reconcile_required'))`, requestID, state, reason)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var existing string
	if err := r.db.QueryRowContext(ctx, `SELECT state FROM subscription_quota_protected_requests WHERE request_id=$1`, requestID).Scan(&existing); err != nil {
		return err
	}
	if existing == state {
		return nil
	}
	return fmt.Errorf("protected request outcome conflicts with persisted state")
}

func (r *subscriptionQuotaProtectedGroupRepository) CountProtectedGroupUnfinished(ctx context.Context, groupID int64) (int64, error) {
	if r == nil || r.db == nil {
		return 0, service.ErrSubscriptionQuotaProtectedUnavailable
	}
	var count int64
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM subscription_quota_protected_requests WHERE group_id=$1 AND state NOT IN ('completed','no_charge')`, groupID).Scan(&count)
	return count, err
}

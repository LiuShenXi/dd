package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// Keep the old three-argument constructor source-compatible. Production Wire
// selects this provider and the same repository instance used by the service.
func ProvideUsageBillingRepository(client *dbent.Client, db *sql.DB, carpool *CarpoolRepository, quota service.SubscriptionQuotaRepository) service.UsageBillingRepository {
	r := NewUsageBillingRepository(client, db, carpool).(*usageBillingRepository)
	r.subscriptionQuotaRepo, _ = quota.(*subscriptionQuotaRepository)
	return r
}
func (r *usageBillingRepository) SubscriptionQuotaBillingReady() bool {
	return r != nil && r.db != nil && r.subscriptionQuotaRepo != nil
}

func (r *usageBillingRepository) applySubscriptionQuotaUsage(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if r.subscriptionQuotaRepo == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := cmd.SubscriptionTermSnapshot
	if err = subscriptionQuotaScopeLock(ctx, tx, snapshot.UserID, snapshot.GroupID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SAVEPOINT subscription_quota_apply`); err != nil {
		return nil, err
	}
	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	stored, err := subscriptionQuotaLockRequest(ctx, tx, *snapshot)
	if err != nil {
		return nil, err
	}
	if !stored.hasCost || stored.known.BillingPayload == nil || (stored.status != "usage_known" && stored.status != "settled") {
		return nil, service.ErrSubscriptionQuotaReceiptConflict
	}
	savedCommand, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(stored.known.BillingPayload)
	if err != nil {
		return nil, err
	}
	inputPayload, err := service.MarshalSubscriptionQuotaUsageBillingReceipt(cmd)
	if err != nil {
		return nil, err
	}
	if !subscriptionQuotaSameJSON(inputPayload, stored.known.BillingPayload) || cmd.RequestFingerprint != stored.known.RequestFingerprint || cmd.RequestFingerprint != savedCommand.RequestFingerprint || cmd.SubscriptionTermCost.StringFixed(8) != savedCommand.SubscriptionTermCost.StringFixed(8) {
		return nil, service.ErrSubscriptionQuotaReceiptConflict
	}
	savedCost, costErr := decimal.NewFromString(stored.known.ActualCostUSD)
	if costErr != nil || !savedCost.Equal(cmd.SubscriptionTermCost) || stored.known.ReceiptVersion != service.SubscriptionQuotaUsageBillingReceiptVersion {
		return nil, service.ErrSubscriptionQuotaReceiptConflict
	}
	var priorConsistent bool
	if err = tx.QueryRowContext(ctx, `SELECT used_usd=COALESCE((SELECT SUM(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1),0) FROM subscription_quota_terms WHERE id=$1`, snapshot.TermID).Scan(&priorConsistent); err != nil {
		return nil, err
	}
	if !priorConsistent {
		return r.holdSubscriptionQuotaLedgerMismatch(ctx, tx, *snapshot)
	}
	var ledgerID, termID int64
	var amount string
	ledgerErr := tx.QueryRowContext(ctx, `SELECT id,term_id,delta_used_usd::text FROM subscription_quota_ledger WHERE request_row_id=$1 AND entry_kind='usage_debit'`, snapshot.RequestRowID).Scan(&ledgerID, &termID, &amount)
	if ledgerErr != nil && !errors.Is(ledgerErr, sql.ErrNoRows) {
		return nil, ledgerErr
	}
	if ledgerErr == nil {
		charged, err := decimal.NewFromString(amount)
		if err != nil || termID != snapshot.TermID || !charged.Equal(cmd.SubscriptionTermCost) {
			return r.holdSubscriptionQuotaLedgerMismatch(ctx, tx, *snapshot)
		}
		var consistent bool
		err = tx.QueryRowContext(ctx, `SELECT used_usd=COALESCE((SELECT SUM(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1),0) FROM subscription_quota_terms WHERE id=$1`, snapshot.TermID).Scan(&consistent)
		if err != nil {
			return nil, err
		}
		if !consistent {
			return r.holdSubscriptionQuotaLedgerMismatch(ctx, tx, *snapshot)
		}
		_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='settled',settled_at=COALESCE(settled_at,clock_timestamp()),revision=revision+CASE WHEN status='settled' THEN 0 ELSE 1 END,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1`, snapshot.RequestRowID)
		if err != nil {
			return nil, err
		}
		if err = subscriptionQuotaConvergeTermTx(ctx, tx, snapshot.TermID); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}
	if !applied || stored.status == "settled" {
		return r.holdSubscriptionQuotaLedgerMismatch(ctx, tx, *snapshot)
	}
	// Counters lock original persistent identities, including soft-deleted rows.
	// Later suspension/deletion never grants a refund for a prior admission.
	var identity int64
	for _, lock := range []struct {
		sql string
		id  int64
	}{{`SELECT id FROM users WHERE id=$1 FOR SHARE`, snapshot.UserID}, {`SELECT id FROM groups WHERE id=$1 FOR SHARE`, snapshot.GroupID}, {`SELECT id FROM api_keys WHERE id=$1 FOR UPDATE`, snapshot.APIKeyID}, {`SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, snapshot.AccountID}} {
		if err = tx.QueryRowContext(ctx, lock.sql, lock.id).Scan(&identity); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,$3,'usage_debit',$4)`, snapshot.TermID, snapshot.RequestRowID, fmt.Sprintf("usage:%d", snapshot.RequestRowID), cmd.SubscriptionTermCost.StringFixed(8))
	if err != nil {
		return nil, err
	}
	var overage decimal.Decimal
	err = tx.QueryRowContext(ctx, `UPDATE subscription_quota_terms SET used_usd=used_usd+$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING GREATEST(used_usd-quota_usd,0)-GREATEST(used_usd-$2-quota_usd,0)`, snapshot.TermID, cmd.SubscriptionTermCost.StringFixed(8)).Scan(&overage)
	if err != nil {
		return nil, err
	}
	result := &service.UsageBillingApplyResult{Applied: true}
	if snapshot.KeyQuotaTracked && cmd.SubscriptionTermCounters.APIKeyQuotaCost.IsPositive() {
		err = tx.QueryRowContext(ctx, `UPDATE api_keys SET quota_used=quota_used+$2,status=CASE WHEN quota>0 AND status=$3 AND quota_used<quota AND quota_used+$2>=quota THEN $4 ELSE status END,updated_at=clock_timestamp() WHERE id=$1 RETURNING quota>0 AND quota_used>=quota AND quota_used-$2<quota`, snapshot.APIKeyID, cmd.SubscriptionTermCost.StringFixed(8), service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&result.APIKeyQuotaExhausted)
		if err != nil {
			return nil, err
		}
	}
	if snapshot.KeyRateLimitTracked && cmd.SubscriptionTermCounters.APIKeyRateLimitCost.IsPositive() {
		_, err = tx.ExecContext(ctx, `UPDATE api_keys SET
 usage_5h=CASE WHEN window_5h_start IS NOT NULL AND window_5h_start+INTERVAL '5 hours'<=NOW() THEN $2 ELSE usage_5h+$2 END,
 usage_1d=CASE WHEN window_1d_start IS NOT NULL AND window_1d_start+INTERVAL '24 hours'<=NOW() THEN $2 ELSE usage_1d+$2 END,
 usage_7d=CASE WHEN window_7d_start IS NOT NULL AND window_7d_start+INTERVAL '7 days'<=NOW() THEN $2 ELSE usage_7d+$2 END,
 window_5h_start=CASE WHEN window_5h_start IS NULL OR window_5h_start+INTERVAL '5 hours'<=NOW() THEN NOW() ELSE window_5h_start END,
 window_1d_start=CASE WHEN window_1d_start IS NULL OR window_1d_start+INTERVAL '24 hours'<=NOW() THEN date_trunc('day',NOW()) ELSE window_1d_start END,
 window_7d_start=CASE WHEN window_7d_start IS NULL OR window_7d_start+INTERVAL '7 days'<=NOW() THEN date_trunc('day',NOW()) ELSE window_7d_start END,updated_at=clock_timestamp() WHERE id=$1`, snapshot.APIKeyID, cmd.SubscriptionTermCost.StringFixed(8))
		if err != nil {
			return nil, err
		}
	}
	if snapshot.AccountQuotaTracked && cmd.SubscriptionTermCounters.AccountQuotaCost.IsPositive() && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		result.QuotaState, err = incrementUsageBillingAccountQuotaHistorical(ctx, tx, snapshot.AccountID, cmd.SubscriptionTermCounters.AccountQuotaCost.StringFixed(8), true)
		if err != nil {
			return nil, err
		}
	}
	// These publication facts commit with the debit and run for live settlement
	// and recovery alike. They do not alter key/account status or configuration.
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_cache_invalidation_outbox(cache_key) SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE id=$1`, snapshot.APIKeyID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET last_used_at=GREATEST(COALESCE(last_used_at,$2::timestamptz),$2::timestamptz) WHERE id=$1`, snapshot.AccountID, snapshot.AdmittedAt); err != nil {
		return nil, err
	}
	if err = enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountLastUsed, &snapshot.AccountID, nil, map[string]any{"last_used": map[string]int64{fmt.Sprint(snapshot.AccountID): snapshot.AdmittedAt.Unix()}}); err != nil {
		return nil, err
	}
	if snapshot.AccountQuotaTracked {
		if err = enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &snapshot.AccountID, nil, nil); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='settled',settled_at=clock_timestamp(),revision=revision+1,lease_owner=NULL,lease_until=NULL,last_error_category=NULL,updated_at=clock_timestamp() WHERE id=$1`, snapshot.RequestRowID)
	if err != nil {
		return nil, err
	}
	if err = subscriptionQuotaConvergeTermTx(ctx, tx, snapshot.TermID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	service.ObserveSubscriptionQuotaOverage(overage)
	return result, nil
}

func (r *usageBillingRepository) holdSubscriptionQuotaLedgerMismatch(ctx context.Context, tx *sql.Tx, snapshot domain.SubscriptionQuotaBillingSnapshot) (*service.UsageBillingApplyResult, error) {
	// Keep the scope lock, but undo this attempt's dedup claim and all later
	// writes/locks before recording a durable hold. No monetary repair occurs.
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT subscription_quota_apply`); err != nil {
		return nil, err
	}
	if _, err := subscriptionQuotaLockRequest(ctx, tx, snapshot); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status=CASE WHEN status IN ('settled','no_charge') THEN status ELSE 'reconcile_required' END,last_error_category='ledger_mismatch',revision=revision+1,updated_at=clock_timestamp(),lease_owner=NULL,lease_until=NULL WHERE id=$1`, snapshot.RequestRowID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	service.ObserveSubscriptionQuotaLedgerMismatch()
	return nil, fmt.Errorf("%w: %w", service.ErrSubscriptionQuotaReceiptConflict, service.ErrSubscriptionQuotaLedgerMismatch)
}

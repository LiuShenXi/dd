package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type subscriptionQuotaStoredRequest struct {
	known       domain.SubscriptionQuotaKnownUsage
	status      string
	hasCost     bool
	logRecorded bool
}

const subscriptionQuotaRequestColumns = `id,request_id,user_id,api_key_id,group_id,subscription_id,term_id,admitted_at,pricing_at,pricing_snapshot,status,actual_cost_usd::text,receipt_version,request_fingerprint,billing_payload,usage_log_payload,revision,lease_owner,recovery_attempts,usage_log_recorded_at IS NOT NULL`

func subscriptionQuotaScanRequest(row carpoolRowScanner) (subscriptionQuotaStoredRequest, error) {
	var r subscriptionQuotaStoredRequest
	var pricing []byte
	var billing, usage []byte
	var cost, fingerprint, owner sql.NullString
	var version sql.NullInt64
	s := &r.known.Snapshot
	err := row.Scan(&s.RequestRowID, &s.RequestID, &s.UserID, &s.APIKeyID, &s.GroupID, &s.SubscriptionID, &s.TermID, &s.AdmittedAt, &s.PricingAt, &pricing, &r.status, &cost, &version, &fingerprint, &billing, &usage, &r.known.RequestRevision, &owner, &r.known.RecoveryAttempts, &r.logRecorded)
	if err != nil {
		return r, err
	}
	r.known.BillingPayload = append(json.RawMessage(nil), billing...)
	r.known.UsageLogPayload = append(json.RawMessage(nil), usage...)
	if err = subscriptionQuotaDecodePricingEnvelope(pricing, s); err != nil {
		return r, err
	}
	r.hasCost = cost.Valid
	r.known.ActualCostUSD = cost.String
	r.known.RequestFingerprint = fingerprint.String
	r.known.ReceiptVersion = int(version.Int64)
	r.known.LeaseOwner = owner.String
	return r, nil
}
func subscriptionQuotaSameSnapshot(a, b domain.SubscriptionQuotaBillingSnapshot) bool {
	if a.SchemaVersion != b.SchemaVersion || a.RequestRowID != b.RequestRowID || a.RequestID != b.RequestID || a.UserID != b.UserID || a.APIKeyID != b.APIKeyID || a.GroupID != b.GroupID || a.SubscriptionID != b.SubscriptionID || a.TermID != b.TermID || a.AccountID != b.AccountID || a.AccountType != b.AccountType || a.KeyQuotaTracked != b.KeyQuotaTracked || a.KeyRateLimitTracked != b.KeyRateLimitTracked || a.AccountQuotaTracked != b.AccountQuotaTracked || !a.AdmittedAt.Equal(b.AdmittedAt) || !a.PricingAt.Equal(b.PricingAt) || a.PricingSnapshotHash != b.PricingSnapshotHash {
		return false
	}
	_, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(a)
	return err == nil && hash == b.PricingSnapshotHash
}
func subscriptionQuotaLockRequest(ctx context.Context, tx *sql.Tx, snapshot domain.SubscriptionQuotaBillingSnapshot) (subscriptionQuotaStoredRequest, error) {
	if err := subscriptionQuotaScopeLock(ctx, tx, snapshot.UserID, snapshot.GroupID); err != nil {
		return subscriptionQuotaStoredRequest{}, err
	}
	var parentID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM user_subscriptions WHERE id=$1 AND user_id=$2 AND group_id=$3 FOR UPDATE`, snapshot.SubscriptionID, snapshot.UserID, snapshot.GroupID).Scan(&parentID)
	if err != nil {
		return subscriptionQuotaStoredRequest{}, err
	}
	var termID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM subscription_quota_terms WHERE id=$1 AND subscription_id=$2 AND user_id=$3 AND group_id=$4 FOR UPDATE`, snapshot.TermID, parentID, snapshot.UserID, snapshot.GroupID).Scan(&termID)
	if err != nil {
		return subscriptionQuotaStoredRequest{}, err
	}
	row, err := subscriptionQuotaScanRequest(tx.QueryRowContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE id=$1 FOR UPDATE`, snapshot.RequestRowID))
	if err != nil {
		return row, err
	}
	if !subscriptionQuotaSameSnapshot(snapshot, row.known.Snapshot) {
		return row, service.ErrSubscriptionQuotaReceiptConflict
	}
	return row, nil
}
func subscriptionQuotaSameJSON(a, b json.RawMessage) bool {
	aa, err := domain.CanonicalSubscriptionQuotaJSON(a)
	if err != nil {
		return false
	}
	bb, err := domain.CanonicalSubscriptionQuotaJSON(b)
	return err == nil && string(aa) == string(bb)
}

func (r *subscriptionQuotaRepository) PersistKnownUsage(ctx context.Context, in domain.SubscriptionQuotaKnownUsage) error {
	normalized, err := domain.NormalizeSubscriptionQuotaCost(in.ActualCostUSD)
	if err != nil {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	in.ActualCostUSD = normalized
	command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(in.BillingPayload)
	if err != nil {
		return err
	}
	if in.ReceiptVersion != service.SubscriptionQuotaUsageBillingReceiptVersion || in.RequestFingerprint != command.RequestFingerprint || !subscriptionQuotaSameSnapshot(in.Snapshot, *command.SubscriptionTermSnapshot) || in.ActualCostUSD != command.SubscriptionTermCost.StringFixed(8) {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	if err = service.ValidateSubscriptionQuotaUsageLogPayload(in.UsageLogPayload, command); err != nil {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	if r == nil || r.db == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := subscriptionQuotaLockRequest(ctx, tx, in.Snapshot)
	if err != nil {
		return err
	}
	if stored.hasCost || stored.known.BillingPayload != nil || stored.status == "no_charge" {
		savedCost, costErr := domain.NormalizeSubscriptionQuotaCost(stored.known.ActualCostUSD)
		if costErr == nil && savedCost == in.ActualCostUSD && stored.known.ReceiptVersion == in.ReceiptVersion && stored.known.RequestFingerprint == in.RequestFingerprint && subscriptionQuotaSameJSON(stored.known.BillingPayload, in.BillingPayload) && subscriptionQuotaSameJSON(stored.known.UsageLogPayload, in.UsageLogPayload) {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET
 status=CASE WHEN status IN ('settled','no_charge') THEN status ELSE 'reconcile_required' END,
 revision=revision+1,last_error_category=CASE WHEN last_error_category='ledger_mismatch' THEN 'ledger_mismatch' WHEN status IN ('settled','no_charge') THEN 'terminal_receipt_conflict' ELSE 'receipt_conflict' END,
 resolution_evidence=COALESCE(resolution_evidence,'{}'::jsonb)||jsonb_build_object('latest_receipt_conflict',jsonb_build_object('fingerprint',$2::text,'actual_cost_usd',$3::text,'recorded_at',clock_timestamp())),
 updated_at=clock_timestamp() WHERE id=$1`, in.Snapshot.RequestRowID, in.RequestFingerprint, in.ActualCostUSD)
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	if stored.status != "admitted" && stored.status != "reconcile_required" {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='usage_known',actual_cost_usd=$2,receipt_version=$3,request_fingerprint=$4,billing_payload=$5,usage_log_payload=$6,revision=revision+1,next_retry_at=clock_timestamp(),lease_owner=NULL,lease_until=NULL,last_error_category=NULL,updated_at=clock_timestamp() WHERE id=$1`, in.Snapshot.RequestRowID, in.ActualCostUSD, in.ReceiptVersion, in.RequestFingerprint, in.BillingPayload, in.UsageLogPayload)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func validSubscriptionQuotaNoCharge(e domain.SubscriptionQuotaNoChargeEvidence) bool {
	if e.Kind != "upstream_rejected" || !strings.HasPrefix(e.Reference, "http:") {
		return false
	}
	status, err := strconv.Atoi(strings.TrimPrefix(e.Reference, "http:"))
	if err != nil || e.Reference != fmt.Sprintf("http:%d", status) {
		return false
	}
	switch status {
	case 400, 401, 403, 404, 409, 413, 415, 422, 429:
		return true
	}
	return false
}
func (r *subscriptionQuotaRepository) MarkNoCharge(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, evidence domain.SubscriptionQuotaNoChargeEvidence) error {
	if r == nil || r.db == nil || snapshot == nil || !validSubscriptionQuotaNoCharge(evidence) {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := subscriptionQuotaLockRequest(ctx, tx, *snapshot)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(evidence)
	if stored.status == "no_charge" {
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT resolution_evidence->>'kind'=$2 AND resolution_evidence->>'reference'=$3 FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID, evidence.Kind, evidence.Reference).Scan(&same)
		if err != nil {
			return err
		}
		if same {
			return nil
		}
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	if stored.status != "admitted" || stored.hasCost || stored.known.BillingPayload != nil {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='no_charge',actual_cost_usd=0,resolution_evidence=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, snapshot.RequestRowID, raw)
	if err != nil {
		return err
	}
	if err = subscriptionQuotaConvergeTermTx(ctx, tx, snapshot.TermID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *subscriptionQuotaRepository) MarkReconcileRequired(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, reason string) error {
	if r == nil || r.db == nil || snapshot == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	switch reason {
	case "usage_missing", "usage_incomplete", "forward_failed", "pricing_failed", "receipt_conflict", "admission_stale", "usage_settlement_failed":
	default:
		reason = "usage_incomplete"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := subscriptionQuotaLockRequest(ctx, tx, *snapshot)
	if err != nil {
		return err
	}
	if stored.status != "admitted" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='reconcile_required',revision=revision+1,last_error_category=$2,updated_at=clock_timestamp() WHERE id=$1 AND status='admitted'`, snapshot.RequestRowID, reason)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *subscriptionQuotaRepository) GetBillingRequest(ctx context.Context, id int64) (domain.SubscriptionQuotaKnownUsage, string, error) {
	row, err := subscriptionQuotaScanRequest(r.db.QueryRowContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return row.known, "", service.ErrSubscriptionQuotaNotFound
	}
	return row.known, row.status, err
}

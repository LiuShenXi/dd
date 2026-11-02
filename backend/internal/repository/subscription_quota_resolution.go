package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type subscriptionQuotaResolutionOperation struct {
	SchemaVersion int                                   `json:"schema_version"`
	Intent        json.RawMessage                       `json:"intent"`
	Response      domain.SubscriptionQuotaResolveResult `json:"response"`
}

func NewSubscriptionQuotaResolutionRepository(db *sql.DB) service.SubscriptionQuotaResolutionRepository {
	return &subscriptionQuotaRepository{db: db}
}

func subscriptionQuotaResolutionFingerprint(intent json.RawMessage) (string, error) {
	canonical, err := domain.CanonicalSubscriptionQuotaJSON(intent)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func subscriptionQuotaReplayResolution(ctx context.Context, tx *sql.Tx, scope, key string, intent json.RawMessage) (*domain.SubscriptionQuotaResolveResult, error) {
	var data []byte
	var fingerprint, kind string
	err := tx.QueryRowContext(ctx, `SELECT operation_kind,request_fingerprint,result FROM subscription_quota_operations WHERE idempotency_scope=$1 AND idempotency_key=$2`, scope, key).Scan(&kind, &fingerprint, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var op subscriptionQuotaResolutionOperation
	if json.Unmarshal(data, &op) != nil || op.SchemaVersion != 1 || kind != "resolve_request" {
		return nil, service.ErrSubscriptionQuotaIdempotency
	}
	incoming, err := subscriptionQuotaResolutionFingerprint(intent)
	if err != nil {
		return nil, err
	}
	saved, err := subscriptionQuotaResolutionFingerprint(op.Intent)
	if err != nil || incoming != saved || fingerprint != saved {
		return nil, service.ErrSubscriptionQuotaIdempotency
	}
	op.Response.Replayed = true
	return &op.Response, nil
}

func subscriptionQuotaReadRequestProjection(ctx context.Context, q subscriptionQuotaQuerier, id int64) (domain.SubscriptionQuotaRequest, error) {
	return subscriptionQuotaScanRequestProjection(q.QueryRowContext(ctx, `SELECT `+subscriptionQuotaRequestProjectionColumns+` FROM subscription_quota_requests WHERE id=$1`, id))
}

func subscriptionQuotaValidateResolvedKnown(stored domain.SubscriptionQuotaKnownUsage, in domain.SubscriptionQuotaKnownUsage) error {
	if !subscriptionQuotaSameSnapshot(stored.Snapshot, in.Snapshot) {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(in.BillingPayload)
	if err != nil {
		return err
	}
	normalized, err := domain.NormalizeSubscriptionQuotaCost(in.ActualCostUSD)
	if err != nil || in.ReceiptVersion != service.SubscriptionQuotaUsageBillingReceiptVersion || command.SubscriptionTermSnapshot == nil || !subscriptionQuotaSameSnapshot(stored.Snapshot, *command.SubscriptionTermSnapshot) || in.RequestFingerprint != command.RequestFingerprint || normalized != command.SubscriptionTermCost.StringFixed(8) {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	return service.ValidateSubscriptionQuotaUsageLogPayload(in.UsageLogPayload, command)
}

func (r *subscriptionQuotaRepository) ResolveRequest(ctx context.Context, p domain.SubscriptionQuotaResolutionParams) (*domain.SubscriptionQuotaResolveResult, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if p.RequestRowID <= 0 || p.ActorID <= 0 || p.ExpectedRequestRevision < 0 || !subscriptionQuotaValidKey(p.IdempotencyKey) || p.EvidenceReference == "" || p.Reason == "" || strings.TrimSpace(p.EvidenceReference) != p.EvidenceReference || strings.TrimSpace(p.Reason) != p.Reason || utf8.RuneCountInString(p.EvidenceReference) > 500 || utf8.RuneCountInString(p.Reason) > 500 {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if p.Resolution != "no_charge" && p.Resolution != "verified_usage" || p.Resolution == "verified_usage" && p.PrepareKnown == nil || p.Resolution == "no_charge" && p.PrepareKnown != nil {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if _, err := subscriptionQuotaResolutionFingerprint(p.NormalizedIntent); err != nil {
		return nil, service.ErrSubscriptionQuotaInput
	}
	var intent struct {
		ExpectedRequestRevision *int64          `json:"expected_request_revision"`
		Resolution              string          `json:"resolution"`
		EvidenceReference       string          `json:"evidence_reference"`
		Reason                  string          `json:"reason"`
		VerifiedUsage           json.RawMessage `json:"verified_usage"`
	}
	if json.Unmarshal(p.NormalizedIntent, &intent) != nil || intent.ExpectedRequestRevision == nil || *intent.ExpectedRequestRevision != p.ExpectedRequestRevision || intent.Resolution != p.Resolution || intent.EvidenceReference != p.EvidenceReference || intent.Reason != p.Reason {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if p.Resolution == "no_charge" && len(intent.VerifiedUsage) > 0 {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if p.Resolution == "verified_usage" {
		if _, err := domain.CanonicalSubscriptionQuotaJSON(intent.VerifiedUsage); err != nil {
			return nil, service.ErrSubscriptionQuotaInput
		}
	}
	discovered, err := subscriptionQuotaScanRequest(r.db.QueryRowContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE id=$1`, p.RequestRowID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	snapshot := discovered.known.Snapshot
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, snapshot.UserID, snapshot.GroupID); err != nil {
		return nil, err
	}
	scope := fmt.Sprintf("admin:%d:user:%d:group:%d:resolve:%d", p.ActorID, snapshot.UserID, snapshot.GroupID, p.RequestRowID)
	if replay, err := subscriptionQuotaReplayResolution(ctx, tx, scope, p.IdempotencyKey, p.NormalizedIntent); err != nil || replay != nil {
		return replay, err
	}
	// The helper reacquires the same scope then locks parent -> term -> request.
	// No request-row lock is taken during discovery or before its parent locks.
	stored, err := subscriptionQuotaLockRequest(ctx, tx, snapshot)
	if err != nil {
		return nil, err
	}
	if stored.known.RequestRevision != p.ExpectedRequestRevision {
		return nil, service.ErrSubscriptionQuotaRequestRevision
	}
	if stored.status != "reconcile_required" || stored.hasCost || stored.known.BillingPayload != nil || stored.known.UsageLogPayload != nil || stored.known.ReceiptVersion != 0 || stored.known.RequestFingerprint != "" {
		return nil, service.ErrSubscriptionQuotaNotResolvable
	}
	var ledger bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_quota_ledger WHERE request_row_id=$1)`, p.RequestRowID).Scan(&ledger); err != nil {
		return nil, err
	}
	if ledger {
		return nil, service.ErrSubscriptionQuotaNotResolvable
	}
	// Recheck the audit actor without requiring the original user, key or group
	// to remain active; those later changes never erase admitted consumption.
	var role, status string
	err = tx.QueryRowContext(ctx, `SELECT role,status FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, p.ActorID).Scan(&role, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	if role != "admin" || status != "active" {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	audit, err := json.Marshal(struct {
		ActorID    int64           `json:"actor_id"`
		Intent     json.RawMessage `json:"intent"`
		ResolvedAt time.Time       `json:"resolved_at"`
	}{p.ActorID, p.NormalizedIntent, now})
	if err != nil {
		return nil, err
	}
	if p.Resolution == "verified_usage" {
		known, err := p.PrepareKnown(stored.known)
		if err != nil {
			return nil, err
		}
		if err = subscriptionQuotaValidateResolvedKnown(stored.known, known); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='usage_known',actual_cost_usd=$2,receipt_version=$3,request_fingerprint=$4,billing_payload=$5,usage_log_payload=$6,resolution_evidence=COALESCE(resolution_evidence,'{}'::jsonb)||jsonb_build_object('manual_resolution',$7::jsonb),revision=revision+1,next_retry_at=$8,lease_owner=NULL,lease_until=NULL,last_error_category=NULL,updated_at=$8 WHERE id=$1`, p.RequestRowID, known.ActualCostUSD, known.ReceiptVersion, known.RequestFingerprint, known.BillingPayload, known.UsageLogPayload, audit, now)
		if err != nil {
			return nil, err
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_requests SET status='no_charge',actual_cost_usd=0,resolution_evidence=COALESCE(resolution_evidence,'{}'::jsonb)||jsonb_build_object('manual_resolution',$2::jsonb),revision=revision+1,lease_owner=NULL,lease_until=NULL,last_error_category=NULL,updated_at=$3 WHERE id=$1`, p.RequestRowID, audit, now)
		if err != nil {
			return nil, err
		}
		if err = subscriptionQuotaConvergeTermTx(ctx, tx, snapshot.TermID); err != nil {
			return nil, err
		}
	}
	projected, err := subscriptionQuotaReadRequestProjection(ctx, tx, p.RequestRowID)
	if err != nil {
		return nil, err
	}
	result := domain.SubscriptionQuotaResolveResult{Request: projected}
	operation, err := json.Marshal(subscriptionQuotaResolutionOperation{SchemaVersion: 1, Intent: p.NormalizedIntent, Response: result})
	if err != nil {
		return nil, err
	}
	fingerprint, err := subscriptionQuotaResolutionFingerprint(p.NormalizedIntent)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_quota_operations(idempotency_scope,idempotency_key,operation_kind,actor_id,request_fingerprint,result) VALUES($1,$2,'resolve_request',$3,$4,$5)`, scope, p.IdempotencyKey, p.ActorID, fingerprint, operation)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

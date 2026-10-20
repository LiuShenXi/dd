package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func validSubscriptionQuotaRequestID(value string) bool {
	if !strings.HasPrefix(value, "subquota:") {
		return false
	}
	id, err := uuid.Parse(strings.TrimPrefix(value, "subquota:"))
	return err == nil && "subquota:"+id.String() == value
}

func (r *subscriptionQuotaRepository) AdmitRequest(ctx context.Context, p domain.SubscriptionQuotaAdmissionParams) (_ *domain.SubscriptionQuotaBillingSnapshot, resultErr error) {
	defer func() { service.ObserveSubscriptionQuotaAdmission(resultErr) }()
	if r == nil || r.db == nil || p.UserID <= 0 || p.GroupID <= 0 || p.APIKeyID <= 0 || p.AccountID <= 0 || p.ExpectedAccountType == "" || !validSubscriptionQuotaRequestID(p.RequestID) || p.FreezePricing == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := r.admitRequestOnce(ctx, p)
		if !errors.Is(err, service.ErrSubscriptionQuotaAdmissionRetry) {
			return snapshot, err
		}
	}
	return nil, service.ErrSubscriptionQuotaAdmissionRetry
}
func (r *subscriptionQuotaRepository) admitRequestOnce(ctx context.Context, p domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, p.UserID, p.GroupID); err != nil {
		return nil, err
	}
	parent, err := subscriptionQuotaReadParent(ctx, tx, p.UserID, p.GroupID, true)
	if err != nil {
		return nil, err
	}
	if parent.id == 0 {
		return nil, service.ErrSubscriptionQuotaNotActive
	}
	before, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parent.id, true)
	if err != nil {
		return nil, err
	}
	var selected *domain.SubscriptionQuotaTerm
	for i := range terms {
		t := &terms[i]
		if t.CancelledAt == nil && !before.Before(t.StartsAt) && before.Before(t.ExpiresAt) {
			if selected != nil {
				return nil, service.ErrSubscriptionQuotaBillingUnavailable
			}
			selected = t
		}
	}
	if err = subscriptionQuotaCheckEntities(ctx, tx, p.UserID, p.GroupID, 0, true); err != nil {
		return nil, err
	}
	if err = subscriptionQuotaCheckClaudeConcurrency(ctx, tx, p.UserID, p.DefaultClaudeConcurrency); err != nil {
		return nil, err
	}
	var userID, groupID int64
	var status string
	var expiry, w5, w1, w7 sql.NullTime
	var quota, used, limit5, limit1, limit7, used5, used1, used7 decimal.Decimal
	err = tx.QueryRowContext(ctx, `SELECT user_id,group_id,status,expires_at,quota::text,quota_used::text,rate_limit_5h::text,rate_limit_1d::text,rate_limit_7d::text,usage_5h::text,usage_1d::text,usage_7d::text,window_5h_start,window_1d_start,window_7d_start FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, p.APIKeyID).Scan(&userID, &groupID, &status, &expiry, &quota, &used, &limit5, &limit1, &limit7, &used5, &used1, &used7, &w5, &w1, &w7)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaNotActive
	}
	if err != nil {
		return nil, err
	}
	var accountType, platform, accountStatus string
	var schedulable, accountQuota bool
	var member bool
	err = tx.QueryRowContext(ctx, `SELECT a.type,a.platform,a.status,a.schedulable,
 (COALESCE((a.extra->>'quota_limit')::numeric,0)>0 OR COALESCE((a.extra->>'quota_daily_limit')::numeric,0)>0 OR COALESCE((a.extra->>'quota_weekly_limit')::numeric,0)>0),
 EXISTS(SELECT 1 FROM account_groups ag WHERE ag.account_id=a.id AND ag.group_id=$2)
 FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL FOR SHARE`, p.AccountID, p.GroupID).Scan(&accountType, &platform, &accountStatus, &schedulable, &accountQuota, &member)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	currentID := int64(0)
	for _, term := range terms {
		if term.CancelledAt == nil && !now.Before(term.StartsAt) && now.Before(term.ExpiresAt) {
			if currentID != 0 {
				return nil, service.ErrSubscriptionQuotaBillingUnavailable
			}
			currentID = term.ID
		}
	}
	if selected == nil && currentID != 0 || selected != nil && selected.ID != currentID {
		return nil, service.ErrSubscriptionQuotaAdmissionRetry
	}
	if selected == nil {
		summary := subscriptionQuotaSummary(p.GroupID, parent.revision, terms, now)
		if summary.LastTerm != nil {
			if summary.LastTerm.CancelledAt != nil {
				return nil, service.ErrSubscriptionQuotaCancelled
			}
			return nil, service.ErrSubscriptionQuotaExpired
		}
		return nil, service.ErrSubscriptionQuotaNotActive
	}
	windowExceeded := func(limit, usage decimal.Decimal, start sql.NullTime, d time.Duration) bool {
		return limit.IsPositive() && start.Valid && start.Time.Add(d).After(now) && !usage.LessThan(limit)
	}
	if userID != p.UserID || groupID != p.GroupID || status != service.StatusActive || (expiry.Valid && !expiry.Time.After(now)) || (quota.IsPositive() && !used.LessThan(quota)) || windowExceeded(limit5, used5, w5, 5*time.Hour) || windowExceeded(limit1, used1, w1, 24*time.Hour) || windowExceeded(limit7, used7, w7, 168*time.Hour) {
		return nil, service.ErrSubscriptionQuotaNotActive
	}
	if platform != service.PlatformAnthropic || accountStatus != service.StatusActive || !schedulable || !member || accountType != p.ExpectedAccountType {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if selected.ReconcileRequiredCount > 0 {
		return nil, service.ErrSubscriptionQuotaReconciliation
	}
	allowed, _ := decimal.NewFromString(selected.QuotaUSD)
	charged, _ := decimal.NewFromString(selected.UsedUSD)
	pending, _ := decimal.NewFromString(selected.PendingUSD)
	if !charged.Add(pending).LessThan(allowed) {
		metadata := map[string]string{}
		for _, term := range terms {
			if term.CancelledAt == nil && term.StartsAt.After(now) {
				metadata["next_available_at"] = term.StartsAt.UTC().Format(time.RFC3339Nano)
				break
			}
		}
		return nil, service.ErrSubscriptionQuotaExhausted.WithMetadata(metadata)
	}
	pricing, err := p.FreezePricing(now)
	if err != nil {
		return nil, err
	}
	snapshot := domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestID: p.RequestID, UserID: p.UserID, APIKeyID: p.APIKeyID, GroupID: p.GroupID, SubscriptionID: parent.id, TermID: selected.ID, AccountID: p.AccountID, AccountType: accountType, KeyQuotaTracked: quota.IsPositive(), KeyRateLimitTracked: limit5.IsPositive() || limit1.IsPositive() || limit7.IsPositive(), AccountQuotaTracked: accountQuota && (accountType == service.AccountTypeAPIKey || accountType == service.AccountTypeBedrock), AdmittedAt: now, PricingAt: now, PricingSnapshot: pricing}
	envelope, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.PricingSnapshotHash = hash
	if len(envelope) > 1024*1024 {
		return nil, fmt.Errorf("quota pricing snapshot is too large")
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO subscription_quota_requests(request_id,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,status,lease_owner,lease_until) VALUES($1,$2,$3,$4,$5,$6,$7::timestamptz,$7::timestamptz,$8,'admitted',$9,$7::timestamptz+INTERVAL '30 seconds') RETURNING id`, p.RequestID, p.APIKeyID, parent.id, selected.ID, p.UserID, p.GroupID, now, envelope, "forward:"+p.RequestID).Scan(&snapshot.RequestRowID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	copy := snapshot.Clone()
	return &copy, nil
}

func (r *subscriptionQuotaRepository) Heartbeat(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot) error {
	if r == nil || r.db == nil || snapshot == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	// A lease-only update never waits for scope/parent/term, and does not change
	// business revision. Its owner is fixed to this server-generated admission.
	_, err := r.db.ExecContext(ctx, `UPDATE subscription_quota_requests SET lease_until=clock_timestamp()+INTERVAL '30 seconds',updated_at=clock_timestamp() WHERE id=$1 AND request_id=$2 AND user_id=$3 AND api_key_id=$4 AND group_id=$5 AND term_id=$6 AND status='admitted' AND lease_owner=$7`, snapshot.RequestRowID, snapshot.RequestID, snapshot.UserID, snapshot.APIKeyID, snapshot.GroupID, snapshot.TermID, "forward:"+snapshot.RequestID)
	return err
}

func subscriptionQuotaDecodePricingEnvelope(raw json.RawMessage, snapshot *domain.SubscriptionQuotaBillingSnapshot) error {
	var envelope domain.SubscriptionQuotaPricingEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.SchemaVersion != 1 || envelope.AccountID <= 0 {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	snapshot.SchemaVersion = 1
	snapshot.AccountID = envelope.AccountID
	snapshot.AccountType = envelope.AccountType
	snapshot.KeyQuotaTracked = envelope.KeyQuotaTracked
	snapshot.KeyRateLimitTracked = envelope.KeyRateLimitTracked
	snapshot.AccountQuotaTracked = envelope.AccountQuotaTracked
	snapshot.PricingSnapshot = append(json.RawMessage(nil), envelope.Pricing...)
	_, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(*snapshot)
	snapshot.PricingSnapshotHash = hash
	return err
}

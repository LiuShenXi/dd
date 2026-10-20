package repository

import (
	"bytes"
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

type subscriptionQuotaRepository struct{ db *sql.DB }

func NewSubscriptionQuotaRepository(db *sql.DB) service.SubscriptionQuotaRepository {
	return &subscriptionQuotaRepository{db: db}
}

type subscriptionQuotaQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type subscriptionQuotaParent struct{ id, revision int64 }
type subscriptionQuotaOperation struct {
	SchemaVersion int                                     `json:"schema_version"`
	Intent        json.RawMessage                         `json:"intent"`
	Snapshot      *domain.SubscriptionQuotaSourceSnapshot `json:"snapshot,omitempty"`
	Response      domain.SubscriptionQuotaMutationResult  `json:"response"`
}

func subscriptionQuotaNormalizeOpen(p domain.SubscriptionQuotaOpenParams) (domain.SubscriptionQuotaOpenParams, error) {
	if p.UserID <= 0 || p.GroupID <= 0 || p.ActorID <= 0 || p.ExpectedRevision < 0 || (p.Operation != "open" && p.Operation != "renew") || (p.PlanID != nil && *p.PlanID <= 0) || utf8.RuneCountInString(p.Notes) > 500 {
		return p, service.ErrSubscriptionQuotaInput
	}
	if p.QuotaUSD != nil {
		quota, err := domain.NormalizeSubscriptionQuotaMoney(*p.QuotaUSD)
		if err != nil {
			return p, service.ErrSubscriptionQuotaInvalid
		}
		p.QuotaUSD = &quota
	} else if p.PlanID == nil {
		return p, service.ErrSubscriptionQuotaRequired
	}
	return p, nil
}
func subscriptionQuotaValidKey(key string) bool {
	return len(key) >= 16 && len(key) <= 128 && strings.TrimSpace(key) == key && !strings.ContainsAny(key, "\r\n\t\x00")
}

func subscriptionQuotaScopeLock(ctx context.Context, tx *sql.Tx, userID, groupID int64) error {
	started := time.Now()
	defer func() { service.ObserveSubscriptionQuotaScopeWait(time.Since(started)) }()
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1397839188))`, fmt.Sprintf("subscription_quota:%d:%d", userID, groupID))
	return err
}
func subscriptionQuotaDBNow(ctx context.Context, q subscriptionQuotaQuerier) (time.Time, error) {
	var now time.Time
	err := q.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), err
}
func subscriptionQuotaReadParent(ctx context.Context, q subscriptionQuotaQuerier, userID, groupID int64, lock bool) (subscriptionQuotaParent, error) {
	var p subscriptionQuotaParent
	query := `SELECT id,quota_revision FROM user_subscriptions WHERE user_id=$1 AND group_id=$2 AND deleted_at IS NULL`
	if lock {
		query += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, query, userID, groupID).Scan(&p.id, &p.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}

// Entity locks are deliberately late and narrow. They do not lock GPT scope,
// terms, cycles or ledger. Lock waits are followed by a fresh DB clock.
func subscriptionQuotaCheckEntities(ctx context.Context, q subscriptionQuotaQuerier, userID, groupID, actorID int64, lock bool) error {
	userQuery := `SELECT status FROM users WHERE id=$1 AND deleted_at IS NULL`
	if lock {
		userQuery += ` FOR SHARE`
	}
	var status string
	if err := q.QueryRowContext(ctx, userQuery, userID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionQuotaNotFound
	} else if err != nil {
		return err
	}
	if status != "active" {
		return service.ErrSubscriptionQuotaNotFound
	}
	var platform, kind, mode string
	var fallback, fallbackInvalid sql.NullInt64
	groupQuery := `SELECT platform,subscription_type,subscription_quota_mode,status,fallback_group_id,fallback_group_id_on_invalid_request FROM groups WHERE id=$1 AND deleted_at IS NULL`
	if lock {
		groupQuery += ` FOR SHARE`
	}
	err := q.QueryRowContext(ctx, groupQuery, groupID).Scan(&platform, &kind, &mode, &status, &fallback, &fallbackInvalid)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionQuotaGroupInvalid
	}
	if err != nil {
		return err
	}
	if platform != "anthropic" || kind != "subscription" || mode != "term" || status != "active" || fallback.Valid || fallbackInvalid.Valid {
		return service.ErrSubscriptionQuotaGroupInvalid
	}
	if actorID > 0 {
		var role string
		actorQuery := `SELECT role,status FROM users WHERE id=$1 AND deleted_at IS NULL`
		if lock {
			actorQuery += ` FOR SHARE`
		}
		err = q.QueryRowContext(ctx, actorQuery, actorID).Scan(&role, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrSubscriptionQuotaNotFound
		}
		if err != nil {
			return err
		}
		if role != "admin" || status != "active" {
			return service.ErrSubscriptionQuotaNotFound
		}
	}
	return nil
}

func subscriptionQuotaResolveSource(ctx context.Context, q subscriptionQuotaQuerier, p domain.SubscriptionQuotaOpenParams, lock bool) (domain.SubscriptionQuotaSourceSnapshot, error) {
	snapshot := domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, DurationHours: 168, QuotaSource: "admin", PlanID: p.PlanID}
	if p.PlanID != nil {
		var groupID int64
		var quota sql.NullString
		var days int
		var unit string
		var name string
		var updated time.Time
		query := `SELECT group_id,term_quota_usd::text,validity_days,validity_unit,name,updated_at FROM subscription_plans WHERE id=$1`
		if lock {
			query += ` FOR SHARE`
		}
		err := q.QueryRowContext(ctx, query, *p.PlanID).Scan(&groupID, &quota, &days, &unit, &name, &updated)
		if errors.Is(err, sql.ErrNoRows) {
			return snapshot, service.ErrSubscriptionQuotaRequired
		}
		if err != nil {
			return snapshot, err
		}
		if groupID != p.GroupID || days != 7 || unit != "day" {
			return snapshot, service.ErrSubscriptionQuotaInput
		}
		if !quota.Valid {
			return snapshot, service.ErrSubscriptionQuotaRequired
		}
		normalized, err := domain.NormalizeSubscriptionQuotaMoney(quota.String)
		if err != nil {
			return snapshot, service.ErrSubscriptionQuotaInvalid
		}
		snapshot.PlanName = name
		snapshot.PlanUpdatedAt = &updated
		snapshot.PlanDefaultQuotaUSD = &normalized
		if p.QuotaUSD == nil {
			snapshot.QuotaUSD = normalized
			snapshot.QuotaSource = "plan_default"
		}
	}
	if p.QuotaUSD != nil {
		snapshot.QuotaUSD = *p.QuotaUSD
		if p.PlanID != nil {
			snapshot.QuotaSource = "admin_override"
		}
	}
	if snapshot.QuotaUSD == "" {
		return snapshot, service.ErrSubscriptionQuotaRequired
	}
	return snapshot, nil
}

func subscriptionQuotaFingerprint(intent json.RawMessage, snapshot *domain.SubscriptionQuotaSourceSnapshot) string {
	data, _ := json.Marshal(struct {
		Intent   json.RawMessage                         `json:"intent"`
		Snapshot *domain.SubscriptionQuotaSourceSnapshot `json:"snapshot"`
	}{intent, snapshot})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func subscriptionQuotaReplay(ctx context.Context, tx *sql.Tx, scope, key string, intent json.RawMessage) (*domain.SubscriptionQuotaMutationResult, error) {
	var data []byte
	var fingerprint string
	err := tx.QueryRowContext(ctx, `SELECT request_fingerprint,result FROM subscription_quota_operations WHERE idempotency_scope=$1 AND idempotency_key=$2`, scope, key).Scan(&fingerprint, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var op subscriptionQuotaOperation
	if err = json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	// JSONB changes key ordering. Compare decoded normalized objects, not bytes.
	var stored, requested any
	storedDecoder := json.NewDecoder(bytes.NewReader(op.Intent))
	storedDecoder.UseNumber()
	if err = storedDecoder.Decode(&stored); err != nil {
		return nil, err
	}
	requestedDecoder := json.NewDecoder(bytes.NewReader(intent))
	requestedDecoder.UseNumber()
	if err = requestedDecoder.Decode(&requested); err != nil {
		return nil, err
	}
	storedJSON, _ := json.Marshal(stored)
	requestedJSON, _ := json.Marshal(requested)
	if op.SchemaVersion != 1 || string(storedJSON) != string(requestedJSON) {
		return nil, service.ErrSubscriptionQuotaIdempotency
	}
	// Re-encode the typed intent into its canonical field order for the hash.
	if fingerprint != subscriptionQuotaFingerprint(intent, op.Snapshot) {
		return nil, service.ErrSubscriptionQuotaIdempotency
	}
	op.Response.Replayed = true
	return &op.Response, nil
}
func subscriptionQuotaSaveOperation(ctx context.Context, tx *sql.Tx, scope, key, kind string, actor int64, intent json.RawMessage, snapshot *domain.SubscriptionQuotaSourceSnapshot, response domain.SubscriptionQuotaMutationResult) error {
	data, err := json.Marshal(subscriptionQuotaOperation{SchemaVersion: 1, Intent: intent, Snapshot: snapshot, Response: response})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_quota_operations(idempotency_scope,idempotency_key,operation_kind,actor_id,request_fingerprint,result) VALUES($1,$2,$3,$4,$5,$6)`, scope, key, kind, actor, subscriptionQuotaFingerprint(intent, snapshot), data)
	return err
}

func subscriptionQuotaSchedule(terms []domain.SubscriptionQuotaTerm, operation string, now time.Time) (time.Time, error) {
	start := now
	for _, term := range terms {
		if term.CancelledAt != nil || !term.ExpiresAt.After(now) {
			continue
		}
		if operation == "open" {
			return time.Time{}, service.ErrSubscriptionQuotaSchedule
		}
		if term.ExpiresAt.After(start) {
			start = term.ExpiresAt
		}
	}
	return start, nil
}

func (r *subscriptionQuotaRepository) PreviewTerm(ctx context.Context, p domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaPreview, error) {
	var err error
	p, err = subscriptionQuotaNormalizeOpen(p)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaCheckEntities(ctx, tx, p.UserID, p.GroupID, p.ActorID, false); err != nil {
		return nil, err
	}
	source, err := subscriptionQuotaResolveSource(ctx, tx, p, false)
	if err != nil {
		return nil, err
	}
	parent, err := subscriptionQuotaReadParent(ctx, tx, p.UserID, p.GroupID, false)
	if err != nil {
		return nil, err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parent.id, false)
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	start, err := subscriptionQuotaSchedule(terms, p.Operation, now)
	if err != nil {
		return nil, err
	}
	summary := subscriptionQuotaSummary(p.GroupID, parent.revision, terms, now)
	return &domain.SubscriptionQuotaPreview{GroupID: p.GroupID, EffectiveQuotaUSD: source.QuotaUSD, ProposedStartsAt: start, ProposedExpiresAt: start.Add(domain.SubscriptionQuotaDuration), ServerTime: now, CurrentRevision: parent.revision, Warnings: []string{}, CurrentTerm: summary.CurrentTerm, NextTerms: summary.NextTerms}, nil
}

func (r *subscriptionQuotaRepository) OpenTerm(ctx context.Context, p domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaMutationResult, error) {
	var err error
	p, err = subscriptionQuotaNormalizeOpen(p)
	if err != nil {
		return nil, err
	}
	if !subscriptionQuotaValidKey(p.IdempotencyKey) {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, p.UserID, p.GroupID); err != nil {
		return nil, err
	}
	scope := fmt.Sprintf("admin:%d:user:%d:group:%d:%s", p.ActorID, p.UserID, p.GroupID, p.Operation)
	intent, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	replay, err := subscriptionQuotaReplay(ctx, tx, scope, p.IdempotencyKey, intent)
	if err != nil || replay != nil {
		return replay, err
	}
	if !p.IssueAllowed {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	parent, err := subscriptionQuotaReadParent(ctx, tx, p.UserID, p.GroupID, true)
	if err != nil {
		return nil, err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parent.id, true)
	if err != nil {
		return nil, err
	}
	if err = subscriptionQuotaCheckEntities(ctx, tx, p.UserID, p.GroupID, p.ActorID, true); err != nil {
		return nil, err
	}
	if err = subscriptionQuotaCheckClaudeConcurrency(ctx, tx, p.UserID, p.DefaultClaudeConcurrency); err != nil {
		return nil, err
	}
	source, err := subscriptionQuotaResolveSource(ctx, tx, p, true)
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	if p.ExpectedRevision != parent.revision {
		return nil, service.ErrSubscriptionQuotaRevision
	}
	start, err := subscriptionQuotaSchedule(terms, p.Operation, now)
	if err != nil {
		return nil, err
	}
	expiry := start.Add(domain.SubscriptionQuotaDuration)
	if parent.id == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by,assigned_at,notes,created_at,updated_at) VALUES($1,$2,$3,$4,'active',0,$5,$6,$7,$6,$6) RETURNING id`, p.UserID, p.GroupID, start, expiry, p.ActorID, now, p.Notes).Scan(&parent.id)
		if err != nil {
			return nil, err
		}
	}
	nextNo := int64(1)
	for _, term := range terms {
		if term.TermNo >= nextNo {
			nextNo = term.TermNo + 1
		}
	}
	state := "active"
	if now.Before(start) {
		state = "scheduled"
	}
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var termID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_plan_id,source_snapshot,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'admin',$9,$10,$11,$12,$12) RETURNING id`, parent.id, p.UserID, p.GroupID, nextNo, source.QuotaUSD, start, expiry, state, p.PlanID, sourceJSON, p.ActorID, now).Scan(&termID)
	if err != nil {
		return nil, err
	}
	term := domain.ProjectSubscriptionQuotaTerm(domain.SubscriptionQuotaTerm{ID: termID, SubscriptionID: parent.id, UserID: p.UserID, GroupID: p.GroupID, TermNo: nextNo, QuotaUSD: source.QuotaUSD, UsedUSD: "0", PendingUSD: "0", StartsAt: start, ExpiresAt: expiry, LifecycleState: state}, now)
	terms = append(terms, term)
	if err = subscriptionQuotaRefreshParent(ctx, tx, parent.id, terms, now); err != nil {
		return nil, err
	}
	parent.revision++
	if err = subscriptionQuotaInvalidateAuth(ctx, tx, p.UserID, p.GroupID); err != nil {
		return nil, err
	}
	response := domain.SubscriptionQuotaMutationResult{Term: term, Summary: subscriptionQuotaSummary(p.GroupID, parent.revision, terms, now)}
	kind := "open_term"
	if p.Operation == "renew" {
		kind = "renew_term"
	}
	if err = subscriptionQuotaSaveOperation(ctx, tx, scope, p.IdempotencyKey, kind, p.ActorID, intent, &source, response); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *subscriptionQuotaRepository) CancelTerm(ctx context.Context, p domain.SubscriptionQuotaCancelParams) (*domain.SubscriptionQuotaMutationResult, error) {
	p.Reason = strings.TrimSpace(p.Reason)
	if p.TermID <= 0 || p.ActorID <= 0 || p.ExpectedRevision < 0 || p.Reason == "" || utf8.RuneCountInString(p.Reason) > 500 || !subscriptionQuotaValidKey(p.IdempotencyKey) {
		return nil, service.ErrSubscriptionQuotaInput
	}
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	var userID, groupID int64
	err := r.db.QueryRowContext(ctx, `SELECT user_id,group_id FROM subscription_quota_terms WHERE id=$1`, p.TermID).Scan(&userID, &groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, userID, groupID); err != nil {
		return nil, err
	}
	scope := fmt.Sprintf("admin:%d:user:%d:group:%d:cancel:%d", p.ActorID, userID, groupID, p.TermID)
	intent, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	replay, err := subscriptionQuotaReplay(ctx, tx, scope, p.IdempotencyKey, intent)
	if err != nil || replay != nil {
		return replay, err
	}
	parent, err := subscriptionQuotaReadParent(ctx, tx, userID, groupID, true)
	if err != nil {
		return nil, err
	}
	if parent.id == 0 {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parent.id, true)
	if err != nil {
		return nil, err
	}
	if err = subscriptionQuotaCheckEntities(ctx, tx, userID, groupID, p.ActorID, true); err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	if p.ExpectedRevision != parent.revision {
		return nil, service.ErrSubscriptionQuotaRevision
	}
	target := -1
	for i := range terms {
		if terms[i].ID == p.TermID {
			target = i
			break
		}
	}
	if target < 0 {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	term := terms[target]
	if term.CancelledAt != nil || !term.ExpiresAt.After(now) {
		return nil, service.ErrSubscriptionQuotaNotCancellable
	}
	term.CancelledAt = &now
	term.Revision++
	term.LifecycleState = "closed"
	var closedAt *time.Time = &now
	if term.UnsettledRequestCount > 0 {
		term.LifecycleState = "closing"
		closedAt = nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_terms SET cancelled_at=$2,lifecycle_state=$3,closed_at=$4,revision=revision+1,updated_at=$2 WHERE id=$1`, term.ID, now, term.LifecycleState, closedAt)
	if err != nil {
		return nil, err
	}
	terms[target] = domain.ProjectSubscriptionQuotaTerm(term, now)
	if err = subscriptionQuotaRefreshParent(ctx, tx, parent.id, terms, now); err != nil {
		return nil, err
	}
	parent.revision++
	if err = subscriptionQuotaInvalidateAuth(ctx, tx, userID, groupID); err != nil {
		return nil, err
	}
	response := domain.SubscriptionQuotaMutationResult{Term: terms[target], Summary: subscriptionQuotaSummary(groupID, parent.revision, terms, now)}
	if err = subscriptionQuotaSaveOperation(ctx, tx, scope, p.IdempotencyKey, "cancel_term", p.ActorID, intent, nil, response); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &response, nil
}

func subscriptionQuotaInvalidateAuth(ctx context.Context, tx *sql.Tx, userID, groupID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO auth_cache_invalidation_outbox(cache_key) SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE user_id=$1 AND group_id=$2 AND deleted_at IS NULL`, userID, groupID)
	return err
}

func subscriptionQuotaRefreshParent(ctx context.Context, tx *sql.Tx, parentID int64, terms []domain.SubscriptionQuotaTerm, now time.Time) error {
	var start, expiry time.Time
	for _, term := range terms {
		if start.IsZero() || term.StartsAt.Before(start) {
			start = term.StartsAt
		}
		if term.CancelledAt != nil {
			continue
		}
		if term.ExpiresAt.After(expiry) {
			expiry = term.ExpiresAt
		}
	}
	status := "active"
	if expiry.IsZero() {
		status = "cancelled"
		for _, term := range terms {
			if start.IsZero() || term.StartsAt.Before(start) {
				start = term.StartsAt
			}
			if term.ExpiresAt.After(expiry) {
				expiry = term.ExpiresAt
			}
		}
	} else if !expiry.After(now) {
		status = "expired"
	}
	_, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET starts_at=$2,expires_at=$3,status=$4,quota_revision=quota_revision+1,updated_at=$5 WHERE id=$1`, parentID, start, expiry, status, now)
	return err
}

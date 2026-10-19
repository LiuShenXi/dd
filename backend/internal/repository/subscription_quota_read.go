package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// One predicate is shared by Key binding/visibility reads. The clock is sampled
// once by a materialized CTE; future parent expiry never grants an entitlement.
const subscriptionQuotaCurrentTermPredicate = `t.cancelled_at IS NULL AND t.starts_at <= qc.now AND qc.now < t.expires_at`
const subscriptionQuotaCurrentTermIdentity = ` FROM subscription_quota_terms t
 JOIN user_subscriptions p ON p.id=t.subscription_id AND p.user_id=t.user_id AND p.group_id=t.group_id AND p.deleted_at IS NULL
 JOIN users u ON u.id=t.user_id AND u.deleted_at IS NULL AND u.status='active'
 JOIN groups g ON g.id=t.group_id AND g.deleted_at IS NULL AND g.status='active'
 AND g.platform='anthropic' AND g.subscription_type='subscription' AND g.subscription_quota_mode='term'
 AND g.fallback_group_id IS NULL AND g.fallback_group_id_on_invalid_request IS NULL
 CROSS JOIN quota_clock qc WHERE `

func (r *subscriptionQuotaRepository) HasCurrentTerm(ctx context.Context, userID, groupID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `WITH quota_clock AS MATERIALIZED (SELECT clock_timestamp() AS now) SELECT EXISTS(SELECT 1`+subscriptionQuotaCurrentTermIdentity+subscriptionQuotaCurrentTermPredicate+` AND t.user_id=$1 AND t.group_id=$2)`, userID, groupID).Scan(&exists)
	return exists, err
}
func (r *subscriptionQuotaRepository) ListCurrentTermGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `WITH quota_clock AS MATERIALIZED (SELECT clock_timestamp() AS now) SELECT DISTINCT t.group_id`+subscriptionQuotaCurrentTermIdentity+subscriptionQuotaCurrentTermPredicate+` AND t.user_id=$1 ORDER BY t.group_id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

const subscriptionQuotaTermColumns = `t.id,t.subscription_id,t.user_id,t.group_id,t.term_no,t.quota_usd::text,t.used_usd::text,t.starts_at,t.expires_at,t.lifecycle_state,t.cancelled_at,t.revision,
 COALESCE((SELECT SUM(r.actual_cost_usd) FROM subscription_quota_requests r WHERE r.term_id=t.id AND r.status='usage_known'),0)::text,
 (SELECT COUNT(*) FROM subscription_quota_requests r WHERE r.term_id=t.id AND r.status IN ('admitted','usage_known','reconcile_required')),
 (SELECT COUNT(*) FROM subscription_quota_requests r WHERE r.term_id=t.id AND r.status='reconcile_required')`

func subscriptionQuotaScanTerm(row carpoolRowScanner) (domain.SubscriptionQuotaTerm, error) {
	var t domain.SubscriptionQuotaTerm
	err := row.Scan(&t.ID, &t.SubscriptionID, &t.UserID, &t.GroupID, &t.TermNo, &t.QuotaUSD, &t.UsedUSD, &t.StartsAt, &t.ExpiresAt, &t.LifecycleState, &t.CancelledAt, &t.Revision, &t.PendingUSD, &t.UnsettledRequestCount, &t.ReconcileRequiredCount)
	return t, err
}
func subscriptionQuotaReadTerms(ctx context.Context, q subscriptionQuotaQuerier, parentID int64, lock bool) ([]domain.SubscriptionQuotaTerm, error) {
	terms := []domain.SubscriptionQuotaTerm{}
	if parentID == 0 {
		return terms, nil
	}
	query := `SELECT ` + subscriptionQuotaTermColumns + ` FROM subscription_quota_terms t WHERE t.subscription_id=$1 ORDER BY t.id`
	if lock {
		query += ` FOR UPDATE OF t`
	}
	rows, err := q.QueryContext(ctx, query, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		term, err := subscriptionQuotaScanTerm(rows)
		if err != nil {
			return nil, err
		}
		terms = append(terms, term)
	}
	return terms, rows.Err()
}
func subscriptionQuotaSummary(groupID, revision int64, terms []domain.SubscriptionQuotaTerm, now time.Time) domain.SubscriptionQuotaSummary {
	summary := domain.SubscriptionQuotaSummary{GroupID: groupID, QuotaMode: "term", Revision: revision, ServerTime: now, NextTerms: []domain.SubscriptionQuotaTerm{}}
	for _, raw := range terms {
		term := domain.ProjectSubscriptionQuotaTerm(raw, now)
		if term.CancelledAt == nil && !now.Before(term.StartsAt) && now.Before(term.ExpiresAt) {
			copy := term
			summary.CurrentTerm = &copy
		} else if term.CancelledAt == nil && now.Before(term.StartsAt) {
			summary.NextTerms = append(summary.NextTerms, term)
		} else if summary.LastTerm == nil || subscriptionQuotaEndedAfter(term, *summary.LastTerm) {
			copy := term
			summary.LastTerm = &copy
		}
	}
	return summary
}

func subscriptionQuotaEndedAfter(a, b domain.SubscriptionQuotaTerm) bool {
	ended := func(term domain.SubscriptionQuotaTerm) time.Time {
		if term.CancelledAt != nil && term.CancelledAt.Before(term.ExpiresAt) {
			return *term.CancelledAt
		}
		return term.ExpiresAt
	}
	aEnd, bEnd := ended(a), ended(b)
	return aEnd.After(bEnd) || aEnd.Equal(bEnd) && a.ID > b.ID
}

// Reading history remains possible after user/group suspension. Mutations and
// entitlement authorization apply their stricter active-entity checks.
func subscriptionQuotaReadIdentity(ctx context.Context, q subscriptionQuotaQuerier, userID, groupID int64) error {
	var userExists, groupExists bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL), EXISTS(SELECT 1 FROM groups WHERE id=$2 AND deleted_at IS NULL AND platform='anthropic' AND subscription_type='subscription' AND subscription_quota_mode='term')`, userID, groupID).Scan(&userExists, &groupExists)
	if err != nil {
		return err
	}
	if !userExists {
		return service.ErrSubscriptionQuotaNotFound
	}
	if !groupExists {
		return service.ErrSubscriptionQuotaGroupInvalid
	}
	return nil
}
func subscriptionQuotaGetSummary(ctx context.Context, q subscriptionQuotaQuerier, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error) {
	if err := subscriptionQuotaReadIdentity(ctx, q, userID, groupID); err != nil {
		return nil, err
	}
	parent, err := subscriptionQuotaReadParent(ctx, q, userID, groupID, false)
	if err != nil {
		return nil, err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, q, parent.id, false)
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, q)
	if err != nil {
		return nil, err
	}
	result := subscriptionQuotaSummary(groupID, parent.revision, terms, now)
	return &result, nil
}
func (r *subscriptionQuotaRepository) GetSummary(ctx context.Context, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	return subscriptionQuotaGetSummary(ctx, tx, userID, groupID)
}
func (r *subscriptionQuotaRepository) ListSummaries(ctx context.Context, userID int64) ([]domain.SubscriptionQuotaSummary, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT t.group_id FROM subscription_quota_terms t JOIN user_subscriptions p ON p.id=t.subscription_id AND p.user_id=t.user_id AND p.group_id=t.group_id AND p.deleted_at IS NULL JOIN groups g ON g.id=t.group_id AND g.deleted_at IS NULL AND g.platform='anthropic' AND g.subscription_type='subscription' AND g.subscription_quota_mode='term' WHERE t.user_id=$1 ORDER BY t.group_id`, userID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	summaries := []domain.SubscriptionQuotaSummary{}
	for _, id := range ids {
		summary, err := subscriptionQuotaGetSummary(ctx, tx, userID, id)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}
	return summaries, nil
}
func (r *subscriptionQuotaRepository) ListTerms(ctx context.Context, userID, groupID int64, p domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaTermPage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaReadIdentity(ctx, tx, userID, groupID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+subscriptionQuotaTermColumns+` FROM subscription_quota_terms t WHERE t.user_id=$1 AND t.group_id=$2 AND ($3::bigint=0 OR t.id<$3) ORDER BY t.id DESC LIMIT $4`, userID, groupID, p.BeforeID, p.Limit+1)
	if err != nil {
		return nil, err
	}
	items := []domain.SubscriptionQuotaTerm{}
	for rows.Next() {
		term, err := subscriptionQuotaScanTerm(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, term)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	result := &domain.SubscriptionQuotaTermPage{Items: []domain.SubscriptionQuotaTerm{}, HasMore: len(items) > p.Limit}
	if result.HasMore {
		items = items[:p.Limit]
		cursor := domain.EncodeSubscriptionQuotaCursor(items[len(items)-1].ID)
		result.NextCursor = &cursor
	}
	for _, term := range items {
		result.Items = append(result.Items, domain.ProjectSubscriptionQuotaTerm(term, now))
	}
	return result, nil
}
func (r *subscriptionQuotaRepository) ListUsage(ctx context.Context, userID, termID int64, p domain.SubscriptionQuotaPageParams) (*domain.SubscriptionQuotaUsagePage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var owner int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM subscription_quota_terms WHERE id=$1`, termID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner != userID {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,request_id,api_key_id,term_id,actual_cost_usd::text,settled_at,usage_log_payload FROM subscription_quota_requests WHERE term_id=$1 AND user_id=$2 AND status='settled' AND ($3::bigint=0 OR id<$3) ORDER BY id DESC LIMIT $4`, termID, userID, p.BeforeID, p.Limit+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := &domain.SubscriptionQuotaUsagePage{Items: []domain.SubscriptionQuotaUsage{}}
	var lastID int64
	for rows.Next() {
		var item domain.SubscriptionQuotaUsage
		var id int64
		var payload []byte
		if err = rows.Scan(&id, &item.RequestID, &item.APIKeyID, &item.TermID, &item.ActualCostUSD, &item.SettledAt, &payload); err != nil {
			return nil, err
		}
		if len(result.Items) == p.Limit {
			result.HasMore = true
			break
		}
		var usage struct {
			Model        string `json:"model"`
			InputTokens  int64  `json:"input_tokens"`
			OutputTokens int64  `json:"output_tokens"`
		}
		if err = json.Unmarshal(payload, &usage); err != nil {
			return nil, err
		}
		item.Model = usage.Model
		item.InputTokens = usage.InputTokens
		item.OutputTokens = usage.OutputTokens
		cost, err := decimal.NewFromString(item.ActualCostUSD)
		if err != nil {
			return nil, err
		}
		item.ActualCostUSD = cost.StringFixed(8)
		result.Items = append(result.Items, item)
		lastID = id
	}
	if result.HasMore {
		cursor := domain.EncodeSubscriptionQuotaCursor(lastID)
		result.NextCursor = &cursor
	}
	return result, rows.Err()
}

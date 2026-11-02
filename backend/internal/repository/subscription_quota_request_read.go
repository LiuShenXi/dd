package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const subscriptionQuotaRequestProjectionColumns = `id,request_id,user_id,group_id,term_id,api_key_id,status,revision,actual_cost_usd::text,admitted_at,updated_at,settled_at,last_error_category`

func subscriptionQuotaScanRequestProjection(row carpoolRowScanner) (domain.SubscriptionQuotaRequest, error) {
	var out domain.SubscriptionQuotaRequest
	var cost, category sql.NullString
	var settled sql.NullTime
	err := row.Scan(&out.ID, &out.RequestID, &out.UserID, &out.GroupID, &out.TermID, &out.APIKeyID, &out.Status, &out.Revision, &cost, &out.AdmittedAt, &out.UpdatedAt, &settled, &category)
	if cost.Valid {
		out.ActualCostUSD = &cost.String
	}
	if settled.Valid {
		out.SettledAt = &settled.Time
	}
	if category.Valid {
		out.LastErrorCategory = &category.String
	}
	return out, err
}

func (r *subscriptionQuotaRepository) ListRequests(ctx context.Context, p domain.SubscriptionQuotaRequestQueryParams) (*domain.SubscriptionQuotaRequestPage, error) {
	if p.Limit < 1 || p.Limit > 100 || p.BeforeID < 0 || p.UserID < 0 || p.GroupID < 0 || !service.ValidSubscriptionQuotaRequestStatus(p.Status) {
		return nil, service.ErrSubscriptionQuotaInput
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionQuotaRequestProjectionColumns+` FROM subscription_quota_requests WHERE ($1::bigint=0 OR id<$1) AND ($2::text='' OR status=$2) AND ($3::bigint=0 OR user_id=$3) AND ($4::bigint=0 OR group_id=$4) ORDER BY id DESC LIMIT $5`, p.BeforeID, p.Status, p.UserID, p.GroupID, p.Limit+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	page := &domain.SubscriptionQuotaRequestPage{Items: []domain.SubscriptionQuotaRequest{}}
	for rows.Next() {
		item, err := subscriptionQuotaScanRequestProjection(rows)
		if err != nil {
			return nil, err
		}
		if len(page.Items) == p.Limit {
			page.HasMore = true
			cursor := domain.EncodeSubscriptionQuotaCursor(page.Items[len(page.Items)-1].ID)
			page.NextCursor = &cursor
			break
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

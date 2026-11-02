package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewSubscriptionQuotaPaymentRepository(db *sql.DB) service.SubscriptionQuotaPaymentRepository {
	return &subscriptionQuotaRepository{db: db}
}

// FulfillPaidTerm deliberately takes no plan or feature flag. Purchased rights
// survive stopped sales, changed defaults, and deletion of their source plan.
func (r *subscriptionQuotaRepository) FulfillPaidTerm(ctx context.Context, p domain.SubscriptionQuotaPaidTermParams) (*domain.SubscriptionQuotaMutationResult, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if p.OrderID <= 0 || p.LeaseVersion.IsZero() {
		return nil, service.ErrSubscriptionQuotaPaymentLease
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	var groupID, planID, days sql.NullInt64
	var status, kind string
	var version time.Time
	var paidAt, refundedAt sql.NullTime
	var data []byte
	err = tx.QueryRowContext(ctx, `SELECT user_id,subscription_group_id,plan_id,subscription_days,status,order_type,updated_at,paid_at,refund_at,subscription_quota_snapshot FROM payment_orders WHERE id=$1 FOR UPDATE`, p.OrderID).Scan(&userID, &groupID, &planID, &days, &status, &kind, &version, &paidAt, &refundedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != service.OrderStatusRecharging || !version.Equal(p.LeaseVersion) || !paidAt.Valid || refundedAt.Valid {
		return nil, service.ErrSubscriptionQuotaPaymentLease
	}
	var raw map[string]any
	decoderErr := json.Unmarshal(data, &raw)
	snapshot, snapshotErr := domain.ParseSubscriptionQuotaOrderSnapshot(raw)
	if decoderErr != nil || snapshotErr != nil || snapshot == nil || kind != "subscription" || userID <= 0 || !groupID.Valid || snapshot.GroupID != groupID.Int64 || !planID.Valid || snapshot.SourcePlanID != planID.Int64 || !days.Valid || days.Int64 != 7 {
		return nil, service.ErrSubscriptionQuotaOrderSnapshot
	}
	if err = subscriptionQuotaScopeLock(ctx, tx, userID, groupID.Int64); err != nil {
		return nil, err
	}
	scope := fmt.Sprintf("payment:order:%d", p.OrderID)
	intent, err := json.Marshal(struct {
		OrderID  int64                                 `json:"order_id"`
		UserID   int64                                 `json:"user_id"`
		Snapshot domain.SubscriptionQuotaOrderSnapshot `json:"snapshot"`
	}{p.OrderID, userID, *snapshot})
	if err != nil {
		return nil, err
	}
	replay, err := subscriptionQuotaReplay(ctx, tx, scope, scope, intent)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		var matches bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_quota_terms t JOIN payment_audit_logs a ON a.order_id=$2 AND a.action='SUBSCRIPTION_ASSIGNED' WHERE t.id=$3 AND t.payment_order_id=$1 AND t.user_id=$4 AND t.group_id=$5)`, p.OrderID, strconv.FormatInt(p.OrderID, 10), replay.Term.ID, userID, groupID.Int64).Scan(&matches)
		if err != nil {
			return nil, err
		}
		if !matches {
			return nil, service.ErrSubscriptionQuotaOrderSnapshot
		}
		return replay, nil
	}
	var alreadyExists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_quota_terms WHERE payment_order_id=$1) OR EXISTS(SELECT 1 FROM payment_audit_logs WHERE order_id=$2 AND action IN ('SUBSCRIPTION_ASSIGNED','SUBSCRIPTION_SUCCESS'))`, p.OrderID, strconv.FormatInt(p.OrderID, 10)).Scan(&alreadyExists)
	if err != nil {
		return nil, err
	}
	if alreadyExists {
		return nil, service.ErrSubscriptionQuotaOrderSnapshot
	}
	parent, err := subscriptionQuotaReadParent(ctx, tx, userID, groupID.Int64, true)
	if err != nil {
		return nil, err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parent.id, true)
	if err != nil {
		return nil, err
	}
	if err = subscriptionQuotaCheckEntities(ctx, tx, userID, groupID.Int64, 0, true); err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	sourceJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	open := domain.SubscriptionQuotaOpenParams{UserID: userID, GroupID: groupID.Int64, Operation: "renew", PlanID: &snapshot.SourcePlanID, Notes: fmt.Sprintf("payment order %d", p.OrderID)}
	result, err := subscriptionQuotaCreateTermTx(ctx, tx, open, parent, terms, now, snapshot.QuotaUSD, sourceJSON, "payment", &p.OrderID)
	if err != nil {
		return nil, err
	}
	source := domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: snapshot.QuotaUSD, DurationHours: 168, QuotaSource: "payment", PlanID: &snapshot.SourcePlanID, OrderSnapshot: snapshot}
	if err = subscriptionQuotaSaveOperation(ctx, tx, scope, scope, "renew_term", nil, intent, &source, *result); err != nil {
		return nil, err
	}
	detail, err := json.Marshal(map[string]any{"groupID": groupID.Int64, "validityDays": 7, "termID": result.Term.ID, "quotaUSD": snapshot.QuotaUSD, "snapshot": snapshot})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO payment_audit_logs(order_id,action,detail,operator,created_at) VALUES($1,'SUBSCRIPTION_ASSIGNED',$2,'system',$3)`, strconv.FormatInt(p.OrderID, 10), string(detail), now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

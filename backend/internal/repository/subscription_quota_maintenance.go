package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Callers already hold scope -> parent -> this term. Request state transitions
// use that same scope, so the unfinished count cannot race an admission or bill.
func subscriptionQuotaConvergeTermTx(ctx context.Context, tx *sql.Tx, termID int64) error {
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return err
	}
	return subscriptionQuotaConvergeTermAt(ctx, tx, termID, now)
}

func subscriptionQuotaConvergeTermAt(ctx context.Context, tx *sql.Tx, termID int64, now time.Time) error {
	var state string
	var starts, expires time.Time
	var cancelled, closed sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT lifecycle_state,starts_at,expires_at,cancelled_at,closed_at FROM subscription_quota_terms WHERE id=$1`, termID).Scan(&state, &starts, &expires, &cancelled, &closed); err != nil {
		return err
	}
	desired := "scheduled"
	var closedAt any
	if cancelled.Valid || !now.Before(expires) {
		var unfinished bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_quota_requests WHERE term_id=$1 AND status IN ('admitted','usage_known','reconcile_required'))`, termID).Scan(&unfinished); err != nil {
			return err
		}
		if unfinished {
			desired = "closing"
		} else {
			desired = "closed"
			closedAt = now
			if closed.Valid {
				closedAt = closed.Time
			}
		}
	} else if !now.Before(starts) {
		desired = "active"
	}
	if desired == state {
		// Move checked closing work to the back of the bounded discovery queue so
		// permanently unknown old requests cannot starve other expired card scopes.
		if desired == "closing" {
			_, err := tx.ExecContext(ctx, `UPDATE subscription_quota_terms SET updated_at=$2 WHERE id=$1`, termID, now)
			return err
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE subscription_quota_terms SET lifecycle_state=$2,closed_at=$3,revision=revision+1,updated_at=$4 WHERE id=$1`, termID, desired, closedAt, now)
	return err
}

func (r *subscriptionQuotaRepository) MaintainTermLifecycles(ctx context.Context, limit int) error {
	if r == nil || r.db == nil {
		return service.ErrSubscriptionQuotaBillingUnavailable
	}
	if limit < 1 || limit > 1000 {
		return service.ErrSubscriptionQuotaInput
	}
	rows, err := r.db.QueryContext(ctx, `SELECT subscription_id,user_id,group_id FROM subscription_quota_terms WHERE lifecycle_state<>'closed' AND (cancelled_at IS NOT NULL OR expires_at<=clock_timestamp() OR lifecycle_state='closing' OR (lifecycle_state='scheduled' AND starts_at<=clock_timestamp())) GROUP BY subscription_id,user_id,group_id ORDER BY min(updated_at),subscription_id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	type scope struct{ parent, user, group int64 }
	batch := []scope{}
	for rows.Next() {
		var item scope
		if err = rows.Scan(&item.parent, &item.user, &item.group); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	var firstErr error
	for _, item := range batch {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		itemCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = r.maintainTermScope(itemCtx, item.parent, item.user, item.group)
		cancel()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *subscriptionQuotaRepository) maintainTermScope(ctx context.Context, parentID, userID, groupID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = subscriptionQuotaScopeLock(ctx, tx, userID, groupID); err != nil {
		return err
	}
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM user_subscriptions WHERE id=$1 AND user_id=$2 AND group_id=$3 FOR UPDATE`, parentID, userID, groupID).Scan(&locked); err != nil {
		return err
	}
	terms, err := subscriptionQuotaReadTerms(ctx, tx, parentID, true)
	if err != nil {
		return err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return err
	}
	for _, term := range terms {
		if err = subscriptionQuotaConvergeTermAt(ctx, tx, term.ID, now); err != nil {
			return err
		}
	}
	// No parent starts/expires/quota_revision and no card starts/expires change.
	return tx.Commit()
}

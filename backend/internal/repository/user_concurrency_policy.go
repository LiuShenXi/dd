package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type claudeConcurrencyPolicyRepository struct{ db *sql.DB }

func subscriptionQuotaCheckClaudeConcurrency(ctx context.Context, q subscriptionQuotaQuerier, userID int64, defaultLimit int) error {
	var override sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT claude_subscription_concurrency FROM users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&override); err != nil {
		return service.ErrTermConcurrencyUnavailable.WithCause(err)
	}
	limit := defaultLimit
	if override.Valid {
		limit = int(override.Int64)
	}
	if !service.ValidClaudeSubscriptionConcurrency(limit) {
		return service.ErrTermConcurrencyNotConfigured
	}
	return nil
}

func NewClaudeConcurrencyPolicyRepository(db *sql.DB) service.ClaudeConcurrencyPolicyRepository {
	return &claudeConcurrencyPolicyRepository{db: db}
}

func (r *claudeConcurrencyPolicyRepository) WithClaudeConcurrencyLimit(ctx context.Context, userID int64, defaultLimit int, apply func(context.Context, int) error) error {
	if r == nil || r.db == nil || userID <= 0 || apply == nil {
		return service.ErrTermConcurrencyUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var override sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT claude_subscription_concurrency FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR SHARE`, userID).Scan(&override)
	if err != nil {
		return service.ErrTermConcurrencyUnavailable
	}
	limit := defaultLimit
	if override.Valid {
		limit = int(override.Int64)
	}
	if !service.ValidClaudeSubscriptionConcurrency(limit) {
		return service.ErrTermConcurrencyNotConfigured
	}
	if err = apply(ctx, limit); err != nil {
		return err
	}
	return tx.Commit()
}

//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaConcurrencyV2ManualIssueRequiresConfiguredPolicy(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	for _, defaultLimit := range []int{0, -1, 2147483648} {
		cfg := *f.cfg
		cfg.Default = config.DefaultConfig{ClaudeSubscriptionConcurrency: defaultLimit}
		api := service.NewSubscriptionQuotaService(f.repo, &cfg)
		_, err := api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", "100", 0), uuid.NewString())
		require.ErrorIs(t, err, service.ErrTermConcurrencyNotConfigured)
		require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "missing/invalid policy writes no parent, card, operation, outbox or money")
	}
	_, err := f.db.Exec(`UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, f.userA)
	require.NoError(t, err)
	cfg := *f.cfg
	cfg.Default = config.DefaultConfig{ClaudeSubscriptionConcurrency: 2147483648}
	api := service.NewSubscriptionQuotaService(f.repo, &cfg)
	key := uuid.NewString()
	original, err := api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", "100", 0), key)
	require.NoError(t, err)
	require.Equal(t, "100.00000000", original.Term.QuotaUSD)
	_, err = f.db.Exec(`UPDATE users SET claude_subscription_concurrency=NULL WHERE id=$1`, f.userA)
	require.NoError(t, err)
	before = subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	replayed, err := api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", "100", 0), key)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, original.Term.ID, replayed.Term.ID)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "successful replay precedes today's now-invalid policy and writes nothing")
}

func TestSubscriptionQuotaConcurrencyV2LegacyBulkKeepsClaudePolicy(t *testing.T) {
	db, userA := newQuotaConcurrencyV2PolicyFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := db.ExecContext(ctx, `UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, userA)
	require.NoError(t, err)
	var userB, userC int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,concurrency,balance) VALUES('bulk-default@example.invalid','synthetic',5,23.12345678) RETURNING id`).Scan(&userB))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,concurrency,claude_subscription_concurrency,balance) VALUES('bulk-foreign@example.invalid','synthetic',13,7,37.87654321) RETURNING id`).Scan(&userC))
	fingerprint := func() string {
		var result string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT md5(jsonb_agg(to_jsonb(u)-'concurrency'-'rpm_limit'-'updated_at' ORDER BY id)::text) FROM users u`).Scan(&result))
		return result
	}
	before := fingerprint()
	repo := newUserRepositoryWithSQL(nil, db)
	policy := NewClaudeConcurrencyPolicyRepository(db)
	assertPolicy := func(legacy int) {
		t.Helper()
		require.Equal(t, before, fingerprint(), "old bulk may change only its original declared fields")
		for _, item := range []struct {
			id    int64
			limit int
		}{{userA, 2}, {userB, 3}, {userC, 7}} {
			observed := 0
			require.NoError(t, policy.WithClaudeConcurrencyLimit(ctx, item.id, 3, func(_ context.Context, limit int) error { observed = limit; return nil }))
			require.Equal(t, item.limit, observed)
		}
		for _, id := range []int64{userA, userB} {
			var current int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT concurrency FROM users WHERE id=$1`, id).Scan(&current))
			require.Equal(t, legacy, current)
		}
		var foreign int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT concurrency FROM users WHERE id=$1`, userC).Scan(&foreign))
		require.Equal(t, 13, foreign)
	}
	n, err := repo.BatchSetConcurrency(ctx, []int64{userA, userB}, 9)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assertPolicy(9)
	n, err = repo.BatchAddConcurrency(ctx, []int64{userA, userB}, -4)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assertPolicy(5)
	limit, rpm := 11, 17
	n, err = repo.BatchUpdateLimits(ctx, []int64{userA, userB}, &limit, &rpm)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assertPolicy(11)
	n, err = repo.BatchSetConcurrency(ctx, []int64{userA, userB}, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assertPolicy(0)
}

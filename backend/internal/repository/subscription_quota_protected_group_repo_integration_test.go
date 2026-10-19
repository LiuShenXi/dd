//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func newProtectedGroupFixture(t *testing.T) (service.SubscriptionQuotaProtectedGroupRepository, service.SubscriptionQuotaProtectedAdmission) {
	t.Helper()
	client := testEntClient(t)
	u := mustCreateUser(t, client, &service.User{Balance: 25})
	g := mustCreateGroup(t, client, &service.Group{Name: "protected-" + uuid.NewString(), Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, GroupID: &g.ID, Key: "sk-protected-synthetic-" + uuid.NewString()})
	return NewSubscriptionQuotaProtectedGroupRepository(integrationDB), service.SubscriptionQuotaProtectedAdmission{RequestID: "protected:" + uuid.NewString(), UserID: u.ID, APIKeyID: k.ID, GroupID: g.ID, SubscriptionType: service.SubscriptionTypeStandard}
}

func TestSubscriptionQuotaProtectedPersistentDrain(t *testing.T) {
	ctx := context.Background()
	repo, in := newProtectedGroupFixture(t)
	require.NoError(t, repo.AdmitProtectedGroup(ctx, in))
	require.Error(t, repo.FinishProtectedGroup(ctx, in.RequestID, "completed", "unsupported_without_usage"))
	payload := json.RawMessage(`{"version":1,"usage":{"input_tokens":4}}`)
	require.NoError(t, repo.RecordProtectedGroupUsage(ctx, in.RequestID, payload))
	// A new repository instance models a restarted process, not an in-memory counter.
	restarted := NewSubscriptionQuotaProtectedGroupRepository(integrationDB)
	count, err := restarted.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, restarted.RecordProtectedGroupUsage(ctx, in.RequestID, payload))
	require.Error(t, restarted.RecordProtectedGroupUsage(ctx, in.RequestID, json.RawMessage(`{"usage":{"input_tokens":5}}`)), "changed usage cannot overwrite original evidence")
	require.NoError(t, restarted.FinishProtectedGroup(ctx, in.RequestID, "completed", "legacy_billing_completed"))
	require.NoError(t, restarted.FinishProtectedGroup(ctx, in.RequestID, "completed", "legacy_billing_completed"))
	require.Error(t, restarted.FinishProtectedGroup(ctx, in.RequestID, "reconcile_required", "late_failure"), "settled outcome cannot move backwards")
	count, err = restarted.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.Zero(t, count)
	in.RequestID = "protected:" + uuid.NewString()
	require.NoError(t, repo.AdmitProtectedGroup(ctx, in))
	_, err = integrationDB.ExecContext(ctx, `UPDATE subscription_quota_protected_requests SET admitted_at=clock_timestamp()-INTERVAL '1 year' WHERE request_id=$1`, in.RequestID)
	require.NoError(t, err)
	count, err = restarted.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "old or crashed requests never expire out of drain")
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, in.UserID).Scan(&balance))
	require.True(t, decimal.RequireFromString(balance).Equal(decimal.NewFromInt(25)))
}

func TestSubscriptionQuotaProtectedFinalDatabaseState(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"term mode", `UPDATE groups SET subscription_type='subscription',subscription_quota_mode='term' WHERE id=$1`},
		{"group disabled", `UPDATE groups SET status='inactive' WHERE id=$1`},
		{"group deleted", `UPDATE groups SET deleted_at=clock_timestamp() WHERE id=$1`},
		{"key disabled", `UPDATE api_keys SET status='disabled' WHERE id=$1`},
		{"key expired", `UPDATE api_keys SET expires_at=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`},
		{"key quota", `UPDATE api_keys SET quota=1,quota_used=1 WHERE id=$1`},
		{"key rate", `UPDATE api_keys SET rate_limit_5h=1,usage_5h=1,window_5h_start=clock_timestamp() WHERE id=$1`},
		{"user disabled", `UPDATE users SET status='disabled' WHERE id=$1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, in := newProtectedGroupFixture(t)
			id := in.GroupID
			if len(tc.name) >= 3 && tc.name[:3] == "key" {
				id = in.APIKeyID
			}
			if tc.name == "user disabled" {
				id = in.UserID
			}
			_, err := integrationDB.ExecContext(ctx, tc.sql, id)
			require.NoError(t, err)
			require.Error(t, repo.AdmitProtectedGroup(ctx, in), "cached active standard Key must not bypass authoritative DB state")
			count, err := repo.CountProtectedGroupUnfinished(ctx, in.GroupID)
			require.NoError(t, err)
			require.Zero(t, count, "no upstream admission is created on rejection")
		})
	}
}

func TestSubscriptionQuotaProtectedUserLockOrderAndModeWait(t *testing.T) {
	ctx := context.Background()
	repo, in := newProtectedGroupFixture(t)
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	var id int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, in.UserID).Scan(&id))
	result := make(chan error, 1)
	go func() { result <- repo.AdmitProtectedGroup(ctx, in) }()
	requireCarpoolQueryBlocked(t, ctx, "SELECT status,restrict_public_groups FROM users")
	// If admission held group before waiting for this user, this would time out.
	_, err = blocker.ExecContext(ctx, `SET LOCAL lock_timeout='500ms'`)
	require.NoError(t, err)
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, in.GroupID).Scan(&id))
	_, err = blocker.ExecContext(ctx, `UPDATE groups SET subscription_type='subscription',subscription_quota_mode='term' WHERE id=$1`, in.GroupID)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit())
	select {
	case err := <-result:
		require.Error(t, err, "must reread converted mode after the user lock wait")
	case <-time.After(5 * time.Second):
		t.Fatal("protected admission did not resume")
	}
	count, err := repo.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSubscriptionQuotaProtectedKeyWaitRefreshesDatabaseTime(t *testing.T) {
	ctx := context.Background()
	repo, in := newProtectedGroupFixture(t)
	var boundary time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `UPDATE api_keys SET expires_at=clock_timestamp()+INTERVAL '500 milliseconds' WHERE id=$1 RETURNING expires_at`, in.APIKeyID).Scan(&boundary))
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	var id int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM api_keys WHERE id=$1 FOR UPDATE`, in.APIKeyID).Scan(&id))
	result := make(chan error, 1)
	go func() { result <- repo.AdmitProtectedGroup(ctx, in) }()
	requireCarpoolQueryBlocked(t, ctx, "FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR SHARE")
	waitForCarpoolDatabaseTime(t, ctx, boundary)
	require.NoError(t, blocker.Commit())
	select {
	case err := <-result:
		require.Error(t, err, "Key expired while waiting must not be admitted")
	case <-time.After(5 * time.Second):
		t.Fatal("protected admission did not resume")
	}
	count, err := repo.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSubscriptionQuotaProtectedConcurrentGPTPrepare(t *testing.T) {
	ctx := context.Background()
	repo, in := newProtectedGroupFixture(t)
	gpt := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "protected-gpt-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard})
	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET balance=0 WHERE id=$1`, in.UserID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='subscription',subscription_quota_mode='term' WHERE id=$1`, in.GroupID)
	require.NoError(t, err)
	gptTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = gptTx.Rollback() }()
	var id int64
	require.NoError(t, gptTx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 FOR SHARE`, gpt.ID).Scan(&id))
	require.NoError(t, gptTx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, in.UserID).Scan(&id))
	result := make(chan error, 1)
	go func() { result <- repo.AdmitProtectedGroup(ctx, in) }()
	requireCarpoolQueryBlocked(t, ctx, "SELECT status,restrict_public_groups FROM users")
	_, err = gptTx.ExecContext(ctx, `SET LOCAL lock_timeout='500ms'`)
	require.NoError(t, err)
	// Exercise the actual existing GPT preparation function, including locking all
	// Keys and reading the Claude group only to verify the narrow term exception.
	require.NoError(t, prepareCarpoolUserGroupTx(ctx, gptTx, domain.CreateCarpoolTermParams{UserID: in.UserID, GroupID: gpt.ID}))
	require.NoError(t, gptTx.Commit())
	select {
	case err := <-result:
		require.Error(t, err, "stale standard admission is rejected after GPT preparation")
	case <-time.After(5 * time.Second):
		t.Fatal("GPT preparation and protected admission did not finish")
	}
	var bound int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM carpool_billing_bindings WHERE user_id=$1`, in.UserID).Scan(&bound))
	require.Equal(t, gpt.ID, bound)
	count, err := repo.CountProtectedGroupUnfinished(ctx, in.GroupID)
	require.NoError(t, err)
	require.Zero(t, count)
}

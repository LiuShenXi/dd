//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaClaudeKeysSurviveGPTOpenAndRenew(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='standard',platform='openai' WHERE id=$1`, groupID)
	require.NoError(t, err)
	client := testEntClient(t)
	claude := mustCreateGroup(t, client, &service.Group{Name: "quota-coexist-" + uuid.NewString(), Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription})
	_, err = client.Group.UpdateOneID(claude.ID).SetSubscriptionQuotaMode("term").Save(ctx)
	require.NoError(t, err)
	claudeKeyA := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &claude.ID, Key: "sk-synthetic-claude-a-" + uuid.NewString()})
	claudeKeyB := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &claude.ID, Key: "sk-synthetic-claude-b-" + uuid.NewString()})
	gpt := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &groupID, Key: "sk-synthetic-gpt-" + uuid.NewString()})
	var before string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT json_agg(k ORDER BY id)::text FROM api_keys k WHERE user_id=$1`, userID).Scan(&before))
	svc := service.NewCarpoolService(repo)
	opened, err := svc.Open(ctx, userID, userID, service.CarpoolOpenInput{GroupID: groupID, PlanID: planID, Mode: "new"}, uuid.NewString())
	require.NoError(t, err)
	renewed, err := svc.Renew(ctx, opened.ID, userID, service.CarpoolRenewInput{}, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, opened.ExpiresAt, renewed.StartsAt)
	var after string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT json_agg(k ORDER BY id)::text FROM api_keys k WHERE user_id=$1`, userID).Scan(&after))
	require.Equal(t, before, after, "GPT opening/renewal cannot rewrite existing Claude Keys")
	keys := NewAPIKeyRepository(client, integrationDB)
	for _, key := range []*service.APIKey{claudeKeyA, claudeKeyB} {
		got, err := keys.GetByKeyForAuth(ctx, key.Key)
		require.NoError(t, err)
		require.True(t, got.Group.IsTermQuota(), "mode remains term even without an active card")
		require.Equal(t, claude.ID, *got.GroupID)
	}
	got, err := keys.GetByKeyForAuth(ctx, gpt.Key)
	require.NoError(t, err)
	require.True(t, got.Group.IsCarpoolType())
	ordinary := mustCreateGroup(t, client, &service.Group{Name: "quota-forbidden-" + uuid.NewString(), Platform: service.PlatformAnthropic})
	wrong := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &ordinary.ID, Key: "sk-synthetic-forbidden-" + uuid.NewString()})
	_, err = keys.GetByKeyForAuth(ctx, wrong.Key)
	require.ErrorIs(t, err, service.ErrCarpoolInvalidRelationship)
	_, err = svc.Renew(ctx, renewed.ID, userID, service.CarpoolRenewInput{}, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCarpoolOpeningKeyGroup)
}

func TestSubscriptionQuotaLegacyRepositorySinksCannotMutateTermParent(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{})
	group := mustCreateGroup(t, client, &service.Group{Name: "quota-legacy-" + uuid.NewString(), Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription})
	_, err := client.Group.UpdateOneID(group.ID).SetSubscriptionQuotaMode("term").Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	sub, err := client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Hour)).SetDailyUsageUsd(7).SetWeeklyUsageUsd(8).SetMonthlyUsageUsd(9).Save(ctx)
	require.NoError(t, err)
	repository := NewUserSubscriptionRepository(client)
	for name, action := range map[string]func() error{
		"activate":      func() error { return repository.ActivateWindows(ctx, sub.ID, now, now) },
		"reset all":     func() error { return repository.ResetUsageWindows(ctx, sub.ID, true, true, true, now, now) },
		"reset daily":   func() error { return repository.ResetDailyUsage(ctx, sub.ID, nil, now) },
		"reset weekly":  func() error { return repository.ResetWeeklyUsage(ctx, sub.ID, nil, now) },
		"reset monthly": func() error { return repository.ResetMonthlyUsage(ctx, sub.ID, nil, now) },
		"increment":     func() error { return repository.IncrementUsage(ctx, sub.ID, 5) },
		"extend":        func() error { return repository.ExtendExpiry(ctx, sub.ID, now.Add(14*24*time.Hour)) },
		"status":        func() error { return repository.UpdateStatus(ctx, sub.ID, service.SubscriptionStatusExpired) },
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, action(), "a skipped term write must not be reported as successful maintenance")
		})
	}
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.Error(t, incrementUsageBillingSubscription(ctx, tx, sub.ID, 2))
	require.NoError(t, tx.Rollback())
	stored, err := client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.DailyUsageUsd, stored.DailyUsageUsd)
	require.Equal(t, sub.WeeklyUsageUsd, stored.WeeklyUsageUsd)
	require.Equal(t, sub.MonthlyUsageUsd, stored.MonthlyUsageUsd)
	require.Equal(t, sub.ExpiresAt, stored.ExpiresAt)
	require.Equal(t, sub.Status, stored.Status)
}

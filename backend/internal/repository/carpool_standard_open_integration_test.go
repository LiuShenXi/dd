//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolStandardGroupOpenRenewAndExistingKeyAuth(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='standard',platform='openai' WHERE id=$1`, groupID)
	require.NoError(t, err)
	client := testEntClient(t)
	other := mustCreateGroup(t, client, &service.Group{Name: "open-other-" + uuid.NewString(), Platform: service.PlatformOpenAI})
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)`, userID, other.ID)
	require.NoError(t, err)
	admin := mustCreateUser(t, client, &service.User{Role: service.RoleAdmin, Balance: 99})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &groupID, Key: "sk-open-" + uuid.NewString()})
	adminKey := mustCreateApiKey(t, client, &service.APIKey{UserID: admin.ID, GroupID: &groupID, Key: "sk-open-admin-" + uuid.NewString()})
	var keysBefore, groupBefore string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT json_agg(k ORDER BY id)::text FROM api_keys k WHERE user_id IN ($1,$2)`, userID, admin.ID).Scan(&keysBefore))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT row_to_json(g)::text FROM groups g WHERE id=$1`, groupID).Scan(&groupBefore))
	cfg := &config.Config{}
	cfg.APIKeyAuth.L1Size = 100
	cfg.APIKeyAuth.L1TTLSeconds = 60
	keys := service.NewAPIKeyService(NewAPIKeyRepository(client, integrationDB), nil, nil, nil, nil, nil, cfg)
	got, err := keys.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, service.SubscriptionTypeStandard, got.Group.SubscriptionType)
	svc := service.NewCarpoolService(repo)
	svc.SetAuthInvalidator(keys)
	input := service.CarpoolOpenInput{GroupID: groupID, PlanID: planID, Mode: "new"}
	operation := uuid.NewString()
	term, err := svc.Open(ctx, userID, admin.ID, input, operation)
	require.NoError(t, err)
	replay, err := svc.Open(ctx, userID, admin.ID, input, operation)
	require.NoError(t, err)
	require.Equal(t, term.ID, replay.ID)
	got, err = keys.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.True(t, got.Group.IsCarpoolType())
	require.True(t, got.User.RestrictPublicGroups)
	require.Equal(t, []int64{groupID}, got.User.AllowedGroups)
	_, err = repo.Admit(ctx, userID, key.ID, groupID, uuid.NewString(), time.Now())
	require.NoError(t, err)
	got, err = keys.GetByKey(ctx, adminKey.Key)
	require.NoError(t, err)
	require.Equal(t, service.SubscriptionTypeStandard, got.Group.SubscriptionType)
	renewed, err := svc.Renew(ctx, term.ID, admin.ID, service.CarpoolRenewInput{}, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, groupID, renewed.GroupID)
	require.Equal(t, term.ExpiresAt, renewed.StartsAt)
	require.Equal(t, term.PlanSnapshot.WeeklyQuotaUSD, renewed.PlanSnapshot.WeeklyQuotaUSD)
	var keysAfter, groupAfter string
	var bound int64
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT json_agg(k ORDER BY id)::text FROM api_keys k WHERE user_id IN ($1,$2)`, userID, admin.ID).Scan(&keysAfter))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT row_to_json(g)::text FROM groups g WHERE id=$1`, groupID).Scan(&groupAfter))
	require.Equal(t, keysBefore, keysAfter)
	require.Equal(t, groupBefore, groupAfter)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM carpool_billing_bindings WHERE user_id=$1`, userID).Scan(&bound))
	require.Equal(t, groupID, bound)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&balance))
	require.Equal(t, "0.00000000", balance)
	var adminBindings int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM carpool_billing_bindings WHERE user_id=$1`, admin.ID).Scan(&adminBindings))
	require.Zero(t, adminBindings)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM carpool_billing_bindings WHERE user_id=$1`, userID)
	require.NoError(t, err)
	_, err = svc.Renew(ctx, renewed.ID, admin.ID, service.CarpoolRenewInput{}, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCarpoolInvalidRelationship, "renewal must not recreate a missing billing relationship")
}

func TestCarpoolStandardOpeningRejectsConflictsWithoutMutation(t *testing.T) {
	for _, scenario := range []string{"balance", "frozen", "nil key group", "different key group", "inactive group", "wrong platform", "existing binding", "late invalid payment"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
			_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='standard',platform='openai' WHERE id=$1`, groupID)
			require.NoError(t, err)
			client := testEntClient(t)
			params := domain.CreateCarpoolTermParams{UserID: userID, GroupID: groupID, PlanID: planID, ActorID: userID, Mode: "new", Operation: carpoolTestOperation("open_term", userID)}
			switch scenario {
			case "balance":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance=25 WHERE id=$1`, userID)
			case "frozen":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET frozen_balance=1 WHERE id=$1`, userID)
			case "nil key group":
				mustCreateApiKey(t, client, &service.APIKey{UserID: userID, Key: "sk-conflict-" + uuid.NewString()})
			case "different key group", "existing binding":
				other := mustCreateGroup(t, client, &service.Group{Name: "conflict-" + uuid.NewString(), Platform: service.PlatformOpenAI})
				if scenario == "existing binding" {
					_, err = integrationDB.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, userID, other.ID)
				} else {
					mustCreateApiKey(t, client, &service.APIKey{UserID: userID, GroupID: &other.ID, Key: "sk-conflict-" + uuid.NewString()})
				}
			case "inactive group":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET status='disabled' WHERE id=$1`, groupID)
			case "wrong platform":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET platform='anthropic' WHERE id=$1`, groupID)
			case "late invalid payment":
				params.Payment = &domain.CarpoolPaymentInput{PaymentKind: "invalid"}
			}
			require.NoError(t, err)
			snapshot := func() string {
				var raw string
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT jsonb_build_object('user',(SELECT to_jsonb(u) FROM users u WHERE id=$1),'keys',(SELECT jsonb_agg(k ORDER BY id) FROM api_keys k WHERE user_id=$1),'bindings',(SELECT jsonb_agg(b) FROM carpool_billing_bindings b WHERE user_id=$1),'groups',(SELECT jsonb_agg(g) FROM user_allowed_groups g WHERE user_id=$1),'terms',(SELECT jsonb_agg(t) FROM carpool_terms t WHERE user_id=$1))::text`, userID).Scan(&raw))
				var normalized any
				require.NoError(t, json.Unmarshal([]byte(raw), &normalized))
				return raw
			}
			before := snapshot()
			_, _, err = repo.CreateTerm(ctx, params)
			require.Error(t, err)
			if scenario == "balance" || scenario == "frozen" {
				require.ErrorIs(t, err, service.ErrCarpoolOpeningBalance)
			}
			if scenario == "nil key group" || scenario == "different key group" {
				require.ErrorIs(t, err, service.ErrCarpoolOpeningKeyGroup)
			}
			require.JSONEq(t, before, snapshot(), "failed opening must roll back binding, permissions, and term together")
		})
	}
}

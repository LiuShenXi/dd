//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_DeduplicatesBalanceBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-" + uuid.NewString(),
		Name:   "billing",
		Quota:  1,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         service.AccountTypeAPIKey,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result1)
	require.True(t, result1.Applied)
	require.True(t, result1.APIKeyQuotaExhausted)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id = $1", apiKey.ID).Scan(&quotaUsed))
	require.InDelta(t, 1.25, quotaUsed, 0.000001)

	var usage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT usage_5h FROM api_keys WHERE id = $1", apiKey.ID).Scan(&usage5h))
	require.InDelta(t, 1.25, usage5h, 0.000001)

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id = $1", apiKey.ID).Scan(&status))
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesSubscriptionBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-sub-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-sub-" + uuid.NewString(),
		Name:    "billing-sub",
	})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:  user.ID,
		GroupID: group.ID,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        0,
		SubscriptionID:   &subscription.ID,
		SubscriptionCost: 2.5,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var dailyUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&dailyUsage))
	require.InDelta(t, 2.5, dailyUsage, 0.000001)
}

func TestUsageBillingRepositoryApply_RequestFingerprintConflict(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-conflict-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-conflict-" + uuid.NewString(),
		Name:   "billing-conflict",
	})

	requestID := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	})
	require.NoError(t, err)

	_, err = repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 2.50,
	})
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestUsageBillingRepositoryApply_UpdatesAccountQuota(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-account-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-account-" + uuid.NewString(),
		Name:   "billing-account",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-quota-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_limit": 100.0,
		},
	})

	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeAPIKey,
		AccountQuotaCost: 3.5,
	})
	require.NoError(t, err)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE((extra->>'quota_used')::numeric, 0) FROM accounts WHERE id = $1", account.ID).Scan(&quotaUsed))
	require.InDelta(t, 3.5, quotaUsed, 0.000001)
}

func TestUsageBillingRepositoryApply_EnqueuesSchedulerOutboxOnQuotaCrossing(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)

	newFixture := func(t *testing.T, extra map[string]any) (int64, int64) {
		t.Helper()
		user := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("usage-billing-outbox-user-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()),
			PasswordHash: "hash",
		})
		apiKey := mustCreateApiKey(t, client, &service.APIKey{
			UserID: user.ID,
			Key:    "sk-usage-billing-outbox-" + uuid.NewString(),
			Name:   "billing-outbox",
		})
		account := mustCreateAccount(t, client, &service.Account{
			Name:  "usage-billing-outbox-" + uuid.NewString(),
			Type:  service.AccountTypeAPIKey,
			Extra: extra,
		})
		return apiKey.ID, account.ID
	}

	outboxCountFor := func(t *testing.T, accountID int64) int {
		t.Helper()
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1 AND account_id = $2",
			service.SchedulerOutboxEventAccountChanged, accountID,
		).Scan(&count))
		return count
	}

	t.Run("daily_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_daily_limit": 10.0,
		})
		// 第一次低于日限额：不应入队 outbox
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 4,
		})
		require.NoError(t, err)
		require.Equal(t, 0, outboxCountFor(t, accountID), "below limit should not enqueue")

		// 第二次跨越日限额：应入队一次 outbox
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 8,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "crossing daily limit should enqueue once")

		// 再次递增（已超）：不应重复入队
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 2,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "subsequent increments beyond limit should not re-enqueue")
	})

	t.Run("weekly_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_weekly_limit": 10.0,
		})
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 15, // 单次即跨越
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "single-shot crossing weekly limit should enqueue once")
	})
}

func TestDashboardAggregationRepositoryCleanupUsageBillingDedup_BatchDeletesOldRows(t *testing.T) {
	ctx := context.Background()
	repo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	oldRequestID := "dedup-old-" + uuid.NewString()
	newRequestID := "dedup-new-" + uuid.NewString()
	oldCreatedAt := time.Now().UTC().AddDate(0, 0, -400)
	newCreatedAt := time.Now().UTC().Add(-time.Hour)

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, created_at)
		VALUES ($1, 1, $2, $3), ($4, 1, $5, $6)
	`,
		oldRequestID, strings.Repeat("a", 64), oldCreatedAt,
		newRequestID, strings.Repeat("b", 64), newCreatedAt,
	)
	require.NoError(t, err)

	require.NoError(t, repo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	var oldCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", oldRequestID).Scan(&oldCount))
	require.Equal(t, 0, oldCount)

	var newCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", newRequestID).Scan(&newCount))
	require.Equal(t, 1, newCount)

	var archivedCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup_archive WHERE request_id = $1", oldRequestID).Scan(&archivedCount))
	require.Equal(t, 1, archivedCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesAgainstArchivedKey(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB, nil)
	aggRepo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-archive-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-archive-" + uuid.NewString(),
		Name:   "billing-archive",
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_dedup
		SET created_at = $1
		WHERE request_id = $2 AND api_key_id = $3
	`, time.Now().UTC().AddDate(0, 0, -400), requestID, apiKey.ID)
	require.NoError(t, err)
	require.NoError(t, aggRepo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)
}

type carpoolUsageBillingFixture struct {
	repo       service.UsageBillingRepository
	snapshot   domain.CarpoolBillingSnapshot
	cost       decimal.Decimal
	apiKeyID   int64
	cycleID    int64
	requestID  string
	baseBefore decimal.Decimal
}

func newCarpoolUsageBillingFixture(t *testing.T) *carpoolUsageBillingFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	carpoolRepo := NewCarpoolRepository(integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        "carpool-usage-billing-" + uuid.NewString() + "@example.com",
		PasswordHash: "synthetic-hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "carpool-usage-billing-" + uuid.NewString(),
		SubscriptionType: service.SubscriptionTypeCarpool,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:    user.ID,
		GroupID:   &group.ID,
		Key:       "sk-carpool-usage-billing-" + uuid.NewString(),
		Name:      "synthetic-carpool-usage-billing",
		Quota:     100,
		QuotaUsed: 3,
	})

	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now))
	plans, err := carpoolRepo.ListPlans(ctx, true)
	require.NoError(t, err)
	var plan *domain.CarpoolPlan
	for i := range plans {
		if plans[i].Code == "four_seat" {
			plan = &plans[i]
			break
		}
	}
	require.NotNil(t, plan)
	planSnapshot, err := json.Marshal(plan.Snapshot())
	require.NoError(t, err)

	startsAt := now.Add(-time.Hour)
	expiresAt := startsAt.Add(30 * 24 * time.Hour)
	var termID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_terms(
			user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,
			status,source_mode,history_complete,statistics_since,created_by
		) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,'active','new',TRUE,$6,$1)
		RETURNING id
	`, user.ID, domain.CarpoolGlobalScopeID, group.ID, plan.ID, string(planSnapshot), startsAt, expiresAt).Scan(&termID))

	baseBefore := decimal.RequireFromString("100")
	cycleEndsAt := startsAt.Add(7 * 24 * time.Hour)
	var cycleID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_cycles(
			term_id,cycle_no,starts_at,ends_at,base_quota_usd,
			base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at
		) VALUES($1,1,$2,$3,550.00000000,$4,0,0,'active',$2)
		RETURNING id
	`, termID, startsAt, cycleEndsAt, baseBefore.StringFixed(8)).Scan(&cycleID))

	requestID := "carpool:usage:" + uuid.NewString()
	snapshot := domain.CarpoolBillingSnapshot{
		RequestID:  requestID,
		UserID:     user.ID,
		APIKeyID:   apiKey.ID,
		GroupID:    group.ID,
		TermID:     termID,
		CycleID:    cycleID,
		AdmittedAt: now,
	}
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO carpool_billing_requests(
			request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,
			status,request_fingerprint
		) VALUES($1,$2,$3,$4,$5,$6,$7,'admitted',$8)
		RETURNING id,admitted_at
	`, requestID, apiKey.ID, user.ID, group.ID, termID, cycleID, now, strings.Repeat("f", 64)).Scan(&snapshot.BillingRequestID, &snapshot.AdmittedAt))

	fixture := &carpoolUsageBillingFixture{
		repo:       NewUsageBillingRepository(client, integrationDB, carpoolRepo),
		snapshot:   snapshot,
		cost:       decimal.RequireFromString("8.25"),
		apiKeyID:   apiKey.ID,
		cycleID:    cycleID,
		requestID:  requestID,
		baseBefore: baseBefore,
	}
	payload, err := service.MarshalCarpoolUsageBillingReceipt(fixture.command(false))
	require.NoError(t, err)
	require.NoError(t, carpoolRepo.PersistKnownUsage(ctx, snapshot, fixture.cost, payload))
	return fixture
}

func (f *carpoolUsageBillingFixture) command(includeAPIKeyQuota bool) *service.UsageBillingCommand {
	snapshot := f.snapshot
	cmd := &service.UsageBillingCommand{
		RequestID:       f.requestID,
		APIKeyID:        f.apiKeyID,
		UserID:          snapshot.UserID,
		CarpoolCost:     f.cost,
		CarpoolSnapshot: &snapshot,
	}
	if includeAPIKeyQuota {
		cmd.APIKeyQuotaCost = f.cost.InexactFloat64()
	}
	return cmd
}

func (f *carpoolUsageBillingFixture) requireSettledOnce(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	expectedBalance := f.baseBefore.Sub(f.cost).StringFixed(service.UsageBillingMonetaryScale)

	var balance, status, settledCost string
	var revision, ledgerCount, dedupCount int
	var settledAt *time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT base_balance_usd::text,revision FROM carpool_cycles WHERE id=$1
	`, f.cycleID).Scan(&balance, &revision))
	require.Equal(t, expectedBalance, balance)
	require.Equal(t, 1, revision)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,actual_cost_usd::text,settled_at FROM carpool_billing_requests WHERE id=$1
	`, f.snapshot.BillingRequestID).Scan(&status, &settledCost, &settledAt))
	require.Equal(t, "settled", status)
	require.Equal(t, f.cost.StringFixed(service.UsageBillingMonetaryScale), settledCost)
	require.NotNil(t, settledAt)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1 AND api_key_id=$2
	`, f.requestID, f.apiKeyID).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2
	`, f.requestID, f.apiKeyID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_CarpoolConcurrentSettlementAppliesOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := newCarpoolUsageBillingFixture(t)

	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := fixture.repo.Apply(ctx, fixture.command(false))
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	applied, replayed := 0, 0
	for outcome := range outcomes {
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		if outcome.result.Applied {
			applied++
		} else {
			replayed++
		}
	}
	require.Equal(t, 1, applied)
	require.Equal(t, 1, replayed)
	fixture.requireSettledOnce(t)
}

func TestUsageBillingRepositoryApply_CarpoolReplayDoesNotDebitAgain(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolUsageBillingFixture(t)

	first, err := fixture.repo.Apply(ctx, fixture.command(false))
	require.NoError(t, err)
	require.True(t, first.Applied)
	fixture.requireSettledOnce(t)

	replay, err := fixture.repo.Apply(ctx, fixture.command(false))
	require.NoError(t, err)
	require.False(t, replay.Applied)
	fixture.requireSettledOnce(t)
}

func TestUsageBillingRepositoryApply_CarpoolFailureRollsBackAllEffects(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolUsageBillingFixture(t)
	suffix := time.Now().UnixNano()
	functionName := fmt.Sprintf("fail_carpool_usage_billing_%d", suffix)
	triggerName := fmt.Sprintf("fail_carpool_usage_billing_trigger_%d", suffix)

	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.id = %d AND NEW.quota_used <> OLD.quota_used THEN
				RAISE EXCEPTION 'forced carpool usage billing failure';
			END IF;
			RETURN NEW;
		END;
		$$
	`, functionName, fixture.apiKeyID))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(
		"CREATE TRIGGER %s BEFORE UPDATE ON api_keys FOR EACH ROW EXECUTE FUNCTION %s()",
		triggerName,
		functionName,
	))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON api_keys", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	result, err := fixture.repo.Apply(ctx, fixture.command(true))
	require.Nil(t, result)
	require.ErrorContains(t, err, "forced carpool usage billing failure")

	var balance, status string
	var revision, ledgerCount, dedupCount int
	var settledAt *time.Time
	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT base_balance_usd::text,revision FROM carpool_cycles WHERE id=$1
	`, fixture.cycleID).Scan(&balance, &revision))
	require.Equal(t, fixture.baseBefore.StringFixed(service.UsageBillingMonetaryScale), balance)
	require.Zero(t, revision)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,settled_at FROM carpool_billing_requests WHERE id=$1
	`, fixture.snapshot.BillingRequestID).Scan(&status, &settledAt))
	require.Equal(t, "usage_known", status)
	require.Nil(t, settledAt)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM carpool_ledger WHERE request_id=$1 AND api_key_id=$2
	`, fixture.requestID, fixture.apiKeyID).Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2
	`, fixture.requestID, fixture.apiKeyID).Scan(&dedupCount))
	require.Zero(t, dedupCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used FROM api_keys WHERE id=$1
	`, fixture.apiKeyID).Scan(&quotaUsed))
	require.InDelta(t, 3, quotaUsed, 0.000001)
}

func TestCarpoolRepositoryMarkReconcileRequired_PreservesKnownUsageReceipt(t *testing.T) {
	ctx := context.Background()
	fixture := newCarpoolUsageBillingFixture(t)
	repo := NewCarpoolRepository(integrationDB)

	require.NoError(t, repo.MarkReconcileRequired(ctx, fixture.snapshot, "usage record task panicked"))

	var status, cost, diagnostic string
	var payloadPresent bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status,actual_cost_usd::text,billing_payload IS NOT NULL,last_error
		FROM carpool_billing_requests WHERE id=$1
	`, fixture.snapshot.BillingRequestID).Scan(&status, &cost, &payloadPresent, &diagnostic))
	require.Equal(t, "usage_known", status)
	require.Equal(t, fixture.cost.StringFixed(service.UsageBillingMonetaryScale), cost)
	require.True(t, payloadPresent)
	require.Equal(t, "usage_reconciliation_required", diagnostic)
}

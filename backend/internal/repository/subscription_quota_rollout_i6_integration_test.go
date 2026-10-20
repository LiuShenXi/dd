//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type quotaRolloutFixture struct {
	*subscriptionQuotaDomainFixture
	target, key int64
	rollout     *service.SubscriptionQuotaRolloutService
	router      *gin.Engine
}

func newQuotaRolloutFixture(t *testing.T, kind string) *quotaRolloutFixture {
	t.Helper()
	f := &quotaRolloutFixture{subscriptionQuotaDomainFixture: newSubscriptionQuotaDomainFixture(t)}
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic controlled conversion','anthropic',$1) RETURNING id`, kind).Scan(&f.target))
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-rollout-one','Synthetic identity one') RETURNING id`, f.userA, f.target).Scan(&f.key))
	_, err := f.db.Exec(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-rollout-two','Synthetic identity two')`, f.userA, f.target)
	require.NoError(t, err)
	if kind == "subscription" {
		_, err = f.db.Exec(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES($1,$2,clock_timestamp()-interval '1 day',clock_timestamp()+interval '90 days',1.1234567890,7.8765432109,19.9999999999)`, f.userA, f.target)
		require.NoError(t, err)
	}
	f.cfg.SubscriptionQuota.AdmissionEnabled = true
	f.cfg.SubscriptionQuota.RecoveryEnabled = true
	f.cfg.SubscriptionQuota.EnabledGroupIDs = append(f.cfg.SubscriptionQuota.EnabledGroupIDs, f.target)
	f.cfg.SubscriptionQuota.ProtectedGroupIDs = []int64{f.target}
	f.cfg.SubscriptionQuota.RolloutReplicaID = "app-1"
	f.cfg.SubscriptionQuota.RolloutExpectedReplicaIDs = []string{"app-1"}
	f.rollout = service.NewSubscriptionQuotaRolloutService(NewSubscriptionQuotaRolloutRepository(f.db), f.cfg)
	require.NoError(t, f.rollout.RegisterRuntime(context.Background()))
	handler := adminhandler.NewSubscriptionQuotaHandler(f.api)
	handler.SetRollout(f.rollout)
	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.admin})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	base := "/api/v1/admin/groups/:id/subscription-quota/rollout"
	f.router.GET(base, handler.GetRollout)
	f.router.POST(base+"/maintenance", handler.MaintainRollout)
	f.router.POST(base+"/preview", handler.PreviewRollout)
	f.router.POST(base+"/convert", handler.ConvertRollout)
	// Snapshot after constructing the synthetic conversion target. Existing GPT,
	// ordinary money, API keys and unrelated native windows must stay identical.
	for _, table := range []string{"groups", "subscription_quota_rollouts", "subscription_quota_rollout_operations", "subscription_quota_runtime_replicas"} {
		delete(f.financialColumns, table)
	}
	f.financialBefore = subscriptionQuotaLegacyFingerprints(t, f.db, f.financialColumns)
	return f
}
func (f *quotaRolloutFixture) http(t *testing.T, suffix, body, key string, want int, out any) {
	t.Helper()
	method := http.MethodPost
	if suffix == "" {
		method = http.MethodGet
	}
	r := httptest.NewRequest(method, fmt.Sprintf("/api/v1/admin/groups/%d/subscription-quota/rollout%s", f.target, suffix), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	require.Equal(t, want, w.Code, w.Body.String())
	if out != nil {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		require.NoError(t, json.Unmarshal(envelope.Data, out))
	}
}
func (f *quotaRolloutFixture) maintenance(t *testing.T, enabled bool, revision int64) domain.SubscriptionQuotaRolloutState {
	t.Helper()
	var out domain.SubscriptionQuotaRolloutState
	f.http(t, "/maintenance", quotaDomainJSON(t, map[string]any{"enabled": enabled, "expected_revision": revision, "reason": "synthetic reviewed conversion"}), uuid.NewString(), 200, &out)
	return out
}
func (f *quotaRolloutFixture) preview(t *testing.T, alloc []domain.SubscriptionQuotaAllocation) domain.SubscriptionQuotaRolloutState {
	t.Helper()
	require.NoError(t, f.rollout.RegisterRuntime(context.Background()))
	var out domain.SubscriptionQuotaRolloutState
	f.http(t, "/preview", quotaDomainJSON(t, map[string]any{"allocations": alloc}), "", 200, &out)
	return out
}
func (f *quotaRolloutFixture) convertBody(t *testing.T, p domain.SubscriptionQuotaRolloutState) string {
	t.Helper()
	return quotaDomainJSON(t, service.SubscriptionQuotaConversionInput{ExpectedRevision: &p.Revision, PreviewToken: p.PreviewToken, Allocations: p.Allocations})
}
func (f *quotaRolloutFixture) allocations() []domain.SubscriptionQuotaAllocation {
	return []domain.SubscriptionQuotaAllocation{{UserID: f.userB, QuotaUSD: "300"}, {UserID: f.userA, QuotaUSD: "100"}}
}
func (f *quotaRolloutFixture) assertUnconverted(t *testing.T) {
	t.Helper()
	var mode, status string
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT subscription_quota_mode,status FROM groups WHERE id=$1`, f.target).Scan(&mode, &status))
	require.Equal(t, "windows", mode)
	require.Equal(t, domain.SubscriptionQuotaMaintenanceStatus, status)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms WHERE group_id=$1`, f.target).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_rollout_operations WHERE group_id=$1 AND operation_kind='conversion'`, f.target).Scan(&count))
	require.Zero(t, count)
}
func TestSubscriptionQuotaRolloutI6_RealAdminPathStandardAndWindows(t *testing.T) {
	for _, kind := range []string{"standard", "subscription"} {
		t.Run(kind, func(t *testing.T) {
			f := newQuotaRolloutFixture(t, kind)
			var otherGroups string
			require.NoError(t, f.db.QueryRow(`SELECT md5(coalesce(jsonb_agg(to_jsonb(g) ORDER BY id)::text,'[]')) FROM groups g WHERE id<>$1`, f.target).Scan(&otherGroups))
			var initial domain.SubscriptionQuotaRolloutState
			f.http(t, "", "", "", 200, &initial)
			require.Contains(t, initial.Blockers, "MAINTENANCE_REQUIRED")
			require.Len(t, initial.KeyUsers, 1)
			require.EqualValues(t, 2, initial.KeyUsers[0].KeyCount)
			m := f.maintenance(t, true, 0)
			require.True(t, m.Maintenance)
			require.EqualValues(t, 1, m.Revision)
			p := f.preview(t, f.allocations())
			require.Empty(t, p.Blockers)
			require.Len(t, p.PreviewToken, 64)
			// Repeated identical preview and routine heartbeat do not change its token.
			require.Equal(t, p.PreviewToken, f.preview(t, f.allocations()).PreviewToken)
			key := uuid.NewString()
			body := f.convertBody(t, p)
			var result domain.SubscriptionQuotaRolloutResult
			f.http(t, "/convert", body, key, 200, &result)
			require.False(t, result.Replayed)
			require.True(t, result.State.Converted)
			require.True(t, result.State.Maintenance)
			require.EqualValues(t, 2, result.State.Revision)
			require.Len(t, result.Terms, 2)
			for _, term := range result.Terms {
				require.Equal(t, 168*time.Hour, term.ExpiresAt.Sub(term.StartsAt))
				require.WithinDuration(t, time.Now(), term.StartsAt, 10*time.Second)
				require.Equal(t, "0.00000000", term.UsedUSD)
			}
			require.Equal(t, "100.00000000", result.Terms[0].QuotaUSD)
			require.Equal(t, "300.00000000", result.Terms[1].QuotaUSD)
			var replay domain.SubscriptionQuotaRolloutResult
			f.http(t, "/convert", body, key, 200, &replay)
			require.True(t, replay.Replayed)
			require.Equal(t, result.Terms, replay.Terms)
			p.Allocations[0].QuotaUSD = "101"
			f.http(t, "/convert", f.convertBody(t, p), key, 409, nil)
			var cards, outbox, operations int
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms WHERE group_id=$1`, f.target).Scan(&cards))
			require.Equal(t, 2, cards)
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_rollout_operations WHERE group_id=$1`, f.target).Scan(&operations))
			require.Equal(t, 2, operations)
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key IN(SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE group_id=$1)`, f.target).Scan(&outbox))
			require.GreaterOrEqual(t, outbox, 4)
			released := f.maintenance(t, false, 2)
			require.False(t, released.Maintenance)
			require.Equal(t, "active", released.GroupStatus)
			require.Equal(t, "term", released.QuotaMode)
			f.assertFinancialIsolation(t)
			var afterGroups string
			require.NoError(t, f.db.QueryRow(`SELECT md5(coalesce(jsonb_agg(to_jsonb(g) ORDER BY id)::text,'[]')) FROM groups g WHERE id<>$1`, f.target).Scan(&afterGroups))
			require.Equal(t, otherGroups, afterGroups, "every unrelated group remains byte-for-byte unchanged")
		})
	}
}
func TestSubscriptionQuotaRolloutI6_ExplicitEmptyAndStrictInputs(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	f.maintenance(t, true, 0)
	for _, body := range []string{`{}`, `{"allocations":null}`, `{"allocations":[],"unknown":1}`, `{"allocations":[],"allocations":[]}`, fmt.Sprintf(`{"allocations":[{"user_id":%d,"quota_usd":100}]}`, f.userA)} {
		f.http(t, "/preview", body, "", 400, nil)
	}
	for _, q := range []string{"0", "1e2", " 100", "-2", "0.000000001"} {
		f.http(t, "/preview", quotaDomainJSON(t, map[string]any{"allocations": []domain.SubscriptionQuotaAllocation{{UserID: f.userA, QuotaUSD: q}}}), "", 400, nil)
	}
	f.http(t, "/maintenance", `{"expected_revision":1,"reason":"missing choice"}`, uuid.NewString(), 400, nil)
	p := f.preview(t, []domain.SubscriptionQuotaAllocation{})
	require.Empty(t, p.Blockers)
	require.Len(t, p.PreviewToken, 64)
	var result domain.SubscriptionQuotaRolloutResult
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 200, &result)
	require.Empty(t, result.Terms)
	require.True(t, result.State.Converted)
}
func TestSubscriptionQuotaRolloutI6_PreviewDriftAndAtomicFailure(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	_, err := f.db.Exec(`UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, f.userA)
	require.NoError(t, err)
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 409, nil)
	f.assertUnconverted(t)
	p = f.preview(t, f.allocations())
	_, err = f.db.Exec(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-rollout-late-key','New membership')`, f.userB, f.target)
	require.NoError(t, err)
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 409, nil)
	f.assertUnconverted(t)
	p = f.preview(t, f.allocations())
	_, err = f.db.Exec(`CREATE FUNCTION synthetic_rollout_outbox_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic transactional outbox failure'; END; $$; CREATE TRIGGER synthetic_rollout_outbox_failure BEFORE INSERT ON auth_cache_invalidation_outbox FOR EACH ROW EXECUTE FUNCTION synthetic_rollout_outbox_failure()`)
	require.NoError(t, err)
	key := uuid.NewString()
	f.http(t, "/convert", f.convertBody(t, p), key, 500, nil)
	f.assertUnconverted(t)
	_, err = f.db.Exec(`DROP TRIGGER synthetic_rollout_outbox_failure ON auth_cache_invalidation_outbox; DROP FUNCTION synthetic_rollout_outbox_failure()`)
	require.NoError(t, err)
	var result domain.SubscriptionQuotaRolloutResult
	f.http(t, "/convert", f.convertBody(t, p), key, 200, &result)
	require.Len(t, result.Terms, 2)
}
func TestSubscriptionQuotaRolloutI6_ProtectedQueueAndAsyncUsageDrain(t *testing.T) {
	f := newQuotaRolloutFixture(t, "subscription")
	ctx := context.Background()
	repo := NewSubscriptionQuotaProtectedGroupRepository(f.db)
	in := service.SubscriptionQuotaProtectedAdmission{RequestID: "rollout-queue:" + uuid.NewString(), UserID: f.userA, APIKeyID: f.key, GroupID: f.target, SubscriptionType: "subscription"}
	require.NoError(t, repo.AdmitProtectedGroup(ctx, in))
	async := in
	async.RequestID = "rollout-async:" + uuid.NewString()
	require.NoError(t, repo.AdmitProtectedGroup(ctx, async))
	require.NoError(t, repo.RecordProtectedGroupUsage(ctx, async.RequestID, json.RawMessage(`{"version":1,"usage":{"input_tokens":4}}`)))
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	require.EqualValues(t, 2, p.ProtectedPending)
	require.Contains(t, p.Blockers, "PROTECTED_REQUESTS_PENDING")
	require.Empty(t, p.PreviewToken)
	late := in
	late.RequestID = "rollout-late:" + uuid.NewString()
	require.Error(t, repo.AdmitProtectedGroup(ctx, late))
	// Proven no-send outcome drains the queue marker, independently of Redis TTL.
	require.NoError(t, repo.FinishProtectedGroup(ctx, in.RequestID, "no_charge", "queue_cancelled"))
	p = f.preview(t, f.allocations())
	require.EqualValues(t, 1, p.ProtectedPending)
	// Known late legacy accounting remains writable while rights are maintained.
	_, err := f.db.Exec(`UPDATE user_subscriptions SET daily_usage_usd=daily_usage_usd+0.0000000001,weekly_usage_usd=weekly_usage_usd+0.0000000001 WHERE user_id=$1 AND group_id=$2`, f.userA, f.target)
	require.NoError(t, err)
	require.NoError(t, repo.FinishProtectedGroup(ctx, async.RequestID, "completed", "legacy_billing_completed"))
	p = f.preview(t, f.allocations())
	require.Empty(t, p.Blockers)
	require.Len(t, p.PreviewToken, 64)
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 200, nil)
}
func TestSubscriptionQuotaRolloutI6_ReplicaAndLegacyMutationGates(t *testing.T) {
	f := newQuotaRolloutFixture(t, "subscription")
	ctx := context.Background()
	f.maintenance(t, true, 0)
	for _, query := range []string{`UPDATE groups SET status='active' WHERE id=$1`, `UPDATE groups SET subscription_quota_mode='term' WHERE id=$1`, `UPDATE user_subscriptions SET expires_at=expires_at+interval '1 day' WHERE group_id=$1`, `DELETE FROM user_subscriptions WHERE group_id=$1`, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) SELECT user_id,group_id,clock_timestamp(),clock_timestamp()+interval '7 day' FROM user_subscriptions WHERE group_id=$1`} {
		_, err := f.db.Exec(query, f.target)
		require.Error(t, err, query)
	}
	_, err := f.db.Exec(`UPDATE subscription_quota_runtime_replicas SET heartbeat_at=clock_timestamp()-interval '1 minute'`)
	require.NoError(t, err)
	state, err := f.rollout.Get(ctx, f.target)
	require.NoError(t, err)
	require.Contains(t, state.Blockers, "REPLICA_MISSING_OR_STALE")
	f.http(t, "/maintenance", `{"enabled":false,"expected_revision":1,"reason":"stale replicas"}`, uuid.NewString(), 409, nil)
	require.NoError(t, f.rollout.RegisterRuntime(ctx))
	for _, mutate := range []string{`capabilities || '{"lease_protocol_version":0}'::jsonb`, `capabilities || '{"claude_default_concurrency":2}'::jsonb`, `capabilities || '{"issue_enabled":false}'::jsonb`, `capabilities || '{"expected_replica_ids":["app-1","app-2"]}'::jsonb`} {
		_, err = f.db.Exec(`UPDATE subscription_quota_runtime_replicas SET capabilities=` + mutate)
		require.NoError(t, err)
		blocked, err := f.rollout.Preview(ctx, f.target, f.admin, f.allocations())
		require.NoError(t, err)
		require.Empty(t, blocked.PreviewToken)
		require.NotEmpty(t, blocked.Blockers)
		require.NoError(t, f.rollout.RegisterRuntime(ctx))
	}
	// A live second process using the same configured identity cannot hide itself.
	duplicate := service.NewSubscriptionQuotaRolloutService(NewSubscriptionQuotaRolloutRepository(f.db), f.cfg)
	require.NoError(t, duplicate.RegisterRuntime(ctx))
	state, err = f.rollout.Get(ctx, f.target)
	require.NoError(t, err)
	require.Contains(t, state.Blockers, "REPLICA_DUPLICATE")
	f.http(t, "/maintenance", `{"enabled":false,"expected_revision":1,"reason":"duplicate process"}`, uuid.NewString(), 409, nil)
}
func TestSubscriptionQuotaRolloutI6_ParallelConfirmReplaysSingleIssuance(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	key := uuid.NewString()
	in := service.SubscriptionQuotaConversionInput{ExpectedRevision: &p.Revision, PreviewToken: p.PreviewToken, Allocations: p.Allocations}
	const n = 8
	results := make(chan *domain.SubscriptionQuotaRolloutResult, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.rollout.Convert(ctx, f.target, f.admin, in, key)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	fresh := 0
	for result := range results {
		require.Len(t, result.Terms, 2)
		if !result.Replayed {
			fresh++
		}
	}
	require.Equal(t, 1, fresh)
}

func TestSubscriptionQuotaRolloutI6_PaidPendingAndLateOrderDrift(t *testing.T) {
	f := newQuotaRolloutFixture(t, "subscription")
	var orderID int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO payment_orders(user_id,amount,pay_amount,expires_at,out_trade_no,order_type,subscription_group_id,subscription_days,status,paid_at) VALUES($1,12.34,12.34,clock_timestamp()+interval '1 day','synthetic-rollout-unfulfilled','subscription',$2,30,'PAID',clock_timestamp()) RETURNING id`, f.userA, f.target).Scan(&orderID))
	_, err := f.db.Exec(`INSERT INTO payment_orders(user_id,amount,pay_amount,expires_at,out_trade_no,order_type,subscription_group_id,subscription_days,status,updated_at) VALUES($1,12.34,12.34,clock_timestamp()-interval '1 day','synthetic-rollout-cancelled','subscription',$2,30,'CANCELLED',clock_timestamp()-interval '1 day'),($1,12.34,12.34,clock_timestamp()-interval '1 day','synthetic-rollout-expired','subscription',$2,30,'EXPIRED',clock_timestamp()-interval '1 day')`, f.userA, f.target)
	require.NoError(t, err)
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	require.EqualValues(t, 2, p.LegacyTerminalOrders)
	require.EqualValues(t, 1, p.PendingOrders)
	require.Contains(t, p.Blockers, "ORDERS_PENDING")
	require.Empty(t, p.PreviewToken)
	// Model an independently verified historical outcome. This is a fixture fact,
	// never the conversion implementation changing a customer's payment status.
	_, err = f.db.Exec(`UPDATE payment_orders SET status='COMPLETED' WHERE id=$1`, orderID)
	require.NoError(t, err)
	p = f.preview(t, f.allocations())
	require.Empty(t, p.Blockers)
	// Any payment/refund drift after review invalidates the complete fingerprint.
	_, err = f.db.Exec(`UPDATE payment_orders SET refund_amount=1 WHERE id=$1`, orderID)
	require.NoError(t, err)
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 409, nil)
	f.assertUnconverted(t)
	_, err = f.db.Exec(`INSERT INTO payment_orders(user_id,amount,pay_amount,expires_at,out_trade_no,order_type,subscription_group_id,subscription_days) VALUES($1,12.34,12.34,clock_timestamp()+interval '1 day','synthetic-maintenance-rejected','subscription',$2,7)`, f.userA, f.target)
	require.Error(t, err)
}

func TestSubscriptionQuotaRolloutI6_GPTGuardDoesNotAcquireGroupRowLocks(t *testing.T) {
	f := newQuotaRolloutFixture(t, "subscription")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var gpt, parent int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic untouched GPT','openai','subscription') RETURNING id`).Scan(&gpt))
	require.NoError(t, f.db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '1 day') RETURNING id`, f.userB, gpt).Scan(&parent))
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, gpt)
	require.NoError(t, err)
	// A GPT accounting update must not wait on the unrelated group lock introduced
	// only for Anthropic conversion. Bounded context makes this a lock assertion.
	_, err = f.db.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=daily_usage_usd+1 WHERE id=$1`, parent)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = f.rollout.Maintain(context.Background(), gpt, f.admin, service.SubscriptionQuotaMaintenanceInput{Enabled: true, ExpectedRevision: quotaDomainInt(0), Reason: "reject GPT"}, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaGroupInvalid)
	f.maintenance(t, true, 0)
	_, err = f.db.Exec(`UPDATE user_subscriptions SET group_id=$1 WHERE group_id=$2`, gpt, f.target)
	require.Error(t, err, "moving existing native rights out of a maintained group cannot bypass the guard")
}

func TestSubscriptionQuotaRolloutI6_ActorRevokedWhileWaitingForScope(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	require.NoError(t, subscriptionQuotaScopeLock(ctx, holder, f.userA, f.target))
	result := make(chan error, 1)
	go func() {
		_, err := f.rollout.Convert(ctx, f.target, f.admin, service.SubscriptionQuotaConversionInput{ExpectedRevision: &p.Revision, PreviewToken: p.PreviewToken, Allocations: p.Allocations}, uuid.NewString())
		result <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE l.locktype='advisory' AND NOT l.granted AND a.datname=current_database())`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = f.db.ExecContext(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.admin)
	require.NoError(t, err)
	require.NoError(t, holder.Commit())
	require.ErrorIs(t, <-result, service.ErrSubscriptionQuotaNotFound)
	f.assertUnconverted(t)
}

func TestSubscriptionQuotaRolloutI6_RealRuntimeRosterRequiresBothProcesses(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	ctx := context.Background()
	f.maintenance(t, true, 0)
	f.cfg.SubscriptionQuota.RolloutExpectedReplicaIDs = []string{"app-1", "app-2"}
	require.NoError(t, f.rollout.RegisterRuntime(ctx))
	p := f.preview(t, f.allocations())
	require.Contains(t, p.Blockers, "REPLICA_MISSING_OR_STALE")
	require.Empty(t, p.PreviewToken)
	otherConfig := *f.cfg
	otherConfig.SubscriptionQuota.RolloutReplicaID = "app-2"
	other := service.NewSubscriptionQuotaRolloutService(NewSubscriptionQuotaRolloutRepository(f.db), &otherConfig)
	require.NoError(t, other.RegisterRuntime(ctx))
	p = f.preview(t, f.allocations())
	require.Empty(t, p.Blockers)
	require.Len(t, p.Replicas, 2)
	// A second running process advertises its actual incompatible default. The
	// administrator cannot repair that by putting capabilities in the request.
	otherConfig.Default.ClaudeSubscriptionConcurrency = 2
	require.NoError(t, other.RegisterRuntime(ctx))
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 409, nil)
	f.assertUnconverted(t)
	otherConfig.Default.ClaudeSubscriptionConcurrency = 3
	require.NoError(t, other.RegisterRuntime(ctx))
	p = f.preview(t, f.allocations())
	f.http(t, "/convert", f.convertBody(t, p), uuid.NewString(), 200, nil)
}

func TestSubscriptionQuotaRolloutI6_AttemptedUnknownCannotExpireIntoConversion(t *testing.T) {
	f := newQuotaRolloutFixture(t, "standard")
	ctx := context.Background()
	repo := NewSubscriptionQuotaProtectedGroupRepository(f.db)
	in := service.SubscriptionQuotaProtectedAdmission{RequestID: "rollout-unknown:" + uuid.NewString(), UserID: f.userA, APIKeyID: f.key, GroupID: f.target, SubscriptionType: "standard"}
	require.NoError(t, repo.AdmitProtectedGroup(ctx, in))
	require.NoError(t, repo.FinishProtectedGroup(ctx, in.RequestID, "reconcile_required", "attempted_usage_unknown"))
	f.maintenance(t, true, 0)
	p := f.preview(t, f.allocations())
	require.Contains(t, p.Blockers, "PROTECTED_REQUESTS_PENDING")
	require.Empty(t, p.PreviewToken)
	require.Error(t, repo.FinishProtectedGroup(ctx, in.RequestID, "no_charge", "cannot_claim_no_send_after_attempt"))
	_, err := f.db.Exec(`UPDATE subscription_quota_protected_requests SET updated_at=clock_timestamp()-interval '1 year' WHERE request_id=$1`, in.RequestID)
	require.NoError(t, err)
	p = f.preview(t, f.allocations())
	require.EqualValues(t, 1, p.ProtectedPending)
	require.Empty(t, p.PreviewToken)
	f.assertUnconverted(t)
}

//go:build integration

package repository

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// I5 uses the original TestMain's disposable PostgreSQL 18 container and a
// separate fresh database per fixture. The only payment provider is an HTTP
// loopback server; its credentials and identities are synthetic test constants.
const quotaPaymentI5Key = "i5-synthetic-easypay-no-real-money"
const quotaPaymentI5EncryptionKey = "0123456789abcdef0123456789abcdef"

type quotaPaymentI5Fixture struct {
	*subscriptionQuotaDomainFixture
	client   *dbent.Client
	pay      *service.PaymentService
	plans    *service.PaymentConfigService
	paidRepo service.SubscriptionQuotaPaymentRepository
	subs     *service.SubscriptionService
	router   *gin.Engine
	creates  atomic.Int32
	refunds  atomic.Int32
	observed chan error
}

type quotaPaymentI5PauseAfterRights struct {
	delegate service.SubscriptionQuotaPaymentRepository
	paused   [2]chan struct{}
	release  [2]chan struct{}
	calls    atomic.Int32
}

func (p *quotaPaymentI5PauseAfterRights) FulfillPaidTerm(ctx context.Context, in domain.SubscriptionQuotaPaidTermParams) (*domain.SubscriptionQuotaMutationResult, error) {
	result, err := p.delegate.FulfillPaidTerm(ctx, in)
	if err != nil {
		return nil, err
	}
	i := p.calls.Add(1) - 1
	if i < 2 {
		close(p.paused[i])
		select {
		case <-p.release[i]:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, nil
}

func newQuotaPaymentI5Fixture(t *testing.T) *quotaPaymentI5Fixture {
	t.Helper()
	f := &quotaPaymentI5Fixture{subscriptionQuotaDomainFixture: newSubscriptionQuotaDomainFixture(t), observed: make(chan error, 32)}
	f.client = dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, f.db)))
	_, err := f.db.Exec(`UPDATE subscription_plans SET for_sale=true,product_name='Original I5 product' WHERE id=$1`, f.plan)
	require.NoError(t, err)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mapi.php" {
			f.refunds.Add(1)
			http.Error(w, "synthetic gateway only permits payment creation", http.StatusBadRequest)
			return
		}
		f.creates.Add(1)
		if err := r.ParseForm(); err != nil {
			f.observed <- err
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("sign") != quotaPaymentI5Sign(r.Form) {
			f.observed <- fmt.Errorf("outbound payment signature is invalid")
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
		var raw []byte
		var group int64
		err := f.db.QueryRow(`SELECT subscription_quota_snapshot,subscription_group_id FROM payment_orders WHERE out_trade_no=$1`, r.Form.Get("out_trade_no")).Scan(&raw, &group)
		if err == nil && group == f.group {
			var snapshot map[string]any
			err = json.Unmarshal(raw, &snapshot)
			if err == nil {
				_, err = domain.ParseSubscriptionQuotaOrderSnapshot(snapshot)
			}
		}
		f.observed <- err
		if err != nil {
			http.Error(w, "order snapshot not committed before provider call", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "trade_no": "fake-" + r.Form.Get("out_trade_no"), "qrcode": "/synthetic-checkout"})
	}))
	t.Cleanup(provider.Close)
	providerConfig, err := json.Marshal(map[string]string{"pid": "i5-synthetic-merchant", "pkey": quotaPaymentI5Key, "apiBase": provider.URL, "notifyUrl": "http://127.0.0.1/synthetic-callback", "returnUrl": "http://127.0.0.1/synthetic-return", "currency": "CNY"})
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(providerConfig), []byte(quotaPaymentI5EncryptionKey))
	require.NoError(t, err)
	_, err = f.client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).SetName("I5 synthetic loopback gateway").SetConfig(encrypted).SetSupportedTypes(payment.TypeAlipay).SetEnabled(true).SetRefundEnabled(true).Save(context.Background())
	require.NoError(t, err)
	settings := NewSettingRepository(f.client)
	require.NoError(t, settings.SetMultiple(context.Background(), map[string]string{
		service.SettingPaymentEnabled: "true", service.SettingEnabledPaymentTypes: "alipay", service.SettingMaxPendingOrders: "100", service.SettingPaymentVisibleMethodAlipayEnabled: "true", service.SettingPaymentVisibleMethodAlipaySource: service.VisibleMethodSourceEasyPayAlipay,
	}))
	f.plans = service.NewPaymentConfigService(f.client, settings, []byte(quotaPaymentI5EncryptionKey))
	registry := payment.NewRegistry()
	groups := NewGroupRepository(f.client, f.db)
	subscription := service.NewSubscriptionService(groups, NewUserSubscriptionRepository(f.client), nil, f.client, f.cfg)
	f.subs = subscription
	t.Cleanup(subscription.Stop)
	f.pay = service.NewPaymentService(f.client, registry, payment.NewDefaultLoadBalancer(f.client, []byte(quotaPaymentI5EncryptionKey)), nil, subscription, f.plans, NewUserRepository(f.client, f.db), groups, nil)
	f.paidRepo = NewSubscriptionQuotaPaymentRepository(f.db)
	f.pay.SetSubscriptionQuotaPayment(f.paidRepo, f.cfg)
	f.router = gin.New()
	orders := handler.NewPaymentHandler(f.pay, f.plans)
	f.router.POST("/api/v1/payment/orders", func(c *gin.Context) {
		// Auth is deliberately synthetic; order binding and all payment/business
		// handlers below are real. No actual user session or Key is used.
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.userA})
		orders.CreateOrder(c)
	})
	webhook := handler.NewPaymentWebhookHandler(f.pay, registry)
	f.router.POST("/api/v1/payment/webhook/easypay", webhook.EasyPayNotify)
	return f
}

func quotaPaymentI5Sign(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "sign" && key != "sign_type" && values.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + quotaPaymentI5Key))
	return hex.EncodeToString(sum[:])
}

func (f *quotaPaymentI5Fixture) createResponse(plan int64) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/payment/orders", strings.NewReader(fmt.Sprintf(`{"order_type":"subscription","payment_type":"alipay","plan_id":%d}`, plan)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func (f *quotaPaymentI5Fixture) create(t *testing.T, plan int64) *dbent.PaymentOrder {
	t.Helper()
	w := f.createResponse(plan)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Data service.CreateOrderResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Positive(t, response.Data.OrderID, w.Body.String())
	select {
	case err := <-f.observed:
		require.NoError(t, err, "the fake provider must observe the committed order and valid snapshot")
	case <-time.After(time.Second):
		t.Fatal("real provider never reached the loopback fake gateway")
	}
	order, err := f.client.PaymentOrder.Get(context.Background(), response.Data.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderStatusPending, order.Status)
	return order
}

func (f *quotaPaymentI5Fixture) callback(order *dbent.PaymentOrder, edit func(url.Values)) *httptest.ResponseRecorder {
	values := url.Values{"pid": {"i5-synthetic-merchant"}, "out_trade_no": {order.OutTradeNo}, "trade_no": {"fake-" + order.OutTradeNo}, "trade_status": {"TRADE_SUCCESS"}, "money": {strconv.FormatFloat(order.PayAmount, 'f', 2, 64)}, "sign_type": {"MD5"}}
	if edit != nil {
		edit(values)
	}
	values.Set("sign", quotaPaymentI5Sign(values))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/easypay", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func (f *quotaPaymentI5Fixture) requireCard(t *testing.T, order *dbent.PaymentOrder, status string) int64 {
	t.Helper()
	var id int64
	var quota, used, source, currentStatus string
	var actor sql.NullInt64
	var seconds float64
	var snapshot []byte
	require.NoError(t, f.db.QueryRow(`SELECT id,quota_usd::text,used_usd::text,source_kind,created_by,extract(epoch from expires_at-starts_at),source_snapshot FROM subscription_quota_terms WHERE payment_order_id=$1`, order.ID).Scan(&id, &quota, &used, &source, &actor, &seconds, &snapshot))
	require.Equal(t, "125.12345678", quota)
	require.Equal(t, "0.00000000", used)
	require.Equal(t, "payment", source)
	require.False(t, actor.Valid, "payment issuance must not forge an administrator")
	require.Equal(t, float64(604800), seconds)
	var frozen map[string]any
	require.NoError(t, json.Unmarshal(snapshot, &frozen))
	require.Equal(t, order.SubscriptionQuotaSnapshot, frozen)
	for _, query := range []string{
		`SELECT count(*) FROM subscription_quota_terms WHERE payment_order_id=$1`,
		`SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='SUBSCRIPTION_ASSIGNED'`,
	} {
		var count int
		require.NoError(t, f.db.QueryRow(query, order.ID).Scan(&count))
		require.Equal(t, 1, count)
	}
	var operations, outbox int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_operations WHERE actor_id IS NULL`).Scan(&operations))
	require.Equal(t, 1, operations)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&outbox))
	require.Positive(t, outbox)
	require.NoError(t, f.db.QueryRow(`SELECT status FROM payment_orders WHERE id=$1`, order.ID).Scan(&currentStatus))
	require.Equal(t, status, currentStatus)
	t.Logf("I5 real PG order=%d term=%d quota=%s duration=%.0fs assignment_audit=1 payment_operation=1 order_status=%s provider_creates=%d", order.ID, id, quota, seconds, currentStatus, f.creates.Load())
	return id
}

func (f *quotaPaymentI5Fixture) requireNoRights(t *testing.T, order int64) {
	t.Helper()
	for _, query := range []string{`SELECT count(*) FROM subscription_quota_terms WHERE payment_order_id=$1`, `SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='SUBSCRIPTION_ASSIGNED'`} {
		var count int
		require.NoError(t, f.db.QueryRow(query, order).Scan(&count))
		require.Zero(t, count)
	}
	for _, table := range []string{"subscription_quota_operations", "auth_cache_invalidation_outbox"} {
		var count int
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
}

func (f *quotaPaymentI5Fixture) moneyGuard(t *testing.T) func() {
	t.Helper()
	columns := make(map[string][]string)
	for table, fields := range subscriptionQuotaLegacyColumns(t, f.db) {
		if table == "users" || table == "redeem_codes" || table == "usage_logs" || strings.HasPrefix(table, "carpool_") {
			columns[table] = fields
		}
	}
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	native := f.nativeFingerprint(t)
	return func() {
		t.Helper()
		require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "payment must not credit ordinary balance, alter GPT or mint redeem codes")
		require.Equal(t, native, f.nativeFingerprint(t), "term payment must not alter native subscription windows")
	}
}

func TestSubscriptionQuotaPaymentI5_MutableDeletedPlanAndConcurrentSignedCallbacks(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	guard := f.moneyGuard(t)
	order := f.create(t, f.plan)
	snapshot, err := domain.ParseSubscriptionQuotaOrderSnapshot(order.SubscriptionQuotaSnapshot)
	require.NoError(t, err)
	require.Equal(t, "125.12345678", snapshot.QuotaUSD)
	require.Equal(t, "Original I5 product", snapshot.ProductName)
	price, forSale := 99.0, false
	_, err = f.plans.UpdatePlan(context.Background(), f.plan, service.UpdatePlanRequest{DefaultQuotaUSD: quotaDomainMoney("999"), Price: &price, ForSale: &forSale, Name: quotaDomainMoney("Changed product")})
	require.NoError(t, err)
	require.NoError(t, f.plans.DeletePlan(context.Background(), f.plan), "a frozen pending order must not prevent deleting its source plan")
	const workers = 16
	var wg sync.WaitGroup
	statuses := make(chan int, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; statuses <- f.callback(order, nil).Code }()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for status := range statuses {
		require.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, status, "overlapping callbacks may encounter the existing fulfillment lease")
	}
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	f.requireCard(t, order, payment.OrderStatusCompleted)
	require.Equal(t, int32(1), f.creates.Load(), "callbacks must never initiate another provider charge")
	guard()
}

func TestSubscriptionQuotaPaymentI5_RollbackBeforeIssuanceThenRetry(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	guard := f.moneyGuard(t)
	order := f.create(t, f.plan)
	_, err := f.db.Exec(`CREATE FUNCTION i5_fail_assignment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='SUBSCRIPTION_ASSIGNED' THEN RAISE EXCEPTION 'I5 injected assignment failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER i5_fail_assignment BEFORE INSERT ON payment_audit_logs FOR EACH ROW EXECUTE FUNCTION i5_fail_assignment()`)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, f.callback(order, nil).Code)
	f.requireNoRights(t, order.ID)
	var parentCount int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, f.userA, f.group).Scan(&parentCount))
	require.Zero(t, parentCount, "assignment audit failure must roll back the new parent too")
	_, err = f.db.Exec(`DROP TRIGGER i5_fail_assignment ON payment_audit_logs`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	f.requireCard(t, order, payment.OrderStatusCompleted)
	require.Equal(t, int32(1), f.creates.Load())
	guard()
}

func TestSubscriptionQuotaPaymentI5_PostRightsFailureAndDisabledIssueRecovery(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	guard := f.moneyGuard(t)
	order := f.create(t, f.plan)
	_, err := f.db.Exec(`CREATE FUNCTION i5_fail_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='COMPLETED' THEN RAISE EXCEPTION 'I5 injected post-rights failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER i5_fail_completion BEFORE UPDATE ON payment_orders FOR EACH ROW EXECUTE FUNCTION i5_fail_completion()`)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, f.callback(order, nil).Code)
	id := f.requireCard(t, order, payment.OrderStatusFailed)
	f.cfg.SubscriptionQuota.IssueEnabled = false
	f.cfg.Default.ClaudeSubscriptionConcurrency = 0
	_, err = f.db.Exec(`DROP TRIGGER i5_fail_completion ON payment_orders`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	require.Equal(t, id, f.requireCard(t, order, payment.OrderStatusCompleted))
	require.NotEqual(t, http.StatusOK, f.createResponse(f.plan).Code, "issue disable must block new purchases")
	require.Equal(t, int32(1), f.creates.Load())
	guard()
}

func TestSubscriptionQuotaPaymentI5_LeaseTakeoverRejectsStaleOwner(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	var oldLease time.Time
	require.NoError(t, f.db.QueryRow(`UPDATE payment_orders SET status='RECHARGING',paid_at=clock_timestamp(),updated_at=clock_timestamp()-interval '10 minutes' WHERE id=$1 RETURNING updated_at`, order.ID).Scan(&oldLease))
	var newLease time.Time
	require.NoError(t, f.db.QueryRow(`UPDATE payment_orders SET updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at`, order.ID).Scan(&newLease))
	_, err := f.paidRepo.FulfillPaidTerm(context.Background(), domain.SubscriptionQuotaPaidTermParams{OrderID: order.ID, LeaseVersion: oldLease})
	require.Error(t, err)
	f.requireNoRights(t, order.ID)
	result, err := f.paidRepo.FulfillPaidTerm(context.Background(), domain.SubscriptionQuotaPaidTermParams{OrderID: order.ID, LeaseVersion: newLease})
	require.NoError(t, err)
	require.False(t, result.Replayed)
	_, err = f.paidRepo.FulfillPaidTerm(context.Background(), domain.SubscriptionQuotaPaidTermParams{OrderID: order.ID, LeaseVersion: oldLease})
	require.Error(t, err, "even an already-issued order cannot authorize a stale lease")
	_, err = f.db.Exec(`UPDATE payment_orders SET updated_at=clock_timestamp()-interval '10 minutes' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	require.NoError(t, f.pay.RetryFulfillment(context.Background(), order.ID))
	require.Equal(t, result.Term.ID, f.requireCard(t, order, payment.OrderStatusCompleted))
}

func TestSubscriptionQuotaPaymentI5_UnissuedPaidRecoverySurvivesIssueDisable(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	f.cfg.SubscriptionQuota.IssueEnabled = false
	f.cfg.SubscriptionQuota.EnabledGroupIDs = nil
	f.cfg.Default.ClaudeSubscriptionConcurrency = 0
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	f.requireCard(t, order, payment.OrderStatusCompleted)
}

func TestSubscriptionQuotaPaymentI5_ConcurrentLeaseHandoffCannotCompleteNewOwner(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	paused := &quotaPaymentI5PauseAfterRights{delegate: f.paidRepo, paused: [2]chan struct{}{make(chan struct{}), make(chan struct{})}, release: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	releaseOld := sync.OnceFunc(func() { close(paused.release[0]) })
	releaseNew := sync.OnceFunc(func() { close(paused.release[1]) })
	defer releaseOld()
	defer releaseNew()
	f.pay.SetSubscriptionQuotaPayment(paused, f.cfg)
	oldResult, newResult := make(chan int, 1), make(chan int, 1)
	go func() { oldResult <- f.callback(order, nil).Code }()
	select {
	case <-paused.paused[0]:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not commit rights")
	}
	f.requireCard(t, order, payment.OrderStatusRecharging)
	// Model the five-minute lease expiring without sleeping in the test. Both
	// workers otherwise execute the real callback, lease and completion code.
	_, err := f.db.Exec(`UPDATE payment_orders SET updated_at=clock_timestamp()-interval '10 minutes' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	go func() { newResult <- f.callback(order, nil).Code }()
	select {
	case <-paused.paused[1]:
	case <-time.After(5 * time.Second):
		t.Fatal("new worker did not acquire expired lease and replay rights")
	}
	var version time.Time
	require.NoError(t, f.db.QueryRow(`SELECT updated_at FROM payment_orders WHERE id=$1`, order.ID).Scan(&version))
	releaseOld()
	select {
	case code := <-oldResult:
		require.Equal(t, http.StatusInternalServerError, code)
	case <-time.After(5 * time.Second):
		t.Fatal("old worker did not return after losing its lease")
	}
	stored, err := f.client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderStatusRecharging, stored.Status)
	require.True(t, version.Equal(stored.UpdatedAt))
	require.Nil(t, stored.CompletedAt)
	require.Nil(t, stored.FailedAt)
	releaseNew()
	select {
	case code := <-newResult:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("new worker did not finish")
	}
	f.requireCard(t, order, payment.OrderStatusCompleted)
}

func TestSubscriptionQuotaPaymentI5_LegacyAndMalformedSnapshotsFailClosed(t *testing.T) {
	for _, scenario := range []string{"missing", "future_version", "wrong_group", "numeric_quota", "missing_quota"} {
		t.Run(scenario, func(t *testing.T) {
			f := newQuotaPaymentI5Fixture(t)
			guard := f.moneyGuard(t)
			template := f.create(t, f.plan)
			snapshot := template.SubscriptionQuotaSnapshot
			switch scenario {
			case "missing":
				snapshot = nil
			case "future_version":
				snapshot["schema_version"] = 999
			case "wrong_group":
				snapshot["group_id"] = f.otherGroup
			case "numeric_quota":
				snapshot["quota_usd"] = 125
			case "missing_quota":
				delete(snapshot, "quota_usd")
			}
			// Seed historical/imported corruption at INSERT. The production
			// immutable-snapshot trigger must stay enabled throughout the test.
			builder := f.client.PaymentOrder.Create().SetUserID(template.UserID).SetUserEmail(template.UserEmail).SetUserName(template.UserName).SetAmount(template.Amount).SetPayAmount(template.PayAmount).SetRechargeCode("I5-review-" + scenario).SetOutTradeNo(template.OutTradeNo + "-review").SetPaymentType(template.PaymentType).SetPaymentTradeNo("").SetOrderType(template.OrderType).SetNillablePlanID(template.PlanID).SetNillableSubscriptionGroupID(template.SubscriptionGroupID).SetNillableSubscriptionDays(template.SubscriptionDays).SetNillableProviderInstanceID(template.ProviderInstanceID).SetNillableProviderKey(template.ProviderKey).SetProviderSnapshot(template.ProviderSnapshot).SetStatus(payment.OrderStatusPending).SetExpiresAt(template.ExpiresAt).SetClientIP("127.0.0.1").SetSrcHost("synthetic.invalid")
			if snapshot != nil {
				builder.SetSubscriptionQuotaSnapshot(snapshot)
			}
			order, err := builder.Save(context.Background())
			require.NoError(t, err)
			require.Equal(t, http.StatusInternalServerError, f.callback(order, nil).Code)
			f.requireNoRights(t, order.ID)
			stored, err := f.client.PaymentOrder.Get(context.Background(), order.ID)
			require.NoError(t, err)
			require.Equal(t, payment.OrderStatusFailed, stored.Status)
			require.NotNil(t, stored.PaidAt, "paid evidence must survive review failure")
			require.NotEmpty(t, stored.FailedReason)
			guard()
		})
	}
}

func TestSubscriptionQuotaPaymentI5_DatabaseRejectsEntitlementRewrite(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	for _, assignment := range []string{
		`subscription_quota_snapshot=NULL`,
		`subscription_quota_snapshot=jsonb_set(subscription_quota_snapshot,'{quota_usd}','"1.00000000"')`,
		`subscription_days=8`,
		`order_type='balance'`,
		`plan_id=NULL`,
		fmt.Sprintf(`subscription_group_id=%d`, f.otherGroup),
		fmt.Sprintf(`user_id=%d`, f.userB),
	} {
		_, err := f.db.Exec(`UPDATE payment_orders SET `+assignment+` WHERE id=$1`, order.ID)
		require.ErrorContains(t, err, "entitlement is immutable")
	}
	stored, err := f.client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, order.SubscriptionQuotaSnapshot, stored.SubscriptionQuotaSnapshot)
	require.Equal(t, order.UserID, stored.UserID)
	f.requireNoRights(t, order.ID)
}

func TestSubscriptionQuotaPaymentI5_FullPartialRefundsCannotAlterRightsOrWallet(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	f.requireCard(t, order, payment.OrderStatusCompleted)
	guard := f.moneyGuard(t)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, subscriptionQuotaLegacyColumns(t, f.db))
	for _, amount := range []float64{0, order.Amount / 2} {
		for _, deduct := range []bool{true, false} {
			_, _, err := f.pay.PrepareRefund(context.Background(), order.ID, amount, "synthetic I5 refund guard", true, deduct)
			require.ErrorIs(t, err, service.ErrLegacySubscriptionOperationUnsupported)
		}
	}
	_, err := f.pay.ExecuteRefund(context.Background(), &service.RefundPlan{OrderID: order.ID, Order: order, RefundAmount: order.Amount, GatewayAmount: order.PayAmount, Force: true})
	require.ErrorIs(t, err, service.ErrLegacySubscriptionOperationUnsupported)
	_, err = f.pay.QueryAndFinalizeRefund(context.Background(), order.ID)
	require.ErrorIs(t, err, service.ErrLegacySubscriptionOperationUnsupported)
	require.Zero(t, f.refunds.Load(), "reject before provider refund invocation")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, subscriptionQuotaLegacyColumns(t, f.db)))
	guard()
}

func TestSubscriptionQuotaPaymentI5_ProviderAmountMismatchCannotIssue(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	require.Equal(t, http.StatusInternalServerError, f.callback(order, func(v url.Values) { v.Set("money", "0.01") }).Code)
	f.requireNoRights(t, order.ID)
	stored, err := f.client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderStatusPending, stored.Status)
	require.Nil(t, stored.PaidAt)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	f.requireCard(t, order, payment.OrderStatusCompleted)
}

func TestSubscriptionQuotaPaymentI5_TamperedSignatureCannotMarkPaid(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	values := url.Values{"pid": {"i5-synthetic-merchant"}, "out_trade_no": {order.OutTradeNo}, "trade_no": {"fake-" + order.OutTradeNo}, "trade_status": {"TRADE_SUCCESS"}, "money": {"12.34"}, "sign": {"tampered-synthetic-signature"}, "sign_type": {"MD5"}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/easypay", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
	f.requireNoRights(t, order.ID)
	stored, err := f.client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderStatusPending, stored.Status)
	require.Nil(t, stored.PaidAt)
}

func TestSubscriptionQuotaPaymentI5_LegacyWindowsPurchaseKeepsNativePath(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	plan, err := f.client.SubscriptionPlan.Create().SetGroupID(f.nativeGroup).SetName("I5 legacy windows product").SetPrice(9.87).SetValidityDays(7).SetValidityUnit("day").SetForSale(true).Save(context.Background())
	require.NoError(t, err)
	var expiry time.Time
	require.NoError(t, f.db.QueryRow(`SELECT expires_at FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, f.userA, f.nativeGroup).Scan(&expiry))
	order := f.create(t, plan.ID)
	require.Nil(t, order.SubscriptionQuotaSnapshot)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	var after time.Time
	require.NoError(t, f.db.QueryRow(`SELECT expires_at FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, f.userA, f.nativeGroup).Scan(&after))
	require.Equal(t, expiry.Add(7*24*time.Hour), after)
	var terms, audit int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms`).Scan(&terms))
	require.Zero(t, terms)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='SUBSCRIPTION_ASSIGNED'`, order.ID).Scan(&audit))
	require.Equal(t, 1, audit)
}

func TestSubscriptionQuotaPaymentI5_LegacyMutationsCannotChangePaidCard(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	require.Equal(t, http.StatusOK, f.callback(order, nil).Code)
	termID := f.requireCard(t, order, payment.OrderStatusCompleted)
	var parentID int64
	require.NoError(t, f.db.QueryRow(`SELECT subscription_id FROM subscription_quota_terms WHERE id=$1`, termID).Scan(&parentID))
	ctx := context.Background()
	parent, err := NewUserSubscriptionRepository(f.client).GetByID(ctx, parentID)
	require.NoError(t, err)
	codes := NewRedeemCodeRepository(f.client)
	for _, code := range []service.RedeemCode{
		{Code: "I5-legacy-extend", Type: service.RedeemTypeSubscription, Value: 1, Status: service.StatusUnused, GroupID: &f.group, ValidityDays: 7},
		{Code: "I5-legacy-reduce", Type: service.RedeemTypeSubscription, Value: 1, Status: service.StatusUnused, GroupID: &f.group, ValidityDays: -7},
	} {
		require.NoError(t, codes.Create(ctx, &code))
	}
	redeem := service.NewRedeemService(codes, NewUserRepository(f.client, f.db), f.subs, nil, nil, f.client, nil, nil)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, subscriptionQuotaLegacyColumns(t, f.db))
	for name, action := range map[string]func() error{
		"assign": func() error {
			_, _, err := f.subs.AssignOrExtendSubscription(ctx, &service.AssignSubscriptionInput{UserID: f.userA, GroupID: f.group, ValidityDays: 7})
			return err
		},
		"extend":      func() error { _, err := f.subs.ExtendSubscription(ctx, parentID, 7); return err },
		"reset":       func() error { _, err := f.subs.AdminResetQuota(ctx, parentID, true, true, true); return err },
		"revoke":      func() error { return f.subs.RevokeSubscription(ctx, parentID) },
		"restore":     func() error { _, err := f.subs.RestoreSubscription(ctx, parentID); return err },
		"activate":    func() error { return f.subs.CheckAndActivateWindow(ctx, parent) },
		"maintenance": func() error { return f.subs.CheckAndResetWindows(ctx, parent) },
		"redeem extend": func() error {
			_, err := redeem.Redeem(ctx, f.userA, "I5-legacy-extend")
			return err
		},
		"redeem reduce": func() error {
			_, err := redeem.RedeemForAdminFulfillment(ctx, f.userA, "I5-legacy-reduce")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, action(), service.ErrLegacySubscriptionOperationUnsupported) })
	}
	bulk, err := f.subs.BulkAssignSubscription(ctx, &service.BulkAssignSubscriptionInput{UserIDs: []int64{f.userA, f.userB}, GroupID: f.group, ValidityDays: 7, AssignedBy: f.admin})
	require.NoError(t, err)
	require.Zero(t, bulk.SuccessCount)
	require.Equal(t, 2, bulk.FailedCount)
	for _, failure := range bulk.Errors {
		require.Contains(t, failure, "LEGACY_SUBSCRIPTION_OPERATION_UNSUPPORTED")
	}
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, subscriptionQuotaLegacyColumns(t, f.db)))
}

func TestSubscriptionQuotaPaymentI5_RefundStateBlocksPaidRepository(t *testing.T) {
	f := newQuotaPaymentI5Fixture(t)
	order := f.create(t, f.plan)
	for _, status := range []string{payment.OrderStatusRefunded, payment.OrderStatusPartiallyRefunded, payment.OrderStatusRefundPending} {
		var lease time.Time
		require.NoError(t, f.db.QueryRow(`UPDATE payment_orders SET status=$2,paid_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at`, order.ID, status).Scan(&lease))
		_, err := f.paidRepo.FulfillPaidTerm(context.Background(), domain.SubscriptionQuotaPaidTermParams{OrderID: order.ID, LeaseVersion: lease})
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaPaymentLease)
		f.requireNoRights(t, order.ID)
	}
}

func TestSubscriptionQuotaPaymentI5_ManualExplicitOverrideWithoutPlanDefault(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	_, err := f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=NULL,for_sale=false WHERE id=$1`, f.plan)
	require.NoError(t, err)
	in := f.input("open", "100", 0)
	in.PlanID = &f.plan
	opened := f.open(t, f.userA, in)
	require.Equal(t, "100.00000000", opened.Term.QuotaUSD)
	var source map[string]any
	var raw []byte
	require.NoError(t, f.db.QueryRow(`SELECT source_snapshot FROM subscription_quota_terms WHERE id=$1`, opened.Term.ID).Scan(&raw))
	require.NoError(t, json.Unmarshal(raw, &source))
	require.Equal(t, "admin_override", source["quota_source"])
	require.NotContains(t, source, "plan_default_quota_usd")
	in.QuotaUSD = nil
	_, err = f.api.OpenTerm(context.Background(), f.userB, f.admin, in, "I5-missing-plan-default")
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRequired)
}

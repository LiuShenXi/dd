//go:build integration

package repository

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// This proof is intentionally opt-in after the final images are frozen. It
// replaces actual server containers on one disposable PostgreSQL database;
// executing repository helpers alone is not reported as an image upgrade.
func TestSubscriptionQuotaI6ImageUpgradeAndRollback(t *testing.T) {
	r1, r2, source, provider := os.Getenv("CWQ_I6_R1_IMAGE"), os.Getenv("CWQ_I6_R2_IMAGE"), os.Getenv("CWQ_I6_SOURCE_SHA256"), os.Getenv("CWQ_I6_PROVIDER_IMAGE")
	if r1 == "" && r2 == "" && source == "" && provider == "" {
		t.Skip("explicit final I6 local-image proof only; a skip is not acceptance")
	}
	require.NotEmpty(t, r1)
	require.NotEmpty(t, r2)
	require.NotEmpty(t, provider)
	require.Len(t, source, 64)
	_, err := hex.DecodeString(source)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	e := &quotaI6ImageProof{ctx: ctx, db: newUpgrade026Database(t), jwt: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", ""), loginPassword: "Synthetic-I6-" + uuid.NewString()}
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	e.password, _ = parsed.User.Password()
	e.databaseUser = parsed.User.Username()
	require.NoError(t, e.db.QueryRow(`SELECT current_database()`).Scan(&e.database))
	for _, im := range []struct{ image, stage string }{{r1, "I6-compatible-R1"}, {r2, "I6-isolated-candidate"}} {
		var labels map[string]string
		require.NoError(t, json.Unmarshal([]byte(e.must(t, "image", "inspect", "--format", "{{json .Config.Labels}}", im.image)), &labels))
		require.Equal(t, source, labels["com.monas.source-sha256"])
		require.Equal(t, im.stage, labels["com.monas.claude-quota-stage"])
	}
	// R0 is a pre-existing, separately verified local image. Never pull a tag or
	// allow an image name supplied through a production deployment variable.
	const r0 = "sub2api:2.8.13-monas-bps.13"
	identities := map[string]string{}
	for _, im := range []string{r0, r1, r2, provider} {
		identities[im] = e.must(t, "image", "inspect", "--format", "{{.Id}}", im)
	}
	provenanceJSON, err := os.ReadFile(filepath.Join("..", "..", "..", "validation", "claude-weekly-quota", "office-r0-provenance.json"))
	require.NoError(t, err)
	var provenance struct {
		Verified bool   `json:"provenance_verified"`
		ImageID  string `json:"local_image_id"`
	}
	require.NoError(t, json.Unmarshal(provenanceJSON, &provenance))
	require.True(t, provenance.Verified)
	require.Equal(t, provenance.ImageID, identities[r0], "the R0 tag must still refer to the independently verified production-baseline image")
	require.Equal(t, e.must(t, "image", "inspect", "--format", "{{json .RootFS.Layers}}", r2), e.must(t, "image", "inspect", "--format", "{{json .RootFS.Layers}}", r1), "compatible R1 and R2 must contain the same final binary/filesystem")
	pg := e.harnessContainer(t, parsed.Port(), postgresImageTag)
	_, redisPort, err := net.SplitHostPort(integrationRedis.Options().Addr)
	require.NoError(t, err)
	redis := e.harnessContainer(t, redisPort, redisImageTag)
	e.name = "cwq-i6-proof-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	e.network = e.name + "-internal"
	// This task-specific /28 is distinct from the retained I4 and I3 proof
	// networks. Docker rejects overlaps; no existing network is modified.
	e.must(t, "network", "create", "--internal", "--subnet", "10.247.192.0/28", "--label", "com.codex.local-task=claude-weekly-quota", e.network)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, args := range [][]string{{"rm", "--force", e.name}, {"rm", "--force", e.name + "-provider"}, {"network", "disconnect", e.network, pg}, {"network", "disconnect", e.network, redis}, {"network", "rm", e.network}} {
			_, _ = subscriptionQuotaDockerCommand(cleanup, args...).CombinedOutput()
		}
	})
	e.must(t, "network", "connect", "--alias", "cwq-i6-pg", e.network, pg)
	e.must(t, "network", "connect", "--alias", "cwq-i6-redis", e.network, redis)
	require.Equal(t, "true", e.must(t, "network", "inspect", "--format", "{{.Internal}}", e.network))
	e.must(t, "run", "--detach", "--pull", "never", "--name", e.name+"-provider", "--network", e.network, "--network-alias", "cwq-i6-provider", "--label", "com.codex.local-task=claude-weekly-quota", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--env", "CWQ_SYNTHETIC_PAYMENT_KEY="+quotaPaymentI5Key, provider)
	require.NoError(t, applyMigrationsFS(ctx, e.db, subscriptionQuotaBaselineFS(t)))
	r := e.seedLegacy(t)
	legacyColumns := map[string][]string{}
	for table, columns := range subscriptionQuotaLegacyColumns(t, e.db) {
		switch table {
		case "users", "groups", "user_subscriptions", "subscription_plans", "payment_orders", "carpool_terms", "carpool_cycles", "carpool_ledger", "carpool_billing_requests", "usage_billing_dedup", "usage_logs":
			legacyColumns[table] = columns
		}
	}
	before := subscriptionQuotaLegacyFingerprints(t, e.db, legacyColumns)
	e.start(t, r0, false, false, 0)
	require.Len(t, readUpgrade026MigrationRecords(t, e.db), 297, "R0 must boot against the exact old schema")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, e.db, legacyColumns), "R0 startup must preserve synthetic financial rows")
	e.stop(t)
	e.start(t, r1, false, true, 0)
	requireSubscriptionQuotaSchema(t, e.db)
	require.Greater(t, len(readUpgrade026MigrationRecords(t, e.db)), 297, "R1 normal startup must apply new migrations itself")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, e.db, legacyColumns), "R1 automatic migration must preserve all selected old columns and exact money")
	migrationsAfterR1 := readUpgrade026MigrationRecords(t, e.db)
	e.stop(t)
	f := e.seedCandidate(t, r)
	e.start(t, r2, true, true, r.group)
	// Login and order creation cross the actual frozen server's normal auth,
	// payment handler and provider client. SQL only seeds synthetic identities.
	status, body := e.http(t, "POST", "/api/v1/auth/login", "application/json", fmt.Sprintf(`{"email":"i6-b@example.invalid","password":%q}`, e.loginPassword), "")
	if status != http.StatusOK {
		t.Logf("isolated login diagnostics: %s", e.redact([]byte(e.must(t, "logs", "--tail", "100", e.container))))
	}
	require.Equal(t, http.StatusOK, status, e.redact(body))
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &login))
	require.NotEmpty(t, login.Data.AccessToken)
	e.token = login.Data.AccessToken
	status, body = e.http(t, "POST", "/api/v1/payment/orders", "application/json", fmt.Sprintf(`{"order_type":"subscription","payment_type":"alipay","plan_id":%d}`, r.plan), e.token)
	require.Equal(t, http.StatusOK, status, e.redact(body))
	var created struct {
		Data service.CreateOrderResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Positive(t, created.Data.OrderID)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, e.db)))
	order, err := client.PaymentOrder.Get(ctx, created.Data.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderStatusPending, order.Status)
	require.NotEmpty(t, order.SubscriptionQuotaSnapshot)
	var providerState struct {
		VerifiedCreates int `json:"verified_creates"`
	}
	require.NoError(t, json.Unmarshal([]byte(e.must(t, "exec", e.container, "wget", "-q", "-T", "3", "-O", "-", "http://cwq-i6-provider:8081/__payment_state")), &providerState))
	require.Equal(t, 1, providerState.VerifiedCreates, "the isolated fake provider must verify the real outbound signature")
	var frozen string
	require.NoError(t, e.db.QueryRow(`SELECT subscription_quota_snapshot::text FROM payment_orders WHERE id=$1`, order.ID).Scan(&frozen))
	_, err = e.db.Exec(`UPDATE payment_orders SET subscription_quota_snapshot=jsonb_set(subscription_quota_snapshot,'{quota_usd}','"999"') WHERE id=$1`, order.ID)
	require.Error(t, err, "the frozen purchased entitlement must be immutable")
	_, err = e.db.Exec(`UPDATE subscription_plans SET term_quota_usd=999,validity_days=30,for_sale=false WHERE id=$1`, r.plan)
	require.NoError(t, err)
	_, err = e.db.Exec(`CREATE FUNCTION i6_fail_assignment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='SUBSCRIPTION_ASSIGNED' THEN RAISE EXCEPTION 'I6 synthetic assignment fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER i6_fail_assignment BEFORE INSERT ON payment_audit_logs FOR EACH ROW EXECUTE FUNCTION i6_fail_assignment()`)
	require.NoError(t, err)
	callback := url.Values{"pid": {"i5-synthetic-merchant"}, "out_trade_no": {order.OutTradeNo}, "trade_no": {"fake-" + order.OutTradeNo}, "trade_status": {"TRADE_SUCCESS"}, "money": {strconv.FormatFloat(order.PayAmount, 'f', 2, 64)}, "sign_type": {"MD5"}}
	callback.Set("sign", quotaPaymentI5Sign(callback))
	status, _ = e.http(t, "POST", "/api/v1/payment/webhook/easypay", "application/x-www-form-urlencoded", callback.Encode(), "")
	require.Equal(t, http.StatusInternalServerError, status)
	var paid bool
	var orderStatus string
	require.NoError(t, e.db.QueryRow(`SELECT paid_at IS NOT NULL,status FROM payment_orders WHERE id=$1`, order.ID).Scan(&paid, &orderStatus))
	require.True(t, paid, "only the actual signed callback may persist payment")
	require.Equal(t, payment.OrderStatusFailed, orderStatus)
	var rights int
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms WHERE payment_order_id=$1`, order.ID).Scan(&rights))
	require.Zero(t, rights, "injected failure must roll back rights before rollback deployment")
	// Enabled admission requires recovery in the actual runtime configuration.
	// Stop R2 before persisting known receipts so only the replacement R1 can
	// consume them; disabling its recovery would bypass the startup contract.
	e.stop(t)
	gpt, gptCommand, snapshot, expectedGPT := prepareSubscriptionQuotaR1KnownReceipts(t, f, ctx)
	var known int
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM subscription_quota_requests WHERE id=$1 AND status='usage_known'`, snapshot.RequestRowID).Scan(&known))
	require.Equal(t, 1, known)
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM carpool_billing_requests WHERE request_id=$1 AND status='usage_known'`, gptCommand.RequestID).Scan(&known))
	require.Equal(t, 1, known)
	var usersBeforeRecovery string
	require.NoError(t, e.db.QueryRow(`SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&usersBeforeRecovery))
	nativeBeforeRecovery := r.nativeFingerprint(t)
	// Roll back binary/flags only. The current schema, receipts, paid state and
	// immutable order snapshot remain in exactly the same PostgreSQL database.
	_, err = e.db.Exec(`DROP TRIGGER i6_fail_assignment ON payment_audit_logs; DROP FUNCTION i6_fail_assignment()`)
	require.NoError(t, err)
	e.start(t, r1, false, true, r.group)
	require.Equal(t, migrationsAfterR1, readUpgrade026MigrationRecords(t, e.db), "rollback keeps the latest schema and original migration identities")
	status, body = e.http(t, "POST", "/api/v1/payment/webhook/easypay", "application/x-www-form-urlencoded", callback.Encode(), "")
	require.Equal(t, http.StatusOK, status, e.redact(body))
	require.Eventually(t, func() bool {
		var complete bool
		err := e.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM payment_orders WHERE id=$1 AND status='COMPLETED') AND EXISTS(SELECT 1 FROM subscription_quota_requests WHERE id=$2 AND status='settled' AND usage_log_recorded_at IS NOT NULL) AND EXISTS(SELECT 1 FROM carpool_billing_requests WHERE request_id=$3 AND status='settled')`, order.ID, snapshot.RequestRowID, gptCommand.RequestID).Scan(&complete)
		return err == nil && complete
	}, 45*time.Second, 250*time.Millisecond, "R1 must recover real paid order and both known receipt protocols with issue/admission disabled")
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	var remaining string
	require.NoError(t, e.db.QueryRow(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, gpt.cycle).Scan(&remaining))
	require.Equal(t, expectedGPT, remaining)
	var quota, sourceSnapshot string
	var duration float64
	require.NoError(t, e.db.QueryRow(`SELECT quota_usd::text,extract(epoch from expires_at-starts_at),source_snapshot::text FROM subscription_quota_terms WHERE payment_order_id=$1`, order.ID).Scan(&quota, &duration, &sourceSnapshot))
	require.Equal(t, "125.12345678", quota)
	require.Equal(t, float64(604800), duration)
	require.JSONEq(t, frozen, sourceSnapshot)
	var usersAfterRecovery string
	require.NoError(t, e.db.QueryRow(`SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&usersAfterRecovery))
	require.Equal(t, usersBeforeRecovery, usersAfterRecovery, "paid rights and both billing recoveries must preserve ordinary/frozen user money")
	require.Equal(t, nativeBeforeRecovery, r.nativeFingerprint(t), "recovery must preserve unrelated native windows and precision")
	for _, q := range []string{`SELECT count(*) FROM subscription_quota_terms WHERE payment_order_id=$1`, `SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='SUBSCRIPTION_ASSIGNED'`} {
		require.NoError(t, e.db.QueryRow(q, order.ID).Scan(&rights))
		require.Equal(t, 1, rights)
	}
	require.NoError(t, e.db.QueryRow(`SELECT key FROM api_keys WHERE id=$1`, f.keyA).Scan(&e.apiKey))
	status, body = e.http(t, "POST", "/v1/messages", "application/json", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"synthetic-no-upstream"}],"max_tokens":1}`, e.apiKey)
	require.Equal(t, http.StatusServiceUnavailable, status, e.redact(body))
	require.Contains(t, string(body), "TERM_BILLING_UNAVAILABLE")
	protected := map[string][]string{}
	for table, columns := range subscriptionQuotaLegacyColumns(t, e.db) {
		if table == "users" || table == "user_subscriptions" || table == "payment_orders" || table == "payment_audit_logs" || strings.HasPrefix(table, "subscription_quota_") && table != "subscription_quota_runtime_replicas" || table == "carpool_cycles" || table == "carpool_ledger" || table == "carpool_billing_requests" || table == "usage_billing_dedup" || table == "usage_logs" {
			protected[table] = columns
		}
	}
	stable := subscriptionQuotaLegacyFingerprints(t, e.db, protected)
	e.stop(t)
	e.start(t, r1, false, true, r.group)
	// Give both real background loops several opportunities to replay receipts.
	time.Sleep(2200 * time.Millisecond)
	require.Equal(t, stable, subscriptionQuotaLegacyFingerprints(t, e.db, protected), "restart must not debit twice, duplicate paid rights/audit or change balances/native windows")
	var frozenAfter string
	require.NoError(t, e.db.QueryRow(`SELECT subscription_quota_snapshot::text FROM payment_orders WHERE id=$1`, order.ID).Scan(&frozenAfter))
	require.Equal(t, frozen, frozenAfter)
	t.Logf("I6 actual R0→R1 automatic migration→R2 enabled→R1 disabled rollback PASS; image_ids=%v source=%s; same synthetic PG18 retained; signed HTTP callback persisted paid then assignment failed; R1 issued original quota/duration once; Claude/GPT known recovered once; restart stable; no host ports or external egress", identities, source)
}

type quotaI6ImageProof struct {
	ctx                                                                                           context.Context
	db                                                                                            *sql.DB
	name, network, container, database, databaseUser, password, jwt, loginPassword, token, apiKey string
}

func (e *quotaI6ImageProof) redact(raw []byte) string {
	s := string(raw)
	for _, value := range []string{e.password, e.jwt, e.loginPassword, e.token, e.apiKey, quotaPaymentI5Key, quotaPaymentI5EncryptionKey} {
		if value != "" {
			s = strings.ReplaceAll(s, value, "[synthetic-secret-redacted]")
		}
	}
	return s
}

func (e *quotaI6ImageProof) must(t *testing.T, args ...string) string {
	t.Helper()
	out, err := subscriptionQuotaDockerOutput(e.ctx, nil, args...)
	require.NoError(t, err, "isolated Docker %s: %s", args[0], e.redact(out))
	return strings.TrimSpace(string(out))
}

func (e *quotaI6ImageProof) harnessContainer(t *testing.T, port, image string) string {
	t.Helper()
	var matches []string
	for _, id := range strings.Fields(e.must(t, "ps", "--filter", "label=org.testcontainers=true", "--format", "{{.ID}}")) {
		var ports map[string][]struct{ HostPort string }
		require.NoError(t, json.Unmarshal([]byte(e.must(t, "inspect", "--format", "{{json .NetworkSettings.Ports}}", id)), &ports))
		matched := false
		for _, bindings := range ports {
			for _, binding := range bindings {
				if binding.HostPort == port {
					matched = true
				}
			}
		}
		if matched {
			matches = append(matches, id)
		}
	}
	require.Len(t, matches, 1)
	require.Equal(t, image, e.must(t, "inspect", "--format", "{{.Config.Image}}", matches[0]))
	return matches[0]
}

func (e *quotaI6ImageProof) start(t *testing.T, image string, enabled, recovery bool, group int64) {
	t.Helper()
	env := []string{"SKIP_SETUP=true", "RUN_MODE=standard", "SERVER_HOST=0.0.0.0", "SERVER_PORT=8080", "SERVER_MODE=release", "DATABASE_HOST=cwq-i6-pg", "DATABASE_PORT=5432", "DATABASE_USER=" + e.databaseUser, "DATABASE_PASSWORD=" + e.password, "DATABASE_DBNAME=" + e.database, "DATABASE_SSLMODE=disable", "REDIS_HOST=cwq-i6-redis", "REDIS_PORT=6379", "REDIS_DB=12", "JWT_SECRET=" + e.jwt, "TOTP_ENCRYPTION_KEY=" + hex.EncodeToString([]byte(quotaPaymentI5EncryptionKey)), "DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=3", "SUBSCRIPTION_QUOTA_ISSUE_ENABLED=" + strconv.FormatBool(enabled), "SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=" + strconv.FormatBool(enabled), "SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=" + strconv.FormatBool(recovery), "SUBSCRIPTION_QUOTA_RECOVERY_INTERVAL=1s", "SUBSCRIPTION_QUOTA_RECOVERY_ITEM_TIMEOUT=5s", "SUBSCRIPTION_QUOTA_REQUEST_STALE_GRACE=1h", "SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID=i6-proof", "SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS=i6-proof", "CARPOOL_BACKGROUND_ENABLED=" + strconv.FormatBool(recovery), "DASHBOARD_AGG_ENABLED=false", "PRICING_DATA_DIR=/app/data/pricing", "SECURITY_URL_ALLOWLIST_ENABLED=false", "SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=true", "SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP=true", "TZ=UTC"}
	if group > 0 {
		env = append(env, "SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS="+strconv.FormatInt(group, 10))
	}
	args := []string{"run", "--detach", "--pull", "never", "--name", e.name, "--network", e.network, "--label", "com.codex.local-task=claude-weekly-quota", "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--cpus", "2", "--memory", "1536m", "--pids-limit", "512", "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000"}
	for _, value := range env {
		args = append(args, "--env", value)
	}
	args = append(args, image)
	e.container = e.must(t, args...)
	healthy := false
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); {
		if _, err := subscriptionQuotaDockerCommand(e.ctx, "exec", e.container, "wget", "-q", "-T", "2", "-O", "/dev/null", "http://127.0.0.1:8080/health").CombinedOutput(); err == nil {
			healthy = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !healthy {
		t.Logf("isolated image diagnostics: %s", e.redact([]byte(e.must(t, "logs", "--tail", "80", e.container))))
	}
	require.True(t, healthy, "actual image must boot its normal HTTP server")
	require.Equal(t, "{}", e.must(t, "inspect", "--format", "{{json .HostConfig.PortBindings}}", e.container))
}

func (e *quotaI6ImageProof) stop(t *testing.T) {
	t.Helper()
	e.must(t, "rm", "--force", e.container)
	e.container = ""
}

func (e *quotaI6ImageProof) http(t *testing.T, method, path, contentType, body, bearer string) (int, []byte) {
	t.Helper()
	request := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Type: %s\r\nContent-Length: %d\r\n", method, path, contentType, len(body))
	if bearer != "" {
		request += "Authorization: Bearer " + bearer + "\r\n"
	}
	request += "\r\n" + body
	command := subscriptionQuotaDockerCommand(e.ctx, "exec", "--interactive", e.container, "nc", "-w", "20", "127.0.0.1", "8080")
	// BusyBox nc closes its network connection on stdin EOF. Keep stdin open
	// through the full response so authentication/payment do not see a cancelled
	// HTTP request as soon as the synthetic request bytes have been written.
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	var diagnostics strings.Builder
	command.Stderr = &diagnostics
	require.NoError(t, command.Start())
	defer command.Process.Kill()
	_, err = io.WriteString(input, request)
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(output), &http.Request{Method: method})
	require.NoError(t, err, "parse actual HTTP %s", path)
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	require.NoError(t, err)
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait(), "isolated actual HTTP %s: %s", path, e.redact([]byte(diagnostics.String())))
	return response.StatusCode, payload
}

func (e *quotaI6ImageProof) seedLegacy(t *testing.T) *subscriptionQuotaDomainFixture {
	t.Helper()
	r := &subscriptionQuotaDomainFixture{db: e.db}
	hash, err := bcrypt.GenerateFromPassword([]byte(e.loginPassword), bcrypt.MinCost)
	require.NoError(t, err)
	for _, u := range []struct {
		email, role, balance string
		id                   *int64
	}{{"i6-admin@example.invalid", "admin", "99.12345678", &r.admin}, {"i6-a@example.invalid", "user", "0", &r.userA}, {"i6-b@example.invalid", "user", "77.13000000", &r.userB}} {
		require.NoError(t, e.db.QueryRow(`INSERT INTO users(email,password_hash,role,balance) VALUES($1,$2,$3,$4::numeric) RETURNING id`, u.email, string(hash), u.role, u.balance).Scan(u.id))
	}
	require.NoError(t, e.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic I6 native guard','anthropic','subscription') RETURNING id`).Scan(&r.nativeGroup))
	_, err = e.db.Exec(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_window_start,weekly_window_start,monthly_window_start,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES($1,$2,clock_timestamp()-INTERVAL '1 day',clock_timestamp()+INTERVAL '1 year',clock_timestamp(),clock_timestamp(),clock_timestamp(),1.1234567890,7.8765432109,19.9999999999)`, r.userA, r.nativeGroup)
	require.NoError(t, err)
	return r
}

func (e *quotaI6ImageProof) seedCandidate(t *testing.T, r *subscriptionQuotaDomainFixture) *subscriptionQuotaBillingFixture {
	t.Helper()
	require.NoError(t, e.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES('Synthetic I6 Claude','anthropic','subscription','term') RETURNING id`).Scan(&r.group))
	require.NoError(t, e.db.QueryRow(`INSERT INTO subscription_plans(group_id,name,product_name,price,term_quota_usd,validity_days,validity_unit,for_sale) VALUES($1,'I6 frozen plan','I6 frozen product',12.34,125.12345678,7,'day',true) RETURNING id`, r.group).Scan(&r.plan))
	r.cfg = &config.Config{RunMode: config.RunModeStandard, Default: config.DefaultConfig{ClaudeSubscriptionConcurrency: 3}, SubscriptionQuota: config.SubscriptionQuotaConfig{IssueEnabled: true, EnabledGroupIDs: []int64{r.group}}}
	r.repo = NewSubscriptionQuotaRepository(e.db)
	r.api = service.NewSubscriptionQuotaService(r.repo, r.cfg)
	f := &subscriptionQuotaBillingFixture{rights: r, db: e.db, port: r.repo.(service.SubscriptionQuotaBillingRepository)}
	for _, key := range []struct {
		user int64
		id   *int64
	}{{r.userA, &f.keyA}, {r.userA, &f.keyA2}, {r.userB, &f.keyB}} {
		require.NoError(t, e.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES($1,$2,$3,'Synthetic I6 key',1000000,1000000,1000000,1000000) RETURNING id`, key.user, r.group, "sk-cwq-i6-"+uuid.NewString()).Scan(key.id))
	}
	require.NoError(t, e.db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status) VALUES('Synthetic I6 no upstream','anthropic','apikey','{}','{"quota_limit":1000000,"quota_used":0,"quota_daily_limit":1000000,"quota_daily_used":0,"quota_weekly_limit":1000000,"quota_weekly_used":0}','active') RETURNING id`).Scan(&f.account))
	_, err := e.db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, f.account, r.group)
	require.NoError(t, err)
	providerConfig, err := json.Marshal(map[string]string{"pid": "i5-synthetic-merchant", "pkey": quotaPaymentI5Key, "apiBase": "http://cwq-i6-provider:8081", "notifyUrl": "http://localhost/api/v1/payment/webhook/easypay", "returnUrl": "http://localhost/synthetic-return", "currency": "CNY"})
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(providerConfig), []byte(quotaPaymentI5EncryptionKey))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, e.db)))
	_, err = client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).SetName("I6 synthetic gateway").SetConfig(encrypted).SetSupportedTypes(payment.TypeAlipay).SetEnabled(true).SetRefundEnabled(true).Save(e.ctx)
	require.NoError(t, err)
	require.NoError(t, NewSettingRepository(client).SetMultiple(e.ctx, map[string]string{service.SettingPaymentEnabled: "true", service.SettingEnabledPaymentTypes: "alipay", service.SettingMaxPendingOrders: "100", service.SettingPaymentVisibleMethodAlipayEnabled: "true", service.SettingPaymentVisibleMethodAlipaySource: service.VisibleMethodSourceEasyPayAlipay}))
	return f
}

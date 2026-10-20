//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This explicit post-build proof is deliberately outside the Billing_ inventory.
// It runs the actual frozen server image, not a test binary pretending to be R1.
// The original TestMain still owns both disposable database/cache containers.
func TestSubscriptionQuotaR1ImageRecovery(t *testing.T) {
	image := os.Getenv("CWQ_I3_R1_IMAGE")
	if image == "" {
		t.Skip("run only with an explicitly built local I3 receipt-compatible image")
	}
	source := os.Getenv("CWQ_I3_R1_SOURCE_SHA256")
	require.True(t, strings.HasPrefix(image, "sub2api:claude-weekly-i3-receipt-r1-"))
	require.Len(t, source, 64)
	f := newSubscriptionQuotaR1ImageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	docker := "C:/Program Files/Docker/Docker/resources/bin/docker.exe"
	if _, err := os.Stat(docker); err != nil {
		docker = "docker"
	}
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	var databaseName, claudeKey string
	require.NoError(t, f.db.QueryRow(`SELECT current_database()`).Scan(&databaseName))
	require.NoError(t, f.db.QueryRow(`SELECT key FROM api_keys WHERE id=$1`, f.keyA).Scan(&claudeKey))
	jwt := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	redact := func(raw []byte) string {
		value := string(raw)
		for _, secret := range []string{password, claudeKey, jwt} {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[synthetic-secret-redacted]")
			}
		}
		return value
	}
	run := func(arguments ...string) ([]byte, error) {
		args := append([]string{"--host", "npipe:////./pipe/docker_engine"}, arguments...)
		return exec.CommandContext(ctx, docker, args...).CombinedOutput()
	}
	must := func(action string, arguments ...string) string {
		t.Helper()
		output, err := run(arguments...)
		require.NoError(t, err, "isolated Docker operation %s: %s", action, redact(output))
		return strings.TrimSpace(string(output))
	}
	var labels map[string]string
	require.NoError(t, json.Unmarshal([]byte(must("inspect frozen image labels", "image", "inspect", "--format", "{{json .Config.Labels}}", image)), &labels))
	require.Equal(t, source, labels["com.monas.source-sha256"])
	require.Equal(t, "I3-current-receipt-R1", labels["com.monas.claude-quota-stage"])
	imageID := must("read immutable image identity", "image", "inspect", "--format", "{{.Id}}", image)
	findHarnessContainer := func(port, expectedImage string) string {
		t.Helper()
		ids := strings.Fields(must("list only synthetic TestMain containers", "ps", "--filter", "label=org.testcontainers=true", "--format", "{{.ID}}"))
		var matches []string
		for _, id := range ids {
			var ports map[string][]struct {
				HostPort string `json:"HostPort"`
			}
			require.NoError(t, json.Unmarshal([]byte(must("read synthetic published ports", "inspect", "--format", "{{json .NetworkSettings.Ports}}", id)), &ports))
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
		require.Len(t, matches, 1, "the container must be identified by this original TestMain's exact ephemeral port")
		require.Equal(t, expectedImage, must("verify original TestMain image", "inspect", "--format", "{{.Config.Image}}", matches[0]))
		return matches[0]
	}
	pg := findHarnessContainer(parsed.Port(), postgresImageTag)
	_, redisPort, err := net.SplitHostPort(integrationRedis.Options().Addr)
	require.NoError(t, err)
	redis := findHarnessContainer(redisPort, redisImageTag)
	name := "cwq-i3-r1-proof-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	network := name + "-internal"
	// The Docker default pools are already exhausted on this host. Read-only
	// inventory verified this adjacent task-only /28 is outside existing pools
	// and the retained task network 10.247.190.0/24. Docker rejects any collision.
	must("create egress-free task network", "network", "create", "--internal", "--subnet", "10.247.191.0/28", "--label", "com.codex.local-task=claude-weekly-quota", network)
	var container string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanup := func(arguments ...string) {
			_, _ = exec.CommandContext(cleanupCtx, docker, append([]string{"--host", "npipe:////./pipe/docker_engine"}, arguments...)...).CombinedOutput()
		}
		if container != "" {
			cleanup("rm", "--force", container)
		}
		cleanup("network", "disconnect", network, pg)
		cleanup("network", "disconnect", network, redis)
		cleanup("network", "rm", network)
	})
	must("attach only original synthetic PG", "network", "connect", "--alias", "cwq-proof-pg", network, pg)
	must("attach only original synthetic Redis", "network", "connect", "--alias", "cwq-proof-redis", network, redis)
	require.Equal(t, "true", must("verify network has no external egress", "network", "inspect", "--format", "{{.Internal}}", network))
	// Persist both protocols before the compatible image starts. The minimum
	// fixture contains no unrelated expired GPT terms or malformed old receipts.
	gpt, gptCommand, snapshot, expectedGPTBalance := prepareSubscriptionQuotaR1KnownReceipts(t, f, ctx)
	var usersBefore, parentsBefore string
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&usersBefore))
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM user_subscriptions p`).Scan(&parentsBefore))
	environment := []string{
		"SKIP_SETUP=true", "RUN_MODE=standard", "SERVER_HOST=0.0.0.0", "SERVER_PORT=8080", "SERVER_MODE=release",
		"DATABASE_HOST=cwq-proof-pg", "DATABASE_PORT=5432", "DATABASE_USER=" + parsed.User.Username(), "DATABASE_PASSWORD=" + password, "DATABASE_DBNAME=" + databaseName, "DATABASE_SSLMODE=disable",
		"REDIS_HOST=cwq-proof-redis", "REDIS_PORT=6379", "REDIS_DB=13", "JWT_SECRET=" + jwt,
		"SUBSCRIPTION_QUOTA_ISSUE_ENABLED=false", "SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=false", "SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=true", "SUBSCRIPTION_QUOTA_RECOVERY_INTERVAL=1s", "SUBSCRIPTION_QUOTA_RECOVERY_ITEM_TIMEOUT=5s", "SUBSCRIPTION_QUOTA_REQUEST_STALE_GRACE=1h",
		"DASHBOARD_AGG_ENABLED=false", "PRICING_DATA_DIR=/app/data/pricing", "CARPOOL_BACKGROUND_ENABLED=true", "TZ=UTC",
	}
	arguments := []string{"run", "--detach", "--pull", "never", "--name", name, "--label", "com.codex.local-task=claude-weekly-quota", "--network", network, "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--cpus", "2", "--memory", "1536m", "--pids-limit", "512", "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000"}
	for _, value := range environment {
		arguments = append(arguments, "--env", value)
	}
	arguments = append(arguments, image)
	container = must("start actual frozen R1 server", arguments...)
	t.Cleanup(func() {
		if t.Failed() {
			output, _ := run("logs", "--tail", "70", container)
			t.Logf("isolated R1 diagnostics (synthetic secrets redacted): %s", redact(output))
		}
	})
	waitHealthy := func() {
		t.Helper()
		require.Eventually(t, func() bool {
			_, err := run("exec", container, "wget", "-q", "-T", "2", "-O", "/dev/null", "http://127.0.0.1:8080/health")
			return err == nil
		}, 70*time.Second, 500*time.Millisecond, "actual image must start its normal server")
	}
	waitHealthy()
	require.Equal(t, "{}", must("verify no host ports", "inspect", "--format", "{{json .HostConfig.PortBindings}}", container))
	require.Eventually(t, func() bool {
		var settled bool
		err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM subscription_quota_requests WHERE id=$1 AND status='settled' AND usage_log_recorded_at IS NOT NULL) AND EXISTS(SELECT 1 FROM carpool_billing_requests WHERE request_id=$2 AND status='settled')`, snapshot.RequestRowID, gptCommand.RequestID).Scan(&settled)
		return err == nil && settled
	}, 40*time.Second, 250*time.Millisecond, "R1 must actually decode and recover both pre-existing known receipt protocols with issuance/admission disabled")
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	var gptRemaining string
	require.NoError(t, f.db.QueryRow(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, gpt.cycle).Scan(&gptRemaining))
	// The original GPT amount is read as a decimal string independently below.
	require.Equal(t, expectedGPTBalance, gptRemaining)
	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"synthetic-no-upstream"}],"max_tokens":1}`
	httpRequest := "POST /v1/messages HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nAuthorization: Bearer " + claudeKey + "\r\nContent-Type: application/json\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	httpCommand := exec.CommandContext(ctx, docker, "--host", "npipe:////./pipe/docker_engine", "exec", "--interactive", container, "nc", "-w", "3", "127.0.0.1", "8080")
	httpCommand.Stdin = strings.NewReader(httpRequest)
	response, httpErr := httpCommand.CombinedOutput()
	require.NoError(t, httpErr, "internal synthetic HTTP exchange: %s", redact(response))
	require.Contains(t, string(response), "503")
	require.Contains(t, string(response), "TERM_BILLING_UNAVAILABLE")
	var requests int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_requests`).Scan(&requests))
	require.Equal(t, 1, requests, "disabled generation must create no extra admission or upstream intent")
	protected := make(map[string][]string)
	for table, columns := range subscriptionQuotaLegacyColumns(t, f.db) {
		if table == "users" || table == "user_subscriptions" || strings.HasPrefix(table, "subscription_quota_") || table == "carpool_terms" || table == "carpool_cycles" || table == "carpool_ledger" || table == "carpool_billing_requests" || table == "usage_billing_dedup" || table == "usage_billing_dedup_archive" || table == "usage_logs" {
			protected[table] = columns
		}
	}
	beforeRestart := subscriptionQuotaLegacyFingerprints(t, f.db, protected)
	must("restart the same compatible server", "restart", "--time", "10", container)
	waitHealthy()
	require.Eventually(t, func() bool {
		var count int
		err := f.db.QueryRow(`SELECT count(*) FROM subscription_quota_requests WHERE id=$1 AND status='settled' AND usage_log_recorded_at IS NOT NULL`, snapshot.RequestRowID).Scan(&count)
		return err == nil && count == 1
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, beforeRestart, subscriptionQuotaLegacyFingerprints(t, f.db, protected), "restart recovery cannot produce a second debit, reset a terminal receipt, or republish acknowledged statistics")
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	var usersAfter, parentsAfter string
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&usersAfter))
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM user_subscriptions p`).Scan(&parentsAfter))
	require.Equal(t, usersBefore, usersAfter, "R1 receipt draining cannot transfer ordinary/frozen balances")
	require.Equal(t, parentsBefore, parentsAfter, "R1 receipt draining cannot alter native windows or parent schedules")
	t.Logf("actual local R1 image=%s image_id=%s frozen_source=%s container=%s internal_network=%s no_host_ports=true no_external_egress=true issue=false admission=false recovery=true; Claude cost0.50000000/account0.10000000 and GPT cost0.25000000 settled once; restart unchanged; real term HTTP503; I5 paid-order recovery NOT claimed", image, imageID, source, container, network)
}

func newSubscriptionQuotaR1ImageFixture(t *testing.T) *subscriptionQuotaBillingFixture {
	t.Helper()
	r := &subscriptionQuotaDomainFixture{db: newUpgrade026Database(t)}
	require.NoError(t, ApplyMigrations(context.Background(), r.db))
	for _, u := range []struct {
		email, role, balance string
		id                   *int64
	}{{"r1-admin@example.invalid", "admin", "99.12345678", &r.admin}, {"r1-a@example.invalid", "user", "0", &r.userA}, {"r1-b@example.invalid", "user", "77.13000000", &r.userB}} {
		require.NoError(t, r.db.QueryRow(`INSERT INTO users(email,password_hash,role,balance) VALUES($1,'synthetic-no-login',$2,$3::numeric) RETURNING id`, u.email, u.role, u.balance).Scan(u.id))
	}
	require.NoError(t, r.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES('Synthetic R1 Claude','anthropic','subscription','term') RETURNING id`).Scan(&r.group))
	require.NoError(t, r.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic R1 native guard','anthropic','subscription') RETURNING id`).Scan(&r.nativeGroup))
	_, err := r.db.Exec(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_window_start,weekly_window_start,monthly_window_start,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES($1,$2,clock_timestamp()-INTERVAL '1 day',clock_timestamp()+INTERVAL '1 year',clock_timestamp(),clock_timestamp(),clock_timestamp(),1.1234567890,7.8765432109,19.9999999999)`, r.userA, r.nativeGroup)
	require.NoError(t, err)
	r.cfg = &config.Config{RunMode: config.RunModeStandard, Default: config.DefaultConfig{ClaudeSubscriptionConcurrency: 3}, SubscriptionQuota: config.SubscriptionQuotaConfig{IssueEnabled: true, EnabledGroupIDs: []int64{r.group}}}
	r.repo = NewSubscriptionQuotaRepository(r.db)
	r.api = service.NewSubscriptionQuotaService(r.repo, r.cfg)
	f := &subscriptionQuotaBillingFixture{rights: r, db: r.db, port: r.repo.(service.SubscriptionQuotaBillingRepository)}
	for _, key := range []struct {
		user int64
		id   *int64
	}{{r.userA, &f.keyA}, {r.userA, &f.keyA2}, {r.userB, &f.keyB}} {
		require.NoError(t, r.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES($1,$2,$3,'Synthetic R1 identity',1000000,1000000,1000000,1000000) RETURNING id`, key.user, r.group, "sk-cwq-r1-"+uuid.NewString()).Scan(key.id))
	}
	require.NoError(t, r.db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status) VALUES('Synthetic R1 account no upstream','anthropic','apikey','{}','{"quota_limit":1000000,"quota_used":0,"quota_daily_limit":1000000,"quota_daily_used":0,"quota_weekly_limit":1000000,"quota_weekly_used":0}','active') RETURNING id`).Scan(&f.account))
	_, err = r.db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, f.account, r.group)
	require.NoError(t, err)
	return f
}

func prepareSubscriptionQuotaR1KnownReceipts(t *testing.T, f *subscriptionQuotaBillingFixture, ctx context.Context) (*subscriptionQuotaGPTBillingFixture, *service.UsageBillingCommand, *domain.SubscriptionQuotaBillingSnapshot, string) {
	t.Helper()
	var gptGroup, gptKey, plan int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic R1 GPT coexist','openai','standard') RETURNING id`).Scan(&gptGroup))
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES($1,$2,$3,'Synthetic R1 GPT identity',1000000,1000000,1000000,1000000) RETURNING id`, f.rights.userA, gptGroup, "sk-cwq-r1-gpt-"+uuid.NewString()).Scan(&gptKey))
	require.NoError(t, f.db.QueryRow(`SELECT id FROM carpool_plans WHERE code='four_seat' AND enabled=TRUE ORDER BY version DESC,id DESC LIMIT 1`).Scan(&plan))
	gptRepo := NewCarpoolRepository(f.db)
	gptTerm, err := service.NewCarpoolService(gptRepo).Open(ctx, f.rights.userA, f.rights.admin, service.CarpoolOpenInput{GroupID: gptGroup, PlanID: plan, Mode: "new"}, uuid.NewString())
	require.NoError(t, err)
	require.NotEmpty(t, gptTerm.Cycles)
	gpt := &subscriptionQuotaGPTBillingFixture{repo: gptRepo, group: gptGroup, key: gptKey, term: gptTerm.ID, cycle: gptTerm.Cycles[0].ID}
	gptCommand := f.knownGPT(t, gpt, "0.25000000")
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	f.persist(t, snapshot, "0.50000000", "0.10000000")
	return gpt, gptCommand, snapshot, gptTerm.PlanSnapshot.WeeklyQuotaUSD.Sub(gptCommand.CarpoolCost).StringFixed(8)
}

func TestSubscriptionQuotaBilling_R1MinimalFixturePersistsBothKnownProtocols(t *testing.T) {
	f := newSubscriptionQuotaR1ImageFixture(t)
	gpt, gptCommand, snapshot, _ := prepareSubscriptionQuotaR1KnownReceipts(t, f, context.Background())
	var gptStatus, base string
	require.NoError(t, f.db.QueryRow(`SELECT status FROM carpool_billing_requests WHERE request_id=$1`, gptCommand.RequestID).Scan(&gptStatus))
	require.Equal(t, "usage_known", gptStatus)
	require.NoError(t, f.db.QueryRow(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, gpt.cycle).Scan(&base))
	require.True(t, strings.HasSuffix(base, ".00000000"))
	a := f.amounts(t, snapshot)
	require.Equal(t, "usage_known", a.Status)
	require.Equal(t, "0.50000000", a.RequestCost)
	require.Equal(t, "0.00000000", a.Used)
	require.Zero(t, a.LedgerRows)
	require.Zero(t, a.DedupRows)
	require.Zero(t, a.LogRows)
	t.Log("actual-image minimal fixture prepared both original GPT and new Claude known receipts with no settlement side effects; image execution is a separate post-build test")
}

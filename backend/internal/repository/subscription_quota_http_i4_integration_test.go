//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type subscriptionQuotaI4Environment struct {
	PasswordHash                                                            string
	BridgePID                                                               int
	BridgePID2                                                              int
	Application2, URL2                                                      string
	RunID, URL, Image, ImageID, Source, ProviderImage, ProviderID           string
	Network, Postgres, Redis, Provider, Application, Volume                 string
	Database, JWT, ProviderKey                                              string
	AdminEmail, AEmail, BEmail, Password                                    string
	AKey, AKey2, BKey, GPTKey                                               string
	AGroup2Key                                                              string
	Admin, A, B, Group, AParent, BParent, ATerm, BTerm                      int64
	AKeyID, AKey2ID, BKeyID, Account                                        int64
	GPTGroup, GPTKeyID, GPTAccount, GPTTerm, GPTCycle                       int64
	Group2, AGroup2Term, AGroup2Parent, AGroup2KeyID, ANextTerm             int64
	BoundaryUser, BoundaryParent, BoundaryTerm, BoundaryNext, BoundaryKeyID int64
	BoundaryEmail, BoundaryKey, BoundaryExpires                             string
}

func subscriptionQuotaI4PrivatePath(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("CWQ_I4_PRIVATE_DIR")
	if dir == "" {
		t.Skip("explicit local synthetic I4 fixture invocation only")
	}
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	root := filepath.Join(os.Getenv("LOCALAPPDATA"), "Codex", "PrivateTests", "claude-weekly-quota")
	rel, err := filepath.Rel(root, abs)
	require.NoError(t, err)
	require.NotEqual(t, "..", rel)
	require.False(t, strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	require.NoError(t, os.MkdirAll(abs, 0700))
	return filepath.Join(abs, "synthetic-fixture.json")
}

func (e *subscriptionQuotaI4Environment) redact(raw []byte) string {
	s := string(raw)
	for _, secret := range []string{e.JWT, e.ProviderKey, e.Password, e.PasswordHash, e.AKey, e.AKey2, e.BKey, e.GPTKey, e.AGroup2Key, e.BoundaryKey} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[synthetic-secret-redacted]")
		}
	}
	return s
}

func (e *subscriptionQuotaI4Environment) docker(t *testing.T, input io.Reader, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	docker := "C:/Program Files/Docker/Docker/resources/bin/docker.exe"
	if _, err := os.Stat(docker); err != nil {
		docker = "docker"
	}
	command := exec.CommandContext(ctx, docker, append([]string{"--host", "npipe:////./pipe/docker_engine"}, args...)...)
	command.Stdin = input
	out, err := command.CombinedOutput()
	require.NoError(t, err, "task-only Docker operation %s: %s", args[0], e.redact(out))
	return out
}

func (e *subscriptionQuotaI4Environment) sql(t *testing.T, statement string) string {
	t.Helper()
	return strings.TrimSpace(string(e.docker(t, strings.NewReader(statement), "exec", "--interactive", e.Postgres, "psql", "--no-psqlrc", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", e.Database)))
}

// Original TestMain owns the seed DB. A PG18 dump is streamed into a new task
// container, leaving a durable browser fixture without altering that harness.
func TestSubscriptionQuotaI4PrepareEnvironment(t *testing.T) {
	private := subscriptionQuotaI4PrivatePath(t)
	_, err := os.Stat(private)
	require.True(t, os.IsNotExist(err), "never overwrite an existing synthetic fixture")
	e := &subscriptionQuotaI4Environment{RunID: "cwq-i4-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Database: "synthetic_i4", Image: os.Getenv("CWQ_I4_IMAGE"), Source: os.Getenv("CWQ_I4_SOURCE"), ProviderImage: os.Getenv("CWQ_I4_PROVIDER_IMAGE"), Network: os.Getenv("CWQ_I4_NETWORK")}
	require.NotEmpty(t, os.Getenv("CWQ_I4_PUBLIC_MANIFEST"))
	t.Cleanup(func() {
		if t.Failed() {
			// Preserve every created resource identity even if setup fails before
			// the final public manifest. Only this private file contains secrets.
			if encoded, err := json.MarshalIndent(e, "", "  "); err == nil {
				_ = os.WriteFile(private, encoded, 0600)
			}
			t.Logf("failed I4 setup resource identities retained privately at %s; run=%s postgres=%s redis=%s provider=%s application=%s volume=%s", private, e.RunID, e.Postgres, e.Redis, e.Provider, e.Application, e.Volume)
		}
	})
	require.True(t, strings.HasPrefix(e.Image, "sub2api:claude-weekly-i4-candidate-"))
	require.True(t, strings.HasPrefix(e.ProviderImage, "claude-weekly-quota-provider:"))
	require.True(t, strings.HasPrefix(e.Network, "cwq-env-"))
	require.Len(t, e.Source, 64)
	require.Equal(t, "true", strings.TrimSpace(string(e.docker(t, nil, "network", "inspect", "--format", "{{.Internal}}", e.Network))))
	var labels map[string]string
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "image", "inspect", "--format", "{{json .Config.Labels}}", e.Image), &labels))
	require.Equal(t, "I4-isolated-candidate", labels["com.monas.claude-quota-stage"])
	require.Equal(t, e.Source, labels["com.monas.source-sha256"])
	e.ImageID = strings.TrimSpace(string(e.docker(t, nil, "image", "inspect", "--format", "{{.Id}}", e.Image)))
	e.ProviderID = strings.TrimSpace(string(e.docker(t, nil, "image", "inspect", "--format", "{{.Id}}", e.ProviderImage)))
	e.JWT = strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	e.ProviderKey = "synthetic-provider-" + uuid.NewString()
	e.Password = "Synthetic-I4-" + uuid.NewString()
	e.AdminEmail, e.AEmail, e.BEmail = "i4-admin@example.invalid", "i4-a@example.invalid", "i4-b@example.invalid"
	f := newSubscriptionQuotaR1ImageFixture(t)
	e.Admin, e.A, e.B, e.Group, e.Account = f.rights.admin, f.rights.userA, f.rights.userB, f.rights.group, f.account
	e.AKeyID, e.AKey2ID, e.BKeyID = f.keyA, f.keyA2, f.keyB
	var user service.User
	require.NoError(t, user.SetPassword(e.Password))
	e.PasswordHash = user.PasswordHash
	for _, u := range []struct {
		id    int64
		email string
	}{{e.Admin, e.AdminEmail}, {e.A, e.AEmail}, {e.B, e.BEmail}} {
		_, err = f.db.Exec(`UPDATE users SET email=$1,password_hash=$2,concurrency=5 WHERE id=$3`, u.email, user.PasswordHash, u.id)
		require.NoError(t, err)
	}
	_, err = f.db.Exec(`UPDATE users SET claude_subscription_concurrency=2 WHERE id=$1`, e.A)
	require.NoError(t, err)
	for _, k := range []struct {
		id  int64
		key *string
	}{{e.AKeyID, &e.AKey}, {e.AKey2ID, &e.AKey2}, {e.BKeyID, &e.BKey}} {
		require.NoError(t, f.db.QueryRow(`SELECT key FROM api_keys WHERE id=$1`, k.id).Scan(k.key))
	}
	_, err = f.db.Exec(`UPDATE groups SET name='I4 Claude independent user cards' WHERE id=$1;`, e.Group)
	require.NoError(t, err)
	a := f.rights.open(t, e.A, f.rights.input("open", "100", 0))
	b := f.rights.open(t, e.B, f.rights.input("open", "300", 0))
	e.ATerm, e.BTerm = a.Term.ID, b.Term.ID
	e.AParent, e.BParent = a.Term.SubscriptionID, b.Term.SubscriptionID
	next := f.rights.open(t, e.A, f.rights.input("renew", "100", a.Summary.Revision))
	e.ANextTerm = next.Term.ID
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES('I4 second Claude group same user concurrency pool','anthropic','subscription','term') RETURNING id`).Scan(&e.Group2))
	f.rights.cfg.SubscriptionQuota.EnabledGroupIDs = append(f.rights.cfg.SubscriptionQuota.EnabledGroupIDs, e.Group2)
	secondInput := f.rights.input("open", "70", 0)
	secondInput.GroupID = e.Group2
	second := f.rights.open(t, e.A, secondInput)
	e.AGroup2Term, e.AGroup2Parent = second.Term.ID, second.Term.SubscriptionID
	e.AGroup2Key = "sk-cwq-i4-second-group-" + uuid.NewString()
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES($1,$2,$3,'I4 second group same user',1000000,1000000,1000000,1000000) RETURNING id`, e.A, e.Group2, e.AGroup2Key).Scan(&e.AGroup2KeyID))
	_, err = f.db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, e.Account, e.Group2)
	require.NoError(t, err)
	credentials, err := json.Marshal(map[string]string{"api_key": e.ProviderKey, "base_url": "http://" + e.RunID + "-provider:8081"})
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE accounts SET name='I4 synthetic Anthropic provider',credentials=$1,extra=extra || '{"anthropic_passthrough":true}',concurrency=16,schedulable=true WHERE id=$2`, credentials, e.Account)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('I4 synthetic GPT independent','openai','standard') RETURNING id`).Scan(&e.GPTGroup))
	e.GPTKey = "sk-cwq-i4-gpt-" + uuid.NewString()
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES($1,$2,$3,'I4 original GPT Key',1000000,1000000,1000000,1000000) RETURNING id`, e.A, e.GPTGroup, e.GPTKey).Scan(&e.GPTKeyID))
	var plan int64
	require.NoError(t, f.db.QueryRow(`SELECT id FROM carpool_plans WHERE code='four_seat' AND enabled ORDER BY version DESC,id DESC LIMIT 1`).Scan(&plan))
	gpt, err := service.NewCarpoolService(NewCarpoolRepository(f.db)).Open(context.Background(), e.A, e.Admin, service.CarpoolOpenInput{GroupID: e.GPTGroup, PlanID: plan, Mode: "new"}, uuid.NewString())
	require.NoError(t, err)
	require.NotEmpty(t, gpt.Cycles)
	e.GPTTerm, e.GPTCycle = gpt.ID, gpt.Cycles[0].ID
	require.NoError(t, f.db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status,concurrency,schedulable) VALUES('I4 synthetic OpenAI provider','openai','apikey',$1,'{"openai_passthrough":true}','active',16,true) RETURNING id`, credentials).Scan(&e.GPTAccount))
	_, err = f.db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, e.GPTAccount, e.GPTGroup)
	require.NoError(t, err)
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	var database string
	require.NoError(t, f.db.QueryRow(`SELECT current_database()`).Scan(&database))
	var seedContainer string
	for _, id := range strings.Fields(string(e.docker(t, nil, "ps", "--filter", "label=org.testcontainers=true", "--format", "{{.ID}}"))) {
		var ports map[string][]struct{ HostPort string }
		require.NoError(t, json.Unmarshal(e.docker(t, nil, "inspect", "--format", "{{json .NetworkSettings.Ports}}", id), &ports))
		for _, binds := range ports {
			for _, bind := range binds {
				if bind.HostPort == parsed.Port() {
					seedContainer = id
				}
			}
		}
	}
	require.NotEmpty(t, seedContainer)
	require.Equal(t, postgresImageTag, strings.TrimSpace(string(e.docker(t, nil, "inspect", "--format", "{{.Config.Image}}", seedContainer))))
	dump := e.docker(t, nil, "exec", seedContainer, "pg_dump", "-U", parsed.User.Username(), "-d", database, "--no-owner", "--no-privileges")
	e.Volume = e.RunID + "-pgdata"
	e.docker(t, nil, "volume", "create", "--label", "com.codex.local-task=claude-weekly-quota", e.Volume)
	e.Postgres = strings.TrimSpace(string(e.docker(t, nil, "run", "--detach", "--pull", "never", "--name", e.RunID+"-postgres", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--network-alias", e.RunID+"-pg", "--memory", "768m", "--cpus", "1", "--mount", "type=volume,source="+e.Volume+",target=/var/lib/postgresql", "--env", "POSTGRES_HOST_AUTH_METHOD=trust", "--env", "POSTGRES_DB="+e.Database, postgresImageTag)))
	var ready bool
	for attempt := 0; attempt < 60; attempt++ {
		// pg_isready is intentionally allowed to return nonzero while init runs.
		cmd := exec.Command("C:/Program Files/Docker/Docker/resources/bin/docker.exe", "--host", "npipe:////./pipe/docker_engine", "exec", e.Postgres, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", e.Database)
		if cmd.Run() == nil {
			ready = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	require.True(t, ready)
	e.docker(t, strings.NewReader(string(dump)), "exec", "--interactive", e.Postgres, "psql", "--no-psqlrc", "-q", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", e.Database)
	e.Redis = strings.TrimSpace(string(e.docker(t, nil, "run", "--detach", "--pull", "never", "--name", e.RunID+"-redis", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--network-alias", e.RunID+"-redis", "--memory", "128m", "--cpus", "0.5", redisImageTag)))
	e.Provider = strings.TrimSpace(string(e.docker(t, nil, "run", "--detach", "--pull", "never", "--name", e.RunID+"-provider", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--network-alias", e.RunID+"-provider", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "128m", "--cpus", "0.5", e.ProviderImage)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	e.URL = "http://127.0.0.1:" + strconv.Itoa(port)
	args := []string{"run", "--detach", "--pull", "never", "--name", e.RunID + "-candidate", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "1536m", "--cpus", "2", "--pids-limit", "512", "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000"}
	for _, value := range []string{"SKIP_SETUP=true", "RUN_MODE=standard", "SERVER_HOST=0.0.0.0", "SERVER_PORT=8080", "SERVER_MODE=release", "DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=3", "DATABASE_HOST=" + e.RunID + "-pg", "DATABASE_PORT=5432", "DATABASE_USER=postgres", "DATABASE_DBNAME=" + e.Database, "DATABASE_SSLMODE=disable", "REDIS_HOST=" + e.RunID + "-redis", "REDIS_DB=0", "JWT_SECRET=" + e.JWT, "SUBSCRIPTION_QUOTA_ISSUE_ENABLED=true", "SUBSCRIPTION_QUOTA_ADMISSION_ENABLED=true", "SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS=" + strconv.FormatInt(e.Group, 10) + "," + strconv.FormatInt(e.Group2, 10), "SUBSCRIPTION_QUOTA_RECOVERY_ENABLED=true", "SUBSCRIPTION_QUOTA_RECOVERY_INTERVAL=1s", "SUBSCRIPTION_QUOTA_RECOVERY_ITEM_TIMEOUT=5s", "DASHBOARD_AGG_ENABLED=false", "PRICING_DATA_DIR=/app/data/pricing", "CARPOOL_BACKGROUND_ENABLED=true", "SECURITY_URL_ALLOWLIST_ENABLED=false", "SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=true", "SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP=true", "TZ=UTC"} {
		args = append(args, "--env", value)
	}
	e.Application = strings.TrimSpace(string(e.docker(t, nil, append(args, e.Image)...)))
	bridge := exec.Command(os.Getenv("CWQ_I4_PWSH"), "-NoProfile", "-NonInteractive", "-File", os.Getenv("CWQ_I4_BRIDGE_SCRIPT"), "-ContainerID", e.Application, "-Port", strconv.Itoa(port), "-PrivateDir", filepath.Dir(private))
	bridgeOutput, err := bridge.CombinedOutput()
	require.NoError(t, err, "start task-only Hidden loopback bridge: %s", e.redact(bridgeOutput))
	e.BridgePID, err = strconv.Atoi(strings.TrimSpace(string(bridgeOutput)))
	require.NoError(t, err)
	encoded, err := json.MarshalIndent(e, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(private, encoded, 0600))
	public := map[string]any{"stage": "I4 diagnostic synthetic environment", "run_id": e.RunID, "url": e.URL, "image": e.Image, "image_id": e.ImageID, "source_sha256": e.Source, "provider_image": e.ProviderImage, "provider_id": e.ProviderID, "network": e.Network, "postgres": e.Postgres, "redis": e.Redis, "provider": e.Provider, "application": e.Application, "volume": e.Volume, "database_host_ports": []any{}, "application_bind": "127.0.0.1", "external_egress": false, "synthetic_only": true, "private_fixture_path": private, "admin_email": e.AdminEmail, "a_email": e.AEmail, "b_email": e.BEmail, "a_quota": "100.00000000", "b_quota": "300.00000000"}
	public["loopback_bridge_pid"] = e.BridgePID
	public["claude_default_concurrency"] = 3
	public["a_claude_override"] = 2
	public["legacy_user_concurrency"] = 5
	public["secondary_claude_group"] = e.Group2
	public["transport"] = "raw TCP through exact-container Docker exec nc; no container ports published"
	encoded, err = json.MarshalIndent(public, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("CWQ_I4_PUBLIC_MANIFEST"), encoded, 0600))
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("I4 candidate diagnostic: %s", e.redact(e.docker(t, nil, "logs", "--tail", "65", e.Application)))
		}
	})
	require.Eventually(t, func() bool {
		client := &http.Client{Timeout: time.Second}
		response, err := client.Get(e.URL + "/health")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 70*time.Second, 500*time.Millisecond)
	require.Equal(t, "100.00000000|300.00000000", e.sql(t, fmt.Sprintf(`SELECT (SELECT quota_usd::text FROM subscription_quota_terms WHERE id=%d)||'|'||(SELECT quota_usd::text FROM subscription_quota_terms WHERE id=%d);`, e.ATerm, e.BTerm)))
	t.Logf("I4 actual candidate=%s image_id=%s source=%s loopback=%s internal_only_provider=true; A100/B300 shared-Key and original GPT cloned from original TestMain; credentials kept only in private fixture", e.RunID, e.ImageID, e.Source, e.URL)
}

func subscriptionQuotaI4Load(t *testing.T) *subscriptionQuotaI4Environment {
	t.Helper()
	raw, err := os.ReadFile(subscriptionQuotaI4PrivatePath(t))
	require.NoError(t, err)
	var e subscriptionQuotaI4Environment
	require.NoError(t, json.Unmarshal(raw, &e))
	require.True(t, strings.HasPrefix(e.RunID, "cwq-i4-"))
	require.True(t, strings.HasPrefix(e.URL, "http://127.0.0.1:"))
	for _, id := range []string{e.Postgres, e.Redis, e.Provider, e.Application} {
		require.Equal(t, "claude-weekly-quota", strings.TrimSpace(string(e.docker(t, nil, "inspect", "--format", "{{index .Config.Labels \"com.codex.local-task\"}}", id))))
	}
	return &e
}

func (e *subscriptionQuotaI4Environment) post(t *testing.T, path, key string, stream bool, gpt bool) (int, []byte, http.Header) {
	t.Helper()
	model := "claude-sonnet-4-5"
	if gpt {
		model = "gpt-5.2"
	}
	body := map[string]any{"model": model, "max_tokens": 64, "stream": stream}
	if strings.Contains(path, "responses") {
		delete(body, "max_tokens")
		body["max_output_tokens"] = 64
		body["input"] = "isolated synthetic quota request"
	} else {
		body["messages"] = []any{map[string]string{"role": "user", "content": "isolated synthetic quota request"}}
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, e.URL+path, strings.NewReader(string(encoded)))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Anthropic-Version", "2023-06-01")
	request.Header.Set("X-Request-ID", "same-synthetic-client-id")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	require.NoError(t, err)
	return response.StatusCode, raw, response.Header
}

// Real HTTP, real auth/router/Forward and real PG settlement. No billing port
// stubs, upstream Internet requests, or business-source test hooks are used.
func TestSubscriptionQuotaI4HTTPWalletMatrix(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	startingRequests, err := strconv.Atoi(e.sql(t, `SELECT count(*) FROM subscription_quota_requests;`))
	require.NoError(t, err)
	paths := []string{"/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"}
	for _, identity := range []struct {
		name              string
		user, term, other int64
		keys              []string
		keyIDs            []int64
	}{{"A", e.A, e.ATerm, e.BTerm, []string{e.AKey, e.AKey2}, []int64{e.AKeyID, e.AKey2ID}}, {"B", e.B, e.BTerm, e.ATerm, []string{e.BKey, e.BKey}, []int64{e.BKeyID, e.BKeyID}}} {
		for mode, stream := range []bool{false, true} {
			for _, path := range paths {
				if !t.Run(identity.name+"/"+map[bool]string{false: "JSON", true: "SSE"}[stream]+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
					keyID := identity.keyIDs[mode]
					guard := e.ownershipCheckpoint(t, identity.term, keyID)
					before := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d;`, identity.term)))
					other := e.sql(t, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM subscription_quota_terms t WHERE id=%d;`, identity.other))
					money := e.fingerprint(t, "ordinary_and_GPT")
					requestCount := e.sql(t, `SELECT count(*) FROM subscription_quota_requests;`)
					status, raw, headers := e.post(t, path, identity.keys[mode], stream, false)
					require.Equal(t, http.StatusOK, status, e.redact(raw))
					if stream {
						require.Contains(t, headers.Get("Content-Type"), "text/event-stream")
					} else {
						require.True(t, json.Valid(raw), e.redact(raw))
					}
					wantedCount, err := strconv.Atoi(requestCount)
					require.NoError(t, err)
					require.Eventually(t, func() bool {
						return e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE status='settled' AND usage_log_recorded_at IS NOT NULL;`) == strconv.Itoa(wantedCount+1)
					}, 12*time.Second, 100*time.Millisecond)
					expected := before.Add(decimal.RequireFromString("0.00060000")).StringFixed(8)
					require.Equal(t, expected, e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d;`, identity.term)))
					require.Equal(t, "1|1|1|100|20|0.00060000", e.sql(t, fmt.Sprintf(`SELECT (SELECT count(*) FROM subscription_quota_ledger l WHERE l.request_row_id=r.id)||'|'||(SELECT count(*) FROM usage_billing_dedup d WHERE d.request_id=r.request_id AND d.api_key_id=r.api_key_id)||'|'||(SELECT count(*) FROM usage_logs l WHERE l.subscription_quota_request_id=r.id)||'|'||(SELECT input_tokens FROM usage_logs l WHERE l.subscription_quota_request_id=r.id)||'|'||(SELECT output_tokens FROM usage_logs l WHERE l.subscription_quota_request_id=r.id)||'|'||r.actual_cost_usd::text FROM subscription_quota_requests r WHERE r.user_id=%d AND r.term_id=%d AND r.api_key_id=%d ORDER BY r.id DESC LIMIT 1;`, identity.user, identity.term, keyID)))
					require.Equal(t, other, e.sql(t, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM subscription_quota_terms t WHERE id=%d;`, identity.other)), "other user's complete card cannot change")
					require.Equal(t, money, e.fingerprint(t, "ordinary_and_GPT"), "Claude HTTP cannot change ordinary/native/GPT wallets")
					guard()
					t.Logf("real HTTP %s %s stream=%t user=%d term=%d key_id=%d exact_cost=0.00060000; known/ledger/dedup/logack once; opposite card/GPT/native/ordinary rows unchanged", identity.name, path, stream, identity.user, identity.term, keyID)
				}) {
					return
				}
			}
			claude := e.fingerprint(t, "Claude")
			before := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=%d;`, e.GPTCycle)))
			gptRequests, err := strconv.Atoi(e.sql(t, `SELECT count(*) FROM carpool_billing_requests;`))
			require.NoError(t, err)
			gptStartedAt := e.sql(t, `SELECT EXTRACT(epoch FROM clock_timestamp())::text;`)
			status, raw, _ := e.post(t, "/v1/responses", e.GPTKey, stream, true)
			require.Equal(t, http.StatusOK, status, e.redact(raw))
			require.Eventually(t, func() bool {
				return e.sql(t, `SELECT count(*) FROM carpool_billing_requests WHERE status='settled';`) == strconv.Itoa(gptRequests+1)
			}, 12*time.Second, 100*time.Millisecond)
			cost := decimal.RequireFromString(e.sql(t, `SELECT actual_cost_usd::text FROM carpool_billing_requests ORDER BY id DESC LIMIT 1;`))
			require.True(t, cost.IsPositive())
			require.Equal(t, before.Sub(cost).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=%d;`, e.GPTCycle)))
			// Original GPT publishes account last-use metadata through the existing
			// 10s DeferredService. Finish that legitimate prior operation before the
			// next Claude request captures a complete foreign-account fingerprint.
			// No foreign columns are excluded from the subsequent strict guard.
			require.Eventually(t, func() bool {
				return e.sql(t, fmt.Sprintf(`SELECT COALESCE(EXTRACT(epoch FROM last_used_at)>=%s,false) FROM accounts WHERE id=%d;`, gptStartedAt, e.GPTAccount)) == "t"
			}, 20*time.Second, 100*time.Millisecond, "original GPT asynchronous account last-use must finish before the next full-row baseline")
			require.Equal(t, claude, e.fingerprint(t, "Claude"), "real GPT HTTP settlement cannot change any Claude row")
		}
	}
	total := strconv.Itoa(startingRequests + 24)
	require.Equal(t, total+"|"+total+"|"+total, e.sql(t, `SELECT count(*)||'|'||count(DISTINCT request_id)||'|'||count(DISTINCT id) FROM subscription_quota_requests;`))
	t.Log("T45 actual six aliases x JSON/SSE x A/B, A two Keys and four interleaved original GPT calls; exact row accounting complete; fixture retained for browser")
}

func (e *subscriptionQuotaI4Environment) ownershipCheckpoint(t *testing.T, term, key int64) func() {
	t.Helper()
	maxRequest := e.sql(t, `SELECT COALESCE(max(id),0) FROM subscription_quota_requests;`)
	maxLog := e.sql(t, `SELECT COALESCE(max(id),0) FROM usage_logs;`)
	var queries []string
	for _, row := range []struct{ label, query string }{
		{"foreign_terms", fmt.Sprintf(`SELECT * FROM subscription_quota_terms WHERE id<>%d`, term)},
		{"prior_requests", `SELECT * FROM subscription_quota_requests WHERE id<=` + maxRequest},
		{"prior_ledger", `SELECT * FROM subscription_quota_ledger WHERE request_row_id<=` + maxRequest},
		{"prior_logs", `SELECT * FROM usage_logs WHERE id<=` + maxLog},
		{"foreign_keys", fmt.Sprintf(`SELECT * FROM api_keys WHERE id<>%d`, key)},
		{"foreign_accounts", fmt.Sprintf(`SELECT * FROM accounts WHERE id<>%d`, e.Account)},
		{"own_key_identity", fmt.Sprintf(`SELECT to_jsonb(k)-'quota_used'-'usage_5h'-'usage_1d'-'usage_7d'-'window_5h_start'-'window_1d_start'-'window_7d_start'-'updated_at'-'last_used_at' FROM api_keys k WHERE id=%d`, key)},
		{"own_account_identity", fmt.Sprintf(`SELECT (to_jsonb(a)-'extra'-'updated_at'-'last_used_at')||jsonb_build_object('extra',a.extra-'quota_used'-'quota_daily_used'-'quota_weekly_used'-'quota_daily_start'-'quota_weekly_start'-'quota_daily_reset_at'-'quota_weekly_reset_at') FROM accounts a WHERE id=%d`, e.Account)},
	} {
		queries = append(queries, fmt.Sprintf(`SELECT '%s',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM (%s) x;`, row.label, row.query))
	}
	statement := strings.Join(queries, "\n")
	before := e.sql(t, statement)
	keyBefore := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT quota_used::text FROM api_keys WHERE id=%d;`, key)))
	keyLastUsedBefore := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT COALESCE(EXTRACT(epoch FROM last_used_at),0)::text FROM api_keys WHERE id=%d;`, key)))
	accountBefore := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT COALESCE(extra->>'quota_used','0') FROM accounts WHERE id=%d;`, e.Account)))
	return func() {
		t.Helper()
		require.Equal(t, before, e.sql(t, statement), "full foreign-row/prior-fact fingerprints and own immutable identities must remain unchanged")
		require.Equal(t, keyBefore.Add(decimal.RequireFromString("0.0006")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT quota_used::numeric(20,8)::text FROM api_keys WHERE id=%d;`, key)))
		keyLastUsed := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT COALESCE(EXTRACT(epoch FROM last_used_at),0)::text FROM api_keys WHERE id=%d;`, key)))
		require.True(t, keyLastUsed.IsPositive() && !keyLastUsed.LessThan(keyLastUsedBefore), "actual auth may only publish monotonic last-use metadata for the admitted Key")
		require.Equal(t, accountBefore.Add(decimal.RequireFromString("0.0006")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT (extra->>'quota_used')::numeric(20,8)::text FROM accounts WHERE id=%d;`, e.Account)))
	}
}

func (e *subscriptionQuotaI4Environment) fingerprint(t *testing.T, scope string) string {
	t.Helper()
	var statements []string
	if scope == "Claude" {
		for _, table := range []string{"subscription_quota_terms", "subscription_quota_requests", "subscription_quota_ledger", "subscription_quota_operations", "user_subscriptions"} {
			statements = append(statements, fmt.Sprintf(`SELECT '%s',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM %s x;`, table, table))
		}
		statements = append(statements, fmt.Sprintf(`SELECT 'claude_keys',md5(jsonb_agg(to_jsonb(x) ORDER BY id)::text) FROM api_keys x WHERE group_id IN (%d,%d); SELECT 'claude_accounts',md5(jsonb_agg(to_jsonb(x) ORDER BY id)::text) FROM accounts x WHERE id=%d; SELECT 'claude_logs',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id)::text,'[]')) FROM usage_logs x WHERE subscription_quota_request_id IS NOT NULL;`, e.Group, e.Group2, e.Account))
	} else {
		for _, table := range []string{"users", "user_subscriptions", "redeem_codes"} {
			statements = append(statements, fmt.Sprintf(`SELECT '%s',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM %s x;`, table, table))
		}
		for _, table := range strings.Fields(e.sql(t, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND (table_name LIKE 'carpool\_%' ESCAPE '\' OR table_name LIKE 'payment\_%' ESCAPE '\') ORDER BY table_name;`)) {
			if scope == "ordinary_native_static" && strings.HasPrefix(table, "carpool_") {
				continue
			}
			require.NotContains(t, table, `"`)
			statements = append(statements, fmt.Sprintf(`SELECT '%s',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM "%s" x;`, table, table))
		}
	}
	return e.sql(t, strings.Join(statements, "\n"))
}

//go:build integration

package repository

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func (e *subscriptionQuotaI4Environment) rolloutJSON(t *testing.T, token, action, key string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	path := fmt.Sprintf("/api/v1/admin/groups/%d/subscription-quota/rollout", e.ConversionGroup)
	method := http.MethodGet
	if action != "" {
		method = http.MethodPost
		path += "/" + action
	}
	request, err := http.NewRequest(method, e.URL+path, strings.NewReader(string(encoded)))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	var data map[string]any
	require.NoError(t, json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&data))
	if nested, ok := data["data"].(map[string]any); ok {
		return response.StatusCode, nested
	}
	return response.StatusCode, data
}

// This reads Redis's actual commands without persisting or logging their payloads.
// The counter includes attempted legacy scripts, even if capacity was denied.
func (e *subscriptionQuotaI4Environment) monitorLegacyScope(t *testing.T, userID int64) (*atomic.Int64, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	command := subscriptionQuotaDockerCommand(ctx, "exec", e.Redis, "redis-cli", "--raw", "MONITOR")
	pipe, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = io.Discard
	require.NoError(t, command.Start())
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	require.True(t, scanner.Scan())
	require.Equal(t, "OK", scanner.Text())
	count := &atomic.Int64{}
	sentinel := "cwq-monitor-fence-" + uuid.NewString()
	var fenced atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, sentinel) {
				fenced.Store(true)
			}
			if !strings.Contains(line, fmt.Sprintf(`"concurrency:user:%d"`, userID)) && !strings.Contains(line, fmt.Sprintf(`"concurrency:wait:%d"`, userID)) {
				continue
			}
			upper := strings.ToUpper(line)
			if strings.Contains(upper, `"EVALSHA"`) || strings.Contains(upper, `"EVAL"`) || strings.Contains(upper, `"ZADD"`) || strings.Contains(upper, `"INCR"`) {
				count.Add(1)
			}
		}
	}()
	// go-redis sends lowercase script commands. Prove a rejected/no-write script
	// is visible before accepting a zero-attempt assertion from this monitor.
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "eval", "return -1", "1", fmt.Sprintf("concurrency:user:%d", userID))
	require.Eventually(t, func() bool { return count.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
	count.Store(0)
	stop := func() {
		e.docker(t, nil, "exec", e.Redis, "redis-cli", "ECHO", sentinel)
		require.Eventually(t, fenced.Load, 3*time.Second, 10*time.Millisecond, "monitor must consume the final Redis fence before zero can be trusted")
		cancel()
		<-done
		_ = command.Wait()
		require.NoError(t, scanner.Err())
	}
	return count, stop
}

func TestSubscriptionQuotaI6RuntimeRejectsMixedReplica(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Equal(t, "I6", e.Stage)
	require.NotEmpty(t, e.Application2)
	_, active := e.providerState(t)
	require.Zero(t, active)
	require.Empty(t, e.redisStrings(t, "ZRANGE", fmt.Sprintf("concurrency:user:%d", e.A), "0", "-1"), "a new replica must never run original startup cleanup during GPT traffic")
	var environment []string
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "inspect", "--format", "{{json .Config.Env}}", e.Application), &environment))
	args := []string{"run", "--detach", "--pull", "never", "--platform", "linux/amd64", "--name", e.RunID + "-incompatible", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "768m", "--cpus", "1", "--tmpfs", "/app/data:rw,size=64m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000"}
	for _, value := range environment {
		if strings.HasPrefix(value, "SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID=") {
			value = "SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID=unapproved-replica"
		}
		if strings.HasPrefix(value, "DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=") {
			value = "DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY=4"
		}
		args = append(args, "--env", value)
	}
	container := strings.TrimSpace(string(e.docker(t, nil, append(args, e.Image)...)))
	t.Cleanup(func() { e.docker(t, nil, "rm", "--force", container) })
	admin := e.loginToken(t, e.AdminEmail)
	var state map[string]any
	require.Eventually(t, func() bool {
		status, latest := e.rolloutJSON(t, admin, "", "", nil)
		state = latest
		blockers, _ := state["blockers"].([]any)
		return status == http.StatusOK && containsQuotaRuntimeCode(blockers, "REPLICA_UNEXPECTED") && containsQuotaRuntimeCode(blockers, "REPLICA_CONFIG_MISMATCH")
	}, 70*time.Second, time.Second, "actual incompatible process must advertise its real default4 and be rejected")
	status, state := e.rolloutJSON(t, admin, "maintenance", uuid.NewString(), map[string]any{"enabled": true, "expected_revision": state["revision"], "reason": "isolated mixed replica rejection"})
	require.Equal(t, http.StatusOK, status)
	status, preview := e.rolloutJSON(t, admin, "preview", "", map[string]any{"allocations": []any{}})
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, preview["blockers"], "REPLICA_CONFIG_MISMATCH")
	require.Empty(t, preview["preview_token"])
	status, _ = e.rolloutJSON(t, admin, "maintenance", uuid.NewString(), map[string]any{"enabled": false, "expected_revision": state["revision"], "reason": "must reject unsafe release"})
	require.Equal(t, http.StatusConflict, status)
	e.docker(t, nil, "stop", "--time", "10", container)
	require.Eventually(t, func() bool {
		_, latest := e.rolloutJSON(t, admin, "", "", nil)
		state = latest
		blockers, _ := state["blockers"].([]any)
		return !containsQuotaRuntimeCode(blockers, "REPLICA_UNEXPECTED") && !containsQuotaRuntimeCode(blockers, "REPLICA_CONFIG_MISMATCH") && !containsQuotaRuntimeCode(blockers, "REPLICA_MISSING_OR_STALE")
	}, 55*time.Second, time.Second, "expired heartbeat can be excluded only after its real freshness period")
	status, state = e.rolloutJSON(t, admin, "maintenance", uuid.NewString(), map[string]any{"enabled": false, "expected_revision": state["revision"], "reason": "isolated compatible roster restored"})
	require.Equal(t, http.StatusOK, status)
	t.Log("actual extra replica with independent default4 blocked preview and maintenance exit; no traffic ran during startup; real heartbeat expiry restored only the declared default3 replicas")
}

func containsQuotaRuntimeCode(values []any, code string) bool {
	for _, value := range values {
		if value == code {
			return true
		}
	}
	return false
}

func TestSubscriptionQuotaI6RuntimeControlledConversion(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Equal(t, "I6", e.Stage)
	require.Positive(t, e.ConversionGroup)
	require.NotEmpty(t, e.Application2, "start all replicas before any original GPT traffic")
	admin := e.loginToken(t, e.AdminEmail)
	status, state := e.rolloutJSON(t, admin, "", "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, false, state["converted"], "this proof needs its fresh, dedicated conversion group")
	allocations := []any{map[string]any{"user_id": e.B, "quota_usd": "75.12345678"}}
	previewBody := map[string]any{"allocations": allocations}
	// Keep an authentic windows cache entry so a stale L2 hit can be injected
	// after the real conversion. No group mode is changed by test-authored SQL.
	other := *e
	other.URL = e.URL2
	status, _ = other.runtimeJSON(t, http.MethodGet, "/v1/usage", e.ConversionKey, "")
	require.Equal(t, http.StatusOK, status)
	hash := sha256.Sum256([]byte(e.ConversionKey))
	cacheHash := hex.EncodeToString(hash[:])
	cacheKey := "apikey:auth:" + cacheHash
	oldCache := e.docker(t, nil, "exec", e.Redis, "redis-cli", "--raw", "GET", cacheKey)
	var cached service.APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(oldCache, &cached), "the real authentication cache must be populated")
	require.NotNil(t, cached.Snapshot)
	require.NotNil(t, cached.Snapshot.Group)
	require.Equal(t, "windows", cached.Snapshot.Group.SubscriptionQuotaMode)
	require.Equal(t, "standard", cached.Snapshot.Group.SubscriptionType)
	require.Equal(t, "active", cached.Snapshot.Group.Status)
	beforeBalance := e.sql(t, fmt.Sprintf(`SELECT balance::text FROM users WHERE id=%d`, e.B))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	finished := make(chan error, 5)
	for i := 0; i < 5; i++ {
		response, err := e.heldRuntimeRequest(ctx, e.URL, e.ConversionKey, false)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if err == nil && !strings.Contains(string(raw), "message_stop") {
				err = fmt.Errorf("protected stream missing actual completion")
			}
			finished <- err
		}()
	}
	queuedCtx, stopQueued := context.WithCancel(ctx)
	defer stopQueued()
	queuedDone := make(chan struct{})
	go func() {
		defer close(queuedDone)
		response, _ := e.heldRuntimeRequest(queuedCtx, e.URL2, e.ConversionKey, false)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	pending := func() string {
		return e.sql(t, fmt.Sprintf(`SELECT count(*) FROM subscription_quota_protected_requests WHERE group_id=%d AND state NOT IN ('completed','no_charge')`, e.ConversionGroup))
	}
	require.Eventually(t, func() bool { return pending() == "6" }, 10*time.Second, 100*time.Millisecond, "pre-slot queue waiting must already own a durable protected marker")
	status, state = e.rolloutJSON(t, admin, "maintenance", uuid.NewString(), map[string]any{"enabled": true, "expected_revision": state["revision"], "reason": "isolated I6 protected request drain"})
	require.Equal(t, http.StatusOK, status, state)
	status, blocked := e.rolloutJSON(t, admin, "preview", "", previewBody)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, blocked["blockers"], "PROTECTED_REQUESTS_PENDING")
	require.Empty(t, blocked["preview_token"])
	stopQueued()
	select {
	case <-queuedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pre-send cancellation did not return")
	}
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT count(*) FROM subscription_quota_protected_requests WHERE group_id=%d AND state='no_charge'`, e.ConversionGroup)) == "1" && pending() == "5"
	}, 10*time.Second, 100*time.Millisecond)
	e.providerGate(t, false)
	for i := 0; i < 5; i++ {
		select {
		case err := <-finished:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("legacy protected stream failed to drain")
		}
	}
	require.Eventually(t, func() bool { return pending() == "0" }, 15*time.Second, 100*time.Millisecond, "trusted asynchronous legacy usage must finish during maintenance")
	require.Equal(t, "5|1", e.sql(t, fmt.Sprintf(`SELECT count(*) FILTER(WHERE state='completed')||'|'||count(*) FILTER(WHERE state='no_charge') FROM subscription_quota_protected_requests WHERE group_id=%d`, e.ConversionGroup)))
	require.Equal(t, "5", e.sql(t, fmt.Sprintf(`SELECT count(*) FROM usage_logs WHERE api_key_id=%d`, e.ConversionKeyID)))
	require.Equal(t, "0.00300000", e.sql(t, fmt.Sprintf(`SELECT (%s::numeric-balance)::numeric(20,8)::text FROM users WHERE id=%d`, beforeBalance, e.B)))

	// Real GPT HTTP and WebSocket turns remain active across conversion and
	// maintenance exit, including the independently refreshed WS ingress lease.
	e.providerGate(t, true)
	beforeGPTRequest := e.sql(t, `SELECT coalesce(max(id),0) FROM carpool_billing_requests`)
	beforeGPTBalance := e.sql(t, fmt.Sprintf(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=%d`, e.GPTCycle))
	gpt, err := e.heldRuntimeRequest(ctx, e.URL, e.GPTKey, true)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, gpt.StatusCode)
	gptDone := make(chan error, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(gpt.Body, 1<<20))
		_ = gpt.Body.Close()
		if err == nil && !strings.Contains(string(raw), "response.completed") {
			err = fmt.Errorf("original GPT stream missing actual completion")
		}
		gptDone <- err
	}()
	beforeWS, _ := e.providerState(t)
	ws, wsResponse, err := websocket.DefaultDialer.DialContext(ctx, strings.Replace(e.URL2, "http://", "ws://", 1)+"/v1/responses", http.Header{"Authorization": []string{"Bearer " + e.GPTKey}})
	if wsResponse != nil && wsResponse.Body != nil {
		_ = wsResponse.Body.Close()
	}
	require.NoError(t, err)
	defer ws.Close()
	require.NoError(t, ws.WriteJSON(map[string]any{"type": "response.create", "model": "gpt-5.2", "max_output_tokens": 64, "input": "isolated conversion preserves active GPT WS"}))
	require.Eventually(t, func() bool { calls, _ := e.providerState(t); return calls == beforeWS+1 }, 10*time.Second, 100*time.Millisecond)
	legacyKey := fmt.Sprintf("concurrency:user:%d", e.A)
	legacyMembers := e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1")
	require.Len(t, legacyMembers, 2)
	ingressKey := openAIWSIngressLeaseKey(e.GPTKeyID)
	ingressMembers := e.redisStrings(t, "ZRANGE", ingressKey, "0", "-1")
	require.Len(t, ingressMembers, 1)
	status, preview := e.rolloutJSON(t, admin, "preview", "", previewBody)
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, preview["blockers"], preview)
	convert := map[string]any{"allocations": allocations, "expected_revision": preview["revision"], "preview_token": preview["preview_token"]}
	idempotency := uuid.NewString()
	status, converted := e.rolloutJSON(t, admin, "convert", idempotency, convert)
	require.Equal(t, http.StatusOK, status, converted)
	status, replay := e.rolloutJSON(t, admin, "convert", idempotency, convert)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, replay["replayed"])
	require.Equal(t, "1|75.12345678", e.sql(t, fmt.Sprintf(`SELECT count(*)||'|'||min(quota_usd)::text FROM subscription_quota_terms WHERE group_id=%d`, e.ConversionGroup)))
	require.Equal(t, "term|quota_maintenance", e.sql(t, fmt.Sprintf(`SELECT subscription_quota_mode||'|'||status FROM groups WHERE id=%d`, e.ConversionGroup)))
	convertedState, ok := converted["state"].(map[string]any)
	require.True(t, ok)
	status, state = e.rolloutJSON(t, admin, "maintenance", uuid.NewString(), map[string]any{"enabled": false, "expected_revision": convertedState["revision"], "reason": "isolated I6 verified conversion"})
	require.Equal(t, http.StatusOK, status, state)
	require.ElementsMatch(t, legacyMembers, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1"))
	// The auth outbox retains its row for a real second invalidation pass after
	// authInvalidationSafetyDelay (30s). Wait for that pass and worker polling
	// before injecting stale L2 data; a 15s zero-row deadline is impossible.
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key='%s'`, cacheHash)) == "0"
	}, 45*time.Second, 100*time.Millisecond)
	// Respect the real 15s L1 TTL plus its 10% jitter before injecting a real old
	// L2 snapshot. This creates the cache race without a business-source test hook.
	time.Sleep(17 * time.Second)
	e.docker(t, strings.NewReader(strings.TrimSpace(string(oldCache))), "exec", "--interactive", e.Redis, "redis-cli", "-x", "SET", cacheKey)
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "EXPIRE", cacheKey, "30")
	status, oldUsage := other.runtimeJSON(t, http.MethodGet, "/v1/usage", e.ConversionKey, "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "quota_limited", oldUsage["mode"], "real L1 must contain the injected old windows snapshot")
	legacyAttempts, stopMonitor := e.monitorLegacyScope(t, e.B)
	beforeCalls, _ := e.providerState(t)
	staleStarted := time.Now().UTC().Format(time.RFC3339Nano)
	status, rejected, _ := other.post(t, "/v1/messages", e.ConversionKey, false, false)
	stopMonitor()
	require.Equal(t, http.StatusServiceUnavailable, status, "old windows cache with authoritative DB term must fail closed")
	require.Contains(t, string(rejected), "Service temporarily unavailable, please retry later")
	logs := e.docker(t, nil, "logs", "--since", staleStarted, e.Application2)
	protectedFailure := false
	for _, line := range strings.Split(string(logs), "\n") {
		start := strings.IndexByte(line, '{')
		if start < 0 {
			continue
		}
		var fields struct {
			Error string `json:"error"`
		}
		// Structured logs escape the quotes inside Error(); inspect the decoded
		// field so valid evidence is not rejected by raw JSON text matching.
		if json.Unmarshal([]byte(line[start:]), &fields) == nil && strings.Contains(fields.Error, service.ErrSubscriptionQuotaProtectedUnavailable.Error()) {
			protectedFailure = true
		}
	}
	require.True(t, protectedFailure, "actual protected-source mismatch must cause this rejection")
	require.Zero(t, legacyAttempts.Load(), "old windows cache cannot attempt any original user acquire/wait operation")
	afterCalls, _ := e.providerState(t)
	require.Equal(t, beforeCalls, afterCalls)
	require.ElementsMatch(t, legacyMembers, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1"), "conversion cannot clear a GPT slot")
	require.ElementsMatch(t, ingressMembers, e.redisStrings(t, "ZRANGE", ingressKey, "0", "-1"), "conversion cannot clear GPT WS ingress")
	e.providerGate(t, false)
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(15*time.Second)))
	for {
		var event map[string]any
		require.NoError(t, ws.ReadJSON(&event))
		require.NotEqual(t, "error", event["type"])
		if event["type"] == "response.completed" {
			break
		}
	}
	require.NoError(t, ws.Close())
	require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", ingressKey, "0", "-1")) == 0 }, 5*time.Second, 100*time.Millisecond)
	select {
	case err := <-gptDone:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("original GPT stream did not survive conversion")
	}
	require.Eventually(t, func() bool {
		return e.sql(t, `SELECT count(*) FROM carpool_billing_requests WHERE id>`+beforeGPTRequest+` AND status='settled' AND actual_cost_usd>0`) == "2"
	}, 12*time.Second, 100*time.Millisecond)
	require.Equal(t, "t", e.sql(t, fmt.Sprintf(`SELECT base_balance_usd=%s::numeric-(SELECT sum(actual_cost_usd) FROM carpool_billing_requests WHERE id>%s) FROM carpool_cycles WHERE id=%d`, beforeGPTBalance, beforeGPTRequest, e.GPTCycle)))
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "DEL", cacheKey)
	time.Sleep(17 * time.Second)
	status, _, _ = other.post(t, "/v1/messages", e.ConversionKey, false, false)
	require.Equal(t, http.StatusOK, status, "the same key succeeds after real stale L1 expiry")
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE group_id=%d AND user_id=%d`, e.ConversionGroup, e.B)) == "0.00060000"
	}, 10*time.Second, 100*time.Millisecond)
	t.Log("actual final image T72: five admitted plus one pre-slot queued marker block real conversion; proven no-send cancellation drains only its marker; async legacy usage drains during maintenance; explicit B75.12345678 converted once; GPT active member preserved; authentic stale windows cache against DBterm performed zero legacy acquire/wait actions and zero upstream calls")
}

func TestSubscriptionQuotaI6RuntimeLostLeaseCancelsUpstream(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Equal(t, "I6", e.Stage)
	e.prepareBoundary(t, 180)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	beforeRequest := e.sql(t, `SELECT coalesce(max(id),0) FROM subscription_quota_requests`)
	otherMoney := e.fingerprint(t, "ordinary_and_GPT")
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	beforeCalls, beforeActive := e.providerState(t)
	require.Zero(t, beforeActive)
	response, err := e.heldRuntimeRequest(ctx, e.URL, e.BoundaryKey, false)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, response.Body); _ = response.Body.Close(); close(done) }()
	key := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.BoundaryUser)
	members := e.redisStrings(t, "ZRANGE", key, "0", "-1")
	require.Len(t, members, 1)
	require.Eventually(t, func() bool { calls, active := e.providerState(t); return calls == beforeCalls+1 && active == 1 }, 5*time.Second, 100*time.Millisecond)
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "ZREM", key, members[0])
	require.Eventually(t, func() bool { _, active := e.providerState(t); return active == 0 }, 20*time.Second, 200*time.Millisecond, "loss of ownership must cancel the actual upstream at the next heartbeat")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client stream survived lost lease")
	}
	var audit []struct {
		Active    bool `json:"active"`
		Cancelled bool `json:"cancelled"`
	}
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "http://127.0.0.1:8081/__audit"), &audit))
	require.Len(t, audit, beforeCalls+1)
	require.False(t, audit[beforeCalls].Active)
	require.True(t, audit[beforeCalls].Cancelled)
	require.Eventually(t, func() bool {
		return e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest+` AND status='reconcile_required'`) == "1"
	}, 10*time.Second, 100*time.Millisecond)
	require.Equal(t, "0", e.sql(t, `SELECT count(*) FROM subscription_quota_ledger WHERE request_row_id>`+beforeRequest))
	require.Equal(t, otherMoney, e.fingerprint(t, "ordinary_and_GPT"))
	t.Log("actual Redis ownership loss cancelled the upstream, retained unknown usage for reconciliation, and changed no ordinary/GPT money")
}

func TestSubscriptionQuotaI6RuntimeScopeMetricEvidence(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Equal(t, "I6", e.Stage)
	pattern := regexp.MustCompile(`^(acquire|enqueue|promote|renew|cleanup|safety_deadline|lost_lease)\.(active|waiting)\.(acquired|full|missing|unavailable|error)$`)
	readCounters := func() map[string]uint64 {
		total := map[string]uint64{}
		for _, app := range []string{e.Application, e.Application2} {
			latest := map[string]uint64{}
			for _, line := range strings.Split(string(e.docker(t, nil, "logs", app)), "\n") {
				if !strings.Contains(line, "claude concurrency metrics") {
					continue
				}
				start := strings.IndexByte(line, '{')
				if start < 0 {
					continue
				}
				var data struct {
					Operations map[string]uint64 `json:"operations_total"`
				}
				if json.Unmarshal([]byte(line[start:]), &data) == nil && data.Operations != nil {
					latest = data.Operations
				}
			}
			for key, value := range latest {
				require.Regexp(t, pattern, key, "metrics may never include user/request identities")
				total[key] += value
			}
		}
		return total
	}
	var counts map[string]uint64
	required := []string{"acquire.active.acquired", "enqueue.waiting.acquired", "promote.active.acquired", "renew.active.acquired", "renew.active.error", "renew.active.missing", "lost_lease.active.missing", "safety_deadline.active.unavailable", "cleanup.active.acquired"}
	require.Eventually(t, func() bool {
		counts = readCounters()
		for _, key := range required {
			if counts[key] == 0 {
				return false
			}
		}
		return true
	}, 10*time.Second, time.Second, "run final real queue, long-stream, Redis failure and lost-lease gates before collecting counters")
	receipt := map[string]any{"image_id": e.ImageID, "source_sha256": e.Source, "operation_totals": counts, "all_required_operations_observed": true, "bounded_cardinality": true, "production_modified": false}
	if path := os.Getenv("CWQ_I6_METRICS_RECEIPT"); path != "" {
		encoded, err := json.MarshalIndent(receipt, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, encoded, 0600))
	}
	t.Logf("final image actual independent scope counters: %v", counts)
}

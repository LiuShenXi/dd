//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type quotaI4StreamReader struct {
	io.Reader
	bytes, lastReceived *atomic.Int64
}

func (r *quotaI4StreamReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.bytes.Add(int64(n))
		r.lastReceived.Store(time.Now().UnixNano())
	}
	return n, err
}

func TestSubscriptionQuotaI4V2PrepareSecondInstance(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Empty(t, e.Application2, "never replace an existing second candidate")
	var environment []string
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "inspect", "--format", "{{json .Config.Env}}", e.Application), &environment))
	args := []string{"run", "--detach", "--pull", "never", "--name", e.RunID + "-candidate-2", "--label", "com.codex.local-task=claude-weekly-quota", "--network", e.Network, "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "1536m", "--cpus", "2", "--pids-limit", "512", "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000"}
	for _, value := range environment {
		args = append(args, "--env", value)
	}
	e.Application2 = strings.TrimSpace(string(e.docker(t, nil, append(args, e.Image)...)))
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	e.URL2 = "http://127.0.0.1:" + strconv.Itoa(port)
	bridge := exec.Command(os.Getenv("CWQ_I4_PWSH"), "-NoProfile", "-NonInteractive", "-File", os.Getenv("CWQ_I4_BRIDGE_SCRIPT"), "-ContainerID", e.Application2, "-Port", strconv.Itoa(port), "-PrivateDir", filepath.Dir(subscriptionQuotaI4PrivatePath(t)))
	output, err := bridge.CombinedOutput()
	require.NoError(t, err, "second candidate bridge: %s", e.redact(output))
	e.BridgePID2, err = strconv.Atoi(strings.TrimSpace(string(output)))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get(e.URL2 + "/health")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 70*time.Second, 500*time.Millisecond)
	encoded, err := json.MarshalIndent(e, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(subscriptionQuotaI4PrivatePath(t), encoded, 0600))
	t.Logf("second actual candidate healthy URL=%s container=%s bridge_PID=%d same_image=%s shared_task_PG_Redis=true; legacy startup cleanup is baseline behavior, so both instances must be started before GPT capacity traffic", e.URL2, e.Application2, e.BridgePID2, e.ImageID)
}

func (e *subscriptionQuotaI4Environment) providerGate(t *testing.T, hold bool) {
	t.Helper()
	e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "--header", "Content-Type: application/json", "--post-data", fmt.Sprintf(`{"hold":%t}`, hold), "http://127.0.0.1:8081/__control")
}

func (e *subscriptionQuotaI4Environment) redisStrings(t *testing.T, args ...string) []string {
	t.Helper()
	raw := e.docker(t, nil, append([]string{"exec", e.Redis, "redis-cli", "--json"}, args...)...)
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var values any
	require.NoError(t, decoder.Decode(&values))
	var result []string
	var appendValue func(any)
	appendValue = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, child := range value {
				appendValue(child)
			}
		case string:
			result = append(result, value)
		case json.Number:
			result = append(result, decimal.RequireFromString(value.String()).String())
		default:
			t.Fatalf("unexpected synthetic Redis reply scalar %T", value)
		}
	}
	appendValue(values)
	return result
}

func (e *subscriptionQuotaI4Environment) providerState(t *testing.T) (int, int) {
	t.Helper()
	var state struct {
		Active map[string]int    `json:"active_by_model"`
		Calls  []json.RawMessage `json:"calls"`
	}
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "http://127.0.0.1:8081/__state"), &state))
	return len(state.Calls), state.Active["claude-sonnet-4-5"]
}

func (e *subscriptionQuotaI4Environment) heldClaudeRequest(ctx context.Context, key string) (*http.Response, error) {
	return e.heldRuntimeRequest(ctx, e.URL, key, false)
}

func (e *subscriptionQuotaI4Environment) heldRuntimeRequest(ctx context.Context, baseURL, key string, gpt bool) (*http.Response, error) {
	path, body := "/v1/messages", `{"model":"claude-sonnet-4-5","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"synthetic lease duration proof"}]}`
	if gpt {
		path = "/v1/responses"
		body = `{"model":"gpt-5.2","max_output_tokens":64,"stream":true,"input":"synthetic independent concurrency pool proof"}`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Anthropic-Version", "2023-06-01")
	return (&http.Client{}).Do(request)
}

func (e *subscriptionQuotaI4Environment) legacyWaiting(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(string(e.docker(t, nil, "exec", e.Redis, "redis-cli", "--raw", "GET", fmt.Sprintf("concurrency:wait:%d", e.A))))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	require.NoError(t, err)
	return value
}

func TestSubscriptionQuotaI4V2ConcurrentIndependentPools(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.NotEmpty(t, e.Application2, "both instances must be started before GPT slots are populated")
	require.NotEmpty(t, e.URL2)
	require.Positive(t, e.Group2)
	require.NotEmpty(t, e.AGroup2Key)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	beforeRequest := e.sql(t, `SELECT COALESCE(max(id),0) FROM subscription_quota_requests`)
	beforeGPT := e.sql(t, `SELECT COALESCE(max(id),0) FROM carpool_billing_requests`)
	beforeBase := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=%d`, e.GPTCycle)))
	beforeA := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.ATerm)))
	beforeGroup2 := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.AGroup2Term)))
	foreignCards := e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.ATerm, e.AGroup2Term))
	foreignCycles := e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM carpool_cycles t WHERE id<>%d`, e.GPTCycle))
	cycleIdentity := e.sql(t, fmt.Sprintf(`SELECT (to_jsonb(t)-'base_balance_usd'-'revision'-'updated_at')::text FROM carpool_cycles t WHERE id=%d`, e.GPTCycle))
	ordinary := e.fingerprint(t, "ordinary_native_static")
	beforeCalls, beforeActive := e.providerState(t)
	require.Zero(t, beforeActive)
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	type result struct {
		gpt bool
		raw []byte
		err error
	}
	done := make(chan result, 8)
	urls := []string{e.URL, e.URL2}
	start := func(key string, gpt bool, index int) {
		response, err := e.heldRuntimeRequest(ctx, urls[index%2], key, gpt)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			done <- result{gpt, raw, err}
		}()
	}
	for i := 0; i < 5; i++ {
		start(e.GPTKey, true, i)
	}
	legacyKey := fmt.Sprintf("concurrency:user:%d", e.A)
	require.Len(t, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1"), 5)
	startWaiting := func(waitCtx context.Context, count int, gpt bool) chan struct{} {
		finished := make(chan struct{}, count)
		for i := 0; i < count; i++ {
			go func(index int) {
				key := e.AKey2
				if gpt {
					key = e.GPTKey
				}
				response, _ := e.heldRuntimeRequest(waitCtx, urls[index%2], key, gpt)
				if response != nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
				}
				finished <- struct{}{}
			}(i)
		}
		return finished
	}
	stopWaiting := func(stop context.CancelFunc, finished chan struct{}, count int) {
		stop()
		for i := 0; i < count; i++ {
			select {
			case <-finished:
			case <-time.After(4 * time.Second):
				t.Fatal("cancelled full-queue request did not return")
			}
		}
	}
	queueGPT := max(1, service.CalculateMaxWait(5)-5)
	gptWaitCtx, gptWaitStop := context.WithCancel(ctx)
	defer gptWaitStop()
	gptWaitDone := startWaiting(gptWaitCtx, queueGPT, true)
	require.Eventually(t, func() bool { return e.legacyWaiting(t) == queueGPT }, 8*time.Second, 50*time.Millisecond)
	assertOverflow := func(gpt bool) {
		probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		key := e.AKey2
		if gpt {
			key = e.GPTKey
		}
		response, err := e.heldRuntimeRequest(probeCtx, e.URL2, key, gpt)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		require.Equal(t, http.StatusTooManyRequests, response.StatusCode, "full actual waiting pool rejects the next request before sending")
	}
	assertOverflow(true)
	// GPT active and waiting pools are both full before Claude starts.
	start(e.AKey, false, 0)
	start(e.AGroup2Key, false, 1)
	activeKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.A)
	waitKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:waiting", e.A)
	require.Len(t, e.redisStrings(t, "ZRANGE", activeKey, "0", "-1"), 2, "Claude capacity is shared across Keys and groups")
	var state struct {
		Active map[string]int `json:"active_by_model"`
	}
	require.NoError(t, json.Unmarshal(e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "http://127.0.0.1:8081/__state"), &state))
	require.Equal(t, 5, state.Active["gpt-5.2"])
	require.Equal(t, 2, state.Active["claude-sonnet-4-5"])
	queueClaude := max(1, service.CalculateMaxWait(2)-2)
	claudeWaitCtx, claudeWaitStop := context.WithCancel(ctx)
	defer claudeWaitStop()
	claudeWaitDone := startWaiting(claudeWaitCtx, queueClaude, false)
	require.Eventually(t, func() bool {
		return e.legacyWaiting(t) == queueGPT && len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == queueClaude
	}, 8*time.Second, 50*time.Millisecond)
	assertOverflow(false)
	require.Len(t, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1"), 5)
	require.Len(t, e.redisStrings(t, "ZRANGE", activeKey, "0", "-1"), 2)
	stopWaiting(gptWaitStop, gptWaitDone, queueGPT)
	require.Eventually(t, func() bool {
		return e.legacyWaiting(t) == 0 && len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == queueClaude
	}, 4*time.Second, 50*time.Millisecond)
	// Release one specific GPT upstream while Claude stays fully occupied and
	// all its wait tickets remain. A new GPT request must take the free GPT slot.
	e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "--header", "Content-Type: application/json", "--post-data", fmt.Sprintf(`{"release_sequence":%d}`, beforeCalls+1), "http://127.0.0.1:8081/__control")
	select {
	case out := <-done:
		require.NoError(t, out.err)
		require.True(t, out.gpt)
		require.Contains(t, string(out.raw), "response.completed")
	case <-time.After(10 * time.Second):
		t.Fatal("selected GPT synthetic call did not finish")
	}
	require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1")) == 4 }, 4*time.Second, 50*time.Millisecond)
	start(e.GPTKey, true, 1)
	require.Len(t, e.redisStrings(t, "ZRANGE", waitKey, "0", "-1"), queueClaude)
	stopWaiting(claudeWaitStop, claudeWaitDone, queueClaude)
	require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == 0 }, 4*time.Second, 50*time.Millisecond)
	require.Len(t, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1"), 5)
	require.Len(t, e.redisStrings(t, "ZRANGE", activeKey, "0", "-1"), 2)
	require.Equal(t, "2", e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest))
	require.Equal(t, "6", e.sql(t, `SELECT count(*) FROM carpool_billing_requests WHERE id>`+beforeGPT))
	e.providerGate(t, false)
	for i := 0; i < 7; i++ {
		select {
		case out := <-done:
			require.NoError(t, out.err)
			if out.gpt {
				require.Contains(t, string(out.raw), "response.completed")
			} else {
				require.Contains(t, string(out.raw), "message_stop")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("released synthetic capacity request did not finish")
		}
	}
	require.Eventually(t, func() bool {
		return e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest+` AND status='settled' AND usage_log_recorded_at IS NOT NULL`) == "2" && e.sql(t, `SELECT count(*) FROM carpool_billing_requests WHERE id>`+beforeGPT+` AND status='settled'`) == "6"
	}, 15*time.Second, 100*time.Millisecond)
	require.Equal(t, beforeA.Add(decimal.RequireFromString("0.00060000")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.ATerm)))
	require.Equal(t, beforeGroup2.Add(decimal.RequireFromString("0.00060000")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.AGroup2Term)))
	cost := decimal.RequireFromString(e.sql(t, `SELECT sum(actual_cost_usd)::text FROM carpool_billing_requests WHERE id>`+beforeGPT))
	require.True(t, cost.IsPositive())
	require.Equal(t, beforeBase.Sub(cost).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=%d`, e.GPTCycle)))
	require.Equal(t, foreignCards, e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.ATerm, e.AGroup2Term)))
	require.Equal(t, foreignCycles, e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM carpool_cycles t WHERE id<>%d`, e.GPTCycle)))
	require.Equal(t, cycleIdentity, e.sql(t, fmt.Sprintf(`SELECT (to_jsonb(t)-'base_balance_usd'-'revision'-'updated_at')::text FROM carpool_cycles t WHERE id=%d`, e.GPTCycle)))
	require.Equal(t, ordinary, e.fingerprint(t, "ordinary_native_static"))
	require.Eventually(t, func() bool {
		return len(e.redisStrings(t, "ZRANGE", activeKey, "0", "-1")) == 0 && len(e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1")) == 0
	}, 4*time.Second, 100*time.Millisecond)
	t.Logf("actual two-instance Router/provider/Redis/PG: GPT5+Claude2 concurrent; GPT waiting%d full before Claude admitted, Claude waiting%d full while a freed GPT slot admits replacement; queue overflow429 and cancellations preserve owners; two Claude groups share one user pool; B/next cards/foreign cycles/native/ordinary/payment rows unchanged, own cards each0.00060000 and six GPT completed turns on original cycle", queueGPT, queueClaude)
}

// This explicitly opt-in test keeps actual Router/provider SSE requests open
// longer than the original fifteen-minute GPT slot TTL. Fast Redis TTL tests do
// not stand in for this test. The same durable fixture is retained afterwards.
func TestSubscriptionQuotaI4V2LongSSELease(t *testing.T) {
	minutesText := os.Getenv("CWQ_I4_LONG_STREAM_MINUTES")
	if minutesText == "" {
		t.Skip("explicit local synthetic long-stream invocation only")
	}
	minutes, err := strconv.Atoi(minutesText)
	require.NoError(t, err)
	require.GreaterOrEqual(t, minutes, 16, "actual proof must exceed the original fifteen-minute TTL")
	e := subscriptionQuotaI4Load(t)
	duration := time.Duration(minutes) * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), duration+2*time.Minute)
	defer cancel()
	beforeRequest := e.sql(t, `SELECT COALESCE(max(id),0) FROM subscription_quota_requests;`)
	beforeUsed := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.ATerm)))
	otherCard := e.sql(t, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM subscription_quota_terms t WHERE id=%d`, e.BTerm))
	money := e.fingerprint(t, "ordinary_and_GPT")
	beforeCalls, beforeActive := e.providerState(t)
	require.Zero(t, beforeActive)
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	type completed struct {
		raw []byte
		err error
	}
	done := make(chan completed, 2)
	var received, lastReceived [2]atomic.Int64
	for index, key := range []string{e.AKey, e.AKey2} {
		response, err := e.heldClaudeRequest(ctx, key)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
		go func() {
			raw, err := io.ReadAll(io.LimitReader(&quotaI4StreamReader{Reader: response.Body, bytes: &received[index], lastReceived: &lastReceived[index]}, 1<<20))
			_ = response.Body.Close()
			done <- completed{raw, err}
		}()
	}
	activeKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.A)
	waitingKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:waiting", e.A)
	initial := e.redisStrings(t, "ZRANGE", activeKey, "0", "-1", "WITHSCORES")
	require.Len(t, initial, 4)
	calls, active := e.providerState(t)
	require.Equal(t, beforeCalls+2, calls)
	require.Equal(t, 2, active)
	initialScores := map[string]int64{}
	for i := 0; i < len(initial); i += 2 {
		_, err := uuid.Parse(initial[i])
		require.NoError(t, err)
		score, err := strconv.ParseInt(initial[i+1], 10, 64)
		require.NoError(t, err)
		initialScores[initial[i]] = score
	}
	started := time.Now()
	deadline := started.Add(duration)
	checks := 0
	for time.Now().Before(deadline) {
		select {
		case early := <-done:
			t.Fatalf("actual held SSE ended before duration proof: elapsed=%s error=%v body=%s", time.Since(started), early.err, e.redact(early.raw))
		case <-ctx.Done():
			t.Fatal("long-stream root context expired before proof completed")
		case <-time.After(15 * time.Second):
		}
		members := e.redisStrings(t, "ZRANGE", activeKey, "0", "-1", "WITHSCORES")
		require.Len(t, members, 4, "the two active HTTP streams retain both original lease members")
		var minimumExtension int64 = 1<<63 - 1
		for i := 0; i < len(members); i += 2 {
			base, exists := initialScores[members[i]]
			require.True(t, exists, "lease renewal must preserve the original server UUID")
			score, err := strconv.ParseInt(members[i+1], 10, 64)
			require.NoError(t, err)
			if score-base < minimumExtension {
				minimumExtension = score - base
			}
		}
		if time.Since(started) >= 45*time.Second {
			require.Greater(t, minimumExtension, int64(15000), "actual Redis expiry must advance through successful heartbeat renewal")
		}
		require.Empty(t, e.redisStrings(t, "ZRANGE", waitingKey, "0", "-1"))
		require.Equal(t, "2|2|0", e.sql(t, `SELECT count(*)||'|'||count(*) FILTER(WHERE status='admitted')||'|'||(SELECT count(*) FROM subscription_quota_ledger WHERE request_row_id>`+beforeRequest+`) FROM subscription_quota_requests WHERE id>`+beforeRequest))
		require.Equal(t, beforeUsed.StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.ATerm)))
		checks++
		require.Positive(t, received[0].Load())
		require.Positive(t, received[1].Load())
		calls, active = e.providerState(t)
		require.Equal(t, beforeCalls+2, calls)
		require.Equal(t, 2, active)
		if checks == 1 || time.Since(started) >= 15*time.Minute {
			// A third actual HTTP request is cancelled while waiting. It must
			// never reach the synthetic upstream or create a third admission.
			probeCtx, stop := context.WithTimeout(context.Background(), time.Second)
			probe, _ := e.heldClaudeRequest(probeCtx, e.AKey2)
			if probe != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(probe.Body, 1<<20))
				_ = probe.Body.Close()
			}
			stop()
			// Queued streaming requests may legally have already emitted an
			// HTTP200 keepalive. Provider calls and durable admissions, rather
			// than that transport status, prove that the third request did not send.
			require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", waitingKey, "0", "-1")) == 0 }, 4*time.Second, 100*time.Millisecond)
			require.Equal(t, "2", e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest))
			calls, active = e.providerState(t)
			require.Equal(t, beforeCalls+2, calls)
			require.Equal(t, 2, active)
		}
		t.Logf("T68 actual HTTP SSE elapsed=%s active=2 waiting=0 original_UUIDs=true minimum_expiry_extension_ms=%d client_bytes=%d,%d last_client_receive_age_ms=%d,%d no_debit_before_known=true", time.Since(started).Round(time.Second), minimumExtension, received[0].Load(), received[1].Load(), (time.Now().UnixNano()-lastReceived[0].Load())/int64(time.Millisecond), (time.Now().UnixNano()-lastReceived[1].Load())/int64(time.Millisecond))
	}
	require.Greater(t, time.Since(started), 15*time.Minute)
	e.providerGate(t, false)
	for i := 0; i < 2; i++ {
		select {
		case result := <-done:
			require.NoError(t, result.err)
			require.Contains(t, string(result.raw), "message_stop")
		case <-time.After(10 * time.Second):
			t.Fatal("synthetic provider release did not complete actual stream")
		}
	}
	require.Eventually(t, func() bool {
		return e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest+` AND status='settled' AND usage_log_recorded_at IS NOT NULL`) == "2"
	}, 15*time.Second, 100*time.Millisecond)
	require.Equal(t, beforeUsed.Add(decimal.RequireFromString("0.00120000")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.ATerm)))
	require.Empty(t, e.redisStrings(t, "ZRANGE", activeKey, "0", "-1"))
	require.Equal(t, otherCard, e.sql(t, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM subscription_quota_terms t WHERE id=%d`, e.BTerm)))
	require.Equal(t, money, e.fingerprint(t, "ordinary_and_GPT"))
	t.Logf("T68 actual long-stream PASS elapsed=%s checks=%d two_keys_one_pool=2 exact_debit=0.00120000 two_known_settled_ack=true other_wallets_unchanged=true", time.Since(started), checks)
}

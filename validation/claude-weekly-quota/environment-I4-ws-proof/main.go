// Standalone synthetic runtime proof; no production endpoints or credentials.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type fixture struct {
	URL, URL2, Postgres, Database, Redis, Provider, ImageID, Source string
	AKey, AKey2, GPTKey                                             string
	A, ATerm, Account, GPTAccount, GPTCycle                         int64
}

var f fixture
var ctx context.Context

func check(ok bool, reason string) {
	if !ok {
		panic(reason)
	}
}
func docker(input string, args ...string) string {
	c := exec.CommandContext(ctx, "C:/Program Files/Docker/Docker/resources/bin/docker.exe", append([]string{"--host", "npipe:////./pipe/docker_engine"}, args...)...)
	if input != "" {
		c.Stdin = strings.NewReader(input)
	}
	b, err := c.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("task Docker %s failed: %v", args[0], err))
	}
	return strings.TrimSpace(string(b))
}
func sql(s string) string {
	return docker(s, "exec", "-i", f.Postgres, "psql", "-U", "postgres", "-d", f.Database, "-X", "-A", "-t", "-q", "-v", "ON_ERROR_STOP=1")
}

type call struct {
	Sequence  int    `json:"sequence"`
	Model     string `json:"model"`
	Active    bool   `json:"active"`
	Cancelled bool   `json:"cancelled"`
}

func calls() []call {
	var c []call
	check(json.Unmarshal([]byte(docker("", "exec", f.Provider, "wget", "-q", "-O", "-", "http://127.0.0.1:8081/__audit")), &c) == nil, "audit JSON")
	return c
}
func control(s string) {
	docker("", "exec", f.Provider, "wget", "-q", "-O", "-", "--header", "Content-Type: application/json", "--post-data", s, "http://127.0.0.1:8081/__control")
}
func eventually(d time.Duration, fn func() bool, reason string) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic(reason)
}
func clauseFingerprint() string {
	var s []string
	for _, table := range []string{"subscription_quota_terms", "subscription_quota_requests", "subscription_quota_ledger", "subscription_quota_operations", "user_subscriptions"} {
		s = append(s, fmt.Sprintf(`SELECT '%s',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM %s x;`, table, table))
	}
	s = append(s, fmt.Sprintf(`SELECT 'Claude_accounts',md5(jsonb_agg(to_jsonb(x))::text) FROM accounts x WHERE id=%d; SELECT 'Claude_keys',md5(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text) FROM api_keys x JOIN groups g ON g.id=x.group_id WHERE g.platform='anthropic';`, f.Account))
	return sql(strings.Join(s, "\n"))
}
func moneyFingerprint() string {
	return sql(`SELECT 'users',md5(jsonb_agg(to_jsonb(x) ORDER BY id)::text) FROM users x; SELECT 'redeem',md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id)::text,'[]')) FROM redeem_codes x;`)
}

func main() {
	if len(os.Args) != 3 {
		panic("private synthetic fixture and public receipt path required")
	}
	raw, err := os.ReadFile(os.Args[1])
	check(err == nil, "read private fixture")
	check(json.Unmarshal(raw, &f) == nil, "fixture JSON")
	check(strings.HasPrefix(f.URL2, "http://127.0.0.1:") && strings.HasPrefix(f.Provider, ""), "dedicated loopback target required")
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := time.Now().UTC()
	check(sql(`SELECT count(*) FROM carpool_billing_requests;`) == "0", "dedicated WS fixture must have no prior billing requests")
	money := moneyFingerprint()
	beforeUsed := sql(fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, f.ATerm))
	control(`{"hold":true}`)
	defer control(`{"hold":false}`)
	done := make(chan error, 2)
	for _, key := range []string{f.AKey, f.AKey2} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.URL+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"isolated WS coexistence"}]}`))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		resp, err := (&http.Client{}).Do(req)
		check(err == nil && resp.StatusCode == 200, "held Claude request")
		go func() {
			b, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err == nil && !strings.Contains(string(b), "message_stop") {
				err = fmt.Errorf("missing Claude completion")
			}
			done <- err
		}()
	}
	active := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", f.A)
	legacy := fmt.Sprintf("concurrency:user:%d", f.A)
	check(docker("", "exec", f.Redis, "redis-cli", "ZCARD", active) == "2", "two active Claude leases")
	claude := clauseFingerprint()
	headers := http.Header{"Authorization": []string{"Bearer " + f.GPTKey}}
	ws, resp, err := websocket.DefaultDialer.DialContext(ctx, strings.Replace(f.URL2, "http://", "ws://", 1)+"/v1/responses", headers)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	check(err == nil, "real GPT websocket upgrade")
	defer ws.Close()
	var observed []map[string]any
	for turn := 1; turn <= 2; turn++ {
		before := len(calls())
		check(ws.WriteJSON(map[string]any{"type": "response.create", "model": "gpt-5.2", "max_output_tokens": 64, "input": fmt.Sprintf("synthetic websocket turn %d", turn)}) == nil, "WS response.create")
		var seq int
		eventually(12*time.Second, func() bool {
			c := calls()
			if len(c) != before+1 {
				return false
			}
			last := c[len(c)-1]
			seq = last.Sequence
			return last.Active && last.Model == "gpt-5.2"
		}, "actual GPT HTTP bridge did not reach synthetic provider")
		check(docker("", "exec", f.Redis, "redis-cli", "ZCARD", legacy) == "1", "each WS turn must acquire original GPT slot")
		check(docker("", "exec", f.Redis, "redis-cli", "ZCARD", active) == "2", "WS cannot consume or clear Claude slots")
		control(fmt.Sprintf(`{"release_sequence":%d}`, seq))
		_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
		events := []string{}
		for {
			_, b, err := ws.ReadMessage()
			check(err == nil, "WS actual response event read")
			var event map[string]any
			check(json.Unmarshal(b, &event) == nil, "WS event JSON")
			kind, _ := event["type"].(string)
			events = append(events, kind)
			check(kind != "error", "WS returned an error event")
			if kind == "response.completed" {
				break
			}
		}
		eventually(12*time.Second, func() bool {
			return sql(`SELECT count(*) FROM carpool_billing_requests WHERE status='settled';`) == strconv.Itoa(turn)
		}, "original GPT WS settlement")
		eventually(5*time.Second, func() bool { return docker("", "exec", f.Redis, "redis-cli", "ZCARD", legacy) == "0" }, "original GPT turn release")
		check(clauseFingerprint() == claude, "GPT WS turn changed a Claude fact or wallet")
		observed = append(observed, map[string]any{"turn": turn, "synthetic_provider_sequence": seq, "events": events, "GPT_slot_during_turn": 1, "Claude_slots_during_turn": 2, "GPT_slot_after_turn": 0, "Claude_all_fact_fingerprint_unchanged": true})
	}
	_ = ws.Close()
	// Linux cannot supply Darwin-only Live attestation. This is an actual
	// authenticated route rejection proof, never a claimed successful Live call.
	beforeLive := len(calls())
	live, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.URL2+"/v1/live", strings.NewReader(`{"model":"gpt-5.2"}`))
	live.Header.Set("Authorization", "Bearer "+f.GPTKey)
	live.Header.Set("Content-Type", "application/json")
	liveResp, err := (&http.Client{}).Do(live)
	check(err == nil, "actual Live route returned")
	_, _ = io.Copy(io.Discard, io.LimitReader(liveResp.Body, 1<<20))
	_ = liveResp.Body.Close()
	check(liveResp.StatusCode >= 400, "Linux synthetic Live must reject")
	check(len(calls()) == beforeLive, "Live reject must not call provider")
	control(`{"hold":false}`)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			check(err == nil, "Claude stream completion")
		case <-time.After(10 * time.Second):
			panic("Claude stream completion timeout")
		}
	}
	eventually(15*time.Second, func() bool {
		return sql(`SELECT count(*) FROM subscription_quota_requests WHERE status='settled' AND usage_log_recorded_at IS NOT NULL;`) == "2"
	}, "Claude original-card settlement")
	check(sql(fmt.Sprintf(`SELECT (used_usd-%s::numeric)::numeric(20,8)::text FROM subscription_quota_terms WHERE id=%d`, beforeUsed, f.ATerm)) == "0.00120000", "only original Claude card charged twice")
	check(money == moneyFingerprint(), "users/native balance or redeem changed")
	check(docker("", "exec", f.Redis, "redis-cli", "ZCARD", active) == "0", "Claude leases released")
	result := map[string]any{"stage": "I4 actual GPT WS HTTP-bridge two turns with Claude concurrent", "pass": true, "synthetic_only": true, "external_upstream_called": false, "image_id": f.ImageID, "image_source_sha256": f.Source, "started_utc": started.Format(time.RFC3339Nano), "finished_utc": time.Now().UTC().Format(time.RFC3339Nano), "configuration_differences": []string{"dedicated mode_router_v2_enabled=true", "synthetic OpenAI apikey account http_bridge"}, "turns": observed, "actual_Live_route_status": liveResp.StatusCode, "Live_success_claimed": false, "Live_provider_calls": 0, "Claude_debit": "0.00120000", "users_native_balance_unchanged": true, "driver_file": "environment-I4-ws-proof/main.go"}
	b, _ := json.MarshalIndent(result, "", "  ")
	check(os.WriteFile(os.Args[2], b, 0600) == nil, "write public proof")
	fmt.Printf("actual WS proof pass image=%s receipt=%s\n", f.ImageID, filepath.Base(os.Args[2]))
}

// The upgrade probe reuses the source-runtime mock protocol and exercises only
// fresh synthetic fixtures in the explicitly named, internal candidate runtime.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	_ "github.com/lib/pq"
)

const appURL = "http://upgrade026-app:8080"
const mockURL = "http://carpool-mock:8090"

type credentials struct {
	Source  string                           `json:"source"`
	BaseURL string                           `json:"base_url"`
	Admin   struct{ Email, Password string } `json:"admin"`
}

type report struct {
	Source     string           `json:"source"`
	RunID      string           `json:"run_id"`
	Stage      string           `json:"stage"`
	Passed     bool             `json:"passed"`
	Checks     []string         `json:"checks"`
	IDs        map[string]int64 `json:"synthetic_ids"`
	Before     *evidence        `json:"before,omitempty"`
	AfterHTTP  *evidence        `json:"after_http,omitempty"`
	AfterWS    *evidence        `json:"after_websocket,omitempty"`
	MockBefore map[string]any   `json:"mock_stats_before,omitempty"`
	Mock       map[string]any   `json:"mock_stats,omitempty"`
}

type evidence struct {
	Balance            string `json:"ordinary_balance"`
	Receipts           int64  `json:"billing_requests"`
	Settled            int64  `json:"settled_receipts"`
	DistinctRequestIDs int64  `json:"distinct_request_ids"`
	DurableSnapshots   int64  `json:"valid_durable_snapshots"`
	Cost               string `json:"receipt_cost"`
	LedgerRows         int64  `json:"usage_ledger_rows"`
	LedgerDelta        string `json:"usage_ledger_delta"`
	UsageRows          int64  `json:"usage_log_rows"`
	UsageCost          string `json:"usage_actual_cost"`
	ScopedUsageRows    int64  `json:"usage_rows_with_correct_carpool_scope"`
}

type runner struct {
	ctx                                      context.Context
	client                                   *http.Client
	db                                       *sql.DB
	private                                  string
	report                                   report
	adminToken, userToken, key, model, email string
}

func save(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}

func (r *runner) request(base, method, path, token string, body any, expected int) (any, error) {
	if base != appURL && base != mockURL {
		return nil, errors.New("untrusted_base")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(r.ctx, method, base+path, reader)
	if err != nil {
		return nil, errors.New("request_build")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method != http.MethodGet {
		req.Header.Set("Idempotency-Key", "upgrade026-"+r.report.RunID+"-"+r.report.Stage)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errors.New("request_failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, errors.New("response_read")
	}
	if resp.StatusCode != expected {
		_ = os.WriteFile(filepath.Join(r.private, "probe-last-error.private.json"), raw, 0600)
		return nil, fmt.Errorf("unexpected_http_status_%d", resp.StatusCode)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("response_json")
	}
	if obj, ok := value.(map[string]any); ok {
		if data, exists := obj["data"]; exists {
			return data, nil
		}
	}
	return value, nil
}

func obj(value any) map[string]any { result, _ := value.(map[string]any); return result }
func number(value any) int64       { result, _ := value.(float64); return int64(result) }
func text(value any) string        { result, _ := value.(string); return result }

func (r *runner) api(method, path, token string, body any, expected int) (map[string]any, error) {
	value, err := r.request(appURL, method, path, token, body, expected)
	if err != nil {
		return nil, err
	}
	result := obj(value)
	if result == nil {
		return nil, errors.New("expected_object")
	}
	return result, nil
}

func (r *runner) step(name string) { r.report.Stage = name; fmt.Println("PROBE " + name) }
func (r *runner) pass(name string) { r.report.Checks = append(r.report.Checks, name) }

func (r *runner) setup(c credentials) error {
	r.step("login_admin")
	data, err := r.api("POST", "/api/v1/auth/login", "", map[string]any{"email": c.Admin.Email, "password": c.Admin.Password}, 200)
	if err != nil {
		return err
	}
	r.adminToken = text(data["access_token"])
	if r.adminToken == "" || text(obj(data["user"])["email"]) != c.Admin.Email {
		return errors.New("admin_identity")
	}
	r.step("fresh_standard_group")
	data, err = r.api("POST", "/api/v1/admin/groups", r.adminToken, map[string]any{
		"name": "upgrade026-standard-" + r.report.RunID, "description": "Synthetic key bootstrap only",
		"platform": "openai", "rate_multiplier": 1, "is_exclusive": true, "subscription_type": "standard",
	}, 200)
	if err != nil {
		return err
	}
	r.report.IDs["standard_group"] = number(data["id"])
	r.step("fresh_user")
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		return err
	}
	password := hex.EncodeToString(passwordBytes)
	r.email = "upgrade026-probe-" + r.report.RunID + "@example.invalid"
	data, err = r.api("POST", "/api/v1/admin/users", r.adminToken, map[string]any{
		"email": r.email, "password": password, "username": "Synthetic upgrade probe", "notes": "Synthetic candidate only",
		"role": "user", "balance": 47.25, "concurrency": 8, "rpm_limit": 0, "allowed_groups": []int64{r.report.IDs["standard_group"]}, "restrict_public_groups": true,
	}, 200)
	if err != nil {
		return err
	}
	r.report.IDs["user"] = number(data["id"])
	r.step("login_user")
	data, err = r.api("POST", "/api/v1/auth/login", "", map[string]any{"email": r.email, "password": password}, 200)
	if err != nil {
		return err
	}
	r.userToken = text(data["access_token"])
	if r.userToken == "" || number(obj(data["user"])["id"]) != r.report.IDs["user"] {
		return errors.New("user_identity")
	}
	r.step("fresh_group")
	r.model = "upgrade026-" + r.report.RunID
	data, err = r.api("POST", "/api/v1/admin/groups", r.adminToken, map[string]any{
		"name": "upgrade026-probe-" + r.report.RunID, "description": "Synthetic local HTTP and WS probe", "platform": "openai",
		"rate_multiplier": 1, "is_exclusive": true, "subscription_type": "carpool",
		"model_pricing": []any{map[string]any{"platform": "openai", "models": []string{r.model}, "billing_mode": "per_request", "per_request_price": 1}},
	}, 200)
	if err != nil {
		return err
	}
	r.report.IDs["group"] = number(data["id"])
	r.step("fresh_mock_account")
	data, err = r.api("POST", "/api/v1/admin/accounts", r.adminToken, map[string]any{
		"name": "upgrade026-mock-" + r.report.RunID, "platform": "openai", "type": "apikey",
		"credentials": map[string]any{"api_key": "synthetic-" + r.report.RunID + "-1", "base_url": mockURL, "model_mapping": map[string]string{r.model: r.model}},
		"extra": map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    "passthrough",
			// The simple mock intentionally does not implement tool calls used by
			// automatic capability probing. Explicitly choose its actual Responses
			// endpoint so this test covers native forwarding, not CC conversion.
			"openai_responses_mode": "force_responses",
		},
		"concurrency": 8, "priority": 100, "rate_multiplier": 1, "group_ids": []int64{r.report.IDs["group"]}, "upstream_billing_probe_enabled": false,
	}, 200)
	if err != nil {
		return err
	}
	if nested := obj(data["account"]); nested != nil {
		data = nested
	}
	r.report.IDs["account"] = number(data["id"])
	r.step("fresh_api_key")
	data, err = r.api("POST", "/api/v1/keys", r.userToken, map[string]any{"name": "upgrade026-probe-" + r.report.RunID, "group_id": r.report.IDs["standard_group"]}, 200)
	if err != nil {
		return err
	}
	r.key = text(data["key"])
	r.report.IDs["api_key"] = number(data["id"])
	if r.key == "" {
		return errors.New("missing_key")
	}
	if err := save(filepath.Join(r.private, "probe-fixture-"+r.report.RunID+".private.json"), map[string]any{"source": "synthetic-local-only", "email": r.email, "password": password, "api_key": r.key, "ids": r.report.IDs}); err != nil {
		return err
	}
	r.step("bind_carpool_key")
	if _, err = r.api("PUT", fmt.Sprintf("/api/v1/admin/api-keys/%d", r.report.IDs["api_key"]), r.adminToken, map[string]any{"group_id": r.report.IDs["group"]}, 200); err != nil {
		return err
	}
	r.step("find_plan")
	plans, err := r.request(appURL, "GET", "/api/v1/admin/carpool/plans", r.adminToken, nil, 200)
	if err != nil {
		return err
	}
	var planID int64
	if list, ok := plans.([]any); ok {
		for _, value := range list {
			item := obj(value)
			if text(item["code"]) == "four_seat" {
				planID = number(item["plan_id"])
			}
		}
	}
	if planID <= 0 {
		return errors.New("fixed_plan_missing")
	}
	r.step("open_carpool_term")
	data, err = r.api("POST", fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", r.report.IDs["user"]), r.adminToken, map[string]any{
		"plan_id": planID, "group_id": r.report.IDs["group"], "starts_at": nil, "mode": "new", "takeover": nil, "notes": "synthetic runtime probe", "payment": nil,
	}, 201)
	if err != nil {
		return err
	}
	r.report.IDs["term"] = number(data["id"])
	r.report.IDs["cycle"] = number(obj(data["current_cycle"])["id"])
	for _, id := range r.report.IDs {
		if id <= 0 {
			return errors.New("fixture_id_missing")
		}
	}
	r.step("configure_mock")
	if _, err := r.request(mockURL, "POST", "/__control", "", map[string]any{"mode": "named", "name": r.model, "input_tokens": 10, "output_tokens": 5, "failures": 0}, 200); err != nil {
		return err
	}
	stats, err := r.request(mockURL, "GET", "/__stats", "", nil, 200)
	if err != nil {
		return err
	}
	r.report.MockBefore = obj(obj(stats)["stats"])
	r.pass("fresh_api_created_user_group_account_key_term")
	return nil
}

func (r *runner) evidence() (*evidence, error) {
	e := &evidence{}
	uid, key, term, cycle, group := r.report.IDs["user"], r.report.IDs["api_key"], r.report.IDs["term"], r.report.IDs["cycle"], r.report.IDs["group"]
	if uid <= 0 || !strings.HasPrefix(r.email, "upgrade026-probe-") || !strings.HasSuffix(r.email, "@example.invalid") {
		return nil, errors.New("unsafe_scope")
	}
	if err := r.db.QueryRowContext(r.ctx, `SELECT balance::text FROM users WHERE id=$1 AND email=$2`, uid, r.email).Scan(&e.Balance); err != nil {
		return nil, errors.New("balance_query")
	}
	if err := r.db.QueryRowContext(r.ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE status='settled'),COUNT(DISTINCT request_id),COALESCE(SUM(actual_cost_usd),0)::text,
	 COUNT(*) FILTER(WHERE billing_payload IS NOT NULL AND billing_payload->>'version'='1'
	 AND (billing_payload #>> '{command,carpool_snapshot,billing_request_id}')::bigint=id
	 AND billing_payload #>> '{command,carpool_snapshot,request_id}'=request_id
	 AND (billing_payload #>> '{command,carpool_snapshot,user_id}')::bigint=$1
	 AND (billing_payload #>> '{command,carpool_snapshot,api_key_id}')::bigint=$2
	 AND (billing_payload #>> '{command,carpool_snapshot,term_id}')::bigint=$3
	 AND (billing_payload #>> '{command,carpool_snapshot,cycle_id}')::bigint=$4
	 AND (billing_payload #>> '{command,carpool_snapshot,group_id}')::bigint=$5
	 AND (billing_payload #>> '{command,carpool_cost}')::numeric=actual_cost_usd)
	 FROM carpool_billing_requests WHERE user_id=$1 AND api_key_id=$2 AND term_id=$3 AND cycle_id=$4 AND group_id=$5`, uid, key, term, cycle, group).Scan(&e.Receipts, &e.Settled, &e.DistinctRequestIDs, &e.Cost, &e.DurableSnapshots); err != nil {
		return nil, errors.New("receipt_query")
	}
	if err := r.db.QueryRowContext(r.ctx, `SELECT COUNT(*),COALESCE(SUM(delta_usd),0)::text FROM carpool_ledger WHERE user_id=$1 AND term_id=$2 AND cycle_id=$3 AND event_type='usage'`, uid, term, cycle).Scan(&e.LedgerRows, &e.LedgerDelta); err != nil {
		return nil, errors.New("ledger_query")
	}
	if err := r.db.QueryRowContext(r.ctx, `SELECT COUNT(*),COALESCE(SUM(actual_cost),0)::text,COUNT(*) FILTER(WHERE carpool_term_id=$3 AND carpool_cycle_id=$4 AND group_id=$5 AND account_id=$6) FROM usage_logs WHERE user_id=$1 AND api_key_id=$2`, uid, key, term, cycle, group, r.report.IDs["account"]).Scan(&e.UsageRows, &e.UsageCost, &e.ScopedUsageRows); err != nil {
		return nil, errors.New("usage_query")
	}
	return e, nil
}

func decimalEqual(a, b string) bool {
	x, okX := new(big.Rat).SetString(a)
	y, okY := new(big.Rat).SetString(b)
	return okX && okY && x.Cmp(y) == 0
}

func validate(e *evidence, n int64) bool {
	return decimalEqual(e.Balance, "47.25") && e.Receipts == n && e.Settled == n && e.DistinctRequestIDs == n && e.DurableSnapshots == n && e.LedgerRows == n && e.UsageRows == n && e.ScopedUsageRows == n && decimalEqual(e.Cost, fmt.Sprint(n)) && decimalEqual(e.LedgerDelta, fmt.Sprint(-n)) && decimalEqual(e.UsageCost, fmt.Sprint(n))
}

func (r *runner) waitEvidence(n int64) (*evidence, error) {
	deadline := time.Now().Add(10 * time.Second)
	var e *evidence
	for time.Now().Before(deadline) {
		var err error
		e, err = r.evidence()
		if err != nil {
			return e, err
		}
		if validate(e, n) {
			return e, nil
		}
		select {
		case <-r.ctx.Done():
			return e, errors.New("timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return e, errors.New("billing_evidence_mismatch")
}

func (r *runner) turn(conn *coderws.Conn, previous string) (string, error) {
	payload := map[string]any{"type": "response.create", "model": r.model, "input": "synthetic upgrade compatibility"}
	if previous != "" {
		payload["previous_response_id"] = previous
	}
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	if err := conn.Write(ctx, coderws.MessageText, raw); err != nil {
		return "", errors.New("ws_write")
	}
	for seen := 0; seen < 64; seen++ {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return "", errors.New("ws_read")
		}
		var event map[string]any
		if json.Unmarshal(raw, &event) != nil {
			return "", errors.New("ws_json")
		}
		switch text(event["type"]) {
		case "response.completed":
			response := obj(event["response"])
			if text(response["status"]) != "completed" || text(response["model"]) != r.model {
				return "", errors.New("ws_completion_identity")
			}
			id := text(response["id"])
			if id == "" {
				return "", errors.New("ws_missing_id")
			}
			return id, nil
		case "error", "response.failed", "response.incomplete":
			_ = os.WriteFile(filepath.Join(r.private, "probe-last-error.private.json"), raw, 0600)
			return "", errors.New("ws_terminal_failure")
		}
	}
	return "", errors.New("ws_terminal_missing")
}

func (r *runner) run(c credentials) error {
	if err := r.setup(c); err != nil {
		return err
	}
	r.step("baseline_finances")
	var err error
	r.report.Before, err = r.evidence()
	if err != nil {
		return err
	}
	before := r.report.Before
	if !decimalEqual(before.Balance, "47.25") || before.Receipts != 0 || before.LedgerRows != 0 || before.UsageRows != 0 {
		return errors.New("dirty_fixture_baseline")
	}
	r.step("http_responses")
	data, err := r.api("POST", "/v1/responses", r.key, map[string]any{"model": r.model, "input": "synthetic upgrade HTTP compatibility", "stream": false}, 200)
	if err != nil {
		return err
	}
	if text(data["status"]) != "completed" || text(data["model"]) != r.model || text(data["id"]) == "" {
		return errors.New("http_completion_identity")
	}
	r.report.AfterHTTP, err = r.waitEvidence(1)
	if err != nil {
		return err
	}
	r.pass("http_response_one_durable_receipt_one_ledger_one_usage_ordinary_balance_unchanged")
	r.step("websocket_two_turns")
	header := http.Header{}
	header.Set("Authorization", "Bearer "+r.key)
	conn, resp, err := coderws.Dial(r.ctx, "ws://upgrade026-app:8080/v1/responses", &coderws.DialOptions{HTTPHeader: header, HTTPClient: r.client})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return errors.New("ws_dial")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	first, err := r.turn(conn, "")
	if err != nil {
		return err
	}
	second, err := r.turn(conn, first)
	if err != nil {
		return err
	}
	if first == second {
		return errors.New("ws_duplicate_response_id")
	}
	r.report.AfterWS, err = r.waitEvidence(3)
	if err != nil {
		return err
	}
	r.pass("same_websocket_two_independent_turn_receipts_and_debits_ordinary_balance_unchanged")
	_ = conn.Close(coderws.StatusNormalClosure, "synthetic acceptance finished")
	r.step("upstream_count")
	value, err := r.request(mockURL, "GET", "/__stats", "", nil, 200)
	if err != nil {
		return err
	}
	stats := obj(obj(value)["stats"])
	r.report.Mock = stats
	// Account background probes and WebSocket pool prewarming may create other
	// requests/connections. Compare this run's unique model after its control
	// setup; the mock's account outcome counters count only WebSocket turns.
	beforeMock := r.report.MockBefore
	inferences := number(obj(stats["by_case"])[r.model]) - number(obj(beforeMock["by_case"])[r.model])
	wsOutcomes := obj(obj(stats["by_case_synthetic_account_outcome"])[r.model])
	beforeWSOutcomes := obj(obj(beforeMock["by_case_synthetic_account_outcome"])[r.model])
	wsTurns := number(wsOutcomes["account_1:200"]) - number(beforeWSOutcomes["account_1:200"])
	if inferences != 3 || wsTurns != 2 {
		return errors.New("mock_forward_count")
	}
	responses := number(obj(stats["by_endpoint"])["/v1/responses"]) - number(obj(beforeMock["by_endpoint"])["/v1/responses"])
	chat := number(obj(stats["by_endpoint"])["/v1/chat/completions"]) - number(obj(beforeMock["by_endpoint"])["/v1/chat/completions"])
	if responses != 3 || chat != 0 {
		return errors.New("native_responses_endpoint_count")
	}
	r.pass("unique_model_received_exactly_one_http_and_two_websocket_turns")
	r.pass("native_responses_upstream_endpoint_three_inferences_no_chat_conversion")
	r.step("complete")
	r.report.Passed = true
	return nil
}

func readDB(envPath string) (*sql.DB, error) {
	file, err := os.Open(envPath)
	if err != nil {
		return nil, errors.New("db_env_open")
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		k, v, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[k] = v
		}
	}
	if scanner.Err() != nil || values["POSTGRES_DB"] != "upgrade026" || values["POSTGRES_USER"] != "upgrade026" || values["POSTGRES_PASSWORD"] == "" {
		return nil, errors.New("db_contract")
	}
	dsn := url.URL{Scheme: "postgres", Host: "sub2api-upgrade026-app-postgres:5432", Path: "upgrade026", User: url.UserPassword("upgrade026", values["POSTGRES_PASSWORD"])}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	query.Set("application_name", "upgrade026_http_ws_probe")
	query.Set("options", "-c default_transaction_read_only=on")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	if err != nil {
		return nil, errors.New("db_open")
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func main() {
	private := flag.String("private-dir", "/private", "mounted private runtime directory")
	flag.Parse()
	raw, err := os.ReadFile(filepath.Join(*private, "app-credentials.private.json"))
	var c credentials
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Source != "synthetic-local-only" || c.BaseURL != "http://127.0.0.1:38626" || c.Admin.Email != "upgrade026-admin@example.invalid" || c.Admin.Password == "" {
		fmt.Println("PROBE invalid_private_credentials")
		os.Exit(2)
	}
	db, err := readDB(filepath.Join(*private, "postgres.private.env"))
	if err != nil {
		fmt.Println("PROBE invalid_database_configuration")
		os.Exit(2)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var readOnly, name, user string
	if db.QueryRowContext(ctx, "SELECT current_setting('default_transaction_read_only'),current_database(),current_user").Scan(&readOnly, &name, &user) != nil || readOnly != "on" || name != "upgrade026" || user != "upgrade026" {
		fmt.Println("PROBE unsafe_database")
		os.Exit(2)
	}
	id := make([]byte, 8)
	if _, err = rand.Read(id); err != nil {
		os.Exit(2)
	}
	r := &runner{ctx: ctx, db: db, private: *private, report: report{Source: "fresh-synthetic-local-only", RunID: hex.EncodeToString(id), IDs: map[string]int64{}}, client: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil}}}
	err = r.run(c)
	if writeErr := save(filepath.Join(*private, "http-ws-probe-report.json"), r.report); writeErr != nil {
		fmt.Println("PROBE report_write_failed")
		os.Exit(2)
	}
	if err != nil {
		fmt.Printf("PROBE FAIL stage=%s category=%s\n", r.report.Stage, err.Error())
		os.Exit(1)
	}
	fmt.Printf("PROBE PASS checks=%d receipts=3 ledger_debits=3 ordinary_balance=47.25000000\n", len(r.report.Checks))
}

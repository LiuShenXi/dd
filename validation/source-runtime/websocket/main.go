package main

import (
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
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	_ "github.com/lib/pq"
)

const (
	fixtureSource      = "synthetic-local-only"
	applicationBaseURL = "http://carpool-app:8080"
	websocketURL       = "ws://carpool-app:8080/v1/responses"
	mockBaseURL        = "http://carpool-mock:8090"
	trustedDBHost      = "carpool-db"
	trustedDBPort      = "5432"
	trustedDBName      = "carpool_test"
	trustedDBUser      = "carpool_test"
	maxJSONBytes       = 4 << 20
	passwordBytes      = 24
	boundaryLead       = 8 * time.Second
)

var failureCategoryPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var scenarioNames = []string{"two_turn", "expiry", "failover", "missing_usage"}

type fixtureFile struct {
	Version int           `json:"version"`
	Source  string        `json:"source"`
	BaseURL string        `json:"base_url"`
	MockURL string        `json:"mock_url"`
	Users   []fixtureUser `json:"users"`
}

type fixtureUser struct {
	Name     string `json:"name"`
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type assertion struct {
	Name     string           `json:"name"`
	Status   string           `json:"status"`
	Category string           `json:"category,omitempty"`
	Numbers  map[string]int64 `json:"numbers,omitempty"`
}

type report struct {
	Version    int         `json:"version"`
	Source     string      `json:"source"`
	Assertions []assertion `json:"assertions"`
	Passed     int         `json:"passed"`
	Failed     int         `json:"failed"`
}

type apiResponse struct {
	Status int
	Header http.Header
	Data   any
}

type apiClient struct {
	baseURL string
	client  *http.Client
}

type runUser struct {
	fixtureUser
	Token   string `json:"-"`
	Key     string `json:"-"`
	KeyID   int64  `json:"api_key_id"`
	TermID  int64  `json:"term_id"`
	CycleID int64  `json:"cycle_id"`
}

type runSecrets struct {
	Version int       `json:"version"`
	Source  string    `json:"source"`
	RunID   string    `json:"run_id"`
	Users   []runUser `json:"users"`
}

type dbConfig struct {
	Host, Port, Name, User, Password string
}

type billingRow struct {
	ID, CycleID, APIKeyID int64
	RequestID, Status     string
	AdmittedAt            time.Time
	ActualCost            sql.NullString
	HasPayload            bool
	Payload               string
	LastError             sql.NullString
}

type billingEvidence struct {
	Rows            []billingRow
	UsageLedgerRows int64
	UsageDelta      string
	UsageLogAccount sql.NullInt64
}

type mockCaseStats struct {
	Requests        int64
	AccountOutcomes map[string]int64
}

type durableReceipt struct {
	Version int `json:"version"`
	Command struct {
		AccountID       int64  `json:"account_id"`
		CarpoolCost     string `json:"carpool_cost"`
		CarpoolSnapshot *struct {
			BillingRequestID int64     `json:"billing_request_id"`
			RequestID        string    `json:"request_id"`
			UserID           int64     `json:"user_id"`
			APIKeyID         int64     `json:"api_key_id"`
			GroupID          int64     `json:"group_id"`
			TermID           int64     `json:"term_id"`
			CycleID          int64     `json:"cycle_id"`
			AdmittedAt       time.Time `json:"admitted_at"`
		} `json:"carpool_snapshot"`
	} `json:"command"`
}

type runner struct {
	api, mock   *apiClient
	db          *sql.DB
	admin       fixtureUser
	adminToken  string
	runID       string
	users       map[string]*runUser
	models      map[string]string
	groupID     int64
	accountIDs  []int64
	planID      int64
	report      report
	secretsPath string
}

func newAPIClient(base, allowed string) (*apiClient, error) {
	if base != allowed {
		return nil, errors.New("invalid_base_url")
	}
	return &apiClient{baseURL: base, client: &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (a *apiClient) do(ctx context.Context, method, path, bearer, idempotency string, body any) (*apiResponse, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errors.New("encode_request")
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return nil, errors.New("build_request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if idempotency != "" {
		req.Header.Set("Idempotency-Key", idempotency)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.New("request_failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil || len(raw) > maxJSONBytes {
		return nil, errors.New("read_response")
	}
	result := &apiResponse{Status: resp.StatusCode, Header: resp.Header.Clone()}
	if len(bytes.TrimSpace(raw)) == 0 {
		return result, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("decode_response")
	}
	if envelope, ok := decoded.(map[string]any); ok {
		if data, exists := envelope["data"]; exists {
			result.Data = data
		} else {
			result.Data = envelope
		}
	} else {
		result.Data = decoded
	}
	return result, nil
}

func loadFixture(path string) (fixtureFile, fixtureUser, error) {
	file, err := os.Open(path)
	if err != nil {
		return fixtureFile{}, fixtureUser{}, errors.New("open_fixture")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	if err != nil || len(raw) > maxJSONBytes {
		return fixtureFile{}, fixtureUser{}, errors.New("read_fixture")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var fixture fixtureFile
	if err := decoder.Decode(&fixture); err != nil {
		return fixtureFile{}, fixtureUser{}, errors.New("decode_fixture")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fixtureFile{}, fixtureUser{}, errors.New("trailing_fixture_data")
	}
	admin, err := validateFixture(fixture)
	return fixture, admin, err
}

func validateFixture(f fixtureFile) (fixtureUser, error) {
	if f.Version != 1 || f.Source != fixtureSource || f.BaseURL != applicationBaseURL || f.MockURL != mockBaseURL {
		return fixtureUser{}, errors.New("invalid_fixture_contract")
	}
	var admin fixtureUser
	adminCount := 0
	for _, user := range f.Users {
		if user.Name != "admin" {
			continue
		}
		adminCount++
		admin = user
	}
	password, decodeErr := hex.DecodeString(admin.Password)
	if adminCount != 1 || admin.ID <= 0 || admin.Email != "carpool-test-admin@example.invalid" || decodeErr != nil || len(password) != passwordBytes {
		return fixtureUser{}, errors.New("invalid_bootstrap_admin")
	}
	return admin, nil
}

func loadDBConfig() (dbConfig, error) {
	cfg := dbConfig{
		Host: os.Getenv("CARPOOL_WS_DB_HOST"), Port: os.Getenv("CARPOOL_WS_DB_PORT"),
		Name: os.Getenv("CARPOOL_WS_DB_NAME"), User: os.Getenv("CARPOOL_WS_DB_USER"), Password: os.Getenv("CARPOOL_WS_DB_PASSWORD"),
	}
	if cfg.Host != trustedDBHost || cfg.Port != trustedDBPort || cfg.Name != trustedDBName || cfg.User != trustedDBUser || cfg.Password == "" {
		return dbConfig{}, errors.New("invalid_database_contract")
	}
	return cfg, nil
}

func openReadOnlyDB(ctx context.Context, cfg dbConfig) (*sql.DB, error) {
	dsn := &url.URL{Scheme: "postgres", Host: cfg.Host + ":" + cfg.Port, Path: cfg.Name, User: url.UserPassword(cfg.User, cfg.Password)}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	query.Set("application_name", "carpool_ws_acceptance")
	query.Set("options", "-c default_transaction_read_only=on")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	if err != nil {
		return nil, errors.New("open_database")
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "SET default_transaction_read_only = on"); err != nil {
		db.Close()
		return nil, errors.New("enforce_read_only")
	}
	var readOnly, databaseName, databaseUser string
	if err = db.QueryRowContext(ctx, "SELECT current_setting('default_transaction_read_only'),current_database(),current_user").Scan(&readOnly, &databaseName, &databaseUser); err != nil || readOnly != "on" || databaseName != trustedDBName || databaseUser != trustedDBUser {
		db.Close()
		return nil, errors.New("verify_read_only_database_identity")
	}
	return db, nil
}

func (r *runner) record(name string, err error, numbers map[string]int64) error {
	status := "pass"
	category := ""
	if err != nil {
		status = "fail"
		category = failureCategory(err)
		r.report.Failed++
	} else {
		r.report.Passed++
	}
	r.report.Assertions = append(r.report.Assertions, assertion{Name: name, Status: status, Category: category, Numbers: numbers})
	return err
}

func failureCategory(err error) string {
	if err == nil {
		return ""
	}
	var closeErr coderws.CloseError
	if errors.As(err, &closeErr) {
		switch {
		case closeErr.Code == coderws.StatusTryAgainLater && strings.Contains(strings.ToLower(closeErr.Reason), "carpool admission failed"):
			return "websocket_admission_denied"
		case closeErr.Code == coderws.StatusTryAgainLater:
			return "websocket_retryable_close"
		case closeErr.Code == coderws.StatusPolicyViolation:
			return "websocket_policy_close"
		default:
			return "websocket_transport_close"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if failureCategoryPattern.MatchString(err.Error()) {
		return err.Error()
	}
	return "acceptance_step_failed"
}

func expectStatus(resp *apiResponse, allowed ...int) error {
	if resp == nil {
		return errors.New("missing_response")
	}
	for _, status := range allowed {
		if resp.Status == status {
			return nil
		}
	}
	return errors.New("unexpected_status")
}

func object(value any) (map[string]any, error) {
	result, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected_object")
	}
	return result, nil
}

func array(value any) ([]any, error) {
	result, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected_array")
	}
	return result, nil
}

func stringField(value map[string]any, key string) (string, error) {
	result, ok := value[key].(string)
	if !ok || result == "" {
		return "", errors.New("missing_string")
	}
	return result, nil
}

func intField(value map[string]any, key string) (int64, error) {
	switch number := value[key].(type) {
	case json.Number:
		return number.Int64()
	case float64:
		return int64(number), nil
	default:
		return 0, errors.New("missing_number")
	}
}

func timeField(value map[string]any, key string) (time.Time, error) {
	raw, err := stringField(value, key)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("invalid_time")
	}
	return parsed, nil
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func syntheticWSAccountExtra() map[string]any {
	return map[string]any{
		"openai_apikey_responses_websockets_v2_enabled": true,
		"openai_apikey_responses_websockets_v2_mode":    "passthrough",
	}
}

func (r *runner) login(ctx context.Context, user *runUser) error {
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/auth/login", "", "", map[string]any{"email": user.Email, "password": user.Password})
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, mapErr := object(resp.Data)
		if mapErr != nil {
			err = mapErr
		} else {
			user.Token, err = stringField(data, "access_token")
			if actual, ok := data["user"].(map[string]any); err == nil && ok {
				id, idErr := intField(actual, "id")
				if idErr != nil || id != user.ID || actual["email"] != user.Email {
					err = errors.New("login_identity_mismatch")
				}
			} else if err == nil {
				err = errors.New("login_identity_missing")
			}
		}
	}
	return err
}

func (r *runner) bootstrap(ctx context.Context) error {
	admin := &runUser{fixtureUser: r.admin}
	if err := r.login(ctx, admin); err != nil {
		return r.record("setup.validated_bootstrap_admin", err, nil)
	}
	r.adminToken = admin.Token
	r.record("setup.validated_bootstrap_admin", nil, nil)
	for _, name := range scenarioNames {
		password, err := randomHex(passwordBytes)
		if err != nil {
			return r.record("setup.fresh_synthetic_users", err, nil)
		}
		email := "carpool-ws-" + r.runID + "-" + strings.ReplaceAll(name, "_", "-") + "@example.invalid"
		resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/users", r.adminToken, "", map[string]any{
			"email": email, "password": password, "username": "Carpool WS " + name,
			"notes": "Synthetic WebSocket acceptance " + r.runID, "role": "user", "balance": 0,
			"concurrency": 8, "rpm_limit": 0, "allowed_groups": []int64{}, "restrict_public_groups": true,
		})
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		if err != nil {
			return r.record("setup.fresh_synthetic_users", err, nil)
		}
		data, err := object(resp.Data)
		if err != nil {
			return r.record("setup.fresh_synthetic_users", err, nil)
		}
		id, err := intField(data, "id")
		if err != nil || data["email"] != email || id <= 0 {
			return r.record("setup.fresh_synthetic_users", errors.New("created_user_mismatch"), nil)
		}
		r.users[name] = &runUser{fixtureUser: fixtureUser{Name: name, ID: id, Email: email, Password: password}}
	}
	if err := r.writeSecrets(); err != nil {
		return r.record("setup.fresh_synthetic_users", err, nil)
	}
	if err := r.record("setup.fresh_synthetic_users", nil, map[string]int64{"users": int64(len(r.users))}); err != nil {
		return err
	}
	for _, name := range scenarioNames {
		if err := r.login(ctx, r.users[name]); err != nil {
			return r.record("setup.synthetic_user_logins", err, nil)
		}
	}
	return r.record("setup.synthetic_user_logins", nil, map[string]int64{"users": int64(len(r.users))})
}

func (r *runner) createInfrastructure(ctx context.Context) error {
	models := make([]string, 0, len(r.models))
	modelMapping := make(map[string]string, len(r.models))
	for _, model := range r.models {
		models = append(models, model)
		modelMapping[model] = model
	}
	sort.Strings(models)
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/groups", r.adminToken, "ws-group-"+r.runID, map[string]any{
		"name": "carpool-ws-" + r.runID, "description": "synthetic websocket acceptance", "platform": "openai",
		"rate_multiplier": 1, "is_exclusive": true, "subscription_type": "carpool",
		"model_pricing": []any{map[string]any{"platform": "openai", "models": models, "billing_mode": "per_request", "per_request_price": 1}},
	})
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, mapErr := object(resp.Data)
		if mapErr != nil {
			err = mapErr
		} else {
			r.groupID, err = intField(data, "id")
		}
	}
	if err != nil || r.groupID <= 0 {
		return r.record("setup.synthetic_group", errors.New("group_setup_failed"), nil)
	}
	r.record("setup.synthetic_group", nil, nil)

	for index := 0; index < 2; index++ {
		resp, err = r.api.do(ctx, http.MethodPost, "/api/v1/admin/accounts", r.adminToken, fmt.Sprintf("ws-account-%d-%s", index, r.runID), map[string]any{
			"name": fmt.Sprintf("carpool-ws-upstream-%d-%s", index+1, r.runID), "platform": "openai", "type": "apikey",
			"credentials": map[string]any{"api_key": fmt.Sprintf("synthetic-%s-%d", r.runID, index+1), "base_url": mockBaseURL, "model_mapping": modelMapping},
			"extra":       syntheticWSAccountExtra(),
			"concurrency": 8, "priority": 100, "rate_multiplier": 1, "group_ids": []int64{r.groupID}, "upstream_billing_probe_enabled": false,
		})
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		if err != nil {
			return r.record("setup.two_mock_backed_accounts", err, nil)
		}
		data, mapErr := object(resp.Data)
		if nested, ok := data["account"].(map[string]any); ok {
			data = nested
		}
		id, idErr := intField(data, "id")
		if mapErr != nil || idErr != nil || id <= 0 {
			return r.record("setup.two_mock_backed_accounts", errors.New("account_setup_failed"), nil)
		}
		r.accountIDs = append(r.accountIDs, id)
	}
	if r.accountIDs[0] == r.accountIDs[1] {
		return r.record("setup.two_mock_backed_accounts", errors.New("duplicate_account_id"), nil)
	}
	r.record("setup.two_mock_backed_accounts", nil, map[string]int64{"accounts": 2})

	for _, name := range scenarioNames {
		user := r.users[name]
		resp, err = r.api.do(ctx, http.MethodPost, "/api/v1/keys", user.Token, "ws-key-"+name+"-"+r.runID, map[string]any{"name": "ws-" + r.runID + "-" + name})
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		if err == nil {
			data, mapErr := object(resp.Data)
			if mapErr != nil {
				err = mapErr
			} else {
				user.KeyID, err = intField(data, "id")
				if err == nil {
					user.Key, err = stringField(data, "key")
				}
			}
		}
		if err != nil {
			return r.record("setup.synthetic_keys", err, nil)
		}
		resp, err = r.api.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/api-keys/%d", user.KeyID), r.adminToken, "ws-bind-"+name+"-"+r.runID, map[string]any{"group_id": r.groupID})
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		if err != nil {
			return r.record("setup.synthetic_keys", err, nil)
		}
	}
	if err := r.writeSecrets(); err != nil {
		return r.record("setup.synthetic_keys", err, nil)
	}
	r.record("setup.synthetic_keys", nil, map[string]int64{"keys": int64(len(r.users))})

	resp, err = r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/plans", r.adminToken, "", nil)
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		plans, listErr := array(resp.Data)
		if listErr != nil {
			err = listErr
		} else {
			for _, raw := range plans {
				plan, _ := object(raw)
				if plan["code"] == "four_seat" {
					r.planID, err = intField(plan, "plan_id")
				}
			}
		}
	}
	if err != nil || r.planID <= 0 {
		return r.record("setup.fixed_plan", errors.New("plan_not_found"), nil)
	}
	return r.record("setup.fixed_plan", nil, nil)
}

func (r *runner) writeSecrets() error {
	users := make([]runUser, 0, len(r.users))
	for _, name := range scenarioNames {
		if user := r.users[name]; user != nil {
			users = append(users, *user)
		}
	}
	return writePrivateJSON(r.secretsPath, runSecrets{Version: 1, Source: fixtureSource, RunID: r.runID, Users: users}, true)
}

func (r *runner) configureMock(ctx context.Context, name string, values map[string]any) error {
	values["mode"] = "named"
	values["name"] = r.models[name]
	resp, err := r.mock.do(ctx, http.MethodPost, "/__control", "", "", values)
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	return err
}

func (r *runner) serverNow(ctx context.Context, name string) (time.Time, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/user/carpool/details", r.users[name].Token, "", nil)
	if err != nil {
		return time.Time{}, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return time.Time{}, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return time.Time{}, err
	}
	return timeField(data, "server_now")
}

func (r *runner) openTerm(ctx context.Context, name string, startsAt *time.Time) (time.Time, error) {
	user := r.users[name]
	resp, err := r.api.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", user.ID), r.adminToken, "ws-open-"+name+"-"+r.runID, map[string]any{
		"plan_id": r.planID, "group_id": r.groupID, "starts_at": startsAt, "mode": "new", "takeover": nil,
		"notes": "synthetic websocket acceptance " + r.runID, "payment": nil,
	})
	if err == nil {
		err = expectStatus(resp, http.StatusCreated)
	}
	if err != nil {
		return time.Time{}, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return time.Time{}, err
	}
	user.TermID, err = intField(data, "id")
	if err != nil || data["user_id"] == nil {
		return time.Time{}, errors.New("term_identity_missing")
	}
	current, err := object(data["current_cycle"])
	if err != nil {
		return time.Time{}, err
	}
	user.CycleID, err = intField(current, "id")
	if err != nil {
		return time.Time{}, err
	}
	if err := r.writeSecrets(); err != nil {
		return time.Time{}, err
	}
	if name == "expiry" {
		return timeField(data, "expires_at")
	}
	return timeField(current, "ends_at")
}

func (r *runner) profileBalance(ctx context.Context, name string) (string, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/user/profile", r.users[name].Token, "", nil)
	if err != nil {
		return "", err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return "", err
	}
	data, err := object(resp.Data)
	if err != nil {
		return "", err
	}
	switch value := data["balance"].(type) {
	case string:
		return value, nil
	case json.Number:
		return value.String(), nil
	default:
		return "", errors.New("balance_missing")
	}
}

func (r *runner) mockStats(ctx context.Context, model string) (mockCaseStats, error) {
	resp, err := r.mock.do(ctx, http.MethodGet, "/__stats", "", "", nil)
	if err != nil {
		return mockCaseStats{}, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return mockCaseStats{}, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return mockCaseStats{}, err
	}
	stats, err := object(data["stats"])
	if err != nil {
		return mockCaseStats{}, err
	}
	byCase, err := object(stats["by_case"])
	if err != nil {
		return mockCaseStats{}, err
	}
	result := mockCaseStats{AccountOutcomes: map[string]int64{}}
	if _, ok := byCase[model]; !ok {
		return result, nil
	}
	result.Requests, err = intField(byCase, model)
	if err != nil {
		return mockCaseStats{}, err
	}
	byOutcomeCase, err := object(stats["by_case_synthetic_account_outcome"])
	if err != nil {
		return mockCaseStats{}, err
	}
	caseOutcomesValue, ok := byOutcomeCase[model]
	if !ok {
		return result, nil
	}
	caseOutcomes, err := object(caseOutcomesValue)
	if err != nil {
		return mockCaseStats{}, err
	}
	for _, label := range []string{"account_1:429", "account_1:200", "account_2:429", "account_2:200"} {
		if _, exists := caseOutcomes[label]; !exists {
			continue
		}
		result.AccountOutcomes[label], err = intField(caseOutcomes, label)
		if err != nil {
			return mockCaseStats{}, err
		}
	}
	return result, nil
}

func dialWS(ctx context.Context, key string) (*coderws.Conn, error) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+key)
	conn, resp, err := coderws.Dial(ctx, websocketURL, &coderws.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, errors.New("websocket_dial_failed")
	}
	conn.SetReadLimit(maxJSONBytes)
	return conn, nil
}

func sendTurn(ctx context.Context, conn *coderws.Conn, model string) (map[string]any, error) {
	payload, err := json.Marshal(map[string]any{"type": "response.create", "model": model, "input": "synthetic websocket acceptance"})
	if err != nil {
		return nil, err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = conn.Write(writeCtx, coderws.MessageText, payload)
	cancel()
	if err != nil {
		return nil, err
	}
	return readTerminal(ctx, conn)
}

func readTerminal(ctx context.Context, conn *coderws.Conn) (map[string]any, error) {
	for seen := 0; seen < 64; seen++ {
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		messageType, payload, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return nil, err
		}
		if messageType != coderws.MessageText && messageType != coderws.MessageBinary {
			continue
		}
		var event map[string]any
		if len(payload) > maxJSONBytes || json.Unmarshal(payload, &event) != nil {
			return nil, errors.New("invalid_websocket_event")
		}
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.completed":
			return event, nil
		case "error":
			return event, errors.New("websocket_error_event")
		case "response.failed":
			return event, errors.New("websocket_response_failed")
		case "response.incomplete", "response.cancelled", "response.canceled":
			return event, errors.New("websocket_noncompleted_terminal")
		}
	}
	return nil, errors.New("terminal_event_missing")
}

func validateAdmissionDenied(err error) error {
	if err == nil {
		return errors.New("admission_succeeded")
	}
	var closeErr coderws.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != coderws.StatusTryAgainLater || !strings.Contains(strings.ToLower(closeErr.Reason), "carpool admission failed") {
		return errors.New("unexpected_websocket_denial")
	}
	return nil
}

func (r *runner) billingEvidence(ctx context.Context, name string) (billingEvidence, error) {
	user := r.users[name]
	prefix := "carpool-ws-" + r.runID + "-"
	if !strings.HasPrefix(user.Email, prefix) || !strings.HasSuffix(user.Email, "@example.invalid") || user.ID <= 0 || user.KeyID <= 0 || user.TermID <= 0 {
		return billingEvidence{}, errors.New("unsafe_evidence_scope")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT b.id,b.cycle_id,b.api_key_id,b.request_id,b.status,b.admitted_at,b.actual_cost_usd::text,
		       b.billing_payload IS NOT NULL,COALESCE(b.billing_payload,'{}'::jsonb)::text,b.last_error
		FROM carpool_billing_requests b
		JOIN users u ON u.id=b.user_id
		WHERE b.user_id=$1 AND u.email=$2 AND u.email LIKE $3 AND b.api_key_id=$4 AND b.term_id=$5
		ORDER BY b.id`, user.ID, user.Email, prefix+"%@example.invalid", user.KeyID, user.TermID)
	if err != nil {
		return billingEvidence{}, err
	}
	defer rows.Close()
	evidence := billingEvidence{}
	for rows.Next() {
		var row billingRow
		if err := rows.Scan(&row.ID, &row.CycleID, &row.APIKeyID, &row.RequestID, &row.Status, &row.AdmittedAt, &row.ActualCost, &row.HasPayload, &row.Payload, &row.LastError); err != nil {
			return billingEvidence{}, err
		}
		evidence.Rows = append(evidence.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return billingEvidence{}, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*),COALESCE(SUM(l.delta_usd),0)::text
		FROM carpool_ledger l JOIN users u ON u.id=l.user_id
		WHERE l.user_id=$1 AND u.email=$2 AND u.email LIKE $3 AND l.term_id=$4 AND l.event_type='usage'`,
		user.ID, user.Email, prefix+"%@example.invalid", user.TermID).Scan(&evidence.UsageLedgerRows, &evidence.UsageDelta); err != nil {
		return billingEvidence{}, err
	}
	if len(evidence.Rows) > 0 {
		_ = r.db.QueryRowContext(ctx, `
			SELECT account_id FROM usage_logs
			WHERE user_id=$1 AND api_key_id=$2 AND carpool_term_id=$3
			ORDER BY id DESC LIMIT 1`, user.ID, user.KeyID, user.TermID).Scan(&evidence.UsageLogAccount)
	}
	return evidence, nil
}

func (r *runner) waitEvidence(ctx context.Context, name string, validate func(billingEvidence) error) (billingEvidence, error) {
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last billingEvidence
	var lastErr error
	for {
		last, lastErr = r.billingEvidence(ctx, name)
		if lastErr == nil {
			lastErr = validate(last)
			if lastErr == nil {
				return last, nil
			}
		}
		select {
		case <-ctx.Done():
			return billingEvidence{}, ctx.Err()
		case <-deadline.C:
			return last, lastErr
		case <-ticker.C:
		}
	}
}

func (r *runner) validateSettled(name string, e billingEvidence, wantRows int, cycleID int64) error {
	if len(e.Rows) != wantRows || e.UsageLedgerRows != int64(wantRows) || e.UsageDelta != fmt.Sprintf("-%d.00000000", wantRows) {
		return errors.New("billing_counts_wrong")
	}
	seen := map[string]struct{}{}
	for _, row := range e.Rows {
		if row.Status != "settled" || !row.ActualCost.Valid || row.ActualCost.String != "1.00000000" || !row.HasPayload || row.CycleID != cycleID || row.RequestID == "" {
			return errors.New("receipt_not_durable_and_settled")
		}
		if _, duplicate := seen[row.RequestID]; duplicate {
			return errors.New("duplicate_request_id")
		}
		seen[row.RequestID] = struct{}{}
		var receipt durableReceipt
		if json.Unmarshal([]byte(row.Payload), &receipt) != nil || receipt.Version != 1 || receipt.Command.CarpoolCost != "1.00000000" || receipt.Command.AccountID <= 0 || receipt.Command.CarpoolSnapshot == nil {
			return errors.New("durable_receipt_invalid")
		}
		snapshot := receipt.Command.CarpoolSnapshot
		user := r.users[name]
		if snapshot.BillingRequestID != row.ID || snapshot.RequestID != row.RequestID || snapshot.UserID != user.ID || snapshot.APIKeyID != user.KeyID || snapshot.GroupID != r.groupID || snapshot.TermID != user.TermID || snapshot.CycleID != row.CycleID || !snapshot.AdmittedAt.Equal(row.AdmittedAt) {
			return errors.New("durable_snapshot_mismatch")
		}
	}
	return nil
}

func (r *runner) testTwoTurns(ctx context.Context) error {
	name := "two_turn"
	if err := r.configureMock(ctx, name, map[string]any{"input_tokens": 10, "output_tokens": 5, "failures": 0}); err != nil {
		return r.record("websocket.two_turns_independent_receipts_and_debits", err, nil)
	}
	if _, err := r.openTerm(ctx, name, nil); err != nil {
		return r.record("websocket.two_turns_independent_receipts_and_debits", err, nil)
	}
	beforeBalance, err := r.profileBalance(ctx, name)
	if err != nil {
		return r.record("websocket.two_turns_independent_receipts_and_debits", err, nil)
	}
	beforeMock, err := r.mockStats(ctx, r.models[name])
	if err != nil {
		return r.record("websocket.two_turns_independent_receipts_and_debits", err, nil)
	}
	conn, err := dialWS(ctx, r.users[name].Key)
	if err == nil {
		defer conn.CloseNow()
		for turn := 0; turn < 2; turn++ {
			_, err = sendTurn(ctx, conn, r.models[name])
			if err != nil {
				break
			}
		}
	}
	var evidence billingEvidence
	if err == nil {
		evidence, err = r.waitEvidence(ctx, name, func(e billingEvidence) error { return r.validateSettled(name, e, 2, r.users[name].CycleID) })
	}
	afterMock, statsErr := r.mockStats(ctx, r.models[name])
	if err == nil && (statsErr != nil || afterMock.Requests != beforeMock.Requests+2) {
		err = errors.New("two_turn_upstream_count_wrong")
	}
	afterBalance, balanceErr := r.profileBalance(ctx, name)
	if err == nil && (balanceErr != nil || beforeBalance != afterBalance) {
		err = errors.New("ordinary_balance_changed")
	}
	return r.record("websocket.two_turns_independent_receipts_and_debits", err, map[string]int64{"mock_turns": afterMock.Requests - beforeMock.Requests, "receipts": int64(len(evidence.Rows)), "usage_debits": evidence.UsageLedgerRows})
}

func (r *runner) testExpiry(ctx context.Context) error {
	name := "expiry"
	if err := r.configureMock(ctx, name, map[string]any{"input_tokens": 10, "output_tokens": 5, "failures": 0}); err != nil {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", err, nil)
	}
	serverNow, err := r.serverNow(ctx, name)
	if err != nil {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", err, nil)
	}
	start := serverNow.Add(-28*24*time.Hour + boundaryLead)
	expiresAt, err := r.openTerm(ctx, name, &start)
	if err != nil {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", err, nil)
	}
	conn, err := dialWS(ctx, r.users[name].Key)
	if err == nil {
		defer conn.CloseNow()
		_, err = sendTurn(ctx, conn, r.models[name])
	}
	if err == nil {
		_, err = r.waitEvidence(ctx, name, func(e billingEvidence) error { return r.validateSettled(name, e, 1, r.users[name].CycleID) })
	}
	if err != nil {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", err, nil)
	}
	beforeMock, err := r.mockStats(ctx, r.models[name])
	if err != nil {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", err, nil)
	}
	wait := time.Until(expiresAt.Add(time.Second))
	if wait <= 0 || wait > boundaryLead+2*time.Second {
		return r.record("websocket.later_turn_expiry_denied_before_upstream", errors.New("expiry_boundary_unavailable"), nil)
	}
	timer := time.NewTimer(wait)
	select {
	case <-ctx.Done():
		timer.Stop()
		return r.record("websocket.later_turn_expiry_denied_before_upstream", ctx.Err(), nil)
	case <-timer.C:
	}
	_, secondErr := sendTurn(ctx, conn, r.models[name])
	err = validateAdmissionDenied(secondErr)
	afterMock, statsErr := r.mockStats(ctx, r.models[name])
	evidence, evidenceErr := r.billingEvidence(ctx, name)
	if err == nil && (statsErr != nil || evidenceErr != nil || afterMock.Requests != beforeMock.Requests || len(evidence.Rows) != 1) {
		err = errors.New("expired_turn_reached_upstream_or_admitted")
	}
	return r.record("websocket.later_turn_expiry_denied_before_upstream", err, map[string]int64{"mock_before": beforeMock.Requests, "mock_after": afterMock.Requests, "receipts": int64(len(evidence.Rows))})
}

func (r *runner) testFailover(ctx context.Context) error {
	name := "failover"
	assertionName := "websocket.same_turn_failover_preserves_admitted_cycle_and_single_debit"
	serverNow, err := r.serverNow(ctx, name)
	if err != nil {
		return r.record(assertionName, err, nil)
	}
	start := serverNow.Add(-7*24*time.Hour + boundaryLead)
	cycleEndsAt, err := r.openTerm(ctx, name, &start)
	if err != nil {
		return r.record(assertionName, err, nil)
	}
	remaining := time.Until(cycleEndsAt)
	delayMS := int(math.Ceil(float64((remaining + time.Second) / time.Millisecond)))
	if remaining <= 2*time.Second || delayMS > 10_000 {
		return r.record(assertionName, errors.New("cycle_boundary_unavailable"), nil)
	}
	if err := r.configureMock(ctx, name, map[string]any{
		"input_tokens": 10, "output_tokens": 5, "status": 429, "failures": 1,
		"delay_ms": delayMS, "ws_failure_mode": "rate_limit_error",
	}); err != nil {
		return r.record(assertionName, err, nil)
	}
	beforeMock, err := r.mockStats(ctx, r.models[name])
	if err != nil {
		return r.record(assertionName, err, nil)
	}
	conn, err := dialWS(ctx, r.users[name].Key)
	if err == nil {
		defer conn.CloseNow()
		_, err = sendTurn(ctx, conn, r.models[name])
	}
	var evidence billingEvidence
	if err == nil {
		evidence, err = r.waitEvidence(ctx, name, func(e billingEvidence) error { return r.validateSettled(name, e, 1, r.users[name].CycleID) })
	}
	if err == nil && (len(evidence.Rows) != 1 || !evidence.Rows[0].AdmittedAt.Before(cycleEndsAt) || time.Now().Before(cycleEndsAt)) {
		err = errors.New("failover_did_not_span_cycle_boundary")
	}
	afterMock, statsErr := r.mockStats(ctx, r.models[name])
	if err == nil && (statsErr != nil || afterMock.Requests != beforeMock.Requests+2 || !evidence.UsageLogAccount.Valid) {
		err = errors.New("failover_attempts_or_final_account_missing")
	}
	if err == nil && evidence.UsageLogAccount.Int64 != r.accountIDs[0] && evidence.UsageLogAccount.Int64 != r.accountIDs[1] {
		err = errors.New("unexpected_final_account")
	}
	var failedAccountSlot, settledAccountSlot int64
	if err == nil {
		for slot := int64(1); slot <= 2; slot++ {
			failed := afterMock.AccountOutcomes[fmt.Sprintf("account_%d:429", slot)] - beforeMock.AccountOutcomes[fmt.Sprintf("account_%d:429", slot)]
			settled := afterMock.AccountOutcomes[fmt.Sprintf("account_%d:200", slot)] - beforeMock.AccountOutcomes[fmt.Sprintf("account_%d:200", slot)]
			if failed == 1 {
				failedAccountSlot = slot
			} else if failed != 0 {
				err = errors.New("failover_account_outcomes_wrong")
				break
			}
			if settled == 1 {
				settledAccountSlot = slot
			} else if settled != 0 {
				err = errors.New("failover_account_outcomes_wrong")
				break
			}
		}
		if err == nil && (failedAccountSlot == 0 || settledAccountSlot == 0 || failedAccountSlot == settledAccountSlot || evidence.UsageLogAccount.Int64 != r.accountIDs[settledAccountSlot-1]) {
			err = errors.New("distinct_failover_accounts_not_proven")
		}
	}
	return r.record(assertionName, err, map[string]int64{"mock_attempts": afterMock.Requests - beforeMock.Requests, "failed_account_slot": failedAccountSlot, "settled_account_slot": settledAccountSlot, "receipts": int64(len(evidence.Rows)), "usage_debits": evidence.UsageLedgerRows})
}

func (r *runner) testMissingUsage(ctx context.Context) error {
	name := "missing_usage"
	if err := r.configureMock(ctx, name, map[string]any{"input_tokens": 10, "output_tokens": 5, "failures": 0, "omit_usage": true}); err != nil {
		return r.record("websocket.missing_usage_creates_visible_exception", err, nil)
	}
	if _, err := r.openTerm(ctx, name, nil); err != nil {
		return r.record("websocket.missing_usage_creates_visible_exception", err, nil)
	}
	beforeBalance, err := r.profileBalance(ctx, name)
	if err != nil {
		return r.record("websocket.missing_usage_creates_visible_exception", err, nil)
	}
	conn, err := dialWS(ctx, r.users[name].Key)
	if err == nil {
		_, err = sendTurn(ctx, conn, r.models[name])
		// Missing usage is reconciled when the gateway connection handler exits.
		// End this isolated connection before polling the durable exception row.
		conn.CloseNow()
	}
	var evidence billingEvidence
	if err == nil {
		evidence, err = r.waitEvidence(ctx, name, func(e billingEvidence) error {
			if len(e.Rows) != 1 || e.Rows[0].Status != "reconcile_required" || e.Rows[0].ActualCost.Valid || e.Rows[0].HasPayload || !e.Rows[0].LastError.Valid || e.Rows[0].LastError.String != "usage_receipt_missing" || e.UsageLedgerRows != 0 {
				return errors.New("missing_usage_not_reconciled")
			}
			return nil
		})
	}
	if err == nil {
		path := fmt.Sprintf("/api/v1/admin/carpool/billing-exceptions?user_id=%d&term_id=%d&page_size=10", r.users[name].ID, r.users[name].TermID)
		resp, requestErr := r.api.do(ctx, http.MethodGet, path, r.adminToken, "", nil)
		if requestErr == nil {
			requestErr = expectStatus(resp, http.StatusOK)
		}
		if requestErr == nil {
			page, mapErr := object(resp.Data)
			items, listErr := array(page["items"])
			if mapErr != nil || listErr != nil || len(items) != 1 {
				requestErr = errors.New("exception_not_visible")
			} else {
				item, _ := object(items[0])
				if item["sanitized_error"] != "usage_receipt_missing" || item["status"] != "reconcile_required" {
					requestErr = errors.New("exception_not_sanitized")
				}
			}
		}
		err = requestErr
	}
	afterBalance, balanceErr := r.profileBalance(ctx, name)
	if err == nil && (balanceErr != nil || afterBalance != beforeBalance) {
		err = errors.New("ordinary_balance_changed")
	}
	return r.record("websocket.missing_usage_creates_visible_exception", err, map[string]int64{"exceptions": int64(len(evidence.Rows)), "usage_debits": evidence.UsageLedgerRows})
}

func (r *runner) run(ctx context.Context) error {
	if err := r.bootstrap(ctx); err != nil {
		return err
	}
	if err := r.createInfrastructure(ctx); err != nil {
		return err
	}
	failed := false
	for _, scenario := range []func(context.Context) error{r.testTwoTurns, r.testExpiry, r.testFailover, r.testMissingUsage} {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := scenario(ctx); err != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("scenario_failed")
	}
	return nil
}

func validateReport(value report) error {
	if value.Version != 1 || value.Source != fixtureSource || len(value.Assertions) == 0 || len(value.Assertions) != value.Passed+value.Failed {
		return errors.New("invalid_report")
	}
	seen := map[string]struct{}{}
	for _, item := range value.Assertions {
		if item.Name == "" || (item.Status != "pass" && item.Status != "fail") {
			return errors.New("invalid_assertion")
		}
		if item.Status == "pass" && item.Category != "" || item.Status == "fail" && !failureCategoryPattern.MatchString(item.Category) {
			return errors.New("invalid_assertion_category")
		}
		if _, exists := seen[item.Name]; exists {
			return errors.New("duplicate_assertion")
		}
		seen[item.Name] = struct{}{}
	}
	if value.Failed == 0 {
		for _, required := range []string{
			"websocket.two_turns_independent_receipts_and_debits", "websocket.later_turn_expiry_denied_before_upstream",
			"websocket.same_turn_failover_preserves_admitted_cycle_and_single_debit", "websocket.missing_usage_creates_visible_exception",
		} {
			found := false
			for _, item := range value.Assertions {
				found = found || item.Name == required && item.Status == "pass"
			}
			if !found {
				return errors.New("required_scenario_missing")
			}
		}
	}
	return nil
}

func writePrivateJSON(path string, value any, replace bool) error {
	if path == "" {
		return errors.New("output_path_missing")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if replace {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(temporary, flags, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !replace {
		if _, err := os.Stat(path); err == nil {
			os.Remove(temporary)
			return errors.New("output_exists")
		}
	}
	return os.Rename(temporary, path)
}

func main() {
	fixturesPath := flag.String("fixtures", "", "path to the validated bootstrap fixture")
	secretsPath := flag.String("run-fixtures", "", "path for private run-owned fixture output")
	reportPath := flag.String("report", "", "path for redacted acceptance report")
	flag.Parse()
	if *fixturesPath == "" || *secretsPath == "" || *reportPath == "" {
		fmt.Fprintln(os.Stderr, "websocket acceptance: -fixtures, -run-fixtures and -report are required")
		os.Exit(2)
	}
	absFixture, _ := filepath.Abs(*fixturesPath)
	absSecrets, _ := filepath.Abs(*secretsPath)
	absReport, _ := filepath.Abs(*reportPath)
	if absFixture == absSecrets || absFixture == absReport || absSecrets == absReport {
		fmt.Fprintln(os.Stderr, "websocket acceptance: input and output paths must be distinct")
		os.Exit(2)
	}
	fixture, admin, err := loadFixture(*fixturesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "websocket acceptance: invalid synthetic fixture")
		os.Exit(2)
	}
	dbConfig, err := loadDBConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "websocket acceptance: invalid private database contract")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, err := openReadOnlyDB(ctx, dbConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "websocket acceptance: read-only database unavailable")
		os.Exit(2)
	}
	defer db.Close()
	api, _ := newAPIClient(fixture.BaseURL, applicationBaseURL)
	mock, _ := newAPIClient(fixture.MockURL, mockBaseURL)
	runID, err := randomHex(8)
	if err != nil {
		fmt.Fprintln(os.Stderr, "websocket acceptance: random source unavailable")
		os.Exit(2)
	}
	models := map[string]string{}
	for _, name := range scenarioNames {
		models[name] = "ws-" + strings.ReplaceAll(name, "_", "-") + "-" + runID
	}
	r := &runner{
		api: api, mock: mock, db: db, admin: admin, runID: runID, users: map[string]*runUser{}, models: models,
		report: report{Version: 1, Source: fixtureSource}, secretsPath: *secretsPath,
	}
	runErr := r.run(ctx)
	if runErr != nil && r.report.Failed == 0 {
		r.record("websocket.execution", runErr, nil)
	}
	if validateErr := validateReport(r.report); validateErr != nil && runErr == nil {
		runErr = validateErr
		r.record("websocket.report_completeness", validateErr, nil)
	}
	if err := writePrivateJSON(*reportPath, r.report, false); err != nil {
		fmt.Fprintln(os.Stderr, "websocket acceptance: report write failed")
		os.Exit(2)
	}
	fmt.Printf("websocket acceptance: passed=%d failed=%d\n", r.report.Passed, r.report.Failed)
	if runErr != nil || r.report.Failed > 0 {
		os.Exit(1)
	}
}

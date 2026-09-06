package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	fixtureSource        = "synthetic-local-only"
	applicationBaseURL   = "http://carpool-app:8080"
	mockBaseURL          = "http://carpool-mock:8090"
	maxBodyBytes         = 4 << 20
	maxFixtureBytes      = 1 << 20
	fixturePasswordBytes = 24
)

var requiredFixtureNames = []string{
	"admin", "four", "three", "two", "fifth_four", "fifth_three", "fifth_two",
	"ordinary", "expired", "renewal", "termination", "takeover",
}

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

type syntheticUserSpec struct {
	Name        string
	Balance     float64
	Concurrency int
}

var syntheticUserSpecs = []syntheticUserSpec{
	{Name: "four", Concurrency: 20},
	{Name: "three", Concurrency: 20},
	{Name: "two", Concurrency: 20},
	{Name: "fifth_four", Concurrency: 20},
	{Name: "fifth_three", Concurrency: 20},
	{Name: "fifth_two", Concurrency: 20},
	{Name: "ordinary", Balance: 100, Concurrency: 20},
	{Name: "expired", Concurrency: 20},
	{Name: "renewal", Concurrency: 20},
	{Name: "termination", Concurrency: 20},
	{Name: "takeover", Balance: 25, Concurrency: 20},
}

type planExpectation struct {
	FixtureName string
	Code        string
	Name        string
	ListPrice   string
	Weekly      string
	Total       string
	Boost       string
}

var planExpectations = []planExpectation{
	{FixtureName: "four", Code: "four_seat", Name: "Four-seat", ListPrice: "330", Weekly: "550", Total: "2200", Boost: "55"},
	{FixtureName: "three", Code: "three_seat", Name: "Three-seat", ListPrice: "420", Weekly: "700", Total: "2800", Boost: "70"},
	{FixtureName: "two", Code: "two_seat", Name: "Two-seat", ListPrice: "655", Weekly: "1100", Total: "4400", Boost: "110"},
}

type assertion struct {
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	HTTPStatus int              `json:"http_status,omitempty"`
	Numbers    map[string]int64 `json:"numbers,omitempty"`
}

type report struct {
	Version      int              `json:"version"`
	Source       string           `json:"source"`
	Assertions   []assertion      `json:"assertions"`
	SyntheticIDs map[string]int64 `json:"synthetic_ids,omitempty"`
	Passed       int              `json:"passed"`
	Failed       int              `json:"failed"`
}

type apiResponse struct {
	Status      int
	ContentType string
	Header      http.Header
	Data        any
}

type apiClient struct {
	baseURL string
	client  *http.Client
}

func newAPIClient(base, allowedBase string) (*apiClient, error) {
	if base != allowedBase {
		return nil, errors.New("invalid_base_url")
	}
	return &apiClient{
		baseURL: base,
		client: &http.Client{
			Timeout: 35 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (a *apiClient) do(ctx context.Context, method, path, bearer, idempotency string, headers map[string]string, body any) (*apiResponse, error) {
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
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.New("request_failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil || len(raw) > maxBodyBytes {
		return nil, errors.New("read_response")
	}
	result := &apiResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Header: resp.Header.Clone()}
	if len(bytes.TrimSpace(raw)) == 0 || strings.Contains(strings.ToLower(result.ContentType), "text/event-stream") {
		result.Data = string(raw)
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

type runner struct {
	fixture           fixtureFile
	users             map[string]fixtureUser
	api               *apiClient
	mock              *apiClient
	runID             string
	report            report
	tokens            map[string]string
	keys              map[string]string
	keyIDs            map[string]int64
	termIDs           map[string]int64
	cycleIDs          map[string]int64
	planIDs           map[string]int64
	planVersions      map[string]int64
	models            map[string]string
	groupID           int64
	accountID         int64
	batchFixturesPath string
}

func newRunner(f fixtureFile, api, mock *apiClient, runID string) *runner {
	users := make(map[string]fixtureUser, len(f.Users))
	for _, user := range f.Users {
		users[user.Name] = user
	}
	models := map[string]string{
		"http":       "accept-http-" + runID,
		"sse":        "accept-sse-" + runID,
		"ordinary":   "accept-ordinary-" + runID,
		"expired":    "accept-expired-" + runID,
		"terminated": "accept-terminated-" + runID,
	}
	return &runner{
		fixture: f, users: users, api: api, mock: mock, runID: runID,
		report: report{Version: 1, Source: fixtureSource, SyntheticIDs: map[string]int64{}},
		tokens: map[string]string{}, keys: map[string]string{}, keyIDs: map[string]int64{},
		termIDs: map[string]int64{}, cycleIDs: map[string]int64{}, planIDs: map[string]int64{}, planVersions: map[string]int64{}, models: models,
	}
}

func (r *runner) record(name string, err error, httpStatus int, numbers map[string]int64) bool {
	status := "pass"
	if err != nil {
		status = "fail"
		r.report.Failed++
	} else {
		r.report.Passed++
	}
	r.report.Assertions = append(r.report.Assertions, assertion{Name: name, Status: status, HTTPStatus: httpStatus, Numbers: numbers})
	return err == nil
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

func validateEmptyPaginatedResult(value any) error {
	page, err := object(value)
	if err != nil {
		return err
	}
	total, err := int64Value(page, "total")
	if err != nil || total != 0 {
		return errors.New("expected_zero_total")
	}
	rawItems, present := page["items"]
	if !present {
		return errors.New("missing_items")
	}
	if rawItems == nil {
		return nil
	}
	items, err := array(rawItems)
	if err != nil || len(items) != 0 {
		return errors.New("expected_empty_items")
	}
	return nil
}

func stringValue(m map[string]any, key string) (string, error) {
	value, ok := m[key].(string)
	if !ok || value == "" {
		return "", errors.New("missing_string")
	}
	return value, nil
}

func optionalString(m map[string]any, key string) (string, error) {
	value, ok := m[key]
	if !ok || value == nil {
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", errors.New("invalid_string")
	}
	return result, nil
}

func int64Value(m map[string]any, key string) (int64, error) {
	value, ok := m[key]
	if !ok {
		return 0, errors.New("missing_number")
	}
	switch number := value.(type) {
	case json.Number:
		return number.Int64()
	case float64:
		return int64(number), nil
	case int64:
		return number, nil
	default:
		return 0, errors.New("invalid_number")
	}
}

func decimalValue(value any) (*big.Rat, error) {
	var text string
	switch current := value.(type) {
	case string:
		text = current
	case json.Number:
		text = current.String()
	case float64:
		text = strconv.FormatFloat(current, 'f', -1, 64)
	default:
		return nil, errors.New("invalid_decimal")
	}
	result, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, errors.New("invalid_decimal")
	}
	return result, nil
}

func decimalField(m map[string]any, key string) (*big.Rat, error) {
	value, ok := m[key]
	if !ok {
		return nil, errors.New("missing_decimal")
	}
	return decimalValue(value)
}

func decimalStringField(m map[string]any, key string) (*big.Rat, error) {
	value, err := stringValue(m, key)
	if err != nil {
		return nil, errors.New("decimal_not_string")
	}
	return decimalValue(value)
}

func timeField(m map[string]any, key string) (time.Time, error) {
	value, ok := m[key].(string)
	if !ok || value == "" {
		return time.Time{}, errors.New("missing_time")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("invalid_time")
	}
	return parsed, nil
}

func optionalTimeField(m map[string]any, key string) (*time.Time, error) {
	value, ok := m[key]
	if !ok {
		return nil, errors.New("missing_time")
	}
	if value == nil {
		return nil, nil
	}
	parsed, err := timeField(m, key)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func validateCycleWindows(cycles []any) error {
	expectedDays := []int{7, 7, 7, 7}
	if len(cycles) != len(expectedDays) {
		return errors.New("cycle_count")
	}
	var previousEnd time.Time
	for index, raw := range cycles {
		cycle, err := object(raw)
		if err != nil {
			return err
		}
		cycleNo, err := int64Value(cycle, "cycle_no")
		if err != nil || cycleNo != int64(index+1) {
			return errors.New("cycle_number")
		}
		startsAt, err := timeField(cycle, "starts_at")
		if err != nil {
			return err
		}
		endsAt, err := timeField(cycle, "ends_at")
		if err != nil || !endsAt.Equal(startsAt.Add(time.Duration(expectedDays[index])*24*time.Hour)) {
			return errors.New("cycle_duration")
		}
		if index > 0 && !startsAt.Equal(previousEnd) {
			return errors.New("cycle_gap")
		}
		previousEnd = endsAt
	}
	return nil
}

func decimalEquals(value any, expected string) bool {
	left, err := decimalValue(value)
	if err != nil {
		return false
	}
	right, ok := new(big.Rat).SetString(expected)
	return ok && left.Cmp(right) == 0
}

func sameJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func exactKeys(m map[string]any, allowed ...string) bool {
	if len(m) != len(allowed) {
		return false
	}
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range m {
		if _, ok := set[key]; !ok {
			return false
		}
	}
	return true
}

func validateFixture(f fixtureFile) error {
	if f.Version != 1 || f.Source != fixtureSource {
		return errors.New("invalid_fixture_contract")
	}
	if len(f.Users) != len(requiredFixtureNames) {
		return errors.New("invalid_fixture_user_count")
	}
	if _, err := newAPIClient(f.BaseURL, applicationBaseURL); err != nil {
		return err
	}
	if _, err := newAPIClient(f.MockURL, mockBaseURL); err != nil {
		return errors.New("invalid_mock_url")
	}
	byName := make(map[string]fixtureUser, len(f.Users))
	ids := make(map[int64]struct{}, len(f.Users))
	for _, user := range f.Users {
		if user.Name == "" || user.ID <= 0 || user.Email == "" || user.Password == "" {
			return errors.New("invalid_fixture_user")
		}
		password, err := hex.DecodeString(user.Password)
		if err != nil || len(password) != fixturePasswordBytes || user.Email != bootstrapFixtureEmail(user.Name) {
			return errors.New("invalid_fixture_user_provenance")
		}
		if _, exists := byName[user.Name]; exists {
			return errors.New("duplicate_fixture_name")
		}
		if _, exists := ids[user.ID]; exists {
			return errors.New("duplicate_fixture_id")
		}
		byName[user.Name] = user
		ids[user.ID] = struct{}{}
	}
	for _, name := range requiredFixtureNames {
		if _, ok := byName[name]; !ok {
			return errors.New("missing_fixture_user")
		}
	}
	return nil
}

func (r *runner) login(ctx context.Context, name string) error {
	user := r.users[name]
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/auth/login", "", "", nil, map[string]any{"email": user.Email, "password": user.Password})
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			r.tokens[name], err = stringValue(data, "access_token")
			if err == nil {
				err = validateLoginIdentity(data, user, name == "admin")
			}
		}
	}
	r.record("auth.login."+name, err, status, map[string]int64{"user_id": user.ID})
	return err
}

func validateLoginIdentity(data map[string]any, expected fixtureUser, admin bool) error {
	user, err := object(data["user"])
	if err != nil {
		return errors.New("login_user_missing")
	}
	id, err := int64Value(user, "id")
	if err != nil || id != expected.ID {
		return errors.New("login_user_id_mismatch")
	}
	email, err := stringValue(user, "email")
	if err != nil || email != expected.Email {
		return errors.New("login_user_email_mismatch")
	}
	role, err := stringValue(user, "role")
	expectedRole := "user"
	if admin {
		expectedRole = "admin"
	}
	if err != nil || role != expectedRole {
		return errors.New("login_user_role_mismatch")
	}
	return nil
}

func bootstrapFixtureEmail(name string) string {
	return "carpool-test-" + name + "@example.invalid"
}

func batchFixtureEmail(runID, name string) string {
	return "carpool-accept-" + runID + "-" + strings.ReplaceAll(name, "_", "-") + "@example.invalid"
}

func (r *runner) createSyntheticUserBatch(ctx context.Context, outputPath string) error {
	admin, ok := r.users["admin"]
	if !ok || r.tokens["admin"] == "" {
		return errors.New("trusted_admin_missing")
	}
	batch := fixtureFile{
		Version: r.fixture.Version,
		Source:  r.fixture.Source,
		BaseURL: r.fixture.BaseURL,
		MockURL: r.fixture.MockURL,
		Users:   []fixtureUser{admin},
	}
	seenIDs := map[int64]struct{}{admin.ID: {}}
	created := int64(0)
	var batchErr error
	status := 0
	for _, spec := range syntheticUserSpecs {
		password, err := randomHex(fixturePasswordBytes)
		if err != nil {
			batchErr = errors.New("generate_batch_password")
			break
		}
		email := batchFixtureEmail(r.runID, spec.Name)
		resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/users", r.tokens["admin"], "", nil, map[string]any{
			"email": email, "password": password, "username": "Carpool Acceptance " + spec.Name,
			"notes": "Synthetic local carpool acceptance batch " + r.runID, "role": "user",
			"balance": spec.Balance, "concurrency": spec.Concurrency, "rpm_limit": 0,
			"allowed_groups": []int64{}, "restrict_public_groups": true,
		})
		if resp != nil {
			status = resp.Status
		}
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		var user fixtureUser
		if err == nil {
			data, objectErr := object(resp.Data)
			if objectErr != nil {
				err = objectErr
			} else {
				user, err = createdFixtureUser(spec, email, password, data, seenIDs)
			}
		}
		if err != nil {
			batchErr = err
			break
		}
		seenIDs[user.ID] = struct{}{}
		batch.Users = append(batch.Users, user)
		created++
	}
	if batchErr == nil {
		batchErr = writeBatchFixture(outputPath, batch)
	}
	if batchErr == nil {
		r.fixture = batch
		r.users = make(map[string]fixtureUser, len(batch.Users))
		for _, user := range batch.Users {
			r.users[user.Name] = user
		}
	}
	r.record("setup.fresh_synthetic_user_batch", batchErr, status, map[string]int64{"created_users": created})
	return batchErr
}

func createdFixtureUser(spec syntheticUserSpec, email, password string, data map[string]any, seenIDs map[int64]struct{}) (fixtureUser, error) {
	id, err := int64Value(data, "id")
	if err != nil || id <= 0 {
		return fixtureUser{}, errors.New("created_user_id")
	}
	if _, exists := seenIDs[id]; exists {
		return fixtureUser{}, errors.New("created_user_id_reused")
	}
	actualEmail, err := stringValue(data, "email")
	if err != nil || actualEmail != email {
		return fixtureUser{}, errors.New("created_user_email")
	}
	role, err := stringValue(data, "role")
	if err != nil || role != "user" {
		return fixtureUser{}, errors.New("created_user_role")
	}
	status, err := stringValue(data, "status")
	if err != nil || status != "active" {
		return fixtureUser{}, errors.New("created_user_status")
	}
	concurrency, err := int64Value(data, "concurrency")
	if err != nil || concurrency != int64(spec.Concurrency) {
		return fixtureUser{}, errors.New("created_user_concurrency")
	}
	balance, err := decimalField(data, "balance")
	if err != nil || balance.Cmp(new(big.Rat).SetFloat64(spec.Balance)) != 0 {
		return fixtureUser{}, errors.New("created_user_balance")
	}
	return fixtureUser{Name: spec.Name, ID: id, Email: email, Password: password}, nil
}

func (r *runner) createInfrastructure(ctx context.Context) error {
	modelNames := make([]string, 0, len(r.models))
	for _, model := range r.models {
		modelNames = append(modelNames, model)
	}
	sort.Strings(modelNames)
	groupBody := map[string]any{
		"name": "carpool-accept-" + r.runID, "description": "synthetic local acceptance",
		"platform": "openai", "rate_multiplier": 1, "is_exclusive": true, "subscription_type": "carpool",
		"model_pricing": []any{map[string]any{
			"platform": "openai", "models": modelNames, "billing_mode": "per_request", "per_request_price": 1,
		}},
	}
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/groups", r.tokens["admin"], "accept-group-"+r.runID, nil, groupBody)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			r.groupID, err = int64Value(data, "id")
		}
	}
	r.record("setup.synthetic_carpool_group", err, status, map[string]int64{"group_id": r.groupID})
	if err != nil {
		return err
	}
	r.report.SyntheticIDs["group_id"] = r.groupID

	accountBody := map[string]any{
		"name": "carpool-accept-upstream-" + r.runID, "platform": "openai", "type": "apikey",
		"credentials": map[string]any{"api_key": "synthetic-" + r.runID, "base_url": strings.TrimRight(r.fixture.MockURL, "/")},
		"extra":       map[string]any{}, "concurrency": 8, "priority": 100, "rate_multiplier": 1,
		"group_ids": []int64{r.groupID}, "upstream_billing_probe_enabled": false,
	}
	resp, err = r.api.do(ctx, http.MethodPost, "/api/v1/admin/accounts", r.tokens["admin"], "accept-account-"+r.runID, nil, accountBody)
	status = 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			account, exists := data["account"]
			if exists {
				data, objectErr = object(account)
			}
			if objectErr == nil {
				r.accountID, err = int64Value(data, "id")
			} else {
				err = objectErr
			}
		}
	}
	r.record("setup.synthetic_upstream_account", err, status, map[string]int64{"account_id": r.accountID})
	if err == nil {
		r.report.SyntheticIDs["account_id"] = r.accountID
	}
	return err
}

func (r *runner) configureMockCases(ctx context.Context) error {
	models := make([]string, 0, len(r.models))
	for _, model := range r.models {
		models = append(models, model)
	}
	sort.Strings(models)
	configured := int64(0)
	status := 0
	var configureErr error
	for _, model := range models {
		resp, err := r.mock.do(ctx, http.MethodPost, "/__control", "", "", nil, map[string]any{
			"mode": "named", "name": model, "input_tokens": 1000, "output_tokens": 100, "failures": 0,
		})
		if resp != nil {
			status = resp.Status
		}
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		if err != nil {
			configureErr = err
			break
		}
		configured++
	}
	r.record("setup.synthetic_mock_cases", configureErr, status, map[string]int64{"configured_cases": configured})
	return configureErr
}

func (r *runner) createAndBindKey(ctx context.Context, name string) error {
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/keys", r.tokens[name], "accept-key-"+name+"-"+r.runID, nil, map[string]any{"name": "accept-" + r.runID})
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			r.keyIDs[name], err = int64Value(data, "id")
			if err == nil {
				r.keys[name], err = stringValue(data, "key")
				r.report.SyntheticIDs["api_key_"+name] = r.keyIDs[name]
			}
		}
	}
	r.record("setup.synthetic_key_create."+name, err, status, map[string]int64{"user_id": r.users[name].ID, "api_key_id": r.keyIDs[name]})
	if err != nil {
		return err
	}
	resp, err = r.api.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/api-keys/%d", r.keyIDs[name]), r.tokens["admin"], "accept-bind-"+name+"-"+r.runID, nil, map[string]any{"group_id": r.groupID})
	status = 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	r.record("setup.admin_bind_carpool_key."+name, err, status, map[string]int64{"user_id": r.users[name].ID, "api_key_id": r.keyIDs[name], "group_id": r.groupID})
	return err
}

func (r *runner) loadPlans(ctx context.Context) error {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/plans", r.tokens["admin"], "", nil, nil)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	var plans []any
	if err == nil {
		plans, err = array(resp.Data)
	}
	if err == nil && len(plans) != len(planExpectations) {
		err = errors.New("unexpected_plan_count")
	}
	if err == nil {
		for _, expected := range planExpectations {
			var matched map[string]any
			for _, raw := range plans {
				plan, currentErr := object(raw)
				if currentErr != nil {
					err = currentErr
					break
				}
				if plan["code"] == expected.Code {
					if matched != nil {
						err = errors.New("duplicate_plan_code")
						break
					}
					matched = plan
				}
			}
			if err != nil {
				break
			}
			if matched == nil {
				err = errors.New("missing_plan_code")
				break
			}
			planID, idErr := int64Value(matched, "plan_id")
			version, versionErr := int64Value(matched, "version")
			if idErr != nil || versionErr != nil || validateAdminPlan(matched, expected, planID, version) != nil {
				err = errors.New("invalid_plan_contract")
				break
			}
			r.planIDs[expected.FixtureName] = planID
			r.planVersions[expected.FixtureName] = version
		}
	}
	r.record("plans.three_fixed_tiers", err, status, map[string]int64{"plan_count": int64(len(plans))})
	return err
}

func validateAdminPlan(plan map[string]any, expected planExpectation, expectedID, expectedVersion int64) error {
	if !exactKeys(plan, "plan_id", "code", "name", "version", "list_price_cny", "weekly_quota_usd", "cycle_5_quota_usd", "duration_days", "cycle_days", "boost_ratio", "boost_amount_usd", "boost_count", "rounding_mode", "enabled", "is_latest") {
		return errors.New("admin_plan_fields")
	}
	latest, ok := plan["is_latest"].(bool)
	if !ok || !latest {
		return errors.New("admin_plan_not_latest")
	}
	snapshot := make(map[string]any, len(plan)-1)
	for key, value := range plan {
		if key != "is_latest" {
			snapshot[key] = value
		}
	}
	return validatePlanSnapshot(snapshot, expected, expectedID, expectedVersion)
}

func validatePlanSnapshot(plan map[string]any, expected planExpectation, expectedID, expectedVersion int64) error {
	if !exactKeys(plan, "plan_id", "code", "name", "version", "list_price_cny", "weekly_quota_usd", "cycle_5_quota_usd", "duration_days", "cycle_days", "boost_ratio", "boost_amount_usd", "boost_count", "rounding_mode", "enabled") {
		return errors.New("plan_fields")
	}
	planID, idErr := int64Value(plan, "plan_id")
	version, versionErr := int64Value(plan, "version")
	duration, durationErr := int64Value(plan, "duration_days")
	cycleDays, cycleErr := int64Value(plan, "cycle_days")
	boostCount, boostErr := int64Value(plan, "boost_count")
	code, codeErr := stringValue(plan, "code")
	name, nameErr := stringValue(plan, "name")
	rounding, roundingErr := stringValue(plan, "rounding_mode")
	enabled, enabledOK := plan["enabled"].(bool)
	if idErr != nil || planID <= 0 || planID != expectedID || versionErr != nil || version <= 0 || version != expectedVersion ||
		durationErr != nil || duration != 28 || cycleErr != nil || cycleDays != 7 || boostErr != nil || boostCount != 2 ||
		codeErr != nil || code != expected.Code || nameErr != nil || name != expected.Name || roundingErr != nil || rounding != "half_up" || !enabledOK || !enabled {
		return errors.New("plan_values")
	}
	for field, value := range map[string]string{
		"list_price_cny": expected.ListPrice, "weekly_quota_usd": expected.Weekly,
		"boost_ratio": "0.1", "boost_amount_usd": expected.Boost,
	} {
		if !decimalEquals(plan[field], value) {
			return errors.New("plan_decimal")
		}
	}
	if cycle5, err := decimalField(plan, "cycle_5_quota_usd"); err != nil || cycle5.Sign() < 0 {
		return errors.New("plan_legacy_cycle_decimal")
	}
	return nil
}

func (r *runner) previewPlan(ctx context.Context, expected planExpectation) error {
	path := fmt.Sprintf("/api/v1/admin/users/%d/carpool/preview", r.users[expected.FixtureName].ID)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "", nil, map[string]any{"plan_id": r.planIDs[expected.FixtureName], "starts_at": nil, "mode": "new", "takeover": nil})
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	cycleCount := int64(0)
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			cycles, _ := array(data["cycles"])
			cycleCount = int64(len(cycles))
			err = validatePreview(data, expected, r.planIDs[expected.FixtureName], r.planVersions[expected.FixtureName])
		}
	}
	r.record("preview."+expected.FixtureName+".exact_contract", err, status, map[string]int64{"user_id": r.users[expected.FixtureName].ID, "cycle_count": cycleCount})
	return err
}

func validatePreview(data map[string]any, expected planExpectation, planID, planVersion int64) error {
	if !exactKeys(data, "calculated_at", "mode", "plan", "starts_at", "expires_at", "cycles", "warnings", "ordinary_balance_deduction_usd") {
		return errors.New("preview_fields")
	}
	calculatedAt, calculatedErr := timeField(data, "calculated_at")
	startsAt, startsErr := timeField(data, "starts_at")
	expiresAt, expiresErr := timeField(data, "expires_at")
	mode, modeErr := stringValue(data, "mode")
	plan, planErr := object(data["plan"])
	cycles, cyclesErr := array(data["cycles"])
	warnings, warningsErr := array(data["warnings"])
	if calculatedErr != nil || startsErr != nil || expiresErr != nil || modeErr != nil || mode != "new" || planErr != nil || cyclesErr != nil || warningsErr != nil || len(warnings) != 0 ||
		!calculatedAt.Equal(startsAt) || !expiresAt.Equal(startsAt.Add(28*24*time.Hour)) || !decimalEquals(data["ordinary_balance_deduction_usd"], "0") {
		return errors.New("preview_values")
	}
	if err := validatePlanSnapshot(plan, expected, planID, planVersion); err != nil {
		return err
	}
	if err := validateCycleWindows(cycles); err != nil {
		return err
	}
	sum := new(big.Rat)
	for index, raw := range cycles {
		cycle, err := object(raw)
		if err != nil || !exactKeys(cycle, "cycle_no", "starts_at", "ends_at", "base_quota_usd", "initial_action") {
			return errors.New("preview_cycle_fields")
		}
		cycleStart, err := timeField(cycle, "starts_at")
		if err != nil || (index == 0 && !cycleStart.Equal(startsAt)) {
			return errors.New("preview_cycle_start")
		}
		quota, err := decimalField(cycle, "base_quota_usd")
		if err != nil {
			return err
		}
		if quota.Cmp(mustRat(expected.Weekly)) != 0 {
			return errors.New("preview_cycle_quota")
		}
		sum.Add(sum, quota)
		action, err := stringValue(cycle, "initial_action")
		expectedAction := "scheduled"
		if index == 0 {
			expectedAction = "grant"
		}
		if err != nil || action != expectedAction {
			return errors.New("preview_initial_action")
		}
	}
	if sum.Cmp(mustRat(expected.Total)) != 0 {
		return errors.New("preview_total_quota")
	}
	return nil
}

func (r *runner) testPreviewValidation(ctx context.Context) error {
	body := map[string]any{"plan_id": r.planIDs["four"], "starts_at": nil, "mode": "new", "takeover": nil}
	cases := []struct {
		name   string
		path   string
		body   map[string]any
		status int
	}{
		{name: "invalid_user_id", path: "/api/v1/admin/users/0/carpool/preview", body: body, status: http.StatusBadRequest},
		{name: "unknown_user", path: "/api/v1/admin/users/9223372036854775807/carpool/preview", body: body, status: http.StatusNotFound},
		{name: "invalid_plan_id", path: fmt.Sprintf("/api/v1/admin/users/%d/carpool/preview", r.users["four"].ID), body: map[string]any{"plan_id": 0, "starts_at": nil, "mode": "new", "takeover": nil}, status: http.StatusBadRequest},
	}
	var firstErr error
	for _, current := range cases {
		resp, err := r.api.do(ctx, http.MethodPost, current.path, r.tokens["admin"], "", nil, current.body)
		if err == nil {
			err = expectStatus(resp, current.status)
		}
		status := 0
		if resp != nil {
			status = resp.Status
		}
		r.record("preview.rejects."+current.name, err, status, nil)
		if firstErr == nil && err != nil {
			firstErr = err
		}
	}
	return firstErr
}

func mustRat(value string) *big.Rat {
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		panic("invalid constant decimal")
	}
	return result
}

func (r *runner) openTerm(ctx context.Context, fixtureName, planName string, start *time.Time, mode string, takeover any, payment any, key string) (*apiResponse, map[string]any, error) {
	body := map[string]any{"plan_id": r.planIDs[planName], "group_id": r.groupID, "starts_at": start, "mode": mode, "takeover": takeover, "notes": "synthetic acceptance", "payment": payment}
	path := fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", r.users[fixtureName].ID)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], key, nil, body)
	if err != nil {
		return resp, body, err
	}
	if err = expectStatus(resp, http.StatusCreated); err != nil {
		return resp, body, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return resp, body, err
	}
	termID, err := int64Value(data, "id")
	if err == nil {
		r.termIDs[fixtureName] = termID
		r.report.SyntheticIDs["term_"+fixtureName] = termID
		if current, ok := data["current_cycle"].(map[string]any); ok {
			r.cycleIDs[fixtureName], _ = int64Value(current, "id")
			if r.cycleIDs[fixtureName] > 0 {
				r.report.SyntheticIDs["cycle_"+fixtureName] = r.cycleIDs[fixtureName]
			}
		}
	}
	return resp, body, err
}

func (r *runner) testOpenAndIdempotency(ctx context.Context) error {
	payment := map[string]any{"amount_cny": "330.00", "payment_kind": "payment", "paid_at": time.Now().UTC().Truncate(time.Second), "channel": "manual", "external_order_no": nil, "notes": "synthetic"}
	key := "accept-open-four-" + r.runID
	path := fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", r.users["four"].ID)
	missingKeyBody := map[string]any{"plan_id": r.planIDs["four"], "group_id": r.groupID, "starts_at": nil, "mode": "new", "takeover": nil, "notes": "synthetic acceptance", "payment": payment}
	missingKey, missingKeyErr := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "", nil, missingKeyBody)
	if missingKeyErr == nil {
		missingKeyErr = expectStatus(missingKey, http.StatusBadRequest)
	}
	if missingKeyErr == nil {
		listPath := fmt.Sprintf("/api/v1/admin/carpool/terms?user_id=%d&page_size=10", r.users["four"].ID)
		list, listErr := r.api.do(ctx, http.MethodGet, listPath, r.tokens["admin"], "", nil, nil)
		if listErr == nil {
			listErr = expectStatus(list, http.StatusOK)
		}
		if listErr == nil {
			if validateEmptyPaginatedResult(list.Data) != nil {
				listErr = errors.New("missing_idempotency_key_mutated_state")
			}
		}
		missingKeyErr = listErr
	}
	missingKeyStatus := 0
	if missingKey != nil {
		missingKeyStatus = missingKey.Status
	}
	r.record("idempotency.open_missing_key_rejected_without_mutation", missingKeyErr, missingKeyStatus, map[string]int64{"user_id": r.users["four"].ID})
	if missingKeyErr != nil {
		return missingKeyErr
	}
	resp, body, err := r.openTerm(ctx, "four", "four", nil, "new", nil, payment, key)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			userID, userErr := int64Value(data, "user_id")
			scopeID, scopeErr := int64Value(data, "scope_id")
			groupID, groupErr := int64Value(data, "group_id")
			planID, planErr := int64Value(data, "plan_id")
			startsAt, startsErr := timeField(data, "starts_at")
			expiresAt, expiresErr := timeField(data, "expires_at")
			plan, snapshotErr := object(data["plan_snapshot"])
			cycles, cyclesErr := array(data["cycles"])
			payments, paymentsErr := array(data["payments"])
			current, currentErr := object(data["current_cycle"])
			if userErr != nil || userID != r.users["four"].ID || scopeErr != nil || scopeID != 1 || groupErr != nil || groupID != r.groupID || planErr != nil || planID != r.planIDs["four"] ||
				startsErr != nil || expiresErr != nil || !expiresAt.Equal(startsAt.Add(28*24*time.Hour)) || data["status"] != "active" || snapshotErr != nil ||
				validatePlanSnapshot(plan, planExpectations[0], r.planIDs["four"], r.planVersions["four"]) != nil || cyclesErr != nil || validateCycleWindows(cycles) != nil || paymentsErr != nil || len(payments) != 1 || currentErr != nil ||
				!decimalEquals(data["payment_net_cny"], "330") {
				err = errors.New("open_contract_invalid")
			} else {
				for index, raw := range cycles {
					cycle, mapErr := object(raw)
					expectedState := "scheduled"
					expectedBalance := "0"
					if index == 0 {
						expectedState = "active"
						expectedBalance = planExpectations[0].Weekly
					}
					if mapErr != nil || cycle["state"] != expectedState || !decimalEquals(cycle["base_balance_usd"], expectedBalance) || !decimalEquals(cycle["boost_balance_usd"], "0") || !decimalEquals(cycle["manual_balance_usd"], "0") {
						err = errors.New("open_cycle_contract_invalid")
						break
					}
				}
				currentID, _ := int64Value(current, "id")
				firstCycle, _ := object(cycles[0])
				firstID, _ := int64Value(firstCycle, "id")
				paymentRecord, _ := object(payments[0])
				if err == nil && (currentID <= 0 || currentID != firstID || paymentRecord["payment_kind"] != "payment" || !decimalEquals(paymentRecord["amount_cny"], "330")) {
					err = errors.New("open_current_or_payment_invalid")
				}
			}
		}
	}
	r.record("open.current_term_with_four_cycles", err, status, map[string]int64{"user_id": r.users["four"].ID, "term_id": r.termIDs["four"], "cycle_id": r.cycleIDs["four"]})
	if err != nil {
		return err
	}

	replay, replayErr := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], key, nil, body)
	if replayErr == nil {
		replayErr = expectStatus(replay, http.StatusCreated)
	}
	if replayErr == nil {
		data, mapErr := object(replay.Data)
		if mapErr != nil {
			replayErr = mapErr
		} else if id, idErr := int64Value(data, "id"); idErr != nil || id != r.termIDs["four"] {
			replayErr = errors.New("replay_changed_result")
		}
	}
	replayStatus := 0
	if replay != nil {
		replayStatus = replay.Status
	}
	r.record("idempotency.open_same_key_same_payload", replayErr, replayStatus, map[string]int64{"term_id": r.termIDs["four"]})

	conflictBody := make(map[string]any, len(body))
	for k, v := range body {
		conflictBody[k] = v
	}
	conflictBody["notes"] = "different synthetic payload"
	conflict, conflictErr := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], key, nil, conflictBody)
	if conflictErr == nil {
		conflictErr = expectStatus(conflict, http.StatusConflict)
	}
	conflictStatus := 0
	if conflict != nil {
		conflictStatus = conflict.Status
	}
	r.record("idempotency.open_same_key_conflict", conflictErr, conflictStatus, map[string]int64{"term_id": r.termIDs["four"]})
	if replayErr != nil {
		return replayErr
	}
	return conflictErr
}

func (r *runner) openSimpleTerms(ctx context.Context) error {
	for _, item := range []struct{ fixture, plan string }{{"three", "three"}, {"two", "two"}, {"renewal", "four"}, {"termination", "four"}} {
		var start *time.Time
		if item.fixture == "three" {
			distinctStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
			start = &distinctStart
		}
		resp, _, err := r.openTerm(ctx, item.fixture, item.plan, start, "new", nil, nil, "accept-open-"+item.fixture+"-"+r.runID)
		status := 0
		if resp != nil {
			status = resp.Status
		}
		r.record("open.synthetic."+item.fixture, err, status, map[string]int64{"user_id": r.users[item.fixture].ID, "term_id": r.termIDs[item.fixture], "cycle_id": r.cycleIDs[item.fixture]})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) openLateCycleAndExpired(ctx context.Context) error {
	startFourth := time.Now().UTC().Add(-22 * 24 * time.Hour).Truncate(time.Second)
	for _, item := range []struct{ fixture, plan, quota string }{{"fifth_four", "four", "550"}, {"fifth_three", "three", "700"}, {"fifth_two", "two", "1100"}} {
		resp, _, err := r.openTerm(ctx, item.fixture, item.plan, &startFourth, "new", nil, nil, "accept-open-"+item.fixture+"-"+r.runID)
		status := 0
		if resp != nil {
			status = resp.Status
		}
		if err == nil {
			data, _ := object(resp.Data)
			current, currentErr := object(data["current_cycle"])
			if currentErr != nil || !decimalEquals(current["base_quota_usd"], item.quota) {
				err = errors.New("fourth_cycle_not_active")
			}
		}
		r.record("open.fourth_cycle."+item.plan, err, status, map[string]int64{"user_id": r.users[item.fixture].ID, "term_id": r.termIDs[item.fixture], "cycle_id": r.cycleIDs[item.fixture]})
		if err != nil {
			return err
		}
	}
	startExpired := time.Now().UTC().Add(-29 * 24 * time.Hour).Truncate(time.Second)
	resp, _, err := r.openTerm(ctx, "expired", "four", &startExpired, "new", nil, nil, "accept-open-expired-"+r.runID)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	r.record("open.expired_historical_term", err, status, map[string]int64{"user_id": r.users["expired"].ID, "term_id": r.termIDs["expired"]})
	return err
}

func (r *runner) claimBoost(ctx context.Context, fixtureName, key string) (*apiResponse, map[string]any, error) {
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/user/carpool/boosts", r.tokens[fixtureName], key, nil, map[string]any{})
	if err != nil {
		return resp, nil, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return resp, nil, err
	}
	data, err := object(resp.Data)
	if err == nil {
		err = validateBoostDTO(data)
	}
	return resp, data, err
}

func (r *runner) testBoosts(ctx context.Context) error {
	quotaBefore, quotaBeforeErr := r.detailsAvailable(ctx, "four")
	if quotaBeforeErr == nil && quotaBefore.Cmp(mustRat("550")) != 0 {
		quotaBeforeErr = errors.New("initial_carpool_quota_invalid")
	}
	r.record("quota.active_cycle_before_boosts", quotaBeforeErr, http.StatusOK, map[string]int64{"user_id": r.users["four"].ID})
	if quotaBeforeErr != nil {
		return quotaBeforeErr
	}

	getResp, getErr := r.api.do(ctx, http.MethodGet, "/api/v1/user/carpool/boosts", r.tokens["four"], "", nil, nil)
	if getErr == nil {
		getErr = expectStatus(getResp, http.StatusOK)
	}
	if getErr == nil {
		data, objectErr := object(getResp.Data)
		if objectErr != nil {
			getErr = objectErr
		} else {
			getErr = validateBoostState(data, true, 2, "55", "")
		}
	}
	getStatus := 0
	if getResp != nil {
		getStatus = getResp.Status
	}
	r.record("boost.get_exact_whitelist", getErr, getStatus, map[string]int64{"user_id": r.users["four"].ID})
	if getErr != nil {
		return getErr
	}

	for _, item := range []struct{ fixture, amount string }{{"fifth_four", "55"}, {"fifth_three", "70"}, {"fifth_two", "110"}} {
		resp, data, err := r.claimBoost(ctx, item.fixture, "accept-fourth-boost-"+item.fixture+"-"+r.runID)
		status := 0
		if resp != nil {
			status = resp.Status
		}
		if err == nil {
			err = validateBoostState(data, true, 1, item.amount, "")
		}
		r.record("boost.fourth_cycle_full_amount."+item.fixture, err, status, map[string]int64{"user_id": r.users[item.fixture].ID, "term_id": r.termIDs[item.fixture]})
		if err != nil {
			return err
		}
	}

	type result struct {
		key  string
		resp *apiResponse
		data map[string]any
		err  error
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := fmt.Sprintf("accept-boost-four-%d-%s", index, r.runID)
			resp, data, err := r.claimBoost(ctx, "four", key)
			results <- result{key: key, resp: resp, data: data, err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	successes := make([]result, 0, 2)
	failures := int64(0)
	for current := range results {
		if current.err == nil {
			successes = append(successes, current)
		} else if current.resp != nil && current.resp.Status >= 400 && current.resp.Status < 500 {
			failures++
		}
	}
	err := error(nil)
	if len(successes) != 2 || failures != 2 {
		err = errors.New("boost_concurrency_violation")
	} else {
		remaining := map[int64]bool{}
		for _, success := range successes {
			value, valueErr := int64Value(success.data, "remaining")
			expectedEligible := value > 0
			expectedReason := "boosts_exhausted"
			if expectedEligible {
				expectedReason = ""
			}
			if valueErr != nil || validateBoostState(success.data, expectedEligible, value, "55", expectedReason) != nil {
				err = errors.New("boost_concurrency_result_invalid")
				break
			}
			remaining[value] = true
		}
		if err == nil && (!remaining[0] || !remaining[1] || len(remaining) != 2) {
			err = errors.New("boost_concurrency_remaining_invalid")
		}
	}
	r.record("boost.four_concurrent_claims_max_two", err, 0, map[string]int64{"user_id": r.users["four"].ID, "successes": int64(len(successes)), "failures": failures})
	if err != nil {
		return err
	}
	replayResp, replayData, replayErr := r.claimBoost(ctx, "four", successes[0].key)
	if replayErr == nil && !sameJSON(replayData, successes[0].data) {
		replayErr = errors.New("boost_replay_changed")
	}
	replayStatus := 0
	if replayResp != nil {
		replayStatus = replayResp.Status
	}
	r.record("boost.successful_replay_is_stable", replayErr, replayStatus, map[string]int64{"user_id": r.users["four"].ID, "term_id": r.termIDs["four"]})
	if replayErr != nil {
		return replayErr
	}
	finalResp, finalErr := r.api.do(ctx, http.MethodGet, "/api/v1/user/carpool/boosts", r.tokens["four"], "", nil, nil)
	if finalErr == nil {
		finalErr = expectStatus(finalResp, http.StatusOK)
	}
	if finalErr == nil {
		data, objectErr := object(finalResp.Data)
		if objectErr != nil {
			finalErr = objectErr
		} else {
			finalErr = validateBoostState(data, false, 0, "55", "boosts_exhausted")
		}
	}
	finalStatus := 0
	if finalResp != nil {
		finalStatus = finalResp.Status
	}
	r.record("boost.exhausted_exact_state", finalErr, finalStatus, map[string]int64{"user_id": r.users["four"].ID, "term_id": r.termIDs["four"]})
	if finalErr != nil {
		return finalErr
	}
	quotaAfter, quotaAfterErr := r.detailsAvailable(ctx, "four")
	if quotaAfterErr == nil && quotaAfter.Cmp(mustRat("660")) != 0 {
		quotaAfterErr = errors.New("boosted_carpool_quota_invalid")
	}
	r.record("quota.active_cycle_after_two_boosts", quotaAfterErr, http.StatusOK, map[string]int64{"user_id": r.users["four"].ID})
	return quotaAfterErr
}

func validateBoostDTO(data map[string]any) error {
	if !exactKeys(data, "eligible", "remaining", "total", "amount_usd", "help_text", "unavailable_reason") {
		return errors.New("boost_fields")
	}
	if _, ok := data["eligible"].(bool); !ok {
		return errors.New("boost_eligible")
	}
	remaining, err := int64Value(data, "remaining")
	if err != nil || remaining < 0 {
		return errors.New("boost_remaining")
	}
	total, err := int64Value(data, "total")
	if err != nil || total != 2 || remaining > total {
		return errors.New("boost_total")
	}
	if _, err := decimalField(data, "amount_usd"); err != nil {
		return errors.New("boost_amount")
	}
	if _, err := stringValue(data, "help_text"); err != nil {
		return errors.New("boost_help_text")
	}
	if _, err := optionalString(data, "unavailable_reason"); err != nil {
		return errors.New("boost_unavailable_reason")
	}
	return nil
}

func validateBoostState(data map[string]any, eligible bool, remaining int64, amount, unavailableReason string) error {
	if err := validateBoostDTO(data); err != nil {
		return err
	}
	actualEligible, _ := data["eligible"].(bool)
	actualRemaining, _ := int64Value(data, "remaining")
	reason, _ := optionalString(data, "unavailable_reason")
	if actualEligible != eligible || actualRemaining != remaining || !decimalEquals(data["amount_usd"], amount) || reason != unavailableReason {
		return errors.New("boost_state")
	}
	return nil
}

func validateQuotaDTO(value any) (*big.Rat, error) {
	if value == nil {
		return nil, nil
	}
	quota, err := object(value)
	if err != nil || !exactKeys(quota, "available_usd") {
		return nil, errors.New("details_quota_fields")
	}
	available, err := decimalStringField(quota, "available_usd")
	if err != nil || available.Sign() < 0 {
		return nil, errors.New("details_quota_value")
	}
	return available, nil
}

func validateDetailsDTO(data map[string]any) error {
	if !exactKeys(data, "server_now", "timezone", "usage", "quota", "term", "reset_window") {
		return errors.New("details_top_level_fields")
	}
	if _, err := timeField(data, "server_now"); err != nil || data["timezone"] != "Asia/Shanghai" {
		return errors.New("details_clock")
	}
	usage, err := object(data["usage"])
	if err != nil || !exactKeys(usage, "total_used_usd", "history_complete", "statistics_since") {
		return errors.New("details_usage_fields")
	}
	used, usedErr := decimalField(usage, "total_used_usd")
	_, historyOK := usage["history_complete"].(bool)
	_, statisticsErr := optionalTimeField(usage, "statistics_since")
	if usedErr != nil || used.Sign() < 0 || !historyOK || statisticsErr != nil {
		return errors.New("details_usage_values")
	}
	quota, quotaErr := validateQuotaDTO(data["quota"])
	if quotaErr != nil {
		return quotaErr
	}
	reset, err := object(data["reset_window"])
	if err != nil || !exactKeys(reset, "status", "scheduled_at", "schedule_revision", "eligible_for_me", "ineligible_reason") {
		return errors.New("details_reset_fields")
	}
	resetStatus, statusErr := stringValue(reset, "status")
	scheduledAt, scheduledErr := optionalTimeField(reset, "scheduled_at")
	revision, revisionErr := int64Value(reset, "schedule_revision")
	eligible, eligibleOK := reset["eligible_for_me"].(bool)
	reason, reasonErr := optionalString(reset, "ineligible_reason")
	if statusErr != nil || !oneOf(resetStatus, "none", "scheduled", "executing", "delayed", "completed") || scheduledErr != nil || revisionErr != nil || revision < 0 || !eligibleOK || reasonErr != nil {
		return errors.New("details_reset_values")
	}
	if (resetStatus == "none" && (scheduledAt != nil || revision != 0 || eligible || reason != "")) ||
		(oneOf(resetStatus, "scheduled", "executing", "delayed") && scheduledAt == nil) ||
		(oneOf(resetStatus, "scheduled", "executing", "delayed") && !eligible && reason != "term_not_covered") ||
		(resetStatus == "completed" && (scheduledAt != nil || !eligible || reason != "")) ||
		(eligible && reason != "") || (!eligible && reason != "" && reason != "term_not_covered") {
		return errors.New("details_reset_consistency")
	}
	if data["term"] == nil {
		if quota != nil {
			return errors.New("details_quota_without_term")
		}
		return nil
	}
	term, err := object(data["term"])
	if err != nil || !exactKeys(term, "status", "starts_at", "expires_at", "reset_count", "reset_count_basis", "reset_events", "current_cycle_no", "cycles") {
		return errors.New("details_term_fields")
	}
	termStatus, termStatusErr := stringValue(term, "status")
	startsAt, startsErr := timeField(term, "starts_at")
	expiresAt, expiresErr := timeField(term, "expires_at")
	resetCount, resetErr := int64Value(term, "reset_count")
	basis, basisErr := stringValue(term, "reset_count_basis")
	var currentCycleNo int64
	if term["current_cycle_no"] != nil {
		currentCycleNo, err = int64Value(term, "current_cycle_no")
		if err != nil || currentCycleNo < 1 {
			return errors.New("details_current_cycle")
		}
	}
	if termStatusErr != nil || !oneOf(termStatus, "pending", "active", "expired", "terminated") || startsErr != nil || expiresErr != nil || resetErr != nil || resetCount < 0 || basisErr != nil || basis != "current_term" {
		return errors.New("details_term_values")
	}
	cycles, err := array(term["cycles"])
	if err != nil || validateDetailsCycleWindows(cycles, startsAt, expiresAt) != nil {
		return errors.New("details_cycles")
	}
	if currentCycleNo > int64(len(cycles)) {
		return errors.New("details_current_cycle")
	}
	activeCycleNo := int64(0)
	for _, raw := range cycles {
		cycle, currentErr := object(raw)
		if currentErr != nil || !exactKeys(cycle, "cycle_no", "starts_at", "ends_at", "status") {
			return errors.New("details_cycle_fields")
		}
		cycleStatus, statusErr := stringValue(cycle, "status")
		cycleNo, numberErr := int64Value(cycle, "cycle_no")
		if statusErr != nil || !oneOf(cycleStatus, "scheduled", "active", "missed", "closing", "closed") || numberErr != nil {
			return errors.New("details_cycle_values")
		}
		if cycleStatus == "active" {
			if activeCycleNo != 0 {
				return errors.New("details_multiple_active_cycles")
			}
			activeCycleNo = cycleNo
		}
	}
	if (termStatus == "active" && (currentCycleNo == 0 || currentCycleNo != activeCycleNo)) || (termStatus != "active" && currentCycleNo != 0) {
		return errors.New("details_current_cycle_mismatch")
	}
	if (termStatus == "active" && quota == nil) || (termStatus != "active" && quota != nil) {
		return errors.New("details_quota_term_mismatch")
	}
	resetEvents, err := array(term["reset_events"])
	if err != nil || int64(len(resetEvents)) != resetCount {
		return errors.New("details_reset_events")
	}
	var previousResetAt time.Time
	for _, raw := range resetEvents {
		event, eventErr := object(raw)
		if eventErr != nil || !exactKeys(event, "cycle_no", "occurred_at", "target_quota_usd") {
			return errors.New("details_reset_event_fields")
		}
		cycleNo, cycleErr := int64Value(event, "cycle_no")
		occurredAt, occurredErr := timeField(event, "occurred_at")
		target, targetErr := decimalStringField(event, "target_quota_usd")
		if cycleErr != nil || cycleNo < 1 || cycleNo > int64(len(cycles)) || occurredErr != nil || targetErr != nil || target.Sign() <= 0 ||
			occurredAt.Before(startsAt) || !occurredAt.Before(expiresAt) || (!previousResetAt.IsZero() && occurredAt.Before(previousResetAt)) {
			return errors.New("details_reset_event_values")
		}
		cycle, _ := object(cycles[cycleNo-1])
		cycleStart, _ := timeField(cycle, "starts_at")
		cycleEnd, _ := timeField(cycle, "ends_at")
		if occurredAt.Before(cycleStart) || !occurredAt.Before(cycleEnd) {
			return errors.New("details_reset_event_cycle")
		}
		previousResetAt = occurredAt
	}
	return nil
}

func validateDetailsCycleWindows(cycles []any, startsAt, expiresAt time.Time) error {
	expectedDays := []int{7, 7, 7, 7}
	if len(cycles) == 5 {
		expectedDays = []int{7, 7, 7, 7, 2}
	}
	if len(cycles) != len(expectedDays) {
		return errors.New("cycle_count")
	}
	previousEnd := startsAt
	for index, raw := range cycles {
		cycle, err := object(raw)
		if err != nil {
			return err
		}
		cycleNo, numberErr := int64Value(cycle, "cycle_no")
		cycleStart, startErr := timeField(cycle, "starts_at")
		cycleEnd, endErr := timeField(cycle, "ends_at")
		if numberErr != nil || cycleNo != int64(index+1) || startErr != nil || endErr != nil || !cycleStart.Equal(previousEnd) ||
			!cycleEnd.Equal(cycleStart.Add(time.Duration(expectedDays[index])*24*time.Hour)) {
			return errors.New("cycle_window")
		}
		previousEnd = cycleEnd
	}
	if !previousEnd.Equal(expiresAt) {
		return errors.New("cycle_term_boundary")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (r *runner) details(ctx context.Context, name, query string) (*apiResponse, map[string]any, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/user/carpool/details"+query, r.tokens[name], "", nil, nil)
	if err != nil {
		return resp, nil, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return resp, nil, err
	}
	data, err := object(resp.Data)
	return resp, data, err
}

func (r *runner) detailsAvailable(ctx context.Context, name string) (*big.Rat, error) {
	_, data, err := r.details(ctx, name, "")
	if err != nil {
		return nil, err
	}
	if err := validateDetailsDTO(data); err != nil {
		return nil, err
	}
	available, err := validateQuotaDTO(data["quota"])
	if err != nil || available == nil {
		return nil, errors.New("active_carpool_quota_missing")
	}
	return available, nil
}

func (r *runner) testPermissionsAndDTO(ctx context.Context) error {
	resp, data, err := r.details(ctx, "four", "")
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = validateDetailsDTO(data)
	}
	r.record("permissions.user_details_exact_whitelist", err, status, map[string]int64{"user_id": r.users["four"].ID})
	if err != nil {
		return err
	}
	for _, name := range []string{"ordinary", "expired"} {
		quotaResp, quotaData, quotaErr := r.details(ctx, name, "")
		if quotaErr == nil {
			quotaErr = validateDetailsDTO(quotaData)
		}
		if quotaErr == nil && quotaData["quota"] != nil {
			quotaErr = errors.New("inactive_user_exposed_carpool_quota")
		}
		quotaStatus := 0
		if quotaResp != nil {
			quotaStatus = quotaResp.Status
		}
		r.record("quota.inactive_or_missing_term_null."+name, quotaErr, quotaStatus, map[string]int64{"user_id": r.users[name].ID})
		if quotaErr != nil {
			return quotaErr
		}
	}
	tampered, tamperedData, tamperedErr := r.details(ctx, "four", fmt.Sprintf("?user_id=%d&term_id=%d&cycle_id=%d", r.users["three"].ID, r.termIDs["three"], r.cycleIDs["three"]))
	if tamperedErr == nil {
		tamperedErr = validateDetailsDTO(tamperedData)
	}
	if tamperedErr == nil {
		if !sameJSON(data["usage"], tamperedData["usage"]) || !sameJSON(data["term"], tamperedData["term"]) || !sameJSON(data["reset_window"], tamperedData["reset_window"]) {
			tamperedErr = errors.New("target_override")
		}
	}
	tamperedStatus := 0
	if tampered != nil {
		tamperedStatus = tampered.Status
	}
	r.record("permissions.user_query_cannot_switch_identity", tamperedErr, tamperedStatus, map[string]int64{"user_id": r.users["four"].ID, "attempted_user_id": r.users["three"].ID})

	adminResp, adminErr := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/terms", r.tokens["ordinary"], "", nil, nil)
	if adminErr == nil {
		adminErr = expectStatus(adminResp, http.StatusForbidden)
	}
	adminStatus := 0
	if adminResp != nil {
		adminStatus = adminResp.Status
	}
	r.record("permissions.ordinary_user_admin_route_forbidden", adminErr, adminStatus, map[string]int64{"user_id": r.users["ordinary"].ID})

	unauthResp, unauthErr := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/terms", "", "", nil, nil)
	if unauthErr == nil {
		unauthErr = expectStatus(unauthResp, http.StatusUnauthorized)
	}
	unauthStatus := 0
	if unauthResp != nil {
		unauthStatus = unauthResp.Status
	}
	r.record("permissions.unauthenticated_admin_route_unauthorized", unauthErr, unauthStatus, nil)
	if tamperedErr != nil {
		return tamperedErr
	}
	if adminErr != nil {
		return adminErr
	}
	return unauthErr
}

func (r *runner) profileBalance(ctx context.Context, name string) (*big.Rat, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/user/profile", r.tokens[name], "", nil, nil)
	if err != nil {
		return nil, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return nil, err
	}
	return decimalField(data, "balance")
}

func (r *runner) mockCaseRequests(ctx context.Context, model string) (int64, error) {
	resp, err := r.mock.do(ctx, http.MethodGet, "/__stats", "", "", nil, nil)
	if err != nil {
		return 0, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return 0, err
	}
	data, err := object(resp.Data)
	if err != nil {
		return 0, err
	}
	stats, err := object(data["stats"])
	if err != nil {
		return 0, err
	}
	byCase, err := object(stats["by_case"])
	if err != nil {
		return 0, err
	}
	if _, ok := byCase[model]; !ok {
		return 0, nil
	}
	return int64Value(byCase, model)
}

func (r *runner) gatewayRequest(ctx context.Context, name, model string, stream bool) (*apiResponse, error) {
	body := map[string]any{"model": model, "input": "synthetic acceptance", "stream": stream}
	resp, err := r.api.do(ctx, http.MethodPost, "/v1/responses", r.keys[name], "", map[string]string{"X-Request-ID": "accept-" + model}, body)
	return resp, err
}

func (r *runner) testNoOrdinaryFallback(ctx context.Context) error {
	beforeBalance, err := r.profileBalance(ctx, "ordinary")
	if err != nil {
		r.record("gateway.ordinary_balance_baseline", err, 0, map[string]int64{"user_id": r.users["ordinary"].ID})
		return err
	}
	if beforeBalance.Sign() <= 0 {
		err = errors.New("ordinary_balance_not_positive")
		r.record("gateway.ordinary_balance_baseline", err, 0, map[string]int64{"user_id": r.users["ordinary"].ID})
		return err
	}
	beforeForward, err := r.mockCaseRequests(ctx, r.models["ordinary"])
	if err != nil {
		return err
	}
	resp, requestErr := r.gatewayRequest(ctx, "ordinary", r.models["ordinary"], false)
	if requestErr == nil {
		requestErr = expectStatus(resp, http.StatusForbidden, http.StatusPaymentRequired, http.StatusTooManyRequests)
	}
	afterForward, statsErr := r.mockCaseRequests(ctx, r.models["ordinary"])
	afterBalance, balanceErr := r.profileBalance(ctx, "ordinary")
	if requestErr == nil && statsErr == nil && balanceErr == nil {
		if afterForward != beforeForward || afterBalance.Cmp(beforeBalance) != 0 {
			requestErr = errors.New("ordinary_fallback_detected")
		}
	} else if requestErr == nil {
		requestErr = errors.New("ordinary_fallback_check_failed")
	}
	status := 0
	if resp != nil {
		status = resp.Status
	}
	r.record("gateway.carpool_key_never_falls_back_to_ordinary_balance", requestErr, status, map[string]int64{"user_id": r.users["ordinary"].ID, "mock_requests_before": beforeForward, "mock_requests_after": afterForward})
	return requestErr
}

func (r *runner) currentCycle(ctx context.Context, name string) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/admin/carpool/cycles?term_id=%d&page_size=10", r.termIDs[name])
	resp, err := r.api.do(ctx, http.MethodGet, path, r.tokens["admin"], "", nil, nil)
	if err != nil {
		return nil, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	page, err := object(resp.Data)
	if err != nil {
		return nil, err
	}
	items, err := array(page["items"])
	if err != nil {
		return nil, err
	}
	for _, raw := range items {
		cycle, currentErr := object(raw)
		if currentErr == nil && cycle["state"] == "active" {
			return cycle, nil
		}
	}
	return nil, errors.New("active_cycle_missing")
}

func (r *runner) adminTerm(ctx context.Context, userID, termID int64) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/admin/carpool/terms?user_id=%d&page_size=100", userID)
	resp, err := r.api.do(ctx, http.MethodGet, path, r.tokens["admin"], "", nil, nil)
	if err != nil {
		return nil, err
	}
	if err = expectStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	page, err := object(resp.Data)
	if err != nil {
		return nil, err
	}
	items, err := array(page["items"])
	if err != nil {
		return nil, err
	}
	for _, raw := range items {
		term, currentErr := object(raw)
		if currentErr != nil {
			return nil, currentErr
		}
		id, currentErr := int64Value(term, "id")
		if currentErr == nil && id == termID {
			return term, nil
		}
	}
	return nil, errors.New("admin_term_missing")
}

type billingState struct {
	used            *big.Rat
	available       *big.Rat
	ordinaryBalance *big.Rat
}

func (r *runner) readBillingState(ctx context.Context, name string) (billingState, error) {
	ordinaryBalance, err := r.profileBalance(ctx, name)
	if err != nil {
		return billingState{}, err
	}
	_, details, err := r.details(ctx, name, "")
	if err != nil {
		return billingState{}, err
	}
	usage, err := object(details["usage"])
	if err != nil {
		return billingState{}, err
	}
	used, err := decimalField(usage, "total_used_usd")
	if err != nil {
		return billingState{}, err
	}
	cycle, err := r.currentCycle(ctx, name)
	if err != nil {
		return billingState{}, err
	}
	available, err := decimalField(cycle, "available_usd")
	if err != nil {
		return billingState{}, err
	}
	return billingState{used: used, available: available, ordinaryBalance: ordinaryBalance}, nil
}

func validateBillingDelta(before, after billingState, expected string) error {
	expectedDelta := mustRat(expected)
	if after.ordinaryBalance.Cmp(before.ordinaryBalance) != 0 {
		return errors.New("carpool_billing_changed_ordinary_balance")
	}
	usedDelta := new(big.Rat).Sub(after.used, before.used)
	availableDelta := new(big.Rat).Sub(before.available, after.available)
	if usedDelta.Cmp(expectedDelta) != 0 || availableDelta.Cmp(expectedDelta) != 0 {
		return errors.New("billing_delta_wrong")
	}
	return nil
}

func (r *runner) waitForBillingDelta(ctx context.Context, name string, before billingState, expected string) (billingState, error) {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		after, err := r.readBillingState(ctx, name)
		if err == nil {
			lastErr = validateBillingDelta(before, after, expected)
			if lastErr == nil {
				return after, nil
			}
			if after.ordinaryBalance.Cmp(before.ordinaryBalance) != 0 || new(big.Rat).Sub(after.used, before.used).Cmp(mustRat(expected)) > 0 || new(big.Rat).Sub(before.available, after.available).Cmp(mustRat(expected)) > 0 {
				return after, lastErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return billingState{}, ctx.Err()
		case <-deadline.C:
			return billingState{}, lastErr
		case <-ticker.C:
		}
	}
}

func (r *runner) testGatewayHTTPAndSSE(ctx context.Context) error {
	beforeHTTPState, err := r.readBillingState(ctx, "three")
	if err != nil || beforeHTTPState.ordinaryBalance.Sign() != 0 {
		return errors.New("carpool_user_ordinary_balance_not_zero")
	}

	beforeHTTP, err := r.mockCaseRequests(ctx, r.models["http"])
	if err != nil {
		return err
	}
	httpResp, httpErr := r.gatewayRequest(ctx, "three", r.models["http"], false)
	if httpErr == nil {
		httpErr = expectStatus(httpResp, http.StatusOK)
	}
	afterHTTP, statsErr := r.mockCaseRequests(ctx, r.models["http"])
	if httpErr == nil && (statsErr != nil || afterHTTP != beforeHTTP+1) {
		httpErr = errors.New("http_not_forwarded_once")
	}
	httpStatus := 0
	if httpResp != nil {
		httpStatus = httpResp.Status
	}
	r.record("gateway.real_http_forward", httpErr, httpStatus, map[string]int64{"user_id": r.users["three"].ID, "mock_requests_before": beforeHTTP, "mock_requests_after": afterHTTP})
	if httpErr != nil {
		return httpErr
	}
	afterHTTPState, billingErr := r.waitForBillingDelta(ctx, "three", beforeHTTPState, "1")
	r.record("gateway.http_debit_exactly_once", billingErr, httpStatus, map[string]int64{"user_id": r.users["three"].ID, "term_id": r.termIDs["three"], "cycle_id": r.cycleIDs["three"]})
	if billingErr != nil {
		return billingErr
	}

	beforeSSE, err := r.mockCaseRequests(ctx, r.models["sse"])
	if err != nil {
		return err
	}
	sseResp, sseErr := r.gatewayRequest(ctx, "three", r.models["sse"], true)
	if sseErr == nil {
		sseErr = expectStatus(sseResp, http.StatusOK)
	}
	if sseErr == nil {
		body, ok := sseResp.Data.(string)
		if !ok || !strings.Contains(strings.ToLower(sseResp.ContentType), "text/event-stream") || !strings.Contains(body, "response.completed") {
			sseErr = errors.New("invalid_sse")
		}
	}
	afterSSE, statsErr := r.mockCaseRequests(ctx, r.models["sse"])
	if sseErr == nil && (statsErr != nil || afterSSE != beforeSSE+1) {
		sseErr = errors.New("sse_not_forwarded_once")
	}
	sseStatus := 0
	if sseResp != nil {
		sseStatus = sseResp.Status
	}
	r.record("gateway.real_sse_forward", sseErr, sseStatus, map[string]int64{"user_id": r.users["three"].ID, "mock_requests_before": beforeSSE, "mock_requests_after": afterSSE})
	if sseErr != nil {
		return sseErr
	}
	_, billingErr = r.waitForBillingDelta(ctx, "three", afterHTTPState, "1")
	r.record("gateway.sse_debit_exactly_once", billingErr, sseStatus, map[string]int64{"user_id": r.users["three"].ID, "term_id": r.termIDs["three"], "cycle_id": r.cycleIDs["three"]})
	return billingErr
}

func (r *runner) testExpiredAndTerminatedAdmission(ctx context.Context) error {
	before, err := r.mockCaseRequests(ctx, r.models["expired"])
	if err != nil {
		return err
	}
	resp, requestErr := r.gatewayRequest(ctx, "expired", r.models["expired"], false)
	if requestErr == nil {
		requestErr = expectStatus(resp, http.StatusForbidden, http.StatusPaymentRequired, http.StatusTooManyRequests)
	}
	after, statsErr := r.mockCaseRequests(ctx, r.models["expired"])
	if requestErr == nil && (statsErr != nil || before != after) {
		requestErr = errors.New("expired_forwarded")
	}
	status := 0
	if resp != nil {
		status = resp.Status
	}
	r.record("gateway.expired_term_blocks_before_forward", requestErr, status, map[string]int64{"user_id": r.users["expired"].ID, "mock_requests_before": before, "mock_requests_after": after})

	termID := r.termIDs["termination"]
	terminatePath := fmt.Sprintf("/api/v1/admin/carpool/terms/%d/terminate", termID)
	terminate, terminateErr := r.api.do(ctx, http.MethodPost, terminatePath, r.tokens["admin"], "accept-terminate-"+r.runID, nil, map[string]any{"reason": "synthetic acceptance termination", "effective_at": nil})
	if terminateErr == nil {
		terminateErr = expectStatus(terminate, http.StatusOK)
	}
	terminateStatus := 0
	if terminate != nil {
		terminateStatus = terminate.Status
	}
	r.record("admin.synthetic_termination", terminateErr, terminateStatus, map[string]int64{"user_id": r.users["termination"].ID, "term_id": termID})
	if terminateErr != nil {
		return terminateErr
	}
	before, err = r.mockCaseRequests(ctx, r.models["terminated"])
	if err != nil {
		return err
	}
	resp, requestErr = r.gatewayRequest(ctx, "termination", r.models["terminated"], false)
	if requestErr == nil {
		requestErr = expectStatus(resp, http.StatusForbidden, http.StatusPaymentRequired, http.StatusTooManyRequests)
	}
	after, statsErr = r.mockCaseRequests(ctx, r.models["terminated"])
	if requestErr == nil && (statsErr != nil || before != after) {
		requestErr = errors.New("terminated_forwarded")
	}
	status = 0
	if resp != nil {
		status = resp.Status
	}
	r.record("gateway.terminated_term_blocks_before_forward", requestErr, status, map[string]int64{"user_id": r.users["termination"].ID, "term_id": termID, "mock_requests_before": before, "mock_requests_after": after})
	return requestErr
}

func (r *runner) testRenewal(ctx context.Context) error {
	oldTerm := r.termIDs["renewal"]
	oldBefore, err := r.adminTerm(ctx, r.users["renewal"].ID, oldTerm)
	if err != nil {
		return err
	}
	oldExpiresAt, err := timeField(oldBefore, "expires_at")
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/admin/carpool/terms/%d/renew", oldTerm)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "accept-renew-"+r.runID, nil, map[string]any{"plan_id": r.planIDs["three"], "notes": "synthetic renewal", "payment": nil})
	status := 0
	newTermID := int64(0)
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusCreated)
	}
	if err == nil {
		data, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			newTermID, err = int64Value(data, "id")
			startsAt, startsErr := timeField(data, "starts_at")
			expiresAt, expiresErr := timeField(data, "expires_at")
			userID, userErr := int64Value(data, "user_id")
			scopeID, scopeErr := int64Value(data, "scope_id")
			groupID, groupErr := int64Value(data, "group_id")
			planID, planErr := int64Value(data, "plan_id")
			plan, snapshotErr := object(data["plan_snapshot"])
			cycles, cyclesErr := array(data["cycles"])
			if err != nil || newTermID <= 0 || newTermID == oldTerm || startsErr != nil || !startsAt.Equal(oldExpiresAt) || expiresErr != nil || !expiresAt.Equal(startsAt.Add(28*24*time.Hour)) ||
				userErr != nil || userID != r.users["renewal"].ID || scopeErr != nil || scopeID != 1 || groupErr != nil || groupID != r.groupID || planErr != nil || planID != r.planIDs["three"] ||
				data["status"] != "pending" || data["current_cycle"] != nil || !decimalEquals(data["payment_net_cny"], "0") || snapshotErr != nil || validatePlanSnapshot(plan, planExpectations[1], r.planIDs["three"], r.planVersions["three"]) != nil || cyclesErr != nil || validateCycleWindows(cycles) != nil {
				err = errors.New("renewal_contract_invalid")
			} else {
				for _, raw := range cycles {
					cycle, mapErr := object(raw)
					if mapErr != nil || cycle["state"] != "scheduled" || !decimalEquals(cycle["available_usd"], "0") || !decimalEquals(cycle["base_balance_usd"], "0") || !decimalEquals(cycle["boost_balance_usd"], "0") || !decimalEquals(cycle["manual_balance_usd"], "0") {
						err = errors.New("renewal_granted_early")
						break
					}
				}
			}
		}
	}
	if err == nil {
		oldAfter, fetchErr := r.adminTerm(ctx, r.users["renewal"].ID, oldTerm)
		if fetchErr != nil || !sameJSON(oldBefore, oldAfter) {
			err = errors.New("renewal_mutated_old_term")
		}
	}
	r.record("renewal.future_term_has_no_early_grant", err, status, map[string]int64{"user_id": r.users["renewal"].ID, "old_term_id": oldTerm, "new_term_id": newTermID})
	if err == nil {
		r.report.SyntheticIDs["renewal_term_id"] = newTermID
	}
	return err
}

func (r *runner) testPaymentAdjustmentAndTakeover(ctx context.Context) error {
	termID := r.termIDs["four"]
	cycleBeforePayment, err := r.currentCycle(ctx, "four")
	if err != nil {
		return err
	}
	availableBeforePayment, err := decimalField(cycleBeforePayment, "available_usd")
	if err != nil {
		return err
	}
	paymentPath := fmt.Sprintf("/api/v1/admin/carpool/terms/%d/payments", termID)
	paymentBody := map[string]any{"amount_cny": "20.00", "payment_kind": "refund", "paid_at": time.Now().UTC().Truncate(time.Second), "channel": "manual", "external_order_no": nil, "notes": "synthetic refund"}
	resp, err := r.api.do(ctx, http.MethodPost, paymentPath, r.tokens["admin"], "accept-refund-"+r.runID, nil, paymentBody)
	status := 0
	refundID := int64(0)
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusCreated)
	}
	if err == nil {
		data, _ := object(resp.Data)
		refundID, err = int64Value(data, "id")
	}
	if err == nil {
		list, listErr := r.api.do(ctx, http.MethodGet, paymentPath+"?page_size=10", r.tokens["admin"], "", nil, nil)
		if listErr == nil {
			listErr = expectStatus(list, http.StatusOK)
		}
		if listErr == nil {
			page, _ := object(list.Data)
			items, arrayErr := array(page["items"])
			if arrayErr != nil || len(items) != 2 {
				listErr = errors.New("payment_history_wrong")
			} else {
				foundPayment := false
				foundRefund := false
				for _, raw := range items {
					item, objectErr := object(raw)
					if objectErr != nil {
						listErr = objectErr
						break
					}
					kind, kindErr := stringValue(item, "payment_kind")
					if kindErr != nil {
						listErr = kindErr
						break
					}
					foundPayment = foundPayment || kind == "payment" && decimalEquals(item["amount_cny"], "330")
					foundRefund = foundRefund || kind == "refund" && decimalEquals(item["amount_cny"], "20")
				}
				if listErr == nil && (!foundPayment || !foundRefund) {
					listErr = errors.New("payment_history_wrong")
				}
			}
		}
		if listErr != nil {
			err = listErr
		}
	}
	if err == nil {
		term, termErr := r.adminTerm(ctx, r.users["four"].ID, termID)
		if termErr != nil || !decimalEquals(term["payment_net_cny"], "310") {
			err = errors.New("payment_net_cny_wrong")
		}
	}
	if err == nil {
		cycleAfterPayment, cycleErr := r.currentCycle(ctx, "four")
		if cycleErr != nil {
			err = cycleErr
		} else {
			availableAfterPayment, valueErr := decimalField(cycleAfterPayment, "available_usd")
			if valueErr != nil || availableAfterPayment.Cmp(availableBeforePayment) != 0 {
				err = errors.New("payment_changed_usd_quota")
			}
		}
	}
	r.record("accounting.payment_and_refund_are_separate_records", err, status, map[string]int64{"term_id": termID, "refund_id": refundID})
	if err != nil {
		return err
	}

	cycleBefore, err := r.currentCycle(ctx, "four")
	if err != nil {
		return err
	}
	availableBefore, _ := decimalField(cycleBefore, "available_usd")
	adjustPath := fmt.Sprintf("/api/v1/admin/carpool/cycles/%d/adjustments", r.cycleIDs["four"])
	addBody := map[string]any{"bucket": "manual", "delta_usd": "25.00000000", "reason": "synthetic adjustment", "reverses_ledger_id": nil}
	add, addErr := r.api.do(ctx, http.MethodPost, adjustPath, r.tokens["admin"], "accept-adjust-add-"+r.runID, nil, addBody)
	addStatus := 0
	ledgerID := int64(0)
	if add != nil {
		addStatus = add.Status
	}
	if addErr == nil {
		addErr = expectStatus(add, http.StatusOK)
	}
	if addErr == nil {
		entries, arrayErr := array(add.Data)
		if arrayErr != nil || len(entries) == 0 {
			addErr = errors.New("missing_adjustment_ledger")
		} else {
			entry, objectErr := object(entries[len(entries)-1])
			if objectErr != nil {
				addErr = objectErr
			} else {
				ledgerID, addErr = int64Value(entry, "id")
				reason, _ := optionalString(entry, "reason")
				if addErr == nil && (entry["event_type"] != "adjustment" || entry["bucket"] != "manual" || !decimalEquals(entry["delta_usd"], "25") || reason != "synthetic adjustment" || entry["reverses_ledger_id"] != nil) {
					addErr = errors.New("adjustment_audit_fields_wrong")
				}
			}
		}
	}
	if addErr == nil {
		reverseBody := map[string]any{"bucket": "manual", "delta_usd": "-25.00000000", "reason": "synthetic reversal", "reverses_ledger_id": ledgerID}
		reverse, reverseErr := r.api.do(ctx, http.MethodPost, adjustPath, r.tokens["admin"], "accept-adjust-reverse-"+r.runID, nil, reverseBody)
		if reverseErr == nil {
			reverseErr = expectStatus(reverse, http.StatusOK)
		}
		if reverseErr == nil {
			entries, arrayErr := array(reverse.Data)
			foundReversal := false
			if arrayErr == nil {
				for _, raw := range entries {
					entry, objectErr := object(raw)
					if objectErr != nil {
						arrayErr = objectErr
						break
					}
					reversesID, idErr := int64Value(entry, "reverses_ledger_id")
					reason, _ := optionalString(entry, "reason")
					if idErr == nil && reversesID == ledgerID && entry["event_type"] == "adjustment" && entry["bucket"] == "manual" && decimalEquals(entry["delta_usd"], "-25") && reason == "synthetic reversal" {
						foundReversal = true
					}
				}
			}
			if arrayErr != nil || !foundReversal {
				reverseErr = errors.New("reversal_audit_link_missing")
			}
		}
		if reverseErr != nil {
			addErr = reverseErr
		}
	}
	if addErr == nil {
		cycleAfter, cycleErr := r.currentCycle(ctx, "four")
		if cycleErr != nil {
			addErr = cycleErr
		} else {
			availableAfter, valueErr := decimalField(cycleAfter, "available_usd")
			if valueErr != nil || availableAfter.Cmp(availableBefore) != 0 {
				addErr = errors.New("adjustment_reversal_not_net_zero")
			}
		}
	}
	r.record("accounting.adjustment_and_reversal_net_zero", addErr, addStatus, map[string]int64{"term_id": termID, "cycle_id": r.cycleIDs["four"], "ledger_id": ledgerID})
	if addErr != nil {
		return addErr
	}

	beforeBalance, err := r.profileBalance(ctx, "takeover")
	if err != nil {
		return err
	}
	start := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	takeover := map[string]any{"current_base_balance_usd": "5.00000000", "current_boost_balance_usd": "2.00000000", "current_manual_balance_usd": "3.00000000", "ordinary_balance_transfer_usd": "10.00000000", "historical_used_usd": nil, "statistics_since": nil, "boost_used": 1, "history_complete": false}
	takeoverKey := "accept-takeover-" + r.runID
	takeoverResp, takeoverBody, takeoverErr := r.openTerm(ctx, "takeover", "four", &start, "takeover", takeover, nil, takeoverKey)
	takeoverStatus := 0
	if takeoverResp != nil {
		takeoverStatus = takeoverResp.Status
	}
	var afterBalance *big.Rat
	if takeoverErr == nil {
		data, objectErr := object(takeoverResp.Data)
		if objectErr != nil {
			takeoverErr = objectErr
		} else {
			current, cycleErr := object(data["current_cycle"])
			statisticsSince, statisticsErr := optionalTimeField(data, "statistics_since")
			if cycleErr != nil || !decimalEquals(current["base_balance_usd"], "5") ||
				!decimalEquals(current["boost_balance_usd"], "2") ||
				!decimalEquals(current["manual_balance_usd"], "3") ||
				!decimalEquals(current["available_usd"], "10") || data["status"] != "active" ||
				data["history_complete"] != false || statisticsErr != nil || statisticsSince == nil ||
				!decimalEquals(data["payment_net_cny"], "0") {
				takeoverErr = errors.New("takeover_bucket_import_wrong")
			} else {
				boostUsed, usedErr := int64Value(data, "boost_used")
				boostRemaining, remainingErr := int64Value(data, "boost_remaining")
				if usedErr != nil || boostUsed != 1 || remainingErr != nil || boostRemaining != 2 {
					takeoverErr = errors.New("takeover_boost_history_wrong")
				}
			}
		}
	}
	if takeoverErr == nil {
		var balanceErr error
		afterBalance, balanceErr = r.profileBalance(ctx, "takeover")
		if balanceErr != nil || new(big.Rat).Sub(beforeBalance, afterBalance).Cmp(mustRat("10")) != 0 {
			takeoverErr = errors.New("takeover_transfer_wrong")
		}
	}
	if takeoverErr == nil {
		path := fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", r.users["takeover"].ID)
		replay, replayErr := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], takeoverKey, nil, takeoverBody)
		if replayErr == nil {
			replayErr = expectStatus(replay, http.StatusCreated)
		}
		if replayErr == nil {
			data, objectErr := object(replay.Data)
			if objectErr != nil {
				replayErr = objectErr
			} else if id, idErr := int64Value(data, "id"); idErr != nil || id != r.termIDs["takeover"] {
				replayErr = errors.New("takeover_replay_changed_result")
			}
		}
		if replayErr == nil {
			balanceAfterReplay, balanceErr := r.profileBalance(ctx, "takeover")
			if balanceErr != nil || balanceAfterReplay.Cmp(afterBalance) != 0 {
				replayErr = errors.New("takeover_replay_transferred_twice")
			}
		}
		if replayErr != nil {
			takeoverErr = replayErr
		}
	}
	r.record("accounting.takeover_explicit_ordinary_transfer_once", takeoverErr, takeoverStatus, map[string]int64{"user_id": r.users["takeover"].ID, "term_id": r.termIDs["takeover"], "cycle_id": r.cycleIDs["takeover"]})
	return takeoverErr
}

func (r *runner) testPlanVersionImmutability(ctx context.Context) error {
	expected := planExpectations[0]
	oldPlanID := r.planIDs["four"]
	oldVersion := r.planVersions["four"]
	termBefore, err := r.adminTerm(ctx, r.users["four"].ID, r.termIDs["four"])
	if err != nil {
		return err
	}
	body := map[string]any{
		"name": expected.Name, "list_price_cny": expected.ListPrice, "weekly_quota_usd": expected.Weekly,
		"boost_ratio": "0.1", "boost_count": 3, "enabled": true,
	}
	path := fmt.Sprintf("/api/v1/admin/carpool/plans/%d/versions", oldPlanID)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "accept-plan-version-"+r.runID, nil, body)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	if err == nil {
		err = expectStatus(resp, http.StatusCreated)
	}
	newPlanID := int64(0)
	newVersion := int64(0)
	if err == nil {
		plan, objectErr := object(resp.Data)
		if objectErr != nil {
			err = objectErr
		} else {
			newPlanID, _ = int64Value(plan, "plan_id")
			newVersion, _ = int64Value(plan, "version")
			if newPlanID <= 0 || newPlanID == oldPlanID || newVersion != oldVersion+1 || validateAdminPlan(plan, expected, newPlanID, newVersion) != nil {
				err = errors.New("plan_version_contract_invalid")
			}
		}
	}
	if err == nil {
		plansResp, listErr := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/plans", r.tokens["admin"], "", nil, nil)
		if listErr == nil {
			listErr = expectStatus(plansResp, http.StatusOK)
		}
		if listErr == nil {
			plans, arrayErr := array(plansResp.Data)
			if arrayErr != nil || len(plans) != len(planExpectations) {
				listErr = errors.New("latest_plan_list_invalid")
			} else {
				foundNew := false
				for _, raw := range plans {
					plan, mapErr := object(raw)
					if mapErr != nil {
						listErr = mapErr
						break
					}
					id, idErr := int64Value(plan, "plan_id")
					if idErr != nil || id == oldPlanID {
						listErr = errors.New("superseded_plan_still_listed")
						break
					}
					if plan["code"] == expected.Code {
						foundNew = id == newPlanID && validateAdminPlan(plan, expected, newPlanID, newVersion) == nil
					}
				}
				if listErr == nil && !foundNew {
					listErr = errors.New("new_plan_version_missing")
				}
			}
		}
		if listErr != nil {
			err = listErr
		}
	}
	previewPath := fmt.Sprintf("/api/v1/admin/users/%d/carpool/preview", r.users["four"].ID)
	if err == nil {
		oldPreview, previewErr := r.api.do(ctx, http.MethodPost, previewPath, r.tokens["admin"], "", nil, map[string]any{"plan_id": oldPlanID, "starts_at": nil, "mode": "new", "takeover": nil})
		if previewErr == nil {
			previewErr = expectStatus(oldPreview, http.StatusForbidden)
		}
		if previewErr != nil {
			err = previewErr
		}
	}
	if err == nil {
		newPreview, previewErr := r.api.do(ctx, http.MethodPost, previewPath, r.tokens["admin"], "", nil, map[string]any{"plan_id": newPlanID, "starts_at": nil, "mode": "new", "takeover": nil})
		if previewErr == nil {
			previewErr = expectStatus(newPreview, http.StatusOK)
		}
		if previewErr == nil {
			preview, objectErr := object(newPreview.Data)
			if objectErr != nil {
				previewErr = objectErr
			} else {
				previewErr = validatePreview(preview, expected, newPlanID, newVersion)
			}
		}
		if previewErr != nil {
			err = previewErr
		}
	}
	if err == nil {
		termAfter, fetchErr := r.adminTerm(ctx, r.users["four"].ID, r.termIDs["four"])
		if fetchErr != nil || !sameJSON(termBefore, termAfter) {
			err = errors.New("plan_version_mutated_existing_term")
		}
	}
	r.record("plans.new_version_preserves_existing_term", err, status, map[string]int64{"old_plan_id": oldPlanID, "new_plan_id": newPlanID, "old_version": oldVersion, "new_version": newVersion, "term_id": r.termIDs["four"]})
	if err == nil {
		r.report.SyntheticIDs["plan_version_four"] = newPlanID
	}
	return err
}

func (r *runner) run(ctx context.Context) error {
	if err := r.login(ctx, "admin"); err != nil {
		return err
	}
	if err := r.createSyntheticUserBatch(ctx, r.batchFixturesPath); err != nil {
		return err
	}
	for _, name := range requiredFixtureNames[1:] {
		if err := r.login(ctx, name); err != nil {
			return err
		}
	}
	if err := r.createInfrastructure(ctx); err != nil {
		return err
	}
	if err := r.configureMockCases(ctx); err != nil {
		return err
	}
	for _, name := range []string{"three", "ordinary", "expired", "termination"} {
		if err := r.createAndBindKey(ctx, name); err != nil {
			return err
		}
	}
	if err := r.loadPlans(ctx); err != nil {
		return err
	}
	if err := r.testPreviewValidation(ctx); err != nil {
		return err
	}
	for _, expected := range planExpectations {
		if err := r.previewPlan(ctx, expected); err != nil {
			return err
		}
	}
	if err := r.testOpenAndIdempotency(ctx); err != nil {
		return err
	}
	if err := r.openSimpleTerms(ctx); err != nil {
		return err
	}
	if err := r.openLateCycleAndExpired(ctx); err != nil {
		return err
	}
	if err := r.testBoosts(ctx); err != nil {
		return err
	}
	if err := r.testPermissionsAndDTO(ctx); err != nil {
		return err
	}
	if err := r.testNoOrdinaryFallback(ctx); err != nil {
		return err
	}
	if err := r.testGatewayHTTPAndSSE(ctx); err != nil {
		return err
	}
	if err := r.testExpiredAndTerminatedAdmission(ctx); err != nil {
		return err
	}
	if err := r.testRenewal(ctx); err != nil {
		return err
	}
	if err := r.testPaymentAdjustmentAndTakeover(ctx); err != nil {
		return err
	}
	if err := r.testPlanVersionImmutability(ctx); err != nil {
		return err
	}
	for name, id := range r.termIDs {
		r.report.SyntheticIDs["term_"+name] = id
	}
	for name, id := range r.cycleIDs {
		r.report.SyntheticIDs["cycle_"+name] = id
	}
	for name, id := range r.keyIDs {
		r.report.SyntheticIDs["api_key_"+name] = id
	}
	return nil
}

func loadFixture(path string) (fixtureFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return fixtureFile{}, errors.New("open_fixture")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFixtureBytes+1))
	if err != nil || len(raw) > maxFixtureBytes {
		return fixtureFile{}, errors.New("read_fixture")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var fixture fixtureFile
	if err := decoder.Decode(&fixture); err != nil {
		return fixtureFile{}, errors.New("decode_fixture")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fixtureFile{}, errors.New("trailing_fixture_data")
	}
	if err := validateFixture(fixture); err != nil {
		return fixtureFile{}, err
	}
	return fixture, nil
}

func randomID() (string, error) {
	return randomHex(6)
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func writeBatchFixture(path string, value fixtureFile) (resultErr error) {
	if path == "" {
		return errors.New("missing_batch_fixture_path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func writeReport(path string, value report) error {
	if path == "" {
		return errors.New("missing_report_path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
	return os.Rename(temporary, path)
}

func main() {
	fixturesPath := flag.String("fixtures", "", "path to the synthetic fixture JSON")
	batchFixturesPath := flag.String("batch-fixtures", "", "path for the private per-run synthetic fixture JSON")
	reportPath := flag.String("report", "", "path for the redacted JSON report")
	flag.Parse()
	if *fixturesPath == "" || *batchFixturesPath == "" || *reportPath == "" {
		fmt.Fprintln(os.Stderr, "acceptance: -fixtures, -batch-fixtures and -report are required")
		os.Exit(2)
	}
	fixtureAbsolute, fixturePathErr := filepath.Abs(*fixturesPath)
	batchAbsolute, batchPathErr := filepath.Abs(*batchFixturesPath)
	reportAbsolute, reportPathErr := filepath.Abs(*reportPath)
	if fixturePathErr != nil || batchPathErr != nil || reportPathErr != nil || fixtureAbsolute == batchAbsolute || fixtureAbsolute == reportAbsolute || batchAbsolute == reportAbsolute {
		fmt.Fprintln(os.Stderr, "acceptance: fixture and output paths must be distinct")
		os.Exit(2)
	}
	fixture, err := loadFixture(*fixturesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: invalid synthetic fixture")
		os.Exit(2)
	}
	api, err := newAPIClient(fixture.BaseURL, applicationBaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: invalid application URL")
		os.Exit(2)
	}
	mock, err := newAPIClient(fixture.MockURL, mockBaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: invalid mock URL")
		os.Exit(2)
	}
	runID, err := randomID()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: random source unavailable")
		os.Exit(2)
	}
	r := newRunner(fixture, api, mock, runID)
	r.batchFixturesPath = *batchFixturesPath
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	runErr := r.run(ctx)
	if runErr != nil && r.report.Failed == 0 {
		r.record("acceptance.execution", runErr, 0, nil)
	}
	if err := writeReport(*reportPath, r.report); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: report write failed")
		os.Exit(2)
	}
	fmt.Printf("acceptance: passed=%d failed=%d\n", r.report.Passed, r.report.Failed)
	if runErr != nil || r.report.Failed > 0 {
		os.Exit(1)
	}
}

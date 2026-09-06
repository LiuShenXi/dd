package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const (
	fixtureVersion            = 1
	fixtureSource             = "synthetic-local-only"
	applicationBaseURL        = "http://carpool-app:8080"
	mockBaseURL               = "http://carpool-mock:8090"
	bootstrapAdminID          = int64(14)
	fixturePasswordBytes      = 24
	maxJSONBytes              = 4 << 20
	announcementWait          = 20 * time.Second
	announcementSnapshotWait  = 2 * time.Second
	announcementSnapshotRetry = 25 * time.Millisecond
	minimumScheduleLead       = 3 * time.Minute
	trustedDBHost             = "carpool-db"
	trustedDBPort             = "5432"
	trustedDBName             = "carpool_test"
	trustedDBUser             = "carpool_test"
)

var (
	bootstrapNames = []string{
		"admin", "four", "three", "two", "fifth_four", "fifth_three", "fifth_two",
		"ordinary", "expired", "renewal", "termination", "takeover",
	}
	runUserNames           = []string{"member", "future_member", "outsider"}
	failureCategoryPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
	shanghaiLocation       = time.FixedZone("Asia/Shanghai", 8*60*60)
	allowedAssertions      = map[string]struct{}{
		"auth.bootstrap_admin":                    {},
		"setup.fresh_synthetic_users":             {},
		"auth.fresh_synthetic_users":              {},
		"setup.synthetic_carpool_group":           {},
		"setup.current_plan_contract":             {},
		"setup.member_terms":                      {},
		"reset.execution_window_safety":           {},
		"reset.scope_preflight":                   {},
		"reset.admin_authorization":               {},
		"reset.register_missing_idempotency_key":  {},
		"reset.register_confirmed_qualification":  {},
		"reset.register_replay_original_response": {},
		"reset.register_conflicting_payload":      {},
		"reset.next_shanghai_2200_schedule":       {},
		"reset.schedule_success":                  {},
		"reset.schedule_replay_original_response": {},
		"reset.schedule_conflicting_payload":      {},
		"reset.execute_out_of_window_refused":     {},
		"reset.execute_refusal_preserves_batch":   {},
		"announcement.publish_within_20_seconds":  {},
		"announcement.version_audience":           {},
		"announcement.list_audience":              {},
		"announcement.mark_read_audience":         {},
		"announcement.merge_no_duplicate":         {},
		"announcement.merge_existing_audience":    {},
		"reset.report_completeness":               {},
		"reset_http.execution":                    {},
	}
)

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
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Category   string           `json:"category,omitempty"`
	HTTPStatus int              `json:"http_status,omitempty"`
	Numbers    map[string]int64 `json:"numbers,omitempty"`
}

type report struct {
	Version        int              `json:"version"`
	Source         string           `json:"source"`
	Scenario       string           `json:"scenario"`
	Assertions     []assertion      `json:"assertions"`
	ExcludedChecks []excludedCheck  `json:"excluded_checks"`
	SyntheticIDs   map[string]int64 `json:"synthetic_ids,omitempty"`
	Passed         int              `json:"passed"`
	Failed         int              `json:"failed"`
}

type excludedCheck struct {
	Name     string `json:"name"`
	Coverage string `json:"coverage"`
}

type dbConfig struct {
	Host, Port, Name, User, Password string
}

type sourceAnnouncement struct {
	ID              int64
	SourceType      string
	SourceID        int64
	SourceEventKind string
	SourceRevision  int
	Status          string
}

type responseEnvelope struct {
	Code     json.RawMessage   `json:"code"`
	Message  string            `json:"message"`
	Reason   string            `json:"reason,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Data     json.RawMessage   `json:"data,omitempty"`
}

type apiResponse struct {
	Status int
	Reason string
	Data   json.RawMessage
}

type apiClient struct {
	baseURL string
	client  *http.Client
}

func newAPIClient(base string) (*apiClient, error) {
	if base != applicationBaseURL {
		return nil, errors.New("invalid_base_url")
	}
	return &apiClient{baseURL: base, client: &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (a *apiClient) do(ctx context.Context, method, path, bearer, idempotency string, body any) (*apiResponse, error) {
	if !strings.HasPrefix(path, "/api/v1/") || strings.Contains(path, "//") {
		return nil, errors.New("invalid_api_path")
	}
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
	result := &apiResponse{Status: resp.StatusCode}
	if len(bytes.TrimSpace(raw)) == 0 {
		return result, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var envelope responseEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("decode_response")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing_response_data")
	}
	result.Reason = envelope.Reason
	result.Data = envelope.Data
	return result, nil
}

func decodeData[T any](resp *apiResponse) (T, error) {
	var value T
	if resp == nil || len(resp.Data) == 0 || string(resp.Data) == "null" {
		return value, errors.New("response_data_missing")
	}
	if err := json.Unmarshal(resp.Data, &value); err != nil {
		return value, errors.New("response_data_invalid")
	}
	return value, nil
}

type loginData struct {
	AccessToken string `json:"access_token"`
	User        struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
}

type createdUser struct {
	ID          int64   `json:"id"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	Balance     float64 `json:"balance"`
	Concurrency int     `json:"concurrency"`
}

type planSnapshot struct {
	PlanID         int64  `json:"plan_id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Version        int    `json:"version"`
	ListPriceCNY   string `json:"list_price_cny"`
	WeeklyQuotaUSD string `json:"weekly_quota_usd"`
	Cycle5QuotaUSD string `json:"cycle_5_quota_usd"`
	DurationDays   int    `json:"duration_days"`
	CycleDays      int    `json:"cycle_days"`
	BoostRatio     string `json:"boost_ratio"`
	BoostAmountUSD string `json:"boost_amount_usd"`
	BoostCount     int    `json:"boost_count"`
	RoundingMode   string `json:"rounding_mode"`
	Enabled        bool   `json:"enabled"`
	IsLatest       bool   `json:"is_latest,omitempty"`
}

type termProjection struct {
	ID           int64        `json:"id"`
	UserID       int64        `json:"user_id"`
	ScopeID      int64        `json:"scope_id"`
	GroupID      int64        `json:"group_id"`
	PlanID       int64        `json:"plan_id"`
	PlanSnapshot planSnapshot `json:"plan_snapshot"`
	StartsAt     time.Time    `json:"starts_at"`
	ExpiresAt    time.Time    `json:"expires_at"`
	Status       string       `json:"status"`
	CurrentCycle *struct {
		ID int64 `json:"id"`
	} `json:"current_cycle"`
}

type detailsProjection struct {
	ServerNow time.Time `json:"server_now"`
	Timezone  string    `json:"timezone"`
}

type resetBatch struct {
	ID                int64      `json:"id"`
	ScopeID           int64      `json:"scope_id"`
	Status            string     `json:"status"`
	DetectedAt        time.Time  `json:"detected_at"`
	QualifiedAt       *time.Time `json:"qualified_at"`
	SlotAt            *time.Time `json:"slot_at"`
	ScheduledAt       *time.Time `json:"scheduled_at"`
	ScheduleRevision  int        `json:"schedule_revision"`
	EffectiveAt       *time.Time `json:"effective_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	DelayReason       *string    `json:"delay_reason"`
	AnnouncementState string     `json:"announcement_state"`
	TargetCount       int        `json:"target_count"`
	GrantedUSD        string     `json:"granted_usd"`
}

type resetBatchPage struct {
	Items    []json.RawMessage `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Pages    int               `json:"pages"`
}

type announcementVersion struct {
	Version     string `json:"version"`
	UnreadCount int    `json:"unread_count"`
}

// Deliberately omit title and content. The driver needs only identity and read state.
type announcementItem struct {
	ID         int64      `json:"id"`
	NotifyMode string     `json:"notify_mode"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	ReadAt     *time.Time `json:"read_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type announcementSnapshot struct {
	Version announcementVersion
	Items   map[int64]announcementItem
}

type runner struct {
	api             *apiClient
	db              *sql.DB
	bootstrap       fixtureFile
	admin           fixtureUser
	runID           string
	runFixturesPath string
	users           map[string]fixtureUser
	tokens          map[string]string
	groupID         int64
	plan            planSnapshot
	termIDs         map[string]int64
	report          report
	scenario        string
	existingBatch   *resetBatch
	sourceAnn       sourceAnnouncement
}

func newRunner(api *apiClient, db *sql.DB, fixture fixtureFile, admin fixtureUser, runID, runFixturesPath string) *runner {
	r := &runner{
		api: api, db: db, bootstrap: fixture, admin: admin, runID: runID, runFixturesPath: runFixturesPath,
		users: map[string]fixtureUser{}, tokens: map[string]string{}, termIDs: map[string]int64{},
		report: report{
			Version: fixtureVersion, Source: fixtureSource, SyntheticIDs: map[string]int64{},
			ExcludedChecks: []excludedCheck{{Name: "reset.actual_due_execution", Coverage: "postgresql_covered"}},
		},
	}
	return r
}

func (r *runner) record(name string, err error, httpStatus int, numbers map[string]int64) error {
	status, category := "pass", ""
	if err != nil {
		status = "fail"
		category = failureCategory(err)
		r.report.Failed++
	} else {
		r.report.Passed++
	}
	r.report.Assertions = append(r.report.Assertions, assertion{Name: name, Status: status, Category: category, HTTPStatus: httpStatus, Numbers: numbers})
	return err
}

func failureCategory(err error) string {
	if err == nil {
		return ""
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

func expectStatus(resp *apiResponse, statuses ...int) error {
	if resp == nil {
		return errors.New("missing_response")
	}
	for _, status := range statuses {
		if resp.Status == status {
			return nil
		}
	}
	return errors.New("unexpected_status")
}

func responseStatus(resp *apiResponse) int {
	if resp == nil {
		return 0
	}
	return resp.Status
}

func validateFixture(f fixtureFile) (fixtureUser, error) {
	if f.Version != fixtureVersion || f.Source != fixtureSource || f.BaseURL != applicationBaseURL || f.MockURL != mockBaseURL || len(f.Users) != len(bootstrapNames) {
		return fixtureUser{}, errors.New("invalid_fixture_contract")
	}
	seenIDs := make(map[int64]struct{}, len(f.Users))
	for index, user := range f.Users {
		password, err := hex.DecodeString(user.Password)
		if user.Name != bootstrapNames[index] || user.Email != bootstrapEmail(user.Name) || user.ID <= 0 || err != nil || len(password) != fixturePasswordBytes {
			return fixtureUser{}, errors.New("invalid_fixture_user")
		}
		if _, duplicate := seenIDs[user.ID]; duplicate {
			return fixtureUser{}, errors.New("duplicate_fixture_user_id")
		}
		seenIDs[user.ID] = struct{}{}
	}
	if f.Users[0].ID != bootstrapAdminID {
		return fixtureUser{}, errors.New("invalid_bootstrap_admin_id")
	}
	return f.Users[0], nil
}

func bootstrapEmail(name string) string { return "carpool-test-" + name + "@example.invalid" }

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

func loadDBConfig() (dbConfig, error) {
	config := dbConfig{
		Host: os.Getenv("CARPOOL_RESET_DB_HOST"), Port: os.Getenv("CARPOOL_RESET_DB_PORT"),
		Name: os.Getenv("CARPOOL_RESET_DB_NAME"), User: os.Getenv("CARPOOL_RESET_DB_USER"), Password: os.Getenv("CARPOOL_RESET_DB_PASSWORD"),
	}
	if config.Host != trustedDBHost || config.Port != trustedDBPort || config.Name != trustedDBName || config.User != trustedDBUser || config.Password == "" {
		return dbConfig{}, errors.New("invalid_database_contract")
	}
	return config, nil
}

func openReadOnlyDB(ctx context.Context, config dbConfig) (*sql.DB, error) {
	dsn := &url.URL{Scheme: "postgres", Host: config.Host + ":" + config.Port, Path: config.Name, User: url.UserPassword(config.User, config.Password)}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	query.Set("application_name", "carpool_reset_http_acceptance")
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

func qualificationSourceHash(sourceEventKey string) string {
	sum := sha256.Sum256([]byte("reset-source:1:" + strings.TrimSpace(sourceEventKey)))
	return hex.EncodeToString(sum[:])
}

func (r *runner) verifySyntheticQualification(ctx context.Context, batchID int64, sourceEventKey string) error {
	var count int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_reset_qualifications WHERE scope_id=1 AND batch_id=$1 AND source='administrator' AND source_event_key_hash=$2`, batchID, qualificationSourceHash(sourceEventKey)).Scan(&count)
	if err != nil || count != 1 {
		return errors.New("synthetic_qualification_source_missing")
	}
	return nil
}

func (r *runner) loadSourceAnnouncement(ctx context.Context, batchID int64) (sourceAnnouncement, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,source_type,source_id,source_event_kind,source_revision,status FROM announcements WHERE source_type='carpool_reset' AND source_id=$1 AND source_event_kind='qualification' ORDER BY id`, batchID)
	if err != nil {
		return sourceAnnouncement{}, errors.New("source_announcement_query_failed")
	}
	defer rows.Close()
	items := make([]sourceAnnouncement, 0, 1)
	for rows.Next() {
		var item sourceAnnouncement
		if err := rows.Scan(&item.ID, &item.SourceType, &item.SourceID, &item.SourceEventKind, &item.SourceRevision, &item.Status); err != nil {
			return sourceAnnouncement{}, errors.New("source_announcement_scan_failed")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil || len(items) != 1 {
		return sourceAnnouncement{}, errors.New("source_announcement_count_invalid")
	}
	item := items[0]
	if item.ID <= 0 || item.SourceType != "carpool_reset" || item.SourceID != batchID || item.SourceEventKind != "qualification" || item.SourceRevision < 0 || item.Status != "active" {
		return sourceAnnouncement{}, errors.New("source_announcement_identity_invalid")
	}
	return item, nil
}

func (r *runner) waitSourceAnnouncement(ctx context.Context, batchID int64) (sourceAnnouncement, error) {
	deadline := time.NewTimer(announcementWait)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		item, err := r.loadSourceAnnouncement(ctx, batchID)
		if err == nil {
			return item, nil
		}
		select {
		case <-ctx.Done():
			return sourceAnnouncement{}, ctx.Err()
		case <-deadline.C:
			return sourceAnnouncement{}, errors.New("source_announcement_publish_timeout")
		case <-ticker.C:
		}
	}
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (r *runner) login(ctx context.Context, user fixtureUser, admin bool) (string, error) {
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/auth/login", "", "", map[string]any{"email": user.Email, "password": user.Password})
	if err != nil {
		return "", err
	}
	if err := expectStatus(resp, http.StatusOK); err != nil {
		return "", err
	}
	data, err := decodeData[loginData](resp)
	expectedRole := "user"
	if admin {
		expectedRole = "admin"
	}
	if err != nil || data.AccessToken == "" || data.User.ID != user.ID || data.User.Email != user.Email || data.User.Role != expectedRole {
		return "", errors.New("login_identity_mismatch")
	}
	return data.AccessToken, nil
}

func (r *runner) authenticateAdmin(ctx context.Context) error {
	token, err := r.login(ctx, r.admin, true)
	if err == nil {
		r.tokens["admin"] = token
	}
	return r.record("auth.bootstrap_admin", err, 0, map[string]int64{"user_id": r.admin.ID})
}

func runEmail(runID, name string) string {
	return "carpool-reset-http-" + runID + "-" + strings.ReplaceAll(name, "_", "-") + "@example.invalid"
}

func (r *runner) createFreshUsers(ctx context.Context) error {
	usedIDs := make(map[int64]struct{}, len(r.bootstrap.Users)+len(runUserNames))
	for _, user := range r.bootstrap.Users {
		usedIDs[user.ID] = struct{}{}
	}
	runFixture := fixtureFile{Version: fixtureVersion, Source: fixtureSource, BaseURL: applicationBaseURL, MockURL: mockBaseURL, Users: []fixtureUser{r.admin}}
	for _, name := range runUserNames {
		password, err := randomHex(fixturePasswordBytes)
		if err != nil {
			return r.record("setup.fresh_synthetic_users", errors.New("generate_run_password"), 0, nil)
		}
		email := runEmail(r.runID, name)
		resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/users", r.tokens["admin"], "", map[string]any{
			"email": email, "password": password, "username": "Reset HTTP " + name,
			"notes": "Synthetic local reset HTTP acceptance " + r.runID, "role": "user",
			"balance": 0, "concurrency": 20, "rpm_limit": 0,
			"allowed_groups": []int64{}, "restrict_public_groups": true,
		})
		if err == nil {
			err = expectStatus(resp, http.StatusOK)
		}
		var created createdUser
		if err == nil {
			created, err = decodeData[createdUser](resp)
		}
		if err == nil {
			_, reused := usedIDs[created.ID]
			if created.ID <= 0 || reused || created.Email != email || created.Role != "user" || created.Status != "active" || created.Balance != 0 || created.Concurrency != 20 {
				err = errors.New("created_user_contract_invalid")
			}
		}
		if err != nil {
			return r.record("setup.fresh_synthetic_users", err, responseStatus(resp), map[string]int64{"created_users": int64(len(r.users))})
		}
		user := fixtureUser{Name: name, ID: created.ID, Email: email, Password: password}
		usedIDs[user.ID] = struct{}{}
		r.users[name] = user
		runFixture.Users = append(runFixture.Users, user)
	}
	if err := writePrivateJSON(r.runFixturesPath, runFixture); err != nil {
		return r.record("setup.fresh_synthetic_users", errors.New("write_run_fixture"), 0, map[string]int64{"created_users": int64(len(r.users))})
	}
	for _, name := range runUserNames {
		r.report.SyntheticIDs["user_"+name] = r.users[name].ID
	}
	return r.record("setup.fresh_synthetic_users", nil, http.StatusOK, map[string]int64{"created_users": int64(len(r.users))})
}

func (r *runner) authenticateFreshUsers(ctx context.Context) error {
	for _, name := range runUserNames {
		token, err := r.login(ctx, r.users[name], false)
		if err != nil {
			return r.record("auth.fresh_synthetic_users", err, 0, map[string]int64{"authenticated_users": int64(len(r.tokens) - 1)})
		}
		r.tokens[name] = token
	}
	return r.record("auth.fresh_synthetic_users", nil, http.StatusOK, map[string]int64{"authenticated_users": int64(len(runUserNames))})
}

func (r *runner) createGroup(ctx context.Context) error {
	model := "reset-http-" + r.runID
	resp, err := r.api.do(ctx, http.MethodPost, "/api/v1/admin/groups", r.tokens["admin"], "reset-http-group-"+r.runID, map[string]any{
		"name": "reset-http-" + r.runID, "description": "synthetic local reset HTTP acceptance",
		"platform": "openai", "rate_multiplier": 1, "is_exclusive": true, "subscription_type": "carpool",
		"model_pricing": []any{map[string]any{"platform": "openai", "models": []string{model}, "billing_mode": "per_request", "per_request_price": 1}},
	})
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	if err == nil {
		var data struct {
			ID int64 `json:"id"`
		}
		data, err = decodeData[struct {
			ID int64 `json:"id"`
		}](resp)
		if err == nil {
			r.groupID = data.ID
			if r.groupID <= 0 {
				err = errors.New("group_id_invalid")
			}
		}
	}
	if err == nil {
		r.report.SyntheticIDs["group_id"] = r.groupID
	}
	return r.record("setup.synthetic_carpool_group", err, responseStatus(resp), map[string]int64{"group_id": r.groupID})
}

func exactKeys(raw json.RawMessage, allowed ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(allowed) {
		return false
	}
	for _, key := range allowed {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func decimalEqual(value, expected string) bool {
	left, leftOK := new(big.Rat).SetString(value)
	right, rightOK := new(big.Rat).SetString(expected)
	return leftOK && rightOK && left.Cmp(right) == 0
}

func decimalNonNegative(value string) bool {
	number, ok := new(big.Rat).SetString(value)
	return ok && number.Sign() >= 0
}

func validateFourSeatPlan(plan planSnapshot, raw json.RawMessage, includeLatest bool) error {
	keys := []string{"plan_id", "code", "name", "version", "list_price_cny", "weekly_quota_usd", "cycle_5_quota_usd", "duration_days", "cycle_days", "boost_ratio", "boost_amount_usd", "boost_count", "rounding_mode", "enabled"}
	if includeLatest {
		keys = append(keys, "is_latest")
	}
	if !exactKeys(raw, keys...) || plan.PlanID <= 0 || plan.Code != "four_seat" || plan.Name != "Four-seat" || plan.Version <= 0 || plan.DurationDays != 28 || plan.CycleDays != 7 || plan.BoostCount != 3 || plan.RoundingMode != "half_up" || !plan.Enabled || (includeLatest && !plan.IsLatest) {
		return errors.New("plan_contract_invalid")
	}
	for value, expected := range map[string]string{plan.ListPriceCNY: "330", plan.WeeklyQuotaUSD: "550", plan.Cycle5QuotaUSD: "157", plan.BoostRatio: "0.1", plan.BoostAmountUSD: "55"} {
		if !decimalEqual(value, expected) {
			return errors.New("plan_decimal_invalid")
		}
	}
	return nil
}

func (r *runner) loadPlan(ctx context.Context) error {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/plans", r.tokens["admin"], "", nil)
	if err == nil {
		err = expectStatus(resp, http.StatusOK)
	}
	var items []json.RawMessage
	if err == nil {
		err = json.Unmarshal(resp.Data, &items)
		if err != nil {
			err = errors.New("plan_list_invalid")
		}
	}
	matches := 0
	if err == nil {
		for _, raw := range items {
			var plan planSnapshot
			if json.Unmarshal(raw, &plan) != nil {
				err = errors.New("plan_list_invalid")
				break
			}
			if plan.Code == "four_seat" {
				matches++
				if validateErr := validateFourSeatPlan(plan, raw, true); validateErr != nil {
					err = validateErr
					break
				}
				r.plan = plan
			}
		}
		if err == nil && matches != 1 {
			err = errors.New("four_seat_plan_count_invalid")
		}
	}
	if err == nil {
		r.report.SyntheticIDs["plan_id"] = r.plan.PlanID
	}
	return r.record("setup.current_plan_contract", err, responseStatus(resp), map[string]int64{"plan_id": r.plan.PlanID, "plan_version": int64(r.plan.Version)})
}

func (r *runner) serverNow(ctx context.Context, name string) (time.Time, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/user/carpool/details", r.tokens[name], "", nil)
	if err != nil {
		return time.Time{}, err
	}
	if err := expectStatus(resp, http.StatusOK); err != nil {
		return time.Time{}, err
	}
	data, err := decodeData[detailsProjection](resp)
	if err != nil || data.ServerNow.IsZero() || data.Timezone != "Asia/Shanghai" {
		return time.Time{}, errors.New("server_clock_contract_invalid")
	}
	return data.ServerNow, nil
}

func (r *runner) openTerm(ctx context.Context, name string, startsAt *time.Time, expectedStatus string) (termProjection, error) {
	path := fmt.Sprintf("/api/v1/admin/users/%d/carpool/terms", r.users[name].ID)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "reset-http-term-"+name+"-"+r.runID, map[string]any{
		"plan_id": r.plan.PlanID, "group_id": r.groupID, "starts_at": startsAt, "mode": "new", "takeover": nil,
		"notes": "synthetic local reset HTTP acceptance " + r.runID, "payment": nil,
	})
	if err == nil {
		err = expectStatus(resp, http.StatusCreated)
	}
	if err != nil {
		return termProjection{}, err
	}
	term, err := decodeData[termProjection](resp)
	if err != nil {
		return termProjection{}, err
	}
	snapshotRaw := json.RawMessage(nil)
	var object map[string]json.RawMessage
	if json.Unmarshal(resp.Data, &object) == nil {
		snapshotRaw = object["plan_snapshot"]
	}
	if term.ID <= 0 || term.UserID != r.users[name].ID || term.ScopeID != 1 || term.GroupID != r.groupID || term.PlanID != r.plan.PlanID || term.Status != expectedStatus || term.ExpiresAt.IsZero() || !term.ExpiresAt.Equal(term.StartsAt.Add(28*24*time.Hour)) || validateFourSeatPlan(term.PlanSnapshot, snapshotRaw, false) != nil {
		return termProjection{}, errors.New("term_contract_invalid")
	}
	if expectedStatus == "active" && (term.CurrentCycle == nil || term.CurrentCycle.ID <= 0) {
		return termProjection{}, errors.New("active_term_cycle_missing")
	}
	if expectedStatus == "pending" && term.CurrentCycle != nil {
		return termProjection{}, errors.New("pending_term_has_current_cycle")
	}
	return term, nil
}

func (r *runner) createTerms(ctx context.Context) error {
	now, err := r.serverNow(ctx, "member")
	if err != nil {
		return r.record("setup.member_terms", err, 0, nil)
	}
	member, err := r.openTerm(ctx, "member", nil, "active")
	if err != nil {
		return r.record("setup.member_terms", err, 0, nil)
	}
	futureStart := now.Add(24 * time.Hour)
	future, err := r.openTerm(ctx, "future_member", &futureStart, "pending")
	if err == nil && !future.StartsAt.Equal(futureStart) {
		err = errors.New("future_term_start_mismatch")
	}
	if err == nil {
		r.termIDs["member"], r.termIDs["future_member"] = member.ID, future.ID
		r.report.SyntheticIDs["term_member"] = member.ID
		r.report.SyntheticIDs["term_future_member"] = future.ID
	}
	return r.record("setup.member_terms", err, 0, map[string]int64{"term_member": member.ID, "term_future_member": future.ID})
}

func nextShanghai22(now time.Time) time.Time {
	local := now.In(shanghaiLocation)
	next := time.Date(local.Year(), local.Month(), local.Day(), 22, 0, 0, 0, shanghaiLocation)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC()
}

func scheduleHasSafeLead(now time.Time, existing *resetBatch) bool {
	scheduledAt := nextShanghai22(now)
	if existing != nil {
		if existing.ScheduledAt == nil {
			return false
		}
		scheduledAt = *existing.ScheduledAt
	}
	return scheduledAt.Sub(now) >= minimumScheduleLead
}

func (r *runner) checkScheduleLead(ctx context.Context) error {
	now, err := r.serverNow(ctx, "member")
	if err == nil && !scheduleHasSafeLead(now, r.existingBatch) {
		err = errors.New("unsafe_near_execution_window")
	}
	return r.record("reset.execution_window_safety", err, 0, nil)
}

func validateResetBatch(raw json.RawMessage) (resetBatch, error) {
	allowed := []string{"id", "scope_id", "status", "detected_at", "qualified_at", "slot_at", "scheduled_at", "schedule_revision", "effective_at", "completed_at", "delay_reason", "announcement_state", "target_count", "granted_usd"}
	if !exactKeys(raw, allowed...) {
		return resetBatch{}, errors.New("reset_batch_fields_invalid")
	}
	var batch resetBatch
	if json.Unmarshal(raw, &batch) != nil {
		return resetBatch{}, errors.New("reset_batch_invalid")
	}
	if batch.ID <= 0 || batch.ScopeID != 1 || batch.DetectedAt.IsZero() || batch.ScheduleRevision < 0 || batch.TargetCount < 0 || !decimalNonNegative(batch.GrantedUSD) {
		return resetBatch{}, errors.New("reset_batch_values_invalid")
	}
	if batch.AnnouncementState != "pending" && batch.AnnouncementState != "published" && batch.AnnouncementState != "correction_pending" && batch.AnnouncementState != "completion_pending" {
		return resetBatch{}, errors.New("reset_announcement_state_invalid")
	}
	return batch, nil
}

func validateNewScheduledBatch(batch resetBatch) error {
	if batch.Status != "scheduled" || batch.QualifiedAt == nil || batch.SlotAt == nil || batch.ScheduledAt == nil || !batch.QualifiedAt.Equal(batch.DetectedAt) || batch.ScheduleRevision != 0 || batch.EffectiveAt != nil || batch.CompletedAt != nil || batch.DelayReason != nil || batch.TargetCount != 0 || !decimalEqual(batch.GrantedUSD, "0") {
		return errors.New("scheduled_batch_contract_invalid")
	}
	expected := nextShanghai22(batch.DetectedAt)
	if !batch.SlotAt.Equal(expected) || !batch.ScheduledAt.Equal(expected) {
		return errors.New("next_shanghai_2200_invalid")
	}
	return nil
}

func validateScheduledBatchShape(batch resetBatch) error {
	if batch.Status != "scheduled" || batch.QualifiedAt == nil || batch.SlotAt == nil || batch.ScheduledAt == nil || batch.EffectiveAt != nil || batch.CompletedAt != nil || batch.TargetCount != 0 || !decimalEqual(batch.GrantedUSD, "0") {
		return errors.New("scheduled_batch_contract_invalid")
	}
	localSlot := batch.SlotAt.In(shanghaiLocation)
	expectedSlot := time.Date(localSlot.Year(), localSlot.Month(), localSlot.Day(), 22, 0, 0, 0, shanghaiLocation)
	if !batch.SlotAt.Equal(expectedSlot) || batch.ScheduledAt.Before(*batch.SlotAt) || !batch.ScheduledAt.Before(batch.SlotAt.Add(time.Minute)) {
		return errors.New("scheduled_batch_shape_invalid")
	}
	return nil
}

func decodeResetBatchResponse(resp *apiResponse) (resetBatch, error) {
	if resp == nil {
		return resetBatch{}, errors.New("missing_response")
	}
	return validateResetBatch(resp.Data)
}

func (r *runner) listResetBatches(ctx context.Context) ([]resetBatch, error) {
	resp, err := r.api.do(ctx, http.MethodGet, "/api/v1/admin/carpool/reset-batches?scope_id=1&page_size=200", r.tokens["admin"], "", nil)
	if err != nil {
		return nil, err
	}
	if err := expectStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	page, err := decodeData[resetBatchPage](resp)
	if err != nil || page.Total != int64(len(page.Items)) || page.Page != 1 || page.PageSize != 200 {
		return nil, errors.New("reset_batch_page_invalid")
	}
	items := make([]resetBatch, 0, len(page.Items))
	for _, raw := range page.Items {
		batch, err := validateResetBatch(raw)
		if err != nil {
			return nil, err
		}
		items = append(items, batch)
	}
	return items, nil
}

func batchIDs(items []resetBatch) map[int64]struct{} {
	result := make(map[int64]struct{}, len(items))
	for _, item := range items {
		result[item.ID] = struct{}{}
	}
	return result
}

func sameIDSet(left, right map[int64]struct{}) bool { return reflect.DeepEqual(left, right) }

func findBatch(items []resetBatch, id int64) (resetBatch, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return resetBatch{}, false
}

func (r *runner) inspectResetScenario(ctx context.Context) error {
	items, err := r.listResetBatches(ctx)
	if err == nil {
		switch {
		case len(items) == 0:
			r.scenario = "first_publication"
		case len(items) == 1 && items[0].Status == "scheduled" && items[0].QualifiedAt != nil && items[0].SlotAt != nil && items[0].ScheduledAt != nil && items[0].EffectiveAt == nil && items[0].CompletedAt == nil && items[0].TargetCount == 0 && decimalEqual(items[0].GrantedUSD, "0"):
			r.scenario = "pending_merge"
			batch := items[0]
			r.existingBatch = &batch
		default:
			err = errors.New("reset_scope_preflight_invalid")
		}
	}
	if err == nil {
		r.report.Scenario = r.scenario
	}
	return r.record("reset.scope_preflight", err, http.StatusOK, map[string]int64{"reset_batch_count": int64(len(items))})
}

func (r *runner) checkAdminAuthorization(ctx context.Context) error {
	path := "/api/v1/admin/carpool/reset-batches?scope_id=1&page_size=1"
	userResp, err := r.api.do(ctx, http.MethodGet, path, r.tokens["outsider"], "", nil)
	if err == nil {
		err = expectStatus(userResp, http.StatusForbidden)
	}
	unauthResp, unauthErr := r.api.do(ctx, http.MethodGet, path, "", "", nil)
	if err == nil && unauthErr != nil {
		err = unauthErr
	}
	if err == nil {
		err = expectStatus(unauthResp, http.StatusUnauthorized)
	}
	writeResp, writeErr := r.api.do(ctx, http.MethodPost, "/api/v1/admin/carpool/reset-batches", r.tokens["outsider"], "reset-http-denied-"+r.runID, map[string]any{
		"scope_id": 1, "confirmed": true, "source_event_key": "reset-http-denied-" + r.runID, "reason": "synthetic authorization check",
	})
	if err == nil && writeErr != nil {
		err = writeErr
	}
	if err == nil {
		err = expectStatus(writeResp, http.StatusForbidden)
	}
	return r.record("reset.admin_authorization", err, responseStatus(writeResp), map[string]int64{"authenticated_read_status": int64(responseStatus(userResp)), "authenticated_write_status": int64(responseStatus(writeResp)), "unauthenticated_status": int64(responseStatus(unauthResp))})
}

func (r *runner) registerAndSchedule(ctx context.Context) (resetBatch, error) {
	before, err := r.listResetBatches(ctx)
	if err != nil {
		return resetBatch{}, err
	}
	beforeIDs := batchIDs(before)
	sourceEventKey := "reset-http-" + r.runID
	body := map[string]any{"scope_id": 1, "confirmed": true, "source_event_key": sourceEventKey, "reason": "synthetic local qualification"}
	missingResp, missingErr := r.api.do(ctx, http.MethodPost, "/api/v1/admin/carpool/reset-batches", r.tokens["admin"], "", body)
	if missingErr == nil {
		missingErr = expectStatus(missingResp, http.StatusBadRequest)
	}
	if missingErr == nil {
		afterMissing, listErr := r.listResetBatches(ctx)
		if listErr != nil || !sameIDSet(beforeIDs, batchIDs(afterMissing)) {
			missingErr = errors.New("missing_key_mutated_reset_state")
		}
	}
	r.record("reset.register_missing_idempotency_key", missingErr, responseStatus(missingResp), nil)
	if missingErr != nil {
		return resetBatch{}, missingErr
	}

	key := "reset-http-register-" + r.runID
	registerResp, registerErr := r.api.do(ctx, http.MethodPost, "/api/v1/admin/carpool/reset-batches", r.tokens["admin"], key, body)
	if registerErr == nil {
		registerErr = expectStatus(registerResp, http.StatusCreated)
	}
	var batch resetBatch
	if registerErr == nil {
		batch, registerErr = decodeResetBatchResponse(registerResp)
	}
	if registerErr == nil {
		_, existed := beforeIDs[batch.ID]
		if r.scenario == "first_publication" {
			if existed {
				registerErr = errors.New("reset_batch_reused")
			} else {
				registerErr = validateNewScheduledBatch(batch)
			}
		} else if !existed || r.existingBatch == nil || batch.ID != r.existingBatch.ID || batch.ScheduleRevision != r.existingBatch.ScheduleRevision || batch.ScheduledAt == nil || !batch.ScheduledAt.Equal(*r.existingBatch.ScheduledAt) {
			registerErr = errors.New("pending_batch_merge_invalid")
		}
	}
	if registerErr == nil {
		registerErr = r.verifySyntheticQualification(ctx, batch.ID, sourceEventKey)
	}
	if registerErr == nil {
		after, listErr := r.listResetBatches(ctx)
		listed, found := findBatch(after, batch.ID)
		expectedCount := len(before) + 1
		if r.scenario == "pending_merge" {
			expectedCount = len(before)
		}
		if listErr != nil || !found || len(after) != expectedCount || listed.Status != batch.Status || listed.ScheduleRevision != batch.ScheduleRevision || listed.ScheduledAt == nil || !listed.ScheduledAt.Equal(*batch.ScheduledAt) {
			registerErr = errors.New("registered_batch_not_listed")
		}
	}
	if registerErr == nil {
		r.report.SyntheticIDs["reset_batch_id"] = batch.ID
	}
	r.record("reset.register_confirmed_qualification", registerErr, responseStatus(registerResp), map[string]int64{"reset_batch_id": batch.ID})
	if registerErr != nil {
		return resetBatch{}, registerErr
	}

	replayResp, replayErr := r.api.do(ctx, http.MethodPost, "/api/v1/admin/carpool/reset-batches", r.tokens["admin"], key, body)
	if replayErr == nil {
		replayErr = expectStatus(replayResp, http.StatusCreated)
	}
	if replayErr == nil {
		replayed, decodeErr := decodeResetBatchResponse(replayResp)
		if decodeErr != nil || !reflect.DeepEqual(batch, replayed) {
			replayErr = errors.New("register_replay_response_changed")
		}
	}
	r.record("reset.register_replay_original_response", replayErr, responseStatus(replayResp), map[string]int64{"reset_batch_id": batch.ID})
	if replayErr != nil {
		return resetBatch{}, replayErr
	}

	conflictBody := map[string]any{"scope_id": 1, "confirmed": true, "source_event_key": "reset-http-" + r.runID, "reason": "different synthetic qualification"}
	conflictResp, conflictErr := r.api.do(ctx, http.MethodPost, "/api/v1/admin/carpool/reset-batches", r.tokens["admin"], key, conflictBody)
	if conflictErr == nil {
		conflictErr = expectStatus(conflictResp, http.StatusConflict)
	}
	if conflictErr == nil && conflictResp.Reason != "CARPOOL_IDEMPOTENCY_CONFLICT" {
		conflictErr = errors.New("register_conflict_reason_invalid")
	}
	r.record("reset.register_conflicting_payload", conflictErr, responseStatus(conflictResp), map[string]int64{"reset_batch_id": batch.ID})
	if conflictErr != nil {
		return resetBatch{}, conflictErr
	}

	scheduleErr := validateNewScheduledBatch(batch)
	if r.scenario == "pending_merge" {
		scheduleErr = validateScheduledBatchShape(batch)
	}
	r.record("reset.next_shanghai_2200_schedule", scheduleErr, http.StatusCreated, map[string]int64{"reset_batch_id": batch.ID})
	if scheduleErr != nil {
		return resetBatch{}, scheduleErr
	}

	schedulePath := fmt.Sprintf("/api/v1/admin/carpool/reset-batches/%d/schedule", batch.ID)
	scheduleBody := map[string]any{"reason": "synthetic schedule review"}
	scheduleKey := "reset-http-schedule-" + r.runID
	scheduleResp, scheduleErr := r.api.do(ctx, http.MethodPost, schedulePath, r.tokens["admin"], scheduleKey, scheduleBody)
	if scheduleErr == nil {
		scheduleErr = expectStatus(scheduleResp, http.StatusOK)
	}
	var scheduled resetBatch
	if scheduleErr == nil {
		scheduled, scheduleErr = decodeResetBatchResponse(scheduleResp)
	}
	if scheduleErr == nil && (scheduled.ID != batch.ID || scheduled.Status != "scheduled" || scheduled.ScheduleRevision != 0 || scheduled.ScheduledAt == nil || !scheduled.ScheduledAt.Equal(*batch.ScheduledAt) || scheduled.TargetCount != 0 || !decimalEqual(scheduled.GrantedUSD, "0")) {
		scheduleErr = errors.New("schedule_response_invalid")
	}
	r.record("reset.schedule_success", scheduleErr, responseStatus(scheduleResp), map[string]int64{"reset_batch_id": batch.ID, "schedule_revision": int64(scheduled.ScheduleRevision)})
	if scheduleErr != nil {
		return resetBatch{}, scheduleErr
	}

	scheduleReplayResp, scheduleReplayErr := r.api.do(ctx, http.MethodPost, schedulePath, r.tokens["admin"], scheduleKey, scheduleBody)
	if scheduleReplayErr == nil {
		scheduleReplayErr = expectStatus(scheduleReplayResp, http.StatusOK)
	}
	if scheduleReplayErr == nil {
		replayed, decodeErr := decodeResetBatchResponse(scheduleReplayResp)
		if decodeErr != nil || !reflect.DeepEqual(scheduled, replayed) {
			scheduleReplayErr = errors.New("schedule_replay_response_changed")
		}
	}
	r.record("reset.schedule_replay_original_response", scheduleReplayErr, responseStatus(scheduleReplayResp), map[string]int64{"reset_batch_id": batch.ID})
	if scheduleReplayErr != nil {
		return resetBatch{}, scheduleReplayErr
	}

	scheduleConflictResp, scheduleConflictErr := r.api.do(ctx, http.MethodPost, schedulePath, r.tokens["admin"], scheduleKey, map[string]any{"reason": "different synthetic schedule review"})
	if scheduleConflictErr == nil {
		scheduleConflictErr = expectStatus(scheduleConflictResp, http.StatusConflict)
	}
	if scheduleConflictErr == nil && scheduleConflictResp.Reason != "CARPOOL_IDEMPOTENCY_CONFLICT" {
		scheduleConflictErr = errors.New("schedule_conflict_reason_invalid")
	}
	r.record("reset.schedule_conflicting_payload", scheduleConflictErr, responseStatus(scheduleConflictResp), map[string]int64{"reset_batch_id": batch.ID})
	if scheduleConflictErr != nil {
		return resetBatch{}, scheduleConflictErr
	}
	return scheduled, nil
}

func (r *runner) refuseEarlyExecution(ctx context.Context, scheduled resetBatch) error {
	path := fmt.Sprintf("/api/v1/admin/carpool/reset-batches/%d/execute", scheduled.ID)
	resp, err := r.api.do(ctx, http.MethodPost, path, r.tokens["admin"], "reset-http-execute-"+r.runID, struct{}{})
	if err == nil {
		err = expectStatus(resp, http.StatusConflict)
	}
	if err == nil && resp.Reason != "CARPOOL_RESET_NOT_DUE" {
		err = errors.New("execute_refusal_reason_invalid")
	}
	r.record("reset.execute_out_of_window_refused", err, responseStatus(resp), map[string]int64{"reset_batch_id": scheduled.ID})
	if err != nil {
		return err
	}
	items, err := r.listResetBatches(ctx)
	current, found := findBatch(items, scheduled.ID)
	if err != nil || !found || current.Status != "scheduled" || current.ScheduleRevision != scheduled.ScheduleRevision || current.ScheduledAt == nil || !current.ScheduledAt.Equal(*scheduled.ScheduledAt) || current.EffectiveAt != nil || current.CompletedAt != nil || current.TargetCount != 0 || !decimalEqual(current.GrantedUSD, "0") {
		err = errors.New("execute_refusal_mutated_batch")
	}
	return r.record("reset.execute_refusal_preserves_batch", err, http.StatusOK, map[string]int64{"reset_batch_id": scheduled.ID, "target_count": int64(current.TargetCount)})
}

func (r *runner) announcementSnapshot(ctx context.Context, name string, unreadOnly bool) (announcementSnapshot, error) {
	snapshotCtx, cancel := context.WithTimeout(ctx, announcementSnapshotWait)
	defer cancel()

	path := "/api/v1/announcements"
	if unreadOnly {
		path += "?unread_only=true"
	}
	for {
		var versions [2]announcementVersion
		var byID map[int64]announcementItem
		unreadCount := 0
		for index := range versions {
			versionResp, err := r.api.do(snapshotCtx, http.MethodGet, "/api/v1/announcements/version", r.tokens[name], "", nil)
			if err != nil {
				return announcementSnapshot{}, err
			}
			if err := expectStatus(versionResp, http.StatusOK); err != nil {
				return announcementSnapshot{}, err
			}
			version, err := decodeData[announcementVersion](versionResp)
			if err != nil || version.Version == "" || version.UnreadCount < 0 {
				return announcementSnapshot{}, errors.New("announcement_version_invalid")
			}
			versions[index] = version
			if index != 0 {
				continue
			}

			listResp, err := r.api.do(snapshotCtx, http.MethodGet, path, r.tokens[name], "", nil)
			if err != nil {
				return announcementSnapshot{}, err
			}
			if err := expectStatus(listResp, http.StatusOK); err != nil {
				return announcementSnapshot{}, err
			}
			items, err := decodeData[[]announcementItem](listResp)
			if err != nil {
				return announcementSnapshot{}, err
			}
			byID = make(map[int64]announcementItem, len(items))
			for _, item := range items {
				if item.ID <= 0 || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
					return announcementSnapshot{}, errors.New("announcement_identity_invalid")
				}
				if _, duplicate := byID[item.ID]; duplicate {
					return announcementSnapshot{}, errors.New("announcement_id_duplicate")
				}
				byID[item.ID] = item
				if item.ReadAt == nil {
					unreadCount++
				}
			}
		}
		if versions[0] == versions[1] && versions[1].UnreadCount == unreadCount {
			return announcementSnapshot{Version: versions[1], Items: byID}, nil
		}

		retry := time.NewTimer(announcementSnapshotRetry)
		select {
		case <-ctx.Done():
			retry.Stop()
			return announcementSnapshot{}, ctx.Err()
		case <-snapshotCtx.Done():
			retry.Stop()
			if err := ctx.Err(); err != nil {
				return announcementSnapshot{}, err
			}
			return announcementSnapshot{}, errors.New("announcement_snapshot_incoherent")
		case <-retry.C:
		}
	}
}

func (r *runner) baselineAnnouncements(ctx context.Context) (map[string]announcementSnapshot, error) {
	result := make(map[string]announcementSnapshot, len(runUserNames))
	for _, name := range runUserNames {
		snapshot, err := r.announcementSnapshot(ctx, name, false)
		if err != nil {
			return nil, err
		}
		result[name] = snapshot
	}
	return result, nil
}

func (r *runner) prepareAnnouncementBaseline(ctx context.Context) (map[string]announcementSnapshot, error) {
	if r.scenario == "pending_merge" {
		item, err := r.waitSourceAnnouncement(ctx, r.existingBatch.ID)
		if err != nil {
			return nil, err
		}
		r.sourceAnn = item
	}
	baseline, err := r.baselineAnnouncements(ctx)
	if err != nil {
		return nil, err
	}
	if r.scenario == "pending_merge" {
		memberItem, memberVisible := baseline["member"].Items[r.sourceAnn.ID]
		futureItem, futureVisible := baseline["future_member"].Items[r.sourceAnn.ID]
		_, outsiderVisible := baseline["outsider"].Items[r.sourceAnn.ID]
		if !memberVisible || !futureVisible || outsiderVisible || memberItem.ReadAt != nil || futureItem.ReadAt != nil {
			return nil, errors.New("existing_source_announcement_audience_invalid")
		}
	}
	return baseline, nil
}

func newAnnouncementIDs(before, after map[int64]announcementItem) []int64 {
	ids := make([]int64, 0)
	for id := range after {
		if _, existed := before[id]; !existed {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (r *runner) waitForAnnouncement(ctx context.Context, baseline map[string]announcementSnapshot, batch resetBatch) (int64, map[string]announcementSnapshot, error) {
	waitCtx, cancel := context.WithTimeout(ctx, announcementWait)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last map[string]announcementSnapshot
	for {
		current, err := r.baselineAnnouncements(waitCtx)
		if err == nil && waitCtx.Err() == nil {
			last = current
			memberNew := newAnnouncementIDs(baseline["member"].Items, current["member"].Items)
			futureNew := newAnnouncementIDs(baseline["future_member"].Items, current["future_member"].Items)
			outsiderNew := newAnnouncementIDs(baseline["outsider"].Items, current["outsider"].Items)
			if len(memberNew) == 1 && len(futureNew) == 1 && len(outsiderNew) == 0 && memberNew[0] == futureNew[0] {
				item := current["member"].Items[memberNew[0]]
				futureItem := current["future_member"].Items[memberNew[0]]
				if item.NotifyMode == "popup" && item.ReadAt == nil && futureItem.ReadAt == nil && !item.CreatedAt.Before(batch.DetectedAt) && item.CreatedAt.Equal(futureItem.CreatedAt) && waitCtx.Err() == nil {
					return memberNew[0], current, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return 0, last, ctx.Err()
		case <-waitCtx.Done():
			if err := ctx.Err(); err != nil {
				return 0, last, err
			}
			return 0, last, errors.New("announcement_publish_timeout")
		case <-ticker.C:
		}
	}
}

func (r *runner) validateAnnouncementAudience(ctx context.Context, baseline map[string]announcementSnapshot, batch resetBatch) error {
	announcementID, current, err := r.waitForAnnouncement(ctx, baseline, batch)
	if err == nil {
		source, sourceErr := r.loadSourceAnnouncement(ctx, batch.ID)
		if sourceErr != nil || source.ID != announcementID || source.SourceRevision != batch.ScheduleRevision {
			err = errors.New("source_announcement_mismatch")
		}
	}
	if err == nil {
		r.report.SyntheticIDs["announcement_id"] = announcementID
	}
	r.record("announcement.publish_within_20_seconds", err, http.StatusOK, map[string]int64{"reset_batch_id": batch.ID, "announcement_id": announcementID})
	if err != nil {
		return err
	}

	versionErr := error(nil)
	if current["member"].Version.Version == baseline["member"].Version.Version || current["future_member"].Version.Version == baseline["future_member"].Version.Version || current["outsider"].Version.Version != baseline["outsider"].Version.Version || current["member"].Version.UnreadCount != baseline["member"].Version.UnreadCount+1 || current["future_member"].Version.UnreadCount != baseline["future_member"].Version.UnreadCount+1 || current["outsider"].Version.UnreadCount != baseline["outsider"].Version.UnreadCount {
		versionErr = errors.New("announcement_version_audience_invalid")
	}
	r.record("announcement.version_audience", versionErr, http.StatusOK, map[string]int64{"announcement_id": announcementID})
	if versionErr != nil {
		return versionErr
	}

	listErr := error(nil)
	if _, memberVisible := current["member"].Items[announcementID]; !memberVisible {
		listErr = errors.New("announcement_member_visibility_invalid")
	}
	if _, futureVisible := current["future_member"].Items[announcementID]; !futureVisible {
		listErr = errors.New("announcement_future_visibility_invalid")
	}
	if _, outsiderVisible := current["outsider"].Items[announcementID]; outsiderVisible {
		listErr = errors.New("announcement_outsider_visibility_invalid")
	}
	r.record("announcement.list_audience", listErr, http.StatusOK, map[string]int64{"announcement_id": announcementID})
	if listErr != nil {
		return listErr
	}
	return r.markAnnouncementRead(ctx, announcementID, current)
}

func (r *runner) markAnnouncementRead(ctx context.Context, announcementID int64, current map[string]announcementSnapshot) error {
	markPath := fmt.Sprintf("/api/v1/announcements/%d/read", announcementID)
	outsiderResp, markErr := r.api.do(ctx, http.MethodPost, markPath, r.tokens["outsider"], "", nil)
	if markErr == nil {
		markErr = expectStatus(outsiderResp, http.StatusNotFound)
	}
	memberResp, memberErr := r.api.do(ctx, http.MethodPost, markPath, r.tokens["member"], "", nil)
	if markErr == nil && memberErr != nil {
		markErr = memberErr
	}
	if markErr == nil {
		markErr = expectStatus(memberResp, http.StatusOK)
	}
	memberAfter, memberAfterErr := r.announcementSnapshot(ctx, "member", false)
	memberUnread, memberUnreadErr := r.announcementSnapshot(ctx, "member", true)
	futureAfter, futureAfterErr := r.announcementSnapshot(ctx, "future_member", false)
	if markErr == nil && (memberAfterErr != nil || memberUnreadErr != nil || futureAfterErr != nil) {
		markErr = errors.New("announcement_post_read_fetch_failed")
	}
	if markErr == nil {
		memberItem, memberVisible := memberAfter.Items[announcementID]
		_, unreadVisible := memberUnread.Items[announcementID]
		futureItem, futureVisible := futureAfter.Items[announcementID]
		if !memberVisible || memberItem.ReadAt == nil || unreadVisible || memberAfter.Version.Version == current["member"].Version.Version || memberAfter.Version.UnreadCount != current["member"].Version.UnreadCount-1 || !futureVisible || futureItem.ReadAt != nil || futureAfter.Version.Version != current["future_member"].Version.Version || futureAfter.Version.UnreadCount != current["future_member"].Version.UnreadCount {
			markErr = errors.New("announcement_mark_read_audience_invalid")
		}
	}
	return r.record("announcement.mark_read_audience", markErr, responseStatus(memberResp), map[string]int64{"announcement_id": announcementID, "outsider_status": int64(responseStatus(outsiderResp))})
}

func (r *runner) validateMergeAnnouncementAudience(ctx context.Context, baseline map[string]announcementSnapshot, batch resetBatch) error {
	currentSource, err := r.loadSourceAnnouncement(ctx, batch.ID)
	if err == nil && !reflect.DeepEqual(currentSource, r.sourceAnn) {
		err = errors.New("merge_source_announcement_changed")
	}
	current, snapshotErr := r.baselineAnnouncements(ctx)
	if err == nil && snapshotErr != nil {
		err = snapshotErr
	}
	if err == nil {
		for _, name := range runUserNames {
			if len(newAnnouncementIDs(baseline[name].Items, current[name].Items)) != 0 || current[name].Version.Version != baseline[name].Version.Version || current[name].Version.UnreadCount != baseline[name].Version.UnreadCount {
				err = errors.New("merge_published_duplicate_announcement")
				break
			}
		}
	}
	r.record("announcement.merge_no_duplicate", err, http.StatusOK, map[string]int64{"reset_batch_id": batch.ID, "announcement_id": r.sourceAnn.ID})
	if err != nil {
		return err
	}
	memberItem, memberVisible := current["member"].Items[r.sourceAnn.ID]
	futureItem, futureVisible := current["future_member"].Items[r.sourceAnn.ID]
	_, outsiderVisible := current["outsider"].Items[r.sourceAnn.ID]
	if !memberVisible || !futureVisible || outsiderVisible || memberItem.ReadAt != nil || futureItem.ReadAt != nil {
		err = errors.New("merge_existing_announcement_audience_invalid")
	}
	r.record("announcement.merge_existing_audience", err, http.StatusOK, map[string]int64{"announcement_id": r.sourceAnn.ID})
	if err != nil {
		return err
	}
	r.report.SyntheticIDs["announcement_id"] = r.sourceAnn.ID
	return r.markAnnouncementRead(ctx, r.sourceAnn.ID, current)
}

func (r *runner) run(ctx context.Context) error {
	if err := r.authenticateAdmin(ctx); err != nil {
		return err
	}
	if err := r.inspectResetScenario(ctx); err != nil {
		return err
	}
	if err := r.createFreshUsers(ctx); err != nil {
		return err
	}
	if err := r.authenticateFreshUsers(ctx); err != nil {
		return err
	}
	if err := r.createGroup(ctx); err != nil {
		return err
	}
	if err := r.loadPlan(ctx); err != nil {
		return err
	}
	if err := r.createTerms(ctx); err != nil {
		return err
	}
	if err := r.checkScheduleLead(ctx); err != nil {
		return err
	}
	baseline, err := r.prepareAnnouncementBaseline(ctx)
	if err != nil {
		return err
	}
	if err := r.checkAdminAuthorization(ctx); err != nil {
		return err
	}
	batch, err := r.registerAndSchedule(ctx)
	if err != nil {
		return err
	}
	if err := r.refuseEarlyExecution(ctx, batch); err != nil {
		return err
	}
	if r.scenario == "pending_merge" {
		return r.validateMergeAnnouncementAudience(ctx, baseline, batch)
	}
	return r.validateAnnouncementAudience(ctx, baseline, batch)
}

func validateReport(value report) error {
	if value.Version != fixtureVersion || value.Source != fixtureSource || (value.Scenario != "" && value.Scenario != "first_publication" && value.Scenario != "pending_merge") || len(value.ExcludedChecks) != 1 || value.ExcludedChecks[0].Name != "reset.actual_due_execution" || value.ExcludedChecks[0].Coverage != "postgresql_covered" {
		return errors.New("report_envelope_invalid")
	}
	seen := make(map[string]struct{}, len(value.Assertions))
	passed, failed := 0, 0
	for _, item := range value.Assertions {
		if _, allowed := allowedAssertions[item.Name]; !allowed {
			return errors.New("report_assertion_name_invalid")
		}
		if _, duplicate := seen[item.Name]; duplicate {
			return errors.New("report_assertion_duplicate")
		}
		seen[item.Name] = struct{}{}
		switch item.Status {
		case "pass":
			passed++
			if item.Category != "" {
				return errors.New("report_pass_category_invalid")
			}
		case "fail":
			failed++
			if !failureCategoryPattern.MatchString(item.Category) {
				return errors.New("report_failure_category_invalid")
			}
		default:
			return errors.New("report_status_invalid")
		}
	}
	if passed != value.Passed || failed != value.Failed || len(value.Assertions) != value.Passed+value.Failed {
		return errors.New("report_counts_invalid")
	}
	return nil
}

func writePrivateJSON(path string, value any) (resultErr error) {
	if path == "" || filepath.Clean(path) == "." {
		return errors.New("output_path_missing")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	random, err := randomHex(6)
	if err != nil {
		return err
	}
	temporary := path + ".pending-" + random
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporary)
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
	if err = os.Link(temporary, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return errors.New("output_exists")
		}
		return err
	}
	return nil
}

func pathsAreDistinct(paths ...string) bool {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return false
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func main() {
	fixturesPath := flag.String("fixtures", "", "path to the validated bootstrap fixture")
	runFixturesPath := flag.String("run-fixtures", "", "path for private run-owned fixture output")
	reportPath := flag.String("report", "", "path for the sanitized acceptance report")
	flag.Parse()
	if *fixturesPath == "" || *runFixturesPath == "" || *reportPath == "" || !pathsAreDistinct(*fixturesPath, *runFixturesPath, *reportPath) {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: -fixtures, -run-fixtures and -report must be distinct")
		os.Exit(2)
	}
	fixture, admin, err := loadFixture(*fixturesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: invalid synthetic fixture")
		os.Exit(2)
	}
	api, err := newAPIClient(fixture.BaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: invalid application URL")
		os.Exit(2)
	}
	runID, err := randomHex(8)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: random source unavailable")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dbConfig, err := loadDBConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: invalid private database contract")
		os.Exit(2)
	}
	db, err := openReadOnlyDB(ctx, dbConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: read-only database unavailable")
		os.Exit(2)
	}
	defer db.Close()
	r := newRunner(api, db, fixture, admin, runID, *runFixturesPath)
	runErr := r.run(ctx)
	if runErr != nil && r.report.Failed == 0 {
		r.record("reset_http.execution", runErr, 0, nil)
	}
	if reportErr := validateReport(r.report); reportErr != nil {
		if runErr == nil {
			runErr = reportErr
			r.record("reset.report_completeness", reportErr, 0, nil)
		}
	}
	if err := writePrivateJSON(*reportPath, r.report); err != nil {
		fmt.Fprintln(os.Stderr, "reset HTTP acceptance: report write failed")
		os.Exit(2)
	}
	fmt.Printf("reset HTTP acceptance: passed=%d failed=%d\n", r.report.Passed, r.report.Failed)
	if runErr != nil || r.report.Failed > 0 {
		os.Exit(1)
	}
}

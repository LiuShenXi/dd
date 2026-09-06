package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validFixtureForTest() fixtureFile {
	fixture := fixtureFile{Version: fixtureVersion, Source: fixtureSource, BaseURL: applicationBaseURL, MockURL: mockBaseURL}
	for index, name := range bootstrapNames {
		fixture.Users = append(fixture.Users, fixtureUser{
			Name: name, ID: bootstrapAdminID + int64(index), Email: bootstrapEmail(name), Password: strings.Repeat("a", fixturePasswordBytes*2),
		})
	}
	return fixture
}

func TestValidateFixtureRequiresExactBootstrapContract(t *testing.T) {
	fixture := validFixtureForTest()
	admin, err := validateFixture(fixture)
	if err != nil || admin.ID != bootstrapAdminID {
		t.Fatalf("valid fixture rejected: admin=%#v err=%v", admin, err)
	}

	tests := []struct {
		name   string
		mutate func(*fixtureFile)
	}{
		{name: "source", mutate: func(f *fixtureFile) { f.Source = "production" }},
		{name: "base URL", mutate: func(f *fixtureFile) { f.BaseURL += "/" }},
		{name: "mock URL", mutate: func(f *fixtureFile) { f.MockURL = "http://127.0.0.1:8090" }},
		{name: "admin ID", mutate: func(f *fixtureFile) { f.Users[0].ID = 99 }},
		{name: "order", mutate: func(f *fixtureFile) { f.Users[1], f.Users[2] = f.Users[2], f.Users[1] }},
		{name: "email", mutate: func(f *fixtureFile) { f.Users[1].Email = "copied@example.invalid" }},
		{name: "password", mutate: func(f *fixtureFile) { f.Users[1].Password = "not-hex" }},
		{name: "duplicate ID", mutate: func(f *fixtureFile) { f.Users[1].ID = f.Users[0].ID }},
		{name: "extra user", mutate: func(f *fixtureFile) { f.Users = append(f.Users, fixtureUser{Name: "extra"}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := validFixtureForTest()
			test.mutate(&candidate)
			if _, err := validateFixture(candidate); err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

func TestLoadFixtureRejectsUnknownAndTrailingJSON(t *testing.T) {
	fixture := validFixtureForTest()
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{
		"trailing": append(append([]byte{}, encoded...), []byte("\n{}")...),
		"unknown":  append(encoded[:len(encoded)-1], []byte(`,"unexpected":true}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.json")
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadFixture(path); err == nil {
				t.Fatal("invalid fixture JSON accepted")
			}
		})
	}
}

func TestAPIClientAcceptsOnlyExactApplicationURL(t *testing.T) {
	if _, err := newAPIClient(applicationBaseURL); err != nil {
		t.Fatalf("exact URL rejected: %v", err)
	}
	for _, candidate := range []string{
		applicationBaseURL + "/",
		applicationBaseURL + "?target=other",
		"http://user@carpool-app:8080",
		"https://carpool-app:8080",
		"http://127.0.0.1:8080",
		mockBaseURL,
	} {
		if _, err := newAPIClient(candidate); err == nil {
			t.Fatalf("URL variant accepted: %q", candidate)
		}
	}
}

func TestAPIClientAcceptsMiddlewareAndBusinessEnvelopes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantReason string
		wantData   bool
	}{
		{name: "unauthorized string code", status: http.StatusUnauthorized, body: `{"code":"UNAUTHORIZED","message":"Authorization required"}`},
		{name: "forbidden string code", status: http.StatusForbidden, body: `{"code":"FORBIDDEN","message":"Admin access required"}`},
		{name: "business numeric code", status: http.StatusConflict, body: `{"code":409,"message":"conflict","reason":"CARPOOL_IDEMPOTENCY_CONFLICT","data":{"id":41}}`, wantReason: "CARPOOL_IDEMPOTENCY_CONFLICT", wantData: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := &apiClient{baseURL: server.URL, client: server.Client()}
			resp, err := client.do(context.Background(), http.MethodGet, "/api/v1/admin/carpool/reset-batches", "", "", nil)
			if err != nil {
				t.Fatalf("valid envelope rejected: %v", err)
			}
			if resp.Status != test.status || resp.Reason != test.wantReason || (len(resp.Data) > 0) != test.wantData {
				t.Fatalf("response = %#v", resp)
			}
		})
	}
}

func TestAPIClientRejectsMalformedAndTrailingResponses(t *testing.T) {
	for name, body := range map[string]string{
		"non JSON":      "permission denied",
		"trailing JSON": `{"code":"FORBIDDEN","message":"denied"}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := &apiClient{baseURL: server.URL, client: server.Client()}
			if _, err := client.do(context.Background(), http.MethodGet, "/api/v1/admin/carpool/reset-batches", "", "", nil); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}

func TestWritePrivateJSONIsPrivateAndRefusesOverwrite(t *testing.T) {
	for _, name := range []string{"run-fixtures.json", "report.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := writePrivateJSON(path, map[string]int{"version": 1}); err != nil {
				t.Fatalf("first write failed: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatalf("mode = %o, want 600", info.Mode().Perm())
			}
			if err := writePrivateJSON(path, map[string]int{"version": 2}); err == nil {
				t.Fatal("existing private output was overwritten")
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), `"version": 1`) {
				t.Fatalf("original output changed: %q err=%v", data, err)
			}
		})
	}
}

func TestNextShanghai22QualificationBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before slot",
			now:  time.Date(2026, 9, 6, 21, 59, 59, 999999999, shanghaiLocation),
			want: time.Date(2026, 9, 6, 22, 0, 0, 0, shanghaiLocation),
		},
		{
			name: "exact slot",
			now:  time.Date(2026, 9, 6, 22, 0, 0, 0, shanghaiLocation),
			want: time.Date(2026, 9, 7, 22, 0, 0, 0, shanghaiLocation),
		},
		{
			name: "after slot",
			now:  time.Date(2026, 9, 6, 22, 0, 0, 1, shanghaiLocation),
			want: time.Date(2026, 9, 7, 22, 0, 0, 0, shanghaiLocation),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nextShanghai22(test.now); !got.Equal(test.want) {
				t.Fatalf("nextShanghai22(%s) = %s, want %s", test.now, got, test.want)
			}
		})
	}
}

func TestPendingMergeUsesActualScheduleForSafetyGuard(t *testing.T) {
	now := time.Date(2026, 9, 6, 22, 0, 20, 0, shanghaiLocation)
	dueNow := now.Add(-20 * time.Second)
	nearFuture := now.Add(minimumScheduleLead - time.Nanosecond)
	safeFuture := now.Add(minimumScheduleLead)
	if scheduleHasSafeLead(now, &resetBatch{ScheduledAt: &dueNow}) {
		t.Fatal("currently due pending batch passed the safety guard")
	}
	if scheduleHasSafeLead(now, &resetBatch{ScheduledAt: &nearFuture}) {
		t.Fatal("near-future pending batch passed the safety guard")
	}
	if !scheduleHasSafeLead(now, &resetBatch{ScheduledAt: &safeFuture}) {
		t.Fatal("exact minimum lead was rejected")
	}
	if scheduleHasSafeLead(now, &resetBatch{}) {
		t.Fatal("pending batch without scheduled_at passed the safety guard")
	}
}

func validResetBatchJSON(t *testing.T) json.RawMessage {
	t.Helper()
	detected := time.Date(2026, 9, 6, 12, 0, 0, 123000000, time.UTC)
	scheduled := nextShanghai22(detected)
	raw, err := json.Marshal(map[string]any{
		"id": 41, "scope_id": 1, "status": "scheduled", "detected_at": detected, "qualified_at": detected,
		"slot_at": scheduled, "scheduled_at": scheduled, "schedule_revision": 0,
		"effective_at": nil, "completed_at": nil, "delay_reason": nil,
		"announcement_state": "pending", "target_count": 0, "granted_usd": "0.00000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestResetBatchWhitelistAndScheduleShape(t *testing.T) {
	raw := validResetBatchJSON(t)
	batch, err := validateResetBatch(raw)
	if err != nil || validateNewScheduledBatch(batch) != nil {
		t.Fatalf("valid batch rejected: %#v err=%v", batch, err)
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["qualification_source"] = "administrator"
	withInternalField, _ := json.Marshal(object)
	if _, err := validateResetBatch(withInternalField); err == nil {
		t.Fatal("internal reset field accepted")
	}
	delete(object, "qualification_source")
	object["scheduled_at"] = time.Date(2026, 9, 6, 22, 0, 1, 0, shanghaiLocation)
	wrongSchedule, _ := json.Marshal(object)
	wrongBatch, err := validateResetBatch(wrongSchedule)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNewScheduledBatch(wrongBatch); err == nil {
		t.Fatal("non-22:00 schedule accepted")
	}
}

func TestNewAnnouncementIDsUsesOnlyExactIdentityDelta(t *testing.T) {
	before := map[int64]announcementItem{2: {ID: 2}, 8: {ID: 8}}
	after := map[int64]announcementItem{2: {ID: 2}, 8: {ID: 8}, 11: {ID: 11}}
	ids := newAnnouncementIDs(before, after)
	if len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("new IDs = %v, want [11]", ids)
	}
	after[12] = announcementItem{ID: 12}
	ids = newAnnouncementIDs(before, after)
	if len(ids) != 2 || ids[0] != 11 || ids[1] != 12 {
		t.Fatalf("sorted new IDs = %v, want [11 12]", ids)
	}
}

func TestAnnouncementSnapshotRetriesPublicationBetweenVersionAndList(t *testing.T) {
	createdAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		request := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var data any
		switch req.URL.RequestURI() {
		case "/api/v1/announcements/version":
			if request == 1 {
				data = announcementVersion{Version: "before-publication", UnreadCount: 0}
			} else {
				data = announcementVersion{Version: "after-publication", UnreadCount: 1}
			}
		case "/api/v1/announcements":
			data = []announcementItem{{ID: 91, NotifyMode: "popup", CreatedAt: createdAt, UpdatedAt: createdAt}}
		default:
			http.NotFound(w, req)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}))
	}))
	defer server.Close()

	r := &runner{
		api:    &apiClient{baseURL: server.URL, client: server.Client()},
		tokens: map[string]string{"member": "synthetic-token"},
	}
	snapshot, err := r.announcementSnapshot(t.Context(), "member", false)
	require.NoError(t, err)
	assert.Equal(t, int32(6), requests.Load(), "the incoherent first version/list/version attempt must be retried")
	assert.Equal(t, announcementVersion{Version: "after-publication", UnreadCount: 1}, snapshot.Version)
	assert.Contains(t, snapshot.Items, int64(91))
}

func TestAnnouncementSnapshotRetriesStableVersionWithWrongUnreadCount(t *testing.T) {
	createdAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		request := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var data any
		switch req.URL.RequestURI() {
		case "/api/v1/announcements/version":
			if request <= 3 {
				data = announcementVersion{Version: "stable-but-wrong-count", UnreadCount: 2}
			} else {
				data = announcementVersion{Version: "coherent-count", UnreadCount: 1}
			}
		case "/api/v1/announcements":
			data = []announcementItem{{ID: 91, NotifyMode: "popup", CreatedAt: createdAt, UpdatedAt: createdAt}}
		default:
			http.NotFound(w, req)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}))
	}))
	defer server.Close()

	r := &runner{
		api:    &apiClient{baseURL: server.URL, client: server.Client()},
		tokens: map[string]string{"member": "synthetic-token"},
	}
	snapshot, err := r.announcementSnapshot(t.Context(), "member", false)
	require.NoError(t, err)
	assert.Equal(t, int32(6), requests.Load(), "stable versions cannot make a list with a different unread count coherent")
	assert.Equal(t, 1, snapshot.Version.UnreadCount)
}

func TestAnnouncementSnapshotSupportsUnreadOnlyAfterRead(t *testing.T) {
	createdAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var data any
		switch req.URL.RequestURI() {
		case "/api/v1/announcements/version":
			data = announcementVersion{Version: "after-read", UnreadCount: 1}
		case "/api/v1/announcements?unread_only=true":
			data = []announcementItem{{ID: 92, NotifyMode: "popup", CreatedAt: createdAt, UpdatedAt: createdAt}}
		default:
			http.NotFound(w, req)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}))
	}))
	defer server.Close()

	r := &runner{
		api:    &apiClient{baseURL: server.URL, client: server.Client()},
		tokens: map[string]string{"member": "synthetic-token"},
	}
	snapshot, err := r.announcementSnapshot(t.Context(), "member", true)
	require.NoError(t, err)
	assert.Equal(t, int32(3), requests.Load())
	assert.NotContains(t, snapshot.Items, int64(91), "the read announcement must stay out of the unread-only snapshot")
	assert.Contains(t, snapshot.Items, int64(92))
}

func TestAnnouncementSnapshotRejectsMalformedResponse(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":`))
	}))
	defer server.Close()

	r := &runner{
		api:    &apiClient{baseURL: server.URL, client: server.Client()},
		tokens: map[string]string{"member": "synthetic-token"},
	}
	_, err := r.announcementSnapshot(t.Context(), "member", false)
	require.Error(t, err)
	assert.Equal(t, int32(1), requests.Load(), "malformed transport data must fail instead of being retried as a race")
}

func TestSourceAnnouncementQueryUsesOnlyExactBatchMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT id,source_type,source_id,source_event_kind,source_revision,status FROM announcements`).WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_type", "source_id", "source_event_kind", "source_revision", "status"}).AddRow(91, "carpool_reset", 41, "qualification", 0, "active"))
	r := &runner{db: db}
	item, err := r.loadSourceAnnouncement(t.Context(), 41)
	if err != nil || item.ID != 91 || item.SourceID != 41 {
		t.Fatalf("exact source metadata rejected: %#v err=%v", item, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAnnouncementRejectsAmbiguousIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT id,source_type,source_id,source_event_kind,source_revision,status FROM announcements`).WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_type", "source_id", "source_event_kind", "source_revision", "status"}).
			AddRow(91, "carpool_reset", 41, "qualification", 0, "active").
			AddRow(92, "carpool_reset", 41, "qualification", 0, "active"))
	r := &runner{db: db}
	if _, err := r.loadSourceAnnouncement(t.Context(), 41); err == nil {
		t.Fatal("ambiguous source announcements accepted")
	}
}

func TestLoadDBConfigRequiresExactReadOnlyRuntimeIdentity(t *testing.T) {
	for key, value := range map[string]string{
		"CARPOOL_RESET_DB_HOST": trustedDBHost, "CARPOOL_RESET_DB_PORT": trustedDBPort,
		"CARPOOL_RESET_DB_NAME": trustedDBName, "CARPOOL_RESET_DB_USER": trustedDBUser,
		"CARPOOL_RESET_DB_PASSWORD": "private-test-value",
	} {
		t.Setenv(key, value)
	}
	if _, err := loadDBConfig(); err != nil {
		t.Fatalf("exact DB contract rejected: %v", err)
	}
	t.Setenv("CARPOOL_RESET_DB_HOST", "127.0.0.1")
	if _, err := loadDBConfig(); err == nil {
		t.Fatal("untrusted DB host accepted")
	}
}

func TestAnnouncementProjectionAndReportCannotLeakContent(t *testing.T) {
	raw := []byte(`[{"id":91,"title":"copied secret title","content":"copied secret body","notify_mode":"popup","created_at":"2026-09-06T12:00:00Z","updated_at":"2026-09-06T12:00:00Z"}]`)
	var items []announcementItem
	if err := json.Unmarshal(raw, &items); err != nil || len(items) != 1 || items[0].ID != 91 {
		t.Fatalf("projection decode failed: %#v err=%v", items, err)
	}
	encodedProjection, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedProjection), "secret") || strings.Contains(string(encodedProjection), "title") || strings.Contains(string(encodedProjection), "content") {
		t.Fatalf("announcement copy leaked through projection: %s", encodedProjection)
	}

	report := report{
		Version: fixtureVersion, Source: fixtureSource,
		Assertions:     []assertion{{Name: "announcement.list_audience", Status: "pass", Numbers: map[string]int64{"announcement_id": 91}}},
		ExcludedChecks: []excludedCheck{{Name: "reset.actual_due_execution", Coverage: "postgresql_covered"}},
		SyntheticIDs:   map[string]int64{"announcement_id": 91}, Passed: 1,
	}
	encodedReport, err := json.Marshal(report)
	if err != nil || validateReport(report) != nil {
		t.Fatalf("safe report rejected: %v", err)
	}
	for _, forbidden := range []string{"password", "access_token", "source_event_key", "reason\"", "title", "content", "copied secret"} {
		if strings.Contains(string(encodedReport), forbidden) {
			t.Fatalf("report contains forbidden field/value %q: %s", forbidden, encodedReport)
		}
	}
}

func TestReportRejectsActualExecutionPassAndUnsafeFailures(t *testing.T) {
	valid := report{
		Version: fixtureVersion, Source: fixtureSource,
		Assertions:     []assertion{{Name: "announcement.list_audience", Status: "pass"}},
		ExcludedChecks: []excludedCheck{{Name: "reset.actual_due_execution", Coverage: "postgresql_covered"}}, Passed: 1,
	}
	if err := validateReport(valid); err != nil {
		t.Fatalf("explicit PG-covered exclusion rejected: %v", err)
	}
	invalid := valid
	invalid.ExcludedChecks = nil
	invalid.Assertions = []assertion{{Name: "reset.actual_due_execution", Status: "pass"}}
	if err := validateReport(invalid); err == nil {
		t.Fatal("actual 22:00 execution was incorrectly reportable as passed")
	}
	invalid = valid
	invalid.Passed = 0
	invalid.Failed = 1
	invalid.Assertions = []assertion{{Name: "reset_http.execution", Status: "fail", Category: "secret: leaked"}}
	if err := validateReport(invalid); err == nil {
		t.Fatal("unsafe failure category accepted")
	}
}

func TestPathsAreDistinctCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	if pathsAreDistinct(filepath.Join(root, "a.json"), filepath.Join(root, "A.json")) {
		t.Fatal("case-only output collision accepted")
	}
	if !pathsAreDistinct(filepath.Join(root, "a.json"), filepath.Join(root, "b.json"), filepath.Join(root, "c.json")) {
		t.Fatal("distinct paths rejected")
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func validFixture() fixtureFile {
	return fixtureFile{
		Version: 1, Source: fixtureSource, BaseURL: applicationBaseURL, MockURL: mockBaseURL,
		Users: []fixtureUser{{Name: "admin", ID: 7, Email: "carpool-test-admin@example.invalid", Password: strings.Repeat("a", passwordBytes*2)}},
	}
}

func TestValidateFixtureUsesOnlyValidatedBootstrapAdmin(t *testing.T) {
	fixture := validFixture()
	fixture.Users = append(fixture.Users, fixtureUser{Name: "ordinary", ID: 8, Email: "copied@example.invalid", Password: "not-used"})
	admin, err := validateFixture(fixture)
	require.NoError(t, err)
	require.Equal(t, int64(7), admin.ID)

	fixture.Users[0].Email = "real-user@example.com"
	_, err = validateFixture(fixture)
	require.Error(t, err)
}

func TestLoadDBConfigFailsClosed(t *testing.T) {
	t.Setenv("CARPOOL_WS_DB_HOST", trustedDBHost)
	t.Setenv("CARPOOL_WS_DB_PORT", trustedDBPort)
	t.Setenv("CARPOOL_WS_DB_NAME", trustedDBName)
	t.Setenv("CARPOOL_WS_DB_USER", trustedDBUser)
	t.Setenv("CARPOOL_WS_DB_PASSWORD", "private-test-value")
	config, err := loadDBConfig()
	require.NoError(t, err)
	require.Equal(t, trustedDBHost, config.Host)

	t.Setenv("CARPOOL_WS_DB_HOST", "127.0.0.1")
	_, err = loadDBConfig()
	require.Error(t, err)
}

func TestSyntheticWSAccountExtraSupportsLegacyAndModeRouterSchedulers(t *testing.T) {
	extra := syntheticWSAccountExtra()
	require.Equal(t, true, extra["openai_apikey_responses_websockets_v2_enabled"])
	require.Equal(t, "passthrough", extra["openai_apikey_responses_websockets_v2_mode"])
	require.Len(t, extra, 2)
}

func TestReadTerminalRejectsFailureAndAcceptsCompletion(t *testing.T) {
	server := httptest.NewServer(httpHandler(func(conn *coderws.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
		_ = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.created"}`))
		_ = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_test"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	event, err := sendTurn(ctx, conn, "synthetic")
	require.NoError(t, err)
	require.Equal(t, "response.completed", event["type"])
}

func TestValidateAdmissionDeniedRequiresGatewayPolicyClose(t *testing.T) {
	require.NoError(t, validateAdmissionDenied(coderws.CloseError{Code: coderws.StatusTryAgainLater, Reason: "carpool admission failed"}))
	require.Error(t, validateAdmissionDenied(coderws.CloseError{Code: coderws.StatusInternalError, Reason: "carpool admission failed"}))
	require.Error(t, validateAdmissionDenied(coderws.CloseError{Code: coderws.StatusTryAgainLater, Reason: "upstream unavailable"}))
	require.Error(t, validateAdmissionDenied(context.DeadlineExceeded))
}

func TestFailureCategoryIsBoundedAndClassifiesWebSocketClose(t *testing.T) {
	require.Equal(t, "websocket_admission_denied", failureCategory(coderws.CloseError{Code: coderws.StatusTryAgainLater, Reason: "carpool admission failed"}))
	require.Equal(t, "websocket_retryable_close", failureCategory(coderws.CloseError{Code: coderws.StatusTryAgainLater, Reason: "upstream unavailable"}))
	require.Equal(t, "context_deadline_exceeded", failureCategory(context.DeadlineExceeded))
	require.Equal(t, "two_turn_upstream_count_wrong", failureCategory(errors.New("two_turn_upstream_count_wrong")))
	require.Equal(t, "acceptance_step_failed", failureCategory(errors.New("unsafe raw detail: secret-value")))
}

func TestMockStatsReturnsOnlyBoundedSyntheticAccountOutcomes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, "/__stats", req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"stats":{"by_case":{"ws-case":2},"by_case_synthetic_account_outcome":{"ws-case":{"account_1:429":1,"account_2:200":1,"unexpected:200":99}}}}`))
		require.NoError(t, err)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	r := &runner{mock: &apiClient{baseURL: server.URL, client: server.Client()}}
	stats, err := r.mockStats(context.Background(), "ws-case")
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Requests)
	require.Equal(t, map[string]int64{"account_1:429": 1, "account_2:200": 1}, stats.AccountOutcomes)
}

func TestValidateReportRejectsFalseSuccess(t *testing.T) {
	value := report{Version: 1, Source: fixtureSource, Passed: 1, Assertions: []assertion{{Name: "setup.only", Status: "pass"}}}
	require.Error(t, validateReport(value))

	required := []string{
		"websocket.two_turns_independent_receipts_and_debits", "websocket.later_turn_expiry_denied_before_upstream",
		"websocket.same_turn_failover_preserves_admitted_cycle_and_single_debit", "websocket.missing_usage_creates_visible_exception",
	}
	value.Assertions = nil
	value.Passed = len(required)
	for _, name := range required {
		value.Assertions = append(value.Assertions, assertion{Name: name, Status: "pass"})
	}
	require.NoError(t, validateReport(value))

	value.Assertions[0].Status = "fail"
	value.Assertions[0].Category = ""
	value.Passed--
	value.Failed++
	require.Error(t, validateReport(value))
}

func TestWritePrivateJSONRefusesExistingReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, writePrivateJSON(path, map[string]any{"ok": true}, false))
	require.Error(t, writePrivateJSON(path, map[string]any{"ok": false}, false))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(raw, &value))
	require.Equal(t, true, value["ok"])
}

type httpHandler func(*coderws.Conn)

func (handler httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	conn, err := coderws.Accept(w, req, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	handler(conn)
}

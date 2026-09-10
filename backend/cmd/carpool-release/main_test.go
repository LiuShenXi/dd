package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/releasedrain"
	"github.com/stretchr/testify/require"
)

func releaseGateEnvironment(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("CARPOOL_RELEASE_APP_URL", endpoint)
	t.Setenv("CARPOOL_RELEASE_OPERATION_ID", "operation-test")
	t.Setenv("CARPOOL_RELEASE_ADMIN_TOKEN", "private-test-token")
	t.Setenv("CARPOOL_RELEASE_ADMIN_API_KEY", "")
}

func TestVerifyGateAdministratorCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, token, key, authorization, header string
		wantError                               bool
	}{
		{name: "bearer token", token: " private-test-token ", authorization: "Bearer private-test-token"},
		{name: "administrator API key", key: " private-admin-key ", header: "private-admin-key"},
		{name: "ambiguous credentials", token: "private-test-token", key: "private-admin-key", wantError: true},
		{name: "empty credentials", token: " ", key: " ", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			body := releaseGateBody(t, releasedrain.Status{OperationID: "operation-test", State: "migrating"})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, tc.authorization, r.Header.Get("Authorization"))
				require.Equal(t, tc.header, r.Header.Get("x-api-key"))
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			releaseGateEnvironment(t, server.URL)
			t.Setenv("CARPOOL_RELEASE_ADMIN_TOKEN", tc.token)
			t.Setenv("CARPOOL_RELEASE_ADMIN_API_KEY", tc.key)
			err := verifyGate(context.Background())
			if tc.wantError {
				require.Error(t, err)
				require.Zero(t, calls.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

func releaseGateBody(t *testing.T, status releasedrain.Status) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"code": 0, "data": status})
	require.NoError(t, err)
	return string(body)
}

func TestVerifyGateRequiresMatchingIdleMigration(t *testing.T) {
	ready := releasedrain.Status{OperationID: "operation-test", State: "migrating"}
	for _, tc := range []struct {
		name      string
		change    func(*releasedrain.Status)
		wantError bool
	}{
		{name: "locked and idle"},
		{name: "queued clients are allowed", change: func(s *releasedrain.Status) { s.QueuedHTTP = 20 }},
		{name: "wrong operation", change: func(s *releasedrain.Status) { s.OperationID = "another-operation" }, wantError: true},
		{name: "missing operation", change: func(s *releasedrain.Status) { s.OperationID = "" }, wantError: true},
		{name: "open", change: func(s *releasedrain.Status) { s.State = "open" }, wantError: true},
		{name: "draining", change: func(s *releasedrain.Status) { s.State = "draining" }, wantError: true},
		{name: "resuming", change: func(s *releasedrain.Status) { s.State = "resuming" }, wantError: true},
		{name: "active request", change: func(s *releasedrain.Status) { s.ActiveHTTP = 1 }, wantError: true},
		{name: "pending settlement", change: func(s *releasedrain.Status) { s.PendingUsage = 1 }, wantError: true},
		{name: "invalid negative count", change: func(s *releasedrain.Status) { s.ActiveHTTP = -1 }, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := ready
			if tc.change != nil {
				tc.change(&status)
			}
			body := releaseGateBody(t, status)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/api/v1/admin/release/status", r.URL.Path)
				require.Equal(t, "Bearer private-test-token", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			releaseGateEnvironment(t, server.URL+"/unrelated/base")
			err := verifyGate(context.Background())
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyGateRejectsInvalidResponses(t *testing.T) {
	valid := releaseGateBody(t, releasedrain.Status{OperationID: "operation-test", State: "migrating"})
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: valid},
		{name: "server failure", status: http.StatusInternalServerError, body: valid},
		{name: "application failure", body: strings.Replace(valid, `"code":0`, `"code":123`, 1)},
		{name: "missing data", body: `{"code":0}`},
		{name: "null data", body: `{"code":0,"data":null}`},
		{name: "missing code", body: `{"data":{"state":"migrating","operation_id":"operation-test","active_http":0,"pending_usage":0}}`},
		{name: "missing HTTP counter", body: `{"code":0,"data":{"state":"migrating","operation_id":"operation-test","pending_usage":0}}`},
		{name: "missing usage counter", body: `{"code":0,"data":{"state":"migrating","operation_id":"operation-test","active_http":0}}`},
		{name: "null counter", body: `{"code":0,"data":{"state":"migrating","operation_id":"operation-test","active_http":null,"pending_usage":0}}`},
		{name: "malformed JSON", body: `{"code":`},
		{name: "wrong counter type", body: `{"code":0,"data":{"state":"migrating","operation_id":"operation-test","active_http":"0","pending_usage":0}}`},
		{name: "extra JSON", body: valid + `{"code":500}`},
		{name: "oversized response", body: strings.Repeat(" ", 64<<10) + valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			releaseGateEnvironment(t, server.URL)
			require.Error(t, verifyGate(context.Background()))
		})
	}
}

func TestVerifyGateNeverFollowsRedirect(t *testing.T) {
	var redirected atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	releaseGateEnvironment(t, source.URL)
	require.Error(t, verifyGate(context.Background()))
	require.Zero(t, redirected.Load())
}

func TestVerifyGateRejectsInvalidConfigurationAndTransport(t *testing.T) {
	for _, value := range []string{"", "file:///tmp/status", "ftp://localhost", "http://user:password@localhost", "http://localhost?token=x", "http://localhost#status", "://invalid"} {
		t.Run(value, func(t *testing.T) {
			releaseGateEnvironment(t, value)
			require.Error(t, verifyGate(context.Background()))
		})
	}
	for _, missing := range []string{"CARPOOL_RELEASE_OPERATION_ID", "CARPOOL_RELEASE_ADMIN_TOKEN"} {
		t.Run(missing, func(t *testing.T) {
			releaseGateEnvironment(t, "http://127.0.0.1")
			t.Setenv(missing, "")
			require.Error(t, verifyGate(context.Background()))
		})
	}
	t.Run("cancelled request", func(t *testing.T) {
		releaseGateEnvironment(t, "http://127.0.0.1")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.Error(t, verifyGate(ctx))
	})
	t.Run("connection failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		endpoint := server.URL
		server.Close()
		releaseGateEnvironment(t, endpoint)
		require.Error(t, verifyGate(context.Background()))
	})
}

//go:build integration

package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (e *subscriptionQuotaI4Environment) runtimeJSON(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	request, err := http.NewRequest(method, e.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data), "runtime JSON endpoint %s", path)
	if nested, ok := data["data"].(map[string]any); ok {
		return response.StatusCode, nested
	}
	return response.StatusCode, data
}

func (e *subscriptionQuotaI4Environment) loginToken(t *testing.T, email string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": e.Password})
	require.NoError(t, err)
	status, data := e.runtimeJSON(t, http.MethodPost, "/api/v1/auth/login", "", string(body))
	require.Equal(t, http.StatusOK, status)
	token, ok := data["access_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, token)
	return token // memory only: never print or persist a returned token
}

func TestSubscriptionQuotaI4MetadataAndUnsupportedRoutes(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	beforeCalls, beforeActive := e.providerState(t)
	require.Zero(t, beforeActive)
	// Metadata authentication legitimately publishes per-Key last-use times.
	// Warm every actual Key before the complete row guards, while protecting
	// every monetary/window/limit/identity column across the warmup itself.
	keyMoneySQL := `SELECT md5(jsonb_agg(to_jsonb(k)-'last_used_at'-'updated_at' ORDER BY id)::text) FROM api_keys k;`
	keyMoney := e.sql(t, keyMoneySQL)
	moneyBeforeWarmup := e.fingerprint(t, "ordinary_and_GPT")
	for _, entry := range []struct {
		label, key          string
		override, effective any
		term                int64
	}{{"AKey", e.AKey, float64(2), float64(2), e.ATerm}, {"AKey2", e.AKey2, float64(2), float64(2), e.ATerm}, {"GPTKey", e.GPTKey, float64(2), float64(2), 0}, {"BKey", e.BKey, nil, float64(3), e.BTerm}} {
		t.Run("public_key_"+entry.label, func(t *testing.T) {
			status, data := e.runtimeJSON(t, http.MethodGet, "/v1/usage", entry.key, "")
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, float64(5), data["concurrency"], "Key owner retains the original GPT limit")
			require.Equal(t, float64(3), data["default_claude_subscription_concurrency"])
			require.Equal(t, entry.override, data["claude_subscription_concurrency"], "public Key usage must project its actual owner, independently of a browser JWT session")
			require.Equal(t, entry.effective, data["effective_claude_subscription_concurrency"])
			if entry.term != 0 {
				summary, ok := data["quota_summary"].(map[string]any)
				require.True(t, ok)
				current, ok := summary["current_term"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, float64(entry.term), current["id"], fmt.Sprintf("%s retains its original card", entry.label))
			}
		})
	}
	require.Equal(t, keyMoney, e.sql(t, keyMoneySQL), "metadata authentication cannot change Key quota/windows/limits/identity")
	require.Equal(t, moneyBeforeWarmup, e.fingerprint(t, "ordinary_and_GPT"), "public Key usage cannot change ordinary/native/GPT wallets")
	claude := e.fingerprint(t, "Claude")
	money := e.fingerprint(t, "ordinary_and_GPT")
	for _, path := range []string{"/v1/models", "/v1/usage", "/v1/sub2api/billing"} {
		t.Run("metadata"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			status, data := e.runtimeJSON(t, http.MethodGet, path, e.AKey, "")
			require.Equal(t, http.StatusOK, status)
			if path == "/v1/usage" {
				require.Equal(t, "claude_subscription", data["billing_scope"])
				require.Equal(t, "term", data["quota_mode"])
				require.IsType(t, "", data["remaining"])
				require.NotNil(t, data["quota_summary"])
			}
		})
	}
	for _, entry := range []struct{ method, path string }{{http.MethodPost, "/v1/responses/compact"}, {http.MethodPost, "/responses/input_tokens"}, {http.MethodPost, "/v1/images/generations"}, {http.MethodPost, "/v1/live"}, {http.MethodPost, "/antigravity/v1/messages"}, {http.MethodPost, "/v1beta/models/claude-sonnet-4-5:generateContent"}, {http.MethodGet, "/v1/responses"}} {
		t.Run("unsupported"+strings.ReplaceAll(entry.path, "/", "_"), func(t *testing.T) {
			request, err := http.NewRequest(entry.method, e.URL+entry.path, strings.NewReader(`{"model":"claude-sonnet-4-5","input":"synthetic unsupported route"}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+e.AKey)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			require.GreaterOrEqual(t, response.StatusCode, 400, "unsupported Claude transport cannot be a successful HTML or generation response")
			t.Logf("actual unsupported %s %s status=%d", entry.method, entry.path, response.StatusCode)
		})
	}
	calls, active := e.providerState(t)
	require.Equal(t, beforeCalls, calls)
	require.Zero(t, active)
	require.Equal(t, claude, e.fingerprint(t, "Claude"))
	require.Equal(t, money, e.fingerprint(t, "ordinary_and_GPT"))
	for _, identity := range []struct {
		email               string
		override, effective any
	}{{e.AEmail, float64(2), float64(2)}, {e.BEmail, nil, float64(3)}} {
		token := e.loginToken(t, identity.email)
		for _, path := range []string{"/api/v1/auth/me", "/api/v1/user/profile"} {
			status, data := e.runtimeJSON(t, http.MethodGet, path, token, "")
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, float64(3), data["default_claude_subscription_concurrency"], path)
			require.Equal(t, identity.override, data["claude_subscription_concurrency"], path)
			require.Equal(t, identity.effective, data["effective_claude_subscription_concurrency"], path)
			require.Equal(t, "available", data["legacy_concurrency_status"], path)
			require.Equal(t, "available", data["claude_concurrency_status"], path)
			require.Equal(t, float64(0), data["legacy_current_concurrency"], path)
			require.Equal(t, float64(0), data["claude_current_concurrency"], path)
			require.Equal(t, float64(0), data["claude_waiting_count"], path)
		}
	}
	t.Log("actual metadata/unsupported paths: zero provider calls and zero Claude/GPT/native/money deltas; auth/me+user/profile observe default3, Aoverride2, Bdefault3 and both idle pools (synthetic logins may update their own authentication activity)")
}

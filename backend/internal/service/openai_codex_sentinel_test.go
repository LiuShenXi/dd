package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const sentinelTestToken = "test-only-not-a-secret-0123456789abcdef0123456789abcdef"

type sentinelTestRepo struct {
	AccountRepository
	mu         sync.Mutex
	account    *Account
	persistErr error
	ignoreSave bool
	writes     int
}

func (r *sentinelTestRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := *r.account
	a.Extra = make(map[string]any)
	for k, v := range r.account.Extra {
		a.Extra[k] = v
	}
	return &a, nil
}
func (r *sentinelTestRepo) UpdateExtra(_ context.Context, _ int64, extra map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes++
	if r.persistErr != nil {
		return r.persistErr
	}
	if !r.ignoreSave {
		for k, v := range extra {
			r.account.Extra[k] = v
		}
	}
	return nil
}

type sentinelTestCache struct {
	SchedulerCache
	account *Account
	err     error
}

func (r *sentinelTestCache) SetAccount(_ context.Context, a *Account) error {
	if r.err != nil {
		return r.err
	}
	r.account = a
	return nil
}
func (r *sentinelTestCache) GetAccount(context.Context, int64) (*Account, error) {
	return r.account, r.err
}

func sentinelFixture(t *testing.T) (*OpenAIGatewayService, *sentinelTestRepo, *httpUpstreamRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): {fakeCodexTicketState(292)}}, Body: io.NopCloser(strings.NewReader(""))}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.invalid", FailClosed: true}, upstream)
	account := ticketTestAccount(2)
	account.Status = StatusActive
	account.Schedulable = true
	account.Extra = map[string]any{}
	repo := &sentinelTestRepo{account: account}
	svc.accountRepo = repo
	sentinel := &codexSentinel{instance: "test-instance", tokenHash: sha256.Sum256([]byte(sentinelTestToken)), accounts: []int64{2}, models: []string{"gpt-6-astra"}, allowed: map[string]bool{openAICodexTicketKey(2, "gpt-6-astra"): true}, nextAttempt: make(map[string]time.Time)}
	sentinel.enabled.Store(true)
	svc.codexSentinel = sentinel
	svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &sentinelTestCache{}}
	return svc, repo, upstream
}
func sentinelRequest(t *testing.T, svc *OpenAIGatewayService, method, body, token string) (int, map[string]any, string) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/internal/codex-sentinel/status?after=0", strings.NewReader(body))
	if token != "" {
		c.Request.Header.Set("Authorization", "Bearer "+token)
	}
	if method == "GET" {
		svc.CodexSentinelStatus(c)
	} else {
		svc.CodexSentinelRefresh(c)
	}
	result := map[string]any{}
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	}
	return w.Code, result, w.Body.String()
}
func sentinelRefreshBody(version, reason, eventID string) string {
	raw, _ := json.Marshal(codexSentinelRefreshRequest{AccountID: 2, Model: "gpt-6-astra", ExpectedVersion: version, Reason: reason, EventID: eventID})
	return string(raw)
}
func TestCodexSentinelAuthAndBoundedBody(t *testing.T) {
	svc, _, upstream := sentinelFixture(t)
	for _, token := range []string{"", "wrong-token"} {
		code, _, _ := sentinelRequest(t, svc, "GET", "", token)
		require.Equal(t, 401, code)
	}
	code, _, _ := sentinelRequest(t, svc, "POST", strings.Repeat("x", 8193), sentinelTestToken)
	require.Equal(t, 400, code)
	svc.codexSentinel = nil
	code, _, _ = sentinelRequest(t, svc, "GET", "", sentinelTestToken)
	require.Equal(t, 404, code)
	require.Empty(t, upstream.requests)
}
func TestCodexSentinelConfigurationFailsClosed(t *testing.T) {
	t.Setenv("CODEX_SENTINEL_TOKEN_FILE", "")
	require.Nil(t, newCodexSentinelFromEnv())
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte(sentinelTestToken), 0600))
	t.Setenv("CODEX_SENTINEL_TOKEN_FILE", path)
	t.Setenv("CODEX_SENTINEL_ACCOUNT_IDS", "2")
	t.Setenv("CODEX_SENTINEL_MODELS", "gpt-6-astra,gpt-5.6-sol")
	require.NotNil(t, newCodexSentinelFromEnv())
	t.Setenv("CODEX_SENTINEL_ACCOUNT_IDS", "0")
	require.Nil(t, newCodexSentinelFromEnv())
	t.Setenv("CODEX_SENTINEL_ACCOUNT_IDS", "2")
	require.NoError(t, os.WriteFile(path, []byte("weak"), 0600))
	require.Nil(t, newCodexSentinelFromEnv())
}
func TestCodexSentinelOnlyOriginalCompletedModelTriggers(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	o := svc.sentinelObservation(repo.account, "gpt-6-astra", h)
	observer := &upstreamResponseModelObserver{codexSentinel: o}
	payload := []byte(`{"response":{"model":"gpt-5.6-luna"}}`)
	observer.ObserveOpenAI(payload, "response.created")
	observer.ObserveOpenAI(payload, "response.failed")
	observer.ObserveOpenAI(payload, "response.incomplete")
	events, _, _ := svc.codexSentinel.snapshot(0)
	require.Empty(t, events)
	observer.ObserveOpenAI(payload, "response.completed")
	events, _, _ = svc.codexSentinel.snapshot(0)
	require.Len(t, events, 1)
	require.Equal(t, "gpt-5.6-luna", events[0].ActualModel)
	require.Equal(t, o.version, events[0].ExpectedTicketVersion)
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(312))
	o.header(h)
	events, _, _ = svc.codexSentinel.snapshot(0)
	require.Len(t, events, 2)
	require.Equal(t, "turn_state_312", events[1].Kind)
	svc.codexSentinel.enabled.Store(false)
	o.completed("gpt-5.6-sol")
	events, _, _ = svc.codexSentinel.snapshot(0)
	require.Len(t, events, 2)
}
func TestCodexSentinelRingBoundAndCursor(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	o := svc.sentinelObservation(repo.account, "gpt-6-astra", h)
	for i := 0; i < codexSentinelCapacity+8; i++ {
		o.completed("gpt-5.6-luna")
	}
	events, latest, oldest := svc.codexSentinel.snapshot(0)
	require.Len(t, events, 1024)
	require.Equal(t, uint64(1032), latest)
	require.Equal(t, uint64(9), oldest)
	events, _, _ = svc.codexSentinel.snapshot(1031)
	require.Len(t, events, 1)
	events, _, _ = svc.codexSentinel.snapshot(^uint64(0))
	require.Empty(t, events)
}
func TestCodexSentinelRefreshPersistenceCacheAndRedaction(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	code, result, raw := sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", ""), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "refreshed", result["status"])
	require.Equal(t, true, result["persisted"])
	require.Equal(t, 1, repo.writes)
	require.Len(t, upstream.requests, 1)
	require.NotContains(t, raw, fakeCodexTicketState(292))
	require.NotContains(t, raw, "access_token")
	code, status, raw := sentinelRequest(t, svc, "GET", "", sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, true, status["enabled"])
	require.NotContains(t, raw, fakeCodexTicketState(292))
	require.NotContains(t, raw, sentinelTestToken)
	code, result, _ = sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", ""), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "stale", result["status"])
	require.Len(t, upstream.requests, 1)
	version := svc.sentinelVersion(repo.account, "gpt-6-astra")
	code, result, _ = sentinelRequest(t, svc, "POST", sentinelRefreshBody(version, "expiry", ""), sentinelTestToken)
	require.Equal(t, 429, code)
	require.Equal(t, "cooldown", result["status"])
	require.Len(t, upstream.requests, 1)
}
func TestCodexSentinelFailurePreservesOldTicket(t *testing.T) {
	for _, failure := range []string{"upstream", "persist", "unchanged", "unverified", "cache"} {
		t.Run(failure, func(t *testing.T) {
			svc, repo, upstream := sentinelFixture(t)
			old := &openAICodexTicket{State: openAICodexTicketStatePrefix + strings.Repeat("C", 292-len(openAICodexTicketStatePrefix)), Length: 292, Model: "gpt-6-astra", CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
			svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
			version := codexTicketVersion(old)
			switch failure {
			case "upstream":
				upstream.err = errors.New("synthetic secret must not escape")
			case "persist":
				repo.persistErr = errors.New("synthetic database credential")
			case "unchanged":
				upstream.resp.Header.Set(openAICodexTurnStateHeader, old.State)
			case "unverified":
				repo.ignoreSave = true
			case "cache":
				svc.schedulerSnapshot.cache = &sentinelTestCache{err: errors.New("cache secret")}
			}
			code, result, raw := sentinelRequest(t, svc, "POST", sentinelRefreshBody(version, "expiry", ""), sentinelTestToken)
			require.Equal(t, 503, code)
			require.Equal(t, "failed", result["status"])
			require.NotContains(t, raw, "secret")
			require.NotContains(t, raw, old.State)
			if failure == "upstream" || failure == "persist" || failure == "unchanged" {
				require.Same(t, old, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			}
		})
	}
}
func TestCodexSentinelStaleEventAndMasterOffNeverCollect(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	old := &openAICodexTicket{State: fakeCodexTicketState(292), Length: 292, Model: "gpt-6-astra", CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, old.State)
	o := svc.sentinelObservation(repo.account, "gpt-6-astra", h)
	o.completed("gpt-5.6-luna")
	// Same digest but recaptured since this request started is still stale.
	newer := *old
	newer.CapturedAt = time.Now().Add(time.Second)
	svc.storeOpenAICodexTicket(context.Background(), repo.account, &newer)
	code, result, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody(codexTicketVersion(old), "model_mismatch", "test-instance:1"), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "stale", result["status"])
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	code, result, _ = sentinelRequest(t, svc, "POST", sentinelRefreshBody(codexTicketVersion(old), "expiry", ""), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "disabled", result["status"])
	require.Empty(t, upstream.requests)
}
func TestCodexSentinelWSReconnectReplaces312AndKeepsProvenance(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	account := repo.account
	factory := svc.codexSentinelWSHeadersFactory(account, "gpt-6-astra")
	old := &openAICodexTicket{State: fakeCodexTicketState(292), Length: 292, Model: "gpt-6-astra", CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	svc.storeOpenAICodexTicket(context.Background(), account, old)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(312))
	first, err := factory(context.Background(), h.Clone())
	require.NoError(t, err)
	require.Equal(t, old.State, first.Get(openAICodexTurnStateHeader))
	observation := svc.sentinelObservation(account, "gpt-6-astra", first)
	replacement := *old
	replacement.State = openAICodexTicketStatePrefix + strings.Repeat("C", 292-len(openAICodexTicketStatePrefix))
	replacement.CapturedAt = time.Now()
	svc.storeOpenAICodexTicket(context.Background(), account, &replacement)
	second, err := factory(context.Background(), h.Clone())
	require.NoError(t, err)
	require.Equal(t, replacement.State, second.Get(openAICodexTurnStateHeader))
	require.Equal(t, codexTicketVersion(old), observation.forModel("gpt-6-astra").version)
	require.Equal(t, observation.sentAt, observation.forModel("gpt-6-astra").sentAt)
}

type sentinelBlockingUpstream struct {
	HTTPUpstream
	started chan struct{}
	release chan struct{}
	count   atomic.Int64
}

func (u *sentinelBlockingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if u.count.Add(1) == 1 {
		close(u.started)
	}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-u.release:
	}
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(""))}, nil
}
func TestCodexSentinelConcurrentRefreshBoundedAndNativeSingleflight(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	upstream := &sentinelBlockingUpstream{started: make(chan struct{}), release: make(chan struct{})}
	svc.httpUpstream = upstream
	nativeDone := make(chan struct{})
	go func() {
		defer close(nativeDone)
		svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	}()
	select {
	case <-upstream.started:
	case <-time.After(5 * time.Second):
		t.Fatal("native collector did not start")
	}
	refreshed := make(chan int, 1)
	go func() {
		code, _, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", ""), sentinelTestToken)
		refreshed <- code
	}()
	// Observe the server-side admission, not a timing guess about goroutine order.
	require.Eventually(t, func() bool {
		svc.codexSentinel.mu.Lock()
		defer svc.codexSentinel.mu.Unlock()
		return !svc.codexSentinel.nextAttempt[openAICodexTicketKey(2, "gpt-6-astra")].IsZero()
	}, time.Second, time.Millisecond)
	code, result, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", ""), sentinelTestToken)
	require.Equal(t, 429, code)
	require.Equal(t, "cooldown", result["status"])
	close(upstream.release)
	select {
	case code = <-refreshed:
		require.Equal(t, 200, code)
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
	<-nativeDone
	require.Equal(t, int64(1), upstream.count.Load())
	require.Equal(t, 1, repo.writes)
}
func TestCodexSentinelHTTPObservesSentVersionAndUnmappedCompleted(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	state := fakeCodexTicketState(292)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, state)
	upstream.resp.Header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(312))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://upstream.invalid", strings.NewReader(`{"model":"gpt-6-astra"}`))
	require.NoError(t, err)
	req.Header = h
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	bound := svc.bindSentinelRequest(c, req, repo.account, "gpt-6-astra")
	originalVersion := sentinelRequestObservation(bound).version
	// A newer cache value must not relabel the old request/response event.
	svc.storeOpenAICodexTicket(context.Background(), repo.account, &openAICodexTicket{Model: "gpt-6-astra", State: openAICodexTicketStatePrefix + strings.Repeat("C", 292-len(openAICodexTicketStatePrefix)), Length: 292, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	response, err := svc.doOpenAIUpstream(bound, "", repo.account)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	observeOpenAISSEBody(upstreamResponseModelObserverFromContext(c), "data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-5.6-sol\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-luna\"}}\n\n")
	events, _, _ := svc.codexSentinel.snapshot(0)
	require.Len(t, events, 2)
	require.Equal(t, "turn_state_312", events[0].Kind)
	require.Equal(t, "gpt-5.6-luna", events[1].ActualModel)
	for _, e := range events {
		require.Equal(t, originalVersion, e.ExpectedTicketVersion)
	}
}
func TestCodexSentinelIneligibleAndCanceledNeverCollect(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	repo.account.Schedulable = false
	code, result, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", ""), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "ineligible", result["status"])
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.collectOpenAICodexTicket(ctx, repo.account, "gpt-6-astra")
	require.Error(t, err)
	require.Empty(t, upstream.requests)
}

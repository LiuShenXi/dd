package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func controlTest(t *testing.T, base string, in input) {
	t.Helper()
	b, _ := json.Marshal(in)
	r, e := http.Post(base+"/__cwq/control", "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("control status %d", r.StatusCode)
	}
}
func readRules(t *testing.T, base string) []rule {
	t.Helper()
	r, e := http.Get(base + "/__cwq/control")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var s struct {
		Rules []rule `json:"rules"`
	}
	if json.NewDecoder(r.Body).Decode(&s) != nil {
		t.Fatal("control JSON")
	}
	return s.Rules
}
func eventuallyTest(t *testing.T, fn func() bool) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatal("proxy state deadline")
}
func proxyTest(t *testing.T, backend *httptest.Server) *httptest.Server {
	t.Helper()
	u, _ := url.Parse(backend.URL)
	p := httptest.NewServer(handlerFor(u))
	t.Cleanup(p.Close)
	return p
}

func TestIndependentResponseGates(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"path":"`+r.URL.Path+`"}}`)
	}))
	defer backend.Close()
	p := proxyTest(t, backend)
	for _, id := range []string{"A", "B"} {
		controlTest(t, p.URL, input{Action: "arm", RuleID: id, Path: "/api/v1/smoke/" + id, Method: "GET", Phase: "after_backend_response", Effect: "hold"})
	}
	a, b := make(chan *http.Response, 1), make(chan *http.Response, 1)
	go func() { r, _ := http.Get(p.URL + "/api/v1/smoke/A"); a <- r }()
	go func() { r, _ := http.Get(p.URL + "/api/v1/smoke/B"); b <- r }()
	eventuallyTest(t, func() bool { rs := readRules(t, p.URL); return len(rs) == 2 && rs[0].Held && rs[1].Held })
	controlTest(t, p.URL, input{Action: "release", RuleID: "A"})
	select {
	case r := <-a:
		if r == nil || r.StatusCode != 200 {
			t.Fatal("A release")
		}
		raw, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if !bytes.Contains(raw, []byte(`/api/v1/smoke/A`)) {
			t.Fatal("A response changed")
		}
	case <-time.After(time.Second):
		t.Fatal("A did not release")
	}
	select {
	case <-b:
		t.Fatal("A release improperly released B")
	default:
	}
	if !readRules(t, p.URL)[1].Held {
		t.Fatal("B no longer pending")
	}
	controlTest(t, p.URL, input{Action: "release", RuleID: "B", Status: 503})
	r := <-b
	if r == nil || r.StatusCode != 503 {
		t.Fatal("B delayed synthetic error")
	}
	_ = r.Body.Close()
}

func TestDropOnlySuccessfulCommitOnceAndObserveRetry(t *testing.T) {
	var count atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"code":400,"message":"synthetic invalid request"}`)
			return
		}
		status, replay := 201, "false"
		if n > 2 {
			status, replay = 200, "true"
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":0,"message":"success","data":{"term":{"id":77},"replayed":`+replay+`}}`)
	}))
	defer backend.Close()
	p := proxyTest(t, backend)
	path := "/api/v1/smoke/terms"
	controlTest(t, p.URL, input{Action: "arm", RuleID: "drop", Path: path, Method: "POST", Phase: "after_backend_response", Effect: "drop"})
	post := func() (*http.Response, error) {
		req, _ := http.NewRequest("POST", p.URL+path, strings.NewReader(`{"amount":"75"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "synthetic-same-key")
		// Go retries replayable POSTs with Idempotency-Key on reused broken
		// sockets. A fresh connection exposes the deliberate disconnect itself.
		return (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
	}
	r, e := post()
	if e != nil || r.StatusCode != 400 {
		t.Fatal("invalid backend response must pass unchanged")
	}
	_ = r.Body.Close()
	rs := readRules(t, p.URL)
	if rs[0].Consumed || rs[0].Dropped != 0 {
		t.Fatal("unsuccessful response consumed a commit drop")
	}
	r, e = post()
	if e == nil {
		if r != nil {
			_ = r.Body.Close()
		}
		t.Fatal("committed response was not disconnected")
	}
	rs = readRules(t, p.URL)
	if !rs[0].Consumed || rs[0].Dropped != 1 || rs[0].Forwarded != 2 || !rs[0].ValidCommit {
		t.Fatal("one successful drop state")
	}
	controlTest(t, p.URL, input{Action: "arm", RuleID: "retry", Path: path, Method: "POST", Phase: "after_backend_response", Effect: "hold"})
	type result struct {
		r *http.Response
		e error
	}
	done := make(chan result, 1)
	go func() { r, e := post(); done <- result{r, e} }()
	eventuallyTest(t, func() bool { return readRules(t, p.URL)[1].Held })
	rs = readRules(t, p.URL)
	drop := rs[0].Observations[1]
	retry := rs[1].Observations[0]
	if drop.BodySHA256 != retry.BodySHA256 || drop.IdempotencySHA256 != retry.IdempotencySHA256 || drop.TermID == nil || retry.TermID == nil || *drop.TermID != *retry.TermID || drop.Replayed == nil || *drop.Replayed || retry.Replayed == nil || !*retry.Replayed {
		t.Fatal("safe retry metadata mismatch")
	}
	controlTest(t, p.URL, input{Action: "release", RuleID: "retry"})
	out := <-done
	if out.e != nil || out.r.StatusCode != 200 {
		t.Fatal("retry did not pass")
	}
	_ = out.r.Body.Close()
	if count.Load() != 3 {
		t.Fatal("extra upstream request")
	}
	encoded, _ := json.Marshal(readRules(t, p.URL))
	if bytes.Contains(encoded, []byte("synthetic-same-key")) || bytes.Contains(encoded, []byte(`amount`)) {
		t.Fatal("raw payload or idempotency key leaked to control state")
	}
}

func TestBeforeUpstream409DoesNotForward(t *testing.T) {
	var count atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count.Add(1); w.WriteHeader(201) }))
	defer backend.Close()
	p := proxyTest(t, backend)
	path := "/api/v1/smoke/conflict"
	controlTest(t, p.URL, input{Action: "arm", RuleID: "conflict", Path: path, Method: "POST", Phase: "before_upstream", Effect: "reject", Status: 409})
	r, e := http.Post(p.URL+path, "application/json", strings.NewReader(`{"synthetic":true}`))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var body struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if r.StatusCode != 409 || body.Code != 409 || body.Reason != "QUOTA_REVISION_CONFLICT" || count.Load() != 0 {
		t.Fatal("409 must precede any backend call")
	}
	rs := readRules(t, p.URL)
	if rs[0].Forwarded != 0 || rs[0].Rejected != 1 || rs[0].Observations[0].UpstreamStatus != nil {
		t.Fatal("false upstream evidence")
	}
}

// Isolated UI fault proxy. No authorization or raw payload is logged.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type input struct {
	Action string `json:"action"`
	RuleID string `json:"rule_id"`
	Path   string `json:"path"`
	Method string `json:"method"`
	Phase  string `json:"phase"`
	Effect string `json:"effect"`
	Status int    `json:"status"`
}
type observation struct {
	BodySHA256        string `json:"body_sha256"`
	IdempotencySHA256 string `json:"idempotency_key_sha256"`
	UpstreamStatus    *int   `json:"upstream_status"`
	TermID            *int64 `json:"term_id"`
	Replayed          *bool  `json:"replayed"`
}
type rule struct {
	ID            string        `json:"rule_id"`
	Path          string        `json:"path"`
	Method        string        `json:"method"`
	Phase         string        `json:"phase"`
	Effect        string        `json:"effect"`
	Consumed      bool          `json:"consumed"`
	InFlight      bool          `json:"in_flight"`
	Forwarded     int           `json:"upstream_round_trips"`
	Held          bool          `json:"held"`
	Released      bool          `json:"released"`
	Delivered     int           `json:"delivered"`
	Dropped       int           `json:"dropped"`
	Rejected      int           `json:"rejected_before_upstream"`
	ValidCommit   bool          `json:"valid_successful_term_response"`
	Observations  []observation `json:"observations"`
	gate          chan struct{}
	releaseStatus int
}
type requestState struct {
	r                  *rule
	bodyHash, idemHash string
}
type stateKey struct{}
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var errDrop = errors.New("task-only committed response disconnect")

func hash(s []byte) string { d := sha256.Sum256(s); return hex.EncodeToString(d[:]) }
func decodeTerm(resp *http.Response, b []byte) (*int64, *bool, bool) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, false
	}
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		g, e := gzip.NewReader(bytes.NewReader(b))
		if e != nil {
			return nil, nil, false
		}
		defer g.Close()
		b, e = io.ReadAll(io.LimitReader(g, 8<<20))
		if e != nil {
			return nil, nil, false
		}
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Term struct {
				ID int64 `json:"id"`
			} `json:"term"`
			Replayed *bool `json:"replayed"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &result) != nil || result.Code != 0 || result.Data.Term.ID <= 0 || result.Data.Replayed == nil {
		return nil, nil, false
	}
	return &result.Data.Term.ID, result.Data.Replayed, true
}
func reject(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CWQ-Synthetic-Fault", "before-upstream")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": "synthetic UI conflict: schedule changed; preview again before submitting", "reason": "QUOTA_REVISION_CONFLICT"})
}

func handlerFor(u *url.URL) http.Handler {
	var mu sync.Mutex
	var rules []*rule
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: false, MaxIdleConns: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	proxy.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if state, _ := req.Context().Value(stateKey{}).(*requestState); state != nil {
			mu.Lock()
			state.r.Forwarded++
			mu.Unlock()
		}
		return transport.RoundTrip(req)
	})
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		if state, _ := req.Context().Value(stateKey{}).(*requestState); state != nil {
			mu.Lock()
			state.r.InFlight = false
			state.r.Held = false
			mu.Unlock()
		}
		if errors.Is(err, errDrop) {
			if h, ok := w.(http.Hijacker); ok {
				if c, _, e := h.Hijack(); e == nil {
					if tcp, ok := c.(*net.TCPConn); ok {
						_ = tcp.SetLinger(0)
					}
					_ = c.Close()
					return
				}
			}
			return
		}
		http.Error(w, "task-only loopback proxy transport failure", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		request, _ := resp.Request.Context().Value(stateKey{}).(*requestState)
		if request == nil {
			return nil
		}
		r := request.r
		body, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
		_ = resp.Body.Close()
		if e != nil {
			return e
		}
		if len(body) > 8<<20 {
			return fmt.Errorf("task response exceeds limit")
		}
		id, replayed, valid := decodeTerm(resp, body)
		status := resp.StatusCode
		mu.Lock()
		r.InFlight = false
		r.ValidCommit = valid
		r.Observations = append(r.Observations, observation{request.bodyHash, request.idemHash, &status, id, replayed})
		effect, gate := r.Effect, r.gate
		if effect == "hold" {
			r.Held = true
		}
		if effect == "drop" && valid {
			r.Consumed = true
			r.Dropped++
		}
		mu.Unlock()
		if effect == "drop" && valid {
			return errDrop
		}
		if effect == "hold" {
			select {
			case <-gate:
			case <-time.After(2 * time.Minute):
				return fmt.Errorf("task response hold expired")
			}
			mu.Lock()
			r.Held = false
			replacement := r.releaseStatus
			mu.Unlock()
			if replacement != 0 {
				resp.StatusCode = replacement
				resp.Status = fmt.Sprintf("%d %s", replacement, http.StatusText(replacement))
				body = []byte(`{"code":503,"message":"synthetic delayed preview error","reason":"SYNTHETIC_PROXY_DELAYED_ERROR"}`)
				resp.Header.Del("Content-Encoding")
				resp.Header.Set("Content-Type", "application/json")
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
		mu.Lock()
		r.Delivered++
		mu.Unlock()
		return nil
	}
	validID := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	mux := http.NewServeMux()
	mux.HandleFunc("/__cwq/control", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if req.Method == http.MethodPost {
			var in input
			decoder := json.NewDecoder(io.LimitReader(req.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&in) != nil || !validID.MatchString(in.RuleID) {
				http.Error(w, "invalid exact rule control", 400)
				return
			}
			var found *rule
			for _, r := range rules {
				if r.ID == in.RuleID {
					found = r
					break
				}
			}
			switch in.Action {
			case "arm":
				if found != nil || !strings.HasPrefix(in.Path, "/api/v1/") || strings.ContainsAny(in.Path, "?\r\n") || strings.Contains(in.Path, "..") || (in.Method != "POST" && in.Method != "GET") || !((in.Phase == "after_backend_response" && (in.Effect == "hold" || in.Effect == "drop")) || (in.Phase == "before_upstream" && in.Effect == "reject" && in.Status == 409)) {
					http.Error(w, "unsupported or reused rule", 400)
					return
				}
				for _, r := range rules {
					if !r.Consumed && r.Path == in.Path && r.Method == in.Method {
						http.Error(w, "one unconsumed rule per exact path/method", 409)
						return
					}
				}
				r := &rule{ID: in.RuleID, Path: in.Path, Method: in.Method, Phase: in.Phase, Effect: in.Effect, gate: make(chan struct{}), Observations: []observation{}}
				rules = append(rules, r)
			case "release":
				if found == nil || found.Effect != "hold" || (in.Status != 0 && in.Status != 503) {
					http.Error(w, "unknown hold or unsupported replacement", 400)
					return
				}
				if !found.Released {
					found.releaseStatus = in.Status
					close(found.gate)
					found.Released = true
				}
			case "disarm":
				if found == nil || found.Consumed {
					http.Error(w, "only an unconsumed rule may be disarmed", 400)
					return
				}
				found.Consumed = true
			default:
				http.Error(w, "unknown action", 400)
				return
			}
		} else if req.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"target": u.String(), "synthetic_only": true, "once_per_rule": true, "payload_logging": false, "rules": rules})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		var selected *rule
		for _, r := range rules {
			if !r.Consumed && !r.InFlight && r.Path == req.URL.Path && r.Method == req.Method {
				if r.Effect == "drop" {
					r.InFlight = true
				} else {
					r.Consumed = true
				}
				selected = r
				break
			}
		}
		mu.Unlock()
		if selected == nil {
			proxy.ServeHTTP(w, req)
			return
		}
		body, e := io.ReadAll(io.LimitReader(req.Body, 1<<20+1))
		if e != nil || len(body) > 1<<20 {
			http.Error(w, "task request exceeds limit", 413)
			return
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		state := &requestState{selected, hash(body), hash([]byte(req.Header.Get("Idempotency-Key")))}
		if selected.Phase == "before_upstream" {
			mu.Lock()
			selected.Rejected++
			selected.Observations = append(selected.Observations, observation{BodySHA256: state.bodyHash, IdempotencySHA256: state.idemHash})
			mu.Unlock()
			reject(w, 409)
			return
		}
		proxy.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), stateKey{}, state)))
	})
	return mux
}

func main() {
	target := flag.String("target", "", "exact synthetic loopback candidate")
	port := flag.Int("port", 0, "loopback-only port")
	flag.Parse()
	u, e := url.Parse(*target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || *port < 1024 || *port > 65535 {
		log.Fatal("exact loopback candidate/port required")
	}
	l, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if e != nil {
		log.Fatal(e)
	}
	server := &http.Server{Handler: handlerFor(u), ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	log.Fatal(server.Serve(l))
}

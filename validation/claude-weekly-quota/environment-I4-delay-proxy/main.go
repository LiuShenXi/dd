// Task-only loopback proxy. It never logs request headers or bodies.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type rule struct {
	Path        string `json:"path"`
	Method      string `json:"method"`
	Hold        bool   `json:"hold"`
	Release     bool   `json:"release"`
	ErrorStatus int    `json:"error_status"`
}

func main() {
	target := flag.String("target", "", "existing synthetic loopback candidate")
	port := flag.Int("port", 0, "loopback-only listener")
	flag.Parse()
	u, err := url.Parse(*target)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || *port < 1024 {
		log.Fatal("exact synthetic HTTP loopback target and port required")
	}
	var mu sync.Mutex
	var current rule
	var gate chan struct{}
	var held, completed int
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ModifyResponse = func(resp *http.Response) error {
		mu.Lock()
		r, g := current, gate
		match := r.Hold && r.Path == resp.Request.URL.Path && (r.Method == "" || r.Method == resp.Request.Method)
		if match {
			held++
		}
		mu.Unlock()
		if !match {
			return nil
		}
		// Capture the real backend response before delaying delivery. This gives
		// UI identity guards an authentic late success with unchanged content.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		select {
		case <-g:
		case <-time.After(2 * time.Minute):
			return fmt.Errorf("task response hold exceeded two minutes")
		}
		if r.ErrorStatus >= 400 && r.ErrorStatus <= 599 {
			resp.StatusCode = r.ErrorStatus
			resp.Status = fmt.Sprintf("%d %s", r.ErrorStatus, http.StatusText(r.ErrorStatus))
			body = []byte(`{"code":"SYNTHETIC_PROXY_DELAYED_ERROR","message":"Task-only delayed response fault"}`)
			resp.Header.Set("Content-Type", "application/json")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
		mu.Lock()
		completed++
		mu.Unlock()
		return nil
	}
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	mux := http.NewServeMux()
	mux.HandleFunc("/__cwq/control", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if req.Method == http.MethodPost {
			var next rule
			if json.NewDecoder(io.LimitReader(req.Body, 4096)).Decode(&next) != nil {
				http.Error(w, "invalid control", 400)
				return
			}
			if next.Release {
				if gate != nil {
					close(gate)
					gate = nil
				}
				current.Hold = false
			} else {
				if !strings.HasPrefix(next.Path, "/api/v1/") || strings.Contains(next.Path, "?") || strings.Contains(next.Path, "..") || gate != nil {
					http.Error(w, "one exact API response hold at a time", 400)
					return
				}
				current, gate = next, make(chan struct{})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": current.Path, "method": current.Method, "holding": current.Hold, "held_responses": held, "released_responses": completed, "target": u.String(), "synthetic_only": true})
	})
	mux.Handle("/", proxy)
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "synthetic delay proxy loopback=%s target=%s no_payload_logging=true\n", listener.Addr(), u.String())
	log.Fatal((&http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}).Serve(listener))
}

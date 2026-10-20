// Synthetic local provider for the isolated Claude quota I4 exercise.
// It never forwards, dials another service, or records authorization headers.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type audit struct {
	Sequence  int    `json:"sequence"`
	Path      string `json:"path"`
	Model     string `json:"model"`
	Stream    bool   `json:"stream"`
	Active    bool   `json:"active"`
	Cancelled bool   `json:"cancelled"`
}

var mu sync.Mutex
var calls []*audit
var hold chan struct{}
var individualReleases = map[int]chan struct{}{}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "synthetic-ready") })
	mux.HandleFunc("/__audit", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(calls)
	})
	mux.HandleFunc("/__control", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Hold            bool `json:"hold"`
			ReleaseSequence int  `json:"release_sequence"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&input) != nil {
			http.Error(w, "invalid synthetic gate", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if input.ReleaseSequence > 0 {
			gate, exists := individualReleases[input.ReleaseSequence]
			if !exists {
				http.Error(w, "unknown synthetic active call", 404)
				return
			}
			close(gate)
			delete(individualReleases, input.ReleaseSequence)
			_, _ = io.WriteString(w, "one synthetic call released")
			return
		}
		if input.Hold {
			if hold != nil {
				http.Error(w, "synthetic gate already holding", 409)
				return
			}
			hold = make(chan struct{})
		} else if hold != nil {
			close(hold)
			hold = nil
		}
		_, _ = io.WriteString(w, "synthetic gate updated")
	})
	mux.HandleFunc("/__state", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		active := map[string]int{}
		for _, call := range calls {
			if call.Active {
				active[call.Model]++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"holding": hold != nil, "active_by_model": active, "calls": calls})
	})
	mux.HandleFunc("/", generate)
	server := &http.Server{Addr: ":8081", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func generate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"claude-sonnet-4-5","object":"model"},{"id":"gpt-5.2","object":"model"}]}`)
		return
	}
	if r.Method != http.MethodPost || (!strings.HasSuffix(r.URL.Path, "/messages") && !strings.HasSuffix(r.URL.Path, "/responses")) {
		http.Error(w, "synthetic provider endpoint unsupported", http.StatusNotFound)
		return
	}
	var body struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid synthetic request", http.StatusBadRequest)
		return
	}
	mu.Lock()
	n := len(calls) + 1
	call := &audit{Sequence: n, Path: r.URL.Path, Model: body.Model, Stream: body.Stream, Active: true}
	calls = append(calls, call)
	gate := hold
	var individual chan struct{}
	if gate != nil {
		individual = make(chan struct{})
		individualReleases[n] = individual
	}
	mu.Unlock()
	defer func() {
		mu.Lock()
		call.Active = false
		call.Cancelled = r.Context().Err() != nil
		delete(individualReleases, n)
		mu.Unlock()
	}()
	w.Header().Set("X-Request-Id", fmt.Sprintf("synthetic-provider-%d", n))
	if !body.Stream {
		if !awaitGate(w, r, gate, individual, false) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("msg_synthetic_%d", n), "type": "message", "role": "assistant", "model": body.Model, "content": []any{map[string]any{"type": "text", "text": "synthetic quota result"}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}})
		} else {
			_ = json.NewEncoder(w).Encode(response(n, body.Model))
		}
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	event := func(kind string, value any) {
		b, _ := json.Marshal(value)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if strings.HasSuffix(r.URL.Path, "/messages") {
		event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": fmt.Sprintf("msg_synthetic_%d", n), "type": "message", "role": "assistant", "model": body.Model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}}})
		if !awaitGate(w, r, gate, individual, true) {
			return
		}
		event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "synthetic quota result"}})
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 20}})
		event("message_stop", map[string]any{"type": "message_stop"})
	} else {
		event("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": fmt.Sprintf("resp_synthetic_%d", n), "model": body.Model, "object": "response", "status": "in_progress", "output": []any{}}})
		if !awaitGate(w, r, gate, individual, true) {
			return
		}
		event("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "response_id": fmt.Sprintf("resp_synthetic_%d", n), "output_index": 0, "content_index": 0, "delta": "synthetic GPT result"})
		event("response.completed", map[string]any{"type": "response.completed", "response": response(n, body.Model)})
	}
}

func awaitGate(w http.ResponseWriter, r *http.Request, gate, individual chan struct{}, stream bool) bool {
	if gate == nil {
		return true
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return false
		case <-gate:
			return true
		case <-individual:
			return true
		case <-ticker.C:
			if stream {
				_, _ = io.WriteString(w, ": synthetic long-stream heartbeat\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}
}

func response(n int, model string) map[string]any {
	return map[string]any{"id": fmt.Sprintf("resp_synthetic_%d", n), "object": "response", "model": model, "status": "completed", "output": []any{map[string]any{"id": fmt.Sprintf("msg_gpt_%d", n), "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "synthetic GPT result", "annotations": []any{}}}}}, "usage": map[string]int{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3}}
}

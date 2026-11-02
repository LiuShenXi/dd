package basispoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStreamPreservesTerminalReasoningAbove16MiB(t *testing.T) {
	_, bridge := mustPrepare(t, testSource(), "", nil)
	opaque := strings.Repeat("r", (20<<20)+17)
	wire := sse(object{"type": "response.completed", "response": object{"id": "resp_large", "status": "completed", "output": []any{object{"type": "reasoning", "encrypted_content": opaque}}, "usage": object{"input_tokens": 17, "output_tokens": 3}}})
	body := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
	defer body.Close()
	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	err = readEvents(bytes.NewReader(out), func(_ string, data []byte) error {
		var event object
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		if text(event["type"]) != "response.completed" {
			t.Fatalf("unexpected terminal event: %s", text(event["type"]))
		}
		response := event["response"].(map[string]any)
		item := response["output"].([]any)[0].(map[string]any)
		if text(item["encrypted_content"]) != opaque {
			t.Fatal("opaque reasoning was truncated or changed")
		}
		found = true
		return nil
	})
	if err != nil || !found {
		t.Fatalf("terminal response missing: %v", err)
	}
}

func TestReadEventsSizeBoundaries(t *testing.T) {
	const limit = 1024
	for _, tc := range []struct {
		name, wire, want string
		fail             bool
	}{
		{"exact_LF", "data: " + strings.Repeat("a", limit) + "\n\n", strings.Repeat("a", limit), false},
		{"exact_CRLF", "data: " + strings.Repeat("a", limit) + "\r\n\r\n", strings.Repeat("a", limit), false},
		{"exact_EOF", "data: " + strings.Repeat("a", limit), strings.Repeat("a", limit), false},
		{"multiline_exact", "data: " + strings.Repeat("a", 512) + "\ndata: " + strings.Repeat("b", 511) + "\n\n", strings.Repeat("a", 512) + "\n" + strings.Repeat("b", 511), false},
		{"single_over", "data: " + strings.Repeat("a", limit+1) + "\n\n", "", true},
		{"multiline_over", "data: " + strings.Repeat("a", 512) + "\ndata: " + strings.Repeat("b", 512) + "\n\n", "", true},
		{"oversized_comment", ":" + strings.Repeat("a", limit+20) + "\n\n", "", true},
		{"empty_data", "data:\n\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := readEventsWithLimit(strings.NewReader(tc.wire), limit, func(_ string, data []byte) error {
				calls++
				if string(data) != tc.want {
					t.Fatal("event data changed")
				}
				return nil
			})
			if tc.fail {
				var protocol protocolError
				if !errors.As(err, &protocol) || calls != 0 {
					t.Fatalf("expected bounded rejection without delivery; calls=%d err=%v", calls, err)
				}
				if strings.Contains(err.Error(), strings.Repeat("a", 20)) {
					t.Fatal("payload leaked into size diagnostic")
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("expected one intact event; calls=%d err=%v", calls, err)
			}
		})
	}
}

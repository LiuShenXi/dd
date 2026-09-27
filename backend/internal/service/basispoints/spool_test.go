package basispoints

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
)

type repeatedByteReader struct{ value byte }

func (r repeatedByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.value
	}
	return len(p), nil
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func TestSpoolLargeCompactionMemoryAndIntegrity(t *testing.T) {
	if testing.Short() {
		t.Skip("large streaming regression")
	}
	const size = 128 << 20
	_, bridge := mustPrepare(t, testSource(), "", nil)
	prefix := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_large_compact\",\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":[{\"type\":\"compaction\",\"encrypted_content\":\""
	suffix := "\"}],\"usage\":{\"input_tokens\":17,\"output_tokens\":3}}}\r\n\r\n"
	wire := io.MultiReader(strings.NewReader(prefix), io.LimitReader(repeatedByteReader{'r'}, size), strings.NewReader(suffix))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	calls := 0
	var retained *spooledJSONString
	err := bridge.Transform(wire, func(e Event) error {
		calls++
		if e.Kind != "response.completed" {
			return fmt.Errorf("unexpected event: %s", e.Kind)
		}
		r := e.Payload["response"].(object)
		item := r["output"].([]any)[0].(object)
		blob, ok := item["encrypted_content"].(*spooledJSONString)
		if !ok || blob.length != size+2 {
			t.Fatal("large compaction was not preserved in spool")
		}
		retained = blob
		actual := sha256.New()
		if _, err := io.Copy(actual, io.NewSectionReader(blob.file, blob.offset+1, size)); err != nil {
			return err
		}
		expected := sha256.New()
		_, _ = io.Copy(expected, io.LimitReader(repeatedByteReader{'r'}, size))
		if !bytes.Equal(actual.Sum(nil), expected.Sum(nil)) {
			t.Fatal("opaque content changed")
		}
		if e.ResponseID() != "resp_large_compact" || e.ResponseModel() != "gpt-6-astra" {
			t.Fatal("metadata lost")
		}
		var usage object
		if json.Unmarshal(e.UsageJSON(), &usage) != nil {
			t.Fatal("usage invalid")
		}
		out := &countingWriter{}
		if err := e.WriteSSE(out); err != nil {
			return err
		}
		if out.n < size {
			t.Fatal("truncated event")
		}
		return nil
	})
	runtime.ReadMemStats(&after)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if retained == nil {
		t.Fatal("missing blob")
	}
	var one [1]byte
	if _, err := retained.file.ReadAt(one[:], 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("spool leaked: %v", err)
	}
	if activeSpoolBytes.Load() != 0 {
		t.Fatal("disk budget leaked")
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("128 MiB compaction streamed intact; total allocated=%.2f MiB", float64(allocated)/(1<<20))
	if allocated > 32<<20 {
		t.Fatalf("large event allocated too much memory: %d", allocated)
	}
}

func TestSpoolFragmentationAndEscapes(t *testing.T) {
	textValue := strings.Repeat("多字节\\\"\n", inlineJSONStringBytes/4)
	raw, _ := json.Marshal(object{"type": "response.completed", "response": object{"output": []any{object{"type": "reasoning", "encrypted_content": textValue}}}})
	for _, ending := range []string{"\n\n", "\r\n\r\n", ""} {
		t.Run(fmt.Sprintf("ending_%q", ending), func(t *testing.T) {
			wire := io.MultiReader(strings.NewReader(": comment\nevent: response.completed\ndata: "), bytes.NewReader(raw), strings.NewReader(ending))
			calls := 0
			err := readSpooledEvents(wire, func(_ string, p object) error {
				calls++
				var out bytes.Buffer
				if err := writeEventJSON(&out, p); err != nil {
					return err
				}
				var decoded object
				if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
					return err
				}
				got := decoded["response"].(object)["output"].([]any)[0].(object)["encrypted_content"]
				if got != textValue {
					t.Fatal("escaped string changed")
				}
				return nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if activeSpoolBytes.Load() != 0 {
				t.Fatal("disk budget leaked")
			}
		})
	}
}

func TestSpoolMultilineAndInvalidCleanup(t *testing.T) {
	calls := 0
	err := readSpooledEvents(strings.NewReader("data: {\"type\":\ndata: \"response.completed\",\"response\":{}}\n\n"), func(_ string, p object) error { calls++; return nil })
	if err != nil || calls != 1 {
		t.Fatalf("multiline calls=%d err=%v", calls, err)
	}
	for _, tail := range []string{"\\q\"}", "\n\"}", "\\uXX00\"}", string([]byte{0xff}) + "\"}", "\",,}", ""} {
		wire := "data: {\"value\":\"" + strings.Repeat("a", inlineJSONStringBytes+3) + tail + "\n\n"
		err := readSpooledEvents(strings.NewReader(wire), func(string, object) error { t.Fatal("invalid event delivered"); return nil })
		if err == nil {
			t.Fatalf("accepted malformed tail %q", tail)
		}
		if activeSpoolBytes.Load() != 0 {
			t.Fatal("error leaked spool budget")
		}
	}
	boom := errors.New("consumer failed")
	wire := "data: {\"value\":\"" + strings.Repeat("a", inlineJSONStringBytes+3) + "\"}\n\n"
	if err := readSpooledEvents(strings.NewReader(wire), func(string, object) error { return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if activeSpoolBytes.Load() != 0 {
		t.Fatal("callback failure leaked spool")
	}
}

func TestSpoolBudgetsAndCleanup(t *testing.T) {
	t.Run("event", func(t *testing.T) {
		s := &eventSpool{size: maxWireEventBytes}
		defer s.close()
		if err := s.write([]byte(" ")); err == nil {
			t.Fatal("accepted oversized event")
		}
	})
	t.Run("metadata", func(t *testing.T) {
		s := &eventSpool{}
		defer s.close()
		if err := s.appendMeta(make([]byte, maxEventMetadataBytes+1)); err == nil {
			t.Fatal("accepted oversized metadata")
		}
	})
	t.Run("shared_disk", func(t *testing.T) {
		if activeSpoolBytes.Load() != 0 {
			t.Fatal("another spool is active")
		}
		activeSpoolBytes.Store(maxActiveSpoolBytes)
		defer activeSpoolBytes.Store(0)
		s := &eventSpool{}
		if err := s.spill([]byte("x")); err == nil {
			t.Fatal("accepted over-budget spill")
		}
		name := s.file.Name()
		s.close()
		if activeSpoolBytes.Load() != maxActiveSpoolBytes {
			t.Fatal("failed spill changed another event's budget")
		}
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed spill left a file: %v", err)
		}
	})
}

func TestSpoolToolArgumentsRemainValidatedAndIntact(t *testing.T) {
	for _, kind := range []string{"function", "custom"} {
		t.Run(kind, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{object{"type": kind, "name": "run"}}
			_, bridge := mustPrepare(t, source, "spool-tool", new(ReplayCache))
			value := strings.Repeat("a", inlineJSONStringBytes+100)
			envelope := object{"name": "run", "arguments": object{"value": value}}
			if kind == "custom" {
				delete(envelope, "arguments")
				envelope["input"] = value
			}
			native := nativeCall(envelope)
			wire := sse(object{"type": "response.output_item.done", "item": native}) +
				sse(object{"type": "response.completed", "response": object{"output": []any{native}}})
			calls, completed := 0, false
			err := bridge.Transform(strings.NewReader(wire), func(e Event) error {
				if e.Kind == "response.failed" {
					t.Fatal("large tool was rejected")
				}
				if e.Kind == "response.completed" {
					completed = true
				}
				if e.Kind == "response.output_item.done" {
					calls++
					item := e.Payload["item"].(object)
					if item["name"] != "run" {
						t.Fatal("native transport leaked")
					}
					if kind == "custom" {
						if item["input"] != value {
							t.Fatal("custom input changed")
						}
					} else {
						var args object
						if err := decode([]byte(text(item["arguments"])), &args); err != nil || args["value"] != value {
							t.Fatal("function argument changed")
						}
					}
				}
				return nil
			})
			if err != nil || calls != 1 || !completed || activeSpoolBytes.Load() != 0 {
				t.Fatalf("calls=%d completed=%v err=%v", calls, completed, err)
			}
		})
	}
}

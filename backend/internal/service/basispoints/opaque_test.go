package basispoints

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeOpaqueFixture(t *testing.T, store *OpaqueStore, size int64) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "opaque-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.WriteString("\"")
	_, _ = io.CopyN(f, repeatedByteReader{'x'}, size)
	_, _ = f.WriteString("\"")
	blob := &spooledJSONString{file: f, offset: 0, length: size + 2}
	item := object{"id": "cmp_test", "type": "compaction", "encrypted_content": blob}
	event := Event{Kind: "response.output_item.done", Payload: object{"item": item}}
	if err := store.CompactEvent("key:2", event); err != nil {
		t.Fatal(err)
	}
	ref, ok := item["encrypted_content"].(string)
	if !ok || !strings.HasPrefix(ref, opaquePrefix) {
		t.Fatal("missing compact reference")
	}
	var out bytes.Buffer
	if err := event.WriteSSE(&out); err != nil {
		t.Fatal(err)
	}
	if out.Len() > 1024 {
		t.Fatal("large ciphertext was sent to the client")
	}
	if item["id"] != "cmp_test" {
		t.Fatal("item identity changed")
	}
	return ref
}

func TestOpaqueLargeRoundTripMemoryAndDurability(t *testing.T) {
	if testing.Short() {
		t.Skip("large regression")
	}
	const size = 128 << 20
	store := &OpaqueStore{Root: t.TempDir()}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	ref := makeOpaqueFixture(t, store, size)
	// Simulate a fresh process using the durable store and the client's next turn.
	reopened := &OpaqueStore{Root: store.Root}
	source := testSource()
	source["input"] = []any{object{"id": "cmp_test", "type": "compaction", "encrypted_content": ref}, message("user", "continue")}
	preparedObject, _ := mustPrepare(t, source, "account:16/key:2/thread:test", nil)
	prepared, _ := json.Marshal(preparedObject)
	if !bytes.Contains(prepared, []byte(ref)) {
		t.Fatal("reference lost during history translation")
	}
	reader, length, err := reopened.OpenRequest("key:2", prepared)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, reader)
	reader.Close()
	if err != nil || n != length {
		t.Fatalf("request length=%d read=%d err=%v", length, n, err)
	}
	needle, _ := json.Marshal(ref)
	start := bytes.Index(prepared, needle)
	expected := sha256.New()
	expected.Write(prepared[:start])
	expected.Write([]byte{'"'})
	_, _ = io.CopyN(expected, repeatedByteReader{'x'}, size)
	expected.Write([]byte{'"'})
	expected.Write(prepared[start+len(needle):])
	if !bytes.Equal(hash.Sum(nil), expected.Sum(nil)) {
		t.Fatal("upstream ciphertext or request changed")
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 16<<20 {
		t.Fatalf("large request allocated %d bytes", allocated)
	}
	t.Logf("128 MiB ciphertext -> %d-byte reference; restored SHA256 identical, allocation %.2f MiB", len(ref), float64(allocated)/(1<<20))
}

func TestOpaqueScopeCorruptionAndMissingRecord(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	ref := makeOpaqueFixture(t, store, 2<<20)
	body, _ := json.Marshal(object{"input": []any{object{"type": "compaction", "encrypted_content": ref}}})
	if _, _, err := store.OpenRequest("key:999", body); err == nil {
		t.Fatal("cross-key reference accepted")
	}
	filename := filepath.Join(store.scopeDir("key:2"), strings.TrimPrefix(ref, opaquePrefix)+".json")
	f, err := os.OpenFile(filename, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteAt([]byte{'y'}, 10)
	f.Close()
	if _, _, err := store.OpenRequest("key:2", body); err == nil {
		t.Fatal("corrupt record accepted")
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.OpenRequest("key:2", body); err == nil {
		t.Fatal("missing record accepted")
	}
}

func TestOpaqueDuplicateEventsAndEscapedUserText(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	ref := makeOpaqueFixture(t, store, 2<<20)
	if second := makeOpaqueFixture(t, store, 2<<20); second != ref {
		t.Fatal("duplicate record not deduplicated")
	}
	files, _ := os.ReadDir(store.scopeDir("key:2"))
	if len(files) != 1 {
		t.Fatal("duplicate files retained")
	}
	prompt, _ := json.Marshal(object{"input": "the text is \"" + ref + "\""})
	reader, n, err := store.OpenRequest("key:2", prompt)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(reader)
	reader.Close()
	if n != int64(len(prompt)) || !bytes.Equal(prompt, raw) {
		t.Fatal("reference-like user text was expanded")
	}
}

func TestOpaqueStreamKeepsToolAndUsage(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	_, bridge := mustPrepare(t, testSource(), "", nil)
	prefix := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":[{\"type\":\"compaction\",\"encrypted_content\":\""
	suffix := "\"}],\"usage\":{\"input_tokens\":17,\"output_tokens\":3}}}\n\n"
	wire := io.MultiReader(strings.NewReader(prefix), io.LimitReader(repeatedByteReader{'a'}, 2<<20), strings.NewReader(suffix))
	var output bytes.Buffer
	err := bridge.Transform(wire, func(e Event) error {
		if err := store.CompactEvent("key:2", e); err != nil {
			return err
		}
		if e.ResponseID() != "resp_test" || e.ResponseModel() != "gpt-6-astra" || !bytes.Contains(e.UsageJSON(), []byte("17")) {
			return fmt.Errorf("response metadata changed")
		}
		return e.WriteSSE(&output)
	})
	if err != nil || output.Len() > 1024 {
		t.Fatalf("stream bytes=%d error=%v", output.Len(), err)
	}
	sum := sha256.Sum256([]byte("unrelated"))
	invalid := []byte("\"" + opaquePrefix + hex.EncodeToString(sum[:]) + "\"")
	if _, _, err := store.OpenRequest("key:2", invalid); err == nil {
		t.Fatal("invented reference accepted")
	}
}

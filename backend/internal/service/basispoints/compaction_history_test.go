package basispoints

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestVerifiedStreamCompactionPrunesCoveredHistory(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	first := object{"type": "compaction", "id": "cmp_first", "encrypted_content": "first-ciphertext"}
	latest := object{"type": "compaction", "id": "cmp_latest", "encrypted_content": "latest-ciphertext"}
	for _, item := range []object{first, latest} {
		err := store.CompactEvent("key:1", Event{Payload: object{"type": "response.output_item.done", "item": item}}, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	source := testSource()
	source["input"] = []any{
		object{"type": "additional_tools", "tools": []any{object{"type": "function", "name": "retained_tool", "parameters": object{"type": "object"}}}},
		message("user", strings.Repeat("covered-history", 100000)), first,
		// Covered obsolete tool records must not be translated or replayed.
		object{"type": "item_reference", "id": "old-unexpandable"}, latest,
		message("developer", "current environment"), message("user", "continue"),
		object{"type": "function_call", "name": "retained_tool", "call_id": "call_new", "arguments": "{}"},
		object{"type": "function_call_output", "call_id": "call_new", "output": "new result"},
	}
	raw, _ := json.Marshal(source)
	body, bridge, err := PrepareWithCompactionStore(raw, "tenant", nil, store, "key:1")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 20000 || strings.Contains(string(body), "covered-history") || strings.Contains(string(body), "first-ciphertext") {
		t.Fatal("covered history leaked to upstream")
	}
	if len(bridge.tools) != 1 || !strings.Contains(string(body), "retained_tool") || !strings.Contains(string(body), "current environment") || !strings.Contains(string(body), "new result") {
		t.Fatal("current tools or post-compaction history were lost")
	}
	var output object
	if err := decode(body, &output); err != nil {
		t.Fatal(err)
	}
	items := output["input"].([]any)
	if !reflect.DeepEqual(items[0], latest) || !strings.Contains(string(body), "Help with code") || text(items[2].(object)["role"]) != "developer" {
		t.Fatal("latest compaction or top-level instructions were lost")
	}
	// Provenance survives restart, but cannot cross authenticated API keys.
	reopened := &OpaqueStore{Root: store.Root}
	history := source["input"].([]any)
	pruned, err := reopened.PruneHistory("key:1", history)
	if err != nil || len(pruned) != 5 {
		t.Fatalf("restart: length=%d err=%v", len(pruned), err)
	}
	unpruned, err := reopened.PruneHistory("key:2", history)
	if err != nil || len(unpruned) != len(history) {
		t.Fatal("cross-key pruning")
	}
	if len(history) != 9 {
		t.Fatal("original input was mutated")
	}
}

func TestPruneNativeRequestPreservesFieldsAndCanonicalWindow(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	if err := store.markHistoryCompaction("key:1", "cmp_verified"); err != nil {
		t.Fatal(err)
	}
	old := message("user", "covered")
	compact := object{"type": "compaction", "id": "cmp_verified", "encrypted_content": "opaque"}
	source := object{"model": "gpt-6-astra", "instructions": "current instructions", "input": []any{old, compact, message("user", "new")}, "stream": false}
	raw, _ := json.MarshalIndent(source, "", "  ")
	got, err := store.PruneRequest("key:1", raw)
	if err != nil {
		t.Fatal(err)
	}
	var output object
	if err := decode(got, &output); err != nil {
		t.Fatal(err)
	}
	want := []any{compact, message("user", "new")}
	if !reflect.DeepEqual(output["input"], want) || output["instructions"] != source["instructions"] || output["stream"] != false {
		t.Fatal("native body pruning changed required data")
	}
	got, err = store.PruneRequest("another-key", raw)
	if err != nil || string(got) != string(raw) {
		t.Fatal("unverified native history changed")
	}
	compact["id"] = "cmp_standalone"
	raw, _ = json.Marshal(source)
	got, err = store.PruneRequest("key:1", raw)
	if err != nil || string(got) != string(raw) {
		t.Fatal("canonical standalone compact input changed")
	}
}

func TestStandaloneCompactWindowAndUnknownCompactionRemainIntact(t *testing.T) {
	store := &OpaqueStore{Root: t.TempDir()}
	compact := object{"type": "compaction", "id": "cmp_standalone", "encrypted_content": "cipher"}
	history := []any{message("user", "retained user message"), compact, message("user", "new question")}
	for _, kind := range []string{"response.output_item.done", "response.completed"} {
		if err := store.CompactEvent("key:1", Event{Payload: object{"type": kind, "item": compact, "response": object{"output": []any{compact}}}}, false); err != nil {
			t.Fatal(err)
		}
		got, err := store.PruneHistory("key:1", history)
		if err != nil || !reflect.DeepEqual(got, history) {
			t.Fatal("standalone canonical window was pruned")
		}
	}
	if err := store.CompactEvent("key:1", Event{Payload: object{"type": "response.output_item.added", "item": compact}}, true); err != nil {
		t.Fatal(err)
	}
	got, err := store.PruneHistory("key:1", history)
	if err != nil || !reflect.DeepEqual(got, history) {
		t.Fatal("unfinished compaction triggered pruning")
	}
	var disabled *OpaqueStore
	got, err = disabled.PruneHistory("key:1", history)
	if err != nil || !reflect.DeepEqual(got, history) {
		t.Fatal("disabled store changed history")
	}
}

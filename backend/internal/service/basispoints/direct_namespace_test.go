package basispoints

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestDirectNamespaceStreamCompletesWithoutExecutingTools(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "namespace", "name": "mcp__node_repl", "tools": []any{object{"type": "function", "name": "js"}}}}
	_, bridge := mustPrepare(t, source, "scope", nil)
	native := object{"type": "function_call", "namespace": "mcp__node_repl", "name": "js", "call_id": "call_split", "id": "fc_split", "arguments": `{"code":"readOnly()"}`}
	wire := sse(object{"type": "response.output_item.done", "item": native}) + sse(object{"type": "response.completed", "response": object{"id": "resp_split", "status": "completed", "output": []any{native}}})
	body := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	var completed object
	if err := readEvents(strings.NewReader(string(raw)), func(event string, data []byte) error {
		if event == "response.failed" {
			t.Fatal("valid split namespace failed")
		}
		if event == "response.completed" {
			return decode(data, &completed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	response, ok := completed["response"].(object)
	if !ok {
		t.Fatal("completion missing")
	}
	output := response["output"].([]any)[0].(object)
	if output["namespace"] != "mcp__node_repl" || output["name"] != "js" {
		t.Fatal("wrong client tool emitted")
	}
}

func TestUnsupportedNativeDiagnosticDoesNotLeakData(t *testing.T) {
	const secret = "private-tool-name-and-payload"
	native := object{"name": secret, "namespace": secret, "arguments": secret}
	message := unsupportedNativeToolError(native, "name_not_in_catalog").Error()
	if strings.Contains(message, secret) || !strings.Contains(message, "native_tool=unknown") || !strings.Contains(message, "namespace=present") || !strings.Contains(message, "identity=") {
		t.Fatal("unsafe or incomplete diagnostic")
	}
	known := unsupportedNativeToolError(object{"name": "functions.read_skills", "arguments": secret}, "name_not_in_catalog").Error()
	if !strings.Contains(known, "native_tool=read_skills") || strings.Contains(known, secret) {
		t.Fatal("fixed native label missing")
	}
}

func TestDirectToolCallAcceptsSeparateNamespace(t *testing.T) {
	source := testSource()
	source["tools"] = []any{
		object{"type": "namespace", "name": "mcp__node_repl", "tools": []any{object{"type": "function", "name": "js"}}},
		object{"type": "namespace", "name": "other", "tools": []any{object{"type": "function", "name": "js"}}},
		object{"type": "namespace", "name": "functions", "tools": []any{object{"type": "custom", "name": "exec"}}},
	}
	for _, native := range []object{
		{"type": "function_call", "namespace": "mcp__node_repl", "name": "js", "arguments": `{"code":"readOnly()","n":9007199254740993}`},
		{"type": "function_call", "namespace": "mcp__node_repl", "name": "mcp__node_repl.js", "arguments": `{"code":"readOnly()"}`},
		{"type": "function_call", "namespace": "functions", "name": "mcp__node_repl.js", "arguments": `{"code":"readOnly()"}`},
		{"type": "custom_tool_call", "namespace": "functions", "name": "exec", "input": "text(1);\n"},
	} {
		native["id"], native["call_id"] = "fc_namespace", "call_namespace"
		source["input"] = "test namespace routing"
		cache := new(ReplayCache)
		_, bridge := mustPrepare(t, source, "namespace-scope", cache)
		got, err := bridge.translateCall(native)
		if err != nil {
			t.Fatal(err)
		}
		wantNS, wantName := "mcp__node_repl", "js"
		if native["type"] == "custom_tool_call" {
			wantNS, wantName = "functions", "exec"
			if got["input"] != native["input"] {
				t.Fatal("custom input changed")
			}
		} else {
			var before, after object
			if decode([]byte(text(native["arguments"])), &before) != nil || decode([]byte(text(got["arguments"])), &after) != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("function arguments changed")
			}
		}
		if got["namespace"] != wantNS || got["name"] != wantName || got["call_id"] != native["call_id"] {
			t.Fatalf("tool identity changed: %#v", got)
		}
		wrapped := cache.get(bridge.scope, "call_namespace")
		if wrapped["name"] != "run_officejs" || !strings.Contains(text(wrapped["arguments"]), wantNS+"."+wantName) {
			t.Fatal("namespaced call lost in replay")
		}
	}
}

func TestDirectToolNamespaceCannotSelectAnotherCatalogTool(t *testing.T) {
	source := testSource()
	source["tools"] = []any{
		object{"type": "function", "name": "js"},
		object{"type": "namespace", "name": "mcp__node_repl", "tools": []any{object{"type": "function", "name": "js"}}},
	}
	_, bridge := mustPrepare(t, source, "scope", nil)
	for _, native := range []object{
		{"name": "js", "namespace": "undeclared"},
		{"name": "mcp__node_repl.js", "namespace": "wrong"},
		{"name": "js", "namespace": 123},
		{"name": "functions.js", "namespace": "wrong"},
	} {
		native["type"], native["arguments"], native["call_id"] = "function_call", "{}", "call_wrong"
		if _, err := bridge.translateCall(native); err == nil {
			t.Fatal("namespace mismatch dispatched a tool")
		}
	}
}

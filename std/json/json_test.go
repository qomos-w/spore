package json_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/json"
)

func TestJsonModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"json.encode", "json.decode", "json.prettyEncode"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestJsonModule_EncodeString(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.encode", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != `"hello"` {
		t.Fatalf(`expected "hello", got %+v`, outcome.Payload)
	}
}

func TestJsonModule_EncodeMap(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.encode", Stage: binding.InvocationStageUnary, Args: []any{map[string]any{"a": 1.0, "b": "x"}}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if s != `{"a":1,"b":"x"}` {
		t.Fatalf(`expected {"a":1,"b":"x"}, got %s`, s)
	}
}

func TestJsonModule_EncodeArray(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.encode", Stage: binding.InvocationStageUnary, Args: []any{[]any{1.0, 2.0, 3.0}}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if s != `[1,2,3]` {
		t.Fatalf("expected [1,2,3], got %s", s)
	}
}

func TestJsonModule_Decode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.decode", Stage: binding.InvocationStageUnary, Args: []any{`{"name":"test","count":42}`}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	m, ok := outcome.Payload.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", outcome.Payload.Value)
	}
	if m["name"] != "test" {
		t.Fatalf("expected name=test, got %v", m["name"])
	}
	if m["count"] != 42.0 {
		t.Fatalf("expected count=42.0, got %v", m["count"])
	}
}

func TestJsonModule_DecodeInvalid(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.decode", Stage: binding.InvocationStageUnary, Args: []any{"not json"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}

func TestJsonModule_PrettyEncode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "json.prettyEncode", Stage: binding.InvocationStageUnary, Args: []any{map[string]any{"a": 1.0}}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	expected := "{\n  \"a\": 1\n}"
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

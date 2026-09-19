package url_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/url"
)

func TestUrlModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := url.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"url.encode", "url.decode", "url.parse"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestUrlModule_Encode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := url.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "url.encode", Stage: binding.InvocationStageUnary, Args: []any{"hello world"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello+world" {
		t.Fatalf("expected hello+world, got %+v", outcome.Payload)
	}
}

func TestUrlModule_Decode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := url.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "url.decode", Stage: binding.InvocationStageUnary, Args: []any{"hello+world%21"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello world!" {
		t.Fatalf("expected hello world!, got %+v", outcome.Payload)
	}
}

func TestUrlModule_Parse(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := url.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "url.parse", Stage: binding.InvocationStageUnary, Args: []any{"https://example.com/path?a=1&b=2#frag"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	m, ok := outcome.Payload.Value.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", outcome.Payload.Value)
	}
	if m["scheme"] != "https" {
		t.Fatalf("expected scheme=https, got %v", m["scheme"])
	}
	if m["host"] != "example.com" {
		t.Fatalf("expected host=example.com, got %v", m["host"])
	}
	if m["path"] != "/path" {
		t.Fatalf("expected path=/path, got %v", m["path"])
	}
	if m["port"] != "" {
		t.Fatalf("expected empty port, got %v", m["port"])
	}
	if m["rawQuery"] != "a=1&b=2" {
		t.Fatalf("expected rawQuery=a=1&b=2, got %v", m["rawQuery"])
	}
	if m["fragment"] != "frag" {
		t.Fatalf("expected fragment=frag, got %v", m["fragment"])
	}
}

func TestUrlModule_ParseInvalid(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := url.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "url.parse", Stage: binding.InvocationStageUnary, Args: []any{"://bad-url"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}

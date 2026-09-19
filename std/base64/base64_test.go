package base64_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/base64"
)

func TestBase64Module_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := base64.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"base64.encode", "base64.decode"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestBase64Module_Encode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := base64.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "base64.encode", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "aGVsbG8=" {
		t.Fatalf("expected aGVsbG8=, got %+v", outcome.Payload)
	}
}

func TestBase64Module_Decode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := base64.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "base64.decode", Stage: binding.InvocationStageUnary, Args: []any{"aGVsbG8="}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %+v", outcome.Payload)
	}
}

func TestBase64Module_DecodeInvalid(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := base64.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "base64.decode", Stage: binding.InvocationStageUnary, Args: []any{"!!!"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}

func TestBase64Module_Roundtrip(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := base64.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	original := "hello world 123 !@#"
	encoded, err := sb.Invoke(binding.InvocationRequest{Callable: "base64.encode", Stage: binding.InvocationStageUnary, Args: []any{original}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := sb.Invoke(binding.InvocationRequest{Callable: "base64.decode", Stage: binding.InvocationStageUnary, Args: []any{encoded.Payload.Value}})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Payload == nil || decoded.Payload.Value != original {
		t.Fatalf("expected %q, got %+v", original, decoded.Payload)
	}
}

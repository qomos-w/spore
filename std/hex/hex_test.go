package hex_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/hex"
)

func TestHexModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hex.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"hex.encode", "hex.decode"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestHexModule_Encode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hex.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "hex.encode", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "68656c6c6f" {
		t.Fatalf("expected 68656c6c6f, got %+v", outcome.Payload)
	}
}

func TestHexModule_Decode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hex.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "hex.decode", Stage: binding.InvocationStageUnary, Args: []any{"68656c6c6f"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %+v", outcome.Payload)
	}
}

func TestHexModule_DecodeInvalid(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hex.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "hex.decode", Stage: binding.InvocationStageUnary, Args: []any{"zz"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}

func TestHexModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hex.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import encode from "hex"
import decode from "hex"
fun roundtrip(): string { return decode(encode("hello")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("roundtrip", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %+v", outcome.Payload)
	}
}

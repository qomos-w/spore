package std_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std"
	"github.com/qomos-w/spore/std/json"
)

// TestStdModule_BatchImport verifies batch import syntax for std modules.
func TestStdModule_BatchImport(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
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

	if err := f.LoadSource(`import { encode, decode } from "json"
fun roundtrip(): string { return decode(encode("batch")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("roundtrip", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "batch" {
		t.Fatalf("expected batch, got %+v", outcome.Payload)
	}
}

// TestStdModule_ReExport verifies that std functions can be re-exported from
// a script module and consumed by another module.
func TestStdModule_ReExport(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`export md5 from "hash"
export encode from "json"
export toUpper from "strings"
fun combine(): string {
    return encode(toUpper(md5("hello")))
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("combine", nil)
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
	if s != `"5D41402ABC4B2A76B9719D911017C592"` {
		t.Fatalf("expected quoted uppercase md5, got %s", s)
	}
}

// TestStdModule_RegisterAllIfAbsentWithCustomModule verifies that
// RegisterAllIfAbsent skips pre-registered capabilities while filling in the
// rest, and scripts can use both custom and standard modules.
func TestStdModule_RegisterAllIfAbsentWithCustomModule(t *testing.T) {
	sb := binding.NewScriptBinding()

	// Register a custom "math" module with a unique function.
	customBuilder := binding.NewCapability("math", "module")
	if err := customBuilder.AddFreeFunction("customAdd", func(a, b float64) float64 {
		return a + b + 999
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := customBuilder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("math"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}

	// Fill in remaining standard modules.
	if err := std.RegisterAllIfAbsent(sb); err != nil {
		t.Fatalf("RegisterAllIfAbsent: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import customAdd from "math"
import encode from "json"
fun test(): string {
    return encode(customAdd(1.0, 2.0))
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("test", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "1002" {
		t.Fatalf("expected 1002, got %+v", outcome.Payload)
	}
}

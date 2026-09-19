package bytecode

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

type phase3CapabilityInput struct {
	Text string
}

type phase3CapabilityOutput struct {
	Text string
}

func phase3CapabilityEcho(in phase3CapabilityInput) (phase3CapabilityOutput, error) {
	return phase3CapabilityOutput{Text: "ok:" + in.Text}, nil
}

func TestVMEvaluator_NativeFlatCallFallback(t *testing.T) {
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)

	result, err := eval.Invoke(nil, "tool.execute", []any{map[string]any{"Text": "hello"}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected projected map result, got %T", result)
	}
	if out["Text"] != "ok:hello" {
		t.Fatalf("expected ok:hello, got %v", out["Text"])
	}
}

func TestVMEvaluator_NativeMemberCallRequiresNamespaceLowering(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result != "ok:hello" {
		t.Fatalf("expected ok:hello, got %v", result)
	}
}

func TestVMEvaluator_MaxHostCallsStopsBeforeOverLimitDispatch(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { tool.execute({"Text": "first"}); return tool.execute({"Text": "second"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	calls := 0
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", func(in phase3CapabilityInput) (phase3CapabilityOutput, error) {
		calls++
		return phase3CapabilityEcho(in)
	}); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = eval.EvaluateContext(context.Background(), binding.ExecutionBudget{MaxHostCalls: 1}, "call_tool", binding.InvocationStageUnary, nil)
	if err == nil || !strings.Contains(err.Error(), "host call budget exceeded") {
		t.Fatalf("expected host call budget error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("native dispatch count = %d, want 1", calls)
	}
}

func TestVMEvaluator_NativeUnknownCallableGetsDiagnostic(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.missing({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected runtime error for unknown native callable")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "undefined_native_callable" {
		t.Fatalf("expected undefined_native_callable code, got %q", rtErr.Code)
	}
	if rtErr.Category != diagnostics.CategoryRuntime {
		t.Fatalf("expected runtime category, got %q", rtErr.Category)
	}
	if rtErr.Path != "vm/call/native" {
		t.Fatalf("expected vm/call/native path, got %q", rtErr.Path)
	}
	if rtErr.Callable != "call_tool" {
		t.Fatalf("expected call_tool callable, got %q", rtErr.Callable)
	}
	if !strings.Contains(rtErr.Message, "tool.missing") {
		t.Fatalf("expected message to identify tool.missing, got %q", rtErr.Message)
	}
}

func TestVMEvaluator_NativeFlatCallHasScriptPrecedence(t *testing.T) {
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)

	prog, err := frontend.ParseModuleForTest(`fun tool(): string { return "script" } fun call_tool(): string { return tool() }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result != "script" {
		t.Fatalf("expected script, got %v", result)
	}
}

func TestVMEvaluator_NativeNamespaceShadowedByGlobalVariable(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`var tool: int = 7 fun call_tool(): int { return tool }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result != 7 {
		t.Fatalf("expected 7, got %v", result)
	}
}

func TestVMEvaluator_NativeNamespaceRequiresExplicitExposureAtRuntime(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil); err == nil {
		t.Fatal("expected runtime error when capability callable is not exposed")
	}
}

func TestVMEvaluator_NativeCapabilityRegistrationAfterCompile(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	eval.AllowNativeNamespace("tool")
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile before registration: %v", err)
	}

	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)

	result, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate after registration: %v", err)
	}
	if result != "ok:hello" {
		t.Fatalf("expected ok:hello, got %v", result)
	}
}

func TestVMEvaluator_NativeExecutableCacheSeesExposureAfterSetNativeBinding(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}

	result, err := eval.Evaluate("call_tool", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate after exposure: %v", err)
	}
	if result != "ok:hello" {
		t.Fatalf("expected ok:hello, got %v", result)
	}
}

func TestVMEvaluator_NativeVoidReturnResolves(t *testing.T) {
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	desc := schema.CallableDesc{Name: "tool.noop", Mode: schema.CallableModeUnary}
	adapter, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}
	if err := sb.Callables.Register(desc); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := sb.Executors.RegisterAdapter(adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}
	eval.SetNativeBinding(sb)

	value, ok, err := eval.InvokeVMNative(context.Background(), "tool.noop", nil)
	if err != nil {
		t.Fatalf("InvokeVMNative: %v", err)
	}
	if !ok {
		t.Fatal("expected void native callable to resolve as handled (ok=true)")
	}
	if value != vm.EncodeInt(0) {
		t.Fatalf("expected encoded void (0) value, got %v", value)
	}
}

func TestVMEvaluator_UnresolvedMemberCallStillFailsCompile(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return typo.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := NewVMEvaluator().CompileProgram(prog); err == nil {
		t.Fatal("expected compile error for unresolved member receiver")
	} else {
		coder, ok := err.(interface{ DiagnosticCode() string })
		if !ok || coder.DiagnosticCode() != "undefined_variable" {
			t.Fatalf("expected undefined_variable diagnostic, got %v", err)
		}
		pather, ok := err.(interface{ DiagnosticPath() string })
		if !ok || pather.DiagnosticPath() != "bytecode/scope/identifier" {
			t.Fatalf("expected bytecode/scope/identifier path, got %v", err)
		}
	}
}

func TestVMEvaluator_NativeNamespaceWithoutAllowFailsCompile(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	// No AllowNativeNamespace("tool") — should fail compile
	err = eval.CompileProgram(prog)
	if err == nil {
		t.Fatal("expected compile error when namespace is not declared")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "undefined_variable" {
		t.Fatalf("expected undefined_variable diagnostic, got %v", err)
	}
}

type taggedPhase3Input struct {
	DisplayName string `json:"display_name"`
}

type taggedPhase3Output struct {
	Message string `json:"msg"`
}

func phase3CapabilityTaggedEcho(in taggedPhase3Input) (taggedPhase3Output, error) {
	return taggedPhase3Output{Message: "ok:" + in.DisplayName}, nil
}

func TestVMEvaluator_NativeCallRespectsJsonTags(t *testing.T) {
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", phase3CapabilityTaggedEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)

	result, err := eval.Invoke(nil, "tool.execute", []any{map[string]any{"display_name": "hello"}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected projected map result, got %T", result)
	}
	if out["msg"] != "ok:hello" {
		t.Fatalf("expected msg=ok:hello, got %v", out["msg"])
	}
	if _, exists := out["Message"]; exists {
		t.Fatal("expected Message key to be remapped to msg via json tag")
	}
}

func TestVMEvaluator_NativeCallSupportsTimeAndBytes(t *testing.T) {
	eval := NewVMEvaluator()
	when := time.Date(2026, 4, 22, 10, 11, 12, 123456789, time.UTC)
	args, err := toVMArgs(nil, eval.VM(), []any{when, []byte("hello")})
	if err != nil {
		t.Fatalf("toVMArgs: %v", err)
	}
	if got := vmValueToAny(eval.VM(), args[0]); got != when.Format(time.RFC3339Nano) {
		t.Fatalf("expected RFC3339 string, got %#v", got)
	}
	if got := vmValueToAny(eval.VM(), args[1]); string(got.([]byte)) != "hello" {
		t.Fatalf("expected bytes hello, got %#v", got)
	}

	projected := projectNativeResult(struct {
		When time.Time
		Data []byte
	}{When: when, Data: []byte("world")}).(map[string]any)
	if projected["When"] != when.Format(time.RFC3339Nano) {
		t.Fatalf("expected projected time string, got %#v", projected["When"])
	}
	if string(projected["Data"].([]byte)) != "world" {
		t.Fatalf("expected projected bytes, got %#v", projected["Data"])
	}
}

func TestVMEvaluator_NativeCallProjectsNonUTF8Bytes(t *testing.T) {
	raw := []byte{0x80, 0x81, 0xfe, 0xff}
	projected := projectNativeResult(struct{ Payload []byte }{Payload: raw}).(map[string]any)
	got, ok := projected["Payload"].([]byte)
	if !ok {
		t.Fatalf("expected []byte projection, got %T", projected["Payload"])
	}
	if !ok || len(got) != len(raw) {
		t.Fatalf("expected raw bytes, got %q", got)
	}
	for i := range raw {
		if got[i] != raw[i] {
			t.Fatalf("byte mismatch at %d: expected %x, got %x", i, raw[i], got[i])
		}
	}
}

func TestVMEvaluator_NativeCallProjectsNestedTimeAndBytes(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projected := projectNativeResult(struct {
		Inner struct {
			When time.Time
			Data []byte
		}
	}{Inner: struct {
		When time.Time
		Data []byte
	}{When: when, Data: []byte("nested")}}).(map[string]any)
	inner, ok := projected["Inner"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", projected["Inner"])
	}
	if inner["When"] != when.Format(time.RFC3339Nano) {
		t.Fatalf("expected nested time string, got %#v", inner["When"])
	}
	if string(inner["Data"].([]byte)) != "nested" {
		t.Fatalf("expected nested bytes, got %v", inner["Data"])
	}
}

func TestVMEvaluator_NativeCallProjectsTimeSlice(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	projected := projectNativeResult(struct{ Times []time.Time }{Times: []time.Time{t1, t2}}).(map[string]any)
	times, ok := projected["Times"].([]any)
	if !ok {
		t.Fatalf("expected slice, got %T", projected["Times"])
	}
	if len(times) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(times))
	}
	if times[0] != t1.Format(time.RFC3339Nano) {
		t.Fatalf("expected t1 string, got %v", times[0])
	}
	if times[1] != t2.Format(time.RFC3339Nano) {
		t.Fatalf("expected t2 string, got %v", times[1])
	}
}

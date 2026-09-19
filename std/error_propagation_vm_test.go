package std_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std"
)

// TestStdErrorPropagation_JsonDecodeInvalid verifies that a native function
// error (json.decode with invalid input) propagates through the VM execution
// chain as a structured runtime error rather than a silent failure.
func TestStdErrorPropagation_JsonDecodeInvalid(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import decode from "json"
fun badDecode(): any { return decode("not json") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("badDecode", nil)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected InvocationResultError for invalid json.decode, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor, got nil")
	}
	msg := outcome.Result.Error.Message
	if !strings.Contains(msg, "invalid character") {
		t.Fatalf("expected invalid character in error message, got %s", msg)
	}
}

// TestStdErrorPropagation_Base64DecodeInvalid verifies base64.decode error
// propagation through the VM chain.
func TestStdErrorPropagation_Base64DecodeInvalid(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import decode from "base64"
fun badDecode(): string { return decode("!!!") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("badDecode", nil)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected InvocationResultError for invalid base64.decode, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor, got nil")
	}
	msg := outcome.Result.Error.Message
	if !strings.Contains(msg, "illegal base64") {
		t.Fatalf("expected illegal data in error message, got %s", msg)
	}
}

// TestStdErrorPropagation_StrconvParseInvalid verifies strconv.parseInt error
// propagation through the VM chain.
func TestStdErrorPropagation_StrconvParseInvalid(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import parseInt from "strconv"
fun badParse(): long { return parseInt("abc") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("badParse", nil)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected InvocationResultError for invalid strconv.parseInt, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor, got nil")
	}
	msg := outcome.Result.Error.Message
	if !strings.Contains(msg, "invalid syntax") {
		t.Fatalf("expected invalid syntax in error message, got %s", msg)
	}
}

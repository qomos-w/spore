package script

import (
	"errors"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

// These tests pin the consumer-facing half of the #29 error-model convergence:
// a failure raised inside a vm function/method body — the seam whose signature
// only carries a value — must reach the host as a structured runtime error, and
// the Runtime must stay usable afterwards.

// panicBoundaryGreeterDesc is the interface descriptor shared by the failing
// host targets below.
func panicBoundaryGreeterDesc() schema.InterfaceDesc {
	return schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}
}

type erroringGreeter struct{}

func (g *erroringGreeter) Greet(name string) (string, error) {
	return "", errors.New("greeter exploded on " + name)
}

type panickingGreeter struct{}

func (g *panickingGreeter) Greet(name string) string {
	panic("greeter crashed on " + name)
}

// TestRuntime_HostInterfaceMethodErrorBecomesStructuredError: a bound Go method
// that returns an error used to escape Call as a Go panic (the host's recover
// was the only way to observe it). It must now arrive as a structured runtime
// error carrying the same stable code the native-callable path uses, and the
// runtime must remain usable for later calls.
func TestRuntime_HostInterfaceMethodErrorBecomesStructuredError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", panicBoundaryGreeterDesc(), &erroringGreeter{}); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun hello(): string { return Greeter.greet("bob") }
export fun ok(): int { return 7 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("hello")
	if err != nil {
		t.Fatalf("Call returned a host-level error %v, want a structured runtime error", err)
	}
	if result.Error == nil {
		t.Fatalf("expected a structured runtime error, got value %#v", result.Value)
	}
	if got := result.Error.Diagnostic.Code; got != "native_call_failed" {
		t.Fatalf("diagnostic code = %q, want native_call_failed", got)
	}
	if msg := result.Error.Diagnostic.Message; !strings.Contains(msg, "greeter exploded on bob") {
		t.Fatalf("diagnostic message %q should keep the host error text", msg)
	}

	// The failure is repeatable rather than a one-shot side effect of a broken
	// interpreter, and unrelated callables still work.
	again, err := rt.Call("hello")
	if err != nil {
		t.Fatalf("second Call returned a host-level error %v", err)
	}
	if again.Error == nil {
		t.Fatal("expected the second Call to fail the same way")
	}
	okResult, err := rt.Call("ok")
	if err != nil {
		t.Fatalf("Call ok: %v", err)
	}
	var got int
	if err := okResult.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != 7 {
		t.Fatalf("ok() = %d, want 7", got)
	}
}

// TestRuntime_HostInterfaceMethodGoPanicBecomesStructuredError: a bound Go
// method that panics is recovered by the binding layer and must surface through
// the same structured channel instead of unwinding into the host.
func TestRuntime_HostInterfaceMethodGoPanicBecomesStructuredError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", panicBoundaryGreeterDesc(), &panickingGreeter{}); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun hello(): string { return Greeter.greet("carol") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("hello")
	if err != nil {
		t.Fatalf("Call returned a host-level error %v, want a structured runtime error", err)
	}
	if result.Error == nil {
		t.Fatalf("expected a structured runtime error, got value %#v", result.Value)
	}
	if got := result.Error.Diagnostic.Code; got != "native_call_failed" {
		t.Fatalf("diagnostic code = %q, want native_call_failed", got)
	}
	if msg := result.Error.Diagnostic.Message; !strings.Contains(msg, "greeter crashed on carol") {
		t.Fatalf("diagnostic message %q should keep the host panic text", msg)
	}
}

// TestRuntime_ClassMethodRuntimeErrorKeepsDiagnosticCode: a runtime error raised
// inside a class method used to escape Call as a panic whose text was the only
// clue. It must now arrive as a structured error, and — because the engine
// forwards the *RuntimeError itself rather than a rendered string — it must
// keep its original code instead of collapsing into vm_internal_panic.
func TestRuntime_ClassMethodRuntimeErrorKeepsDiagnosticCode(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `
class Kernel {
  fun divide(a: int, b: int): int { return a / b }
}
export fun boom(): int {
  var k: Kernel = new Kernel()
  return k.divide(1, 0)
}
export fun ok(): int {
  var k: Kernel = new Kernel()
  return k.divide(6, 2)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("boom")
	if err != nil {
		t.Fatalf("Call returned a host-level error %v, want a structured runtime error", err)
	}
	if result.Error == nil {
		t.Fatalf("expected a structured runtime error, got value %#v", result.Value)
	}
	if got := result.Error.Diagnostic.Code; got != "division_by_zero" {
		t.Fatalf("diagnostic code = %q, want division_by_zero (not a generic panic code)", got)
	}

	// The aborted run must not poison the shared interpreter: a method call
	// that succeeds has to keep working after the recovered panic.
	okResult, err := rt.Call("ok")
	if err != nil {
		t.Fatalf("Call ok: %v", err)
	}
	var got int
	if err := okResult.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != 3 {
		t.Fatalf("ok() = %d, want 3", got)
	}
}

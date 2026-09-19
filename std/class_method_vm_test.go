package std_test

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std"
)

// TestStdInClassMethod verifies that std functions can be called from class
// methods in scripts.
func TestStdInClassMethod(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import md5 from "hash"
import toUpper from "strings"
class Hasher {
    fun hash(s: string): string {
        return toUpper(md5(s))
    }
}
fun test(): string {
    var h: Hasher = new Hasher()
    return h.hash("hello")
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("test", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "5D41402ABC4B2A76B9719D911017C592" {
		t.Fatalf("expected uppercase md5, got %+v", outcome.Payload)
	}
}

// TestStdInNestedFunctionCall verifies std functions work as arguments to
// other functions and in nested expressions.
func TestStdInNestedFunctionCall(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import abs from "math"
import formatFloat from "strconv"
fun helper(x: double): string {
    return formatFloat(abs(x))
}
fun test(): string {
    return helper(-42.0)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("test", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "42" {
		t.Fatalf("expected 42, got %+v", outcome.Payload)
	}
}

package strings_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/strings"
)

func TestStringsModule_VMEndToEnd_Import(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
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

	if err := f.LoadSource(`import contains from "strings"
import toUpper from "strings"
import replace from "strings"
fun transform(): string {
    if contains("hello", "ell") {
        return toUpper(replace("hello", "l", "L", -1))
    }
    return ""
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("transform", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "HELLO" {
		t.Fatalf("expected HELLO, got %+v", outcome.Payload)
	}
}

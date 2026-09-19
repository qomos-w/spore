package regexp_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/regexp"
)

func TestRegexpModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
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

	if err := f.LoadSource(`import match from "regexp"
import replace from "regexp"
fun testMatch(): bool { return match("^hello", "hello world") }
fun testReplace(): string { return replace("world", "hello world", "spore") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("testMatch", nil)
	if err != nil {
		t.Fatalf("Invoke match: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}

	outcome, err = f.Invoke("testReplace", nil)
	if err != nil {
		t.Fatalf("Invoke replace: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello spore" {
		t.Fatalf("expected hello spore, got %+v", outcome.Payload)
	}
}

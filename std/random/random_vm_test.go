package random_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/random"
)

func TestRandomModule_VMEndToEnd_Import(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := random.Register(sb); err != nil {
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

	if err := f.LoadSource(`import intn from "random"
fun roll(): long { return intn(6) + 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("roll", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	n, ok := outcome.Payload.Value.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", outcome.Payload.Value)
	}
	if n < 1 || n > 6 {
		t.Fatalf("expected 1-6, got %d", n)
	}
}

package random_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/random"
)

func TestRandomModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := random.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"random.intn", "random.float64"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestRandomModule_Intn(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := random.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 20; i++ {
		outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "random.intn", Stage: binding.InvocationStageUnary, Args: []any{int64(100)}})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		v, ok := outcome.Payload.Value.(int64)
		if !ok {
			t.Fatalf("expected int64, got %T", outcome.Payload.Value)
		}
		if v < 0 || v >= 100 {
			t.Fatalf("expected 0 <= v < 100, got %d", v)
		}
	}
}

func TestRandomModule_Float64(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := random.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 20; i++ {
		outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "random.float64", Stage: binding.InvocationStageUnary})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		v, ok := outcome.Payload.Value.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T", outcome.Payload.Value)
		}
		if v < 0.0 || v >= 1.0 {
			t.Fatalf("expected 0.0 <= v < 1.0, got %f", v)
		}
	}
}

func TestRandomModule_VMEndToEnd(t *testing.T) {
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

	// Use double return type to avoid int/long mismatch with intn's int64 return
	if err := f.LoadSource(`import intn from "random"
fun roll(): double { return intn(6) + 1.0 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	for i := 0; i < 30; i++ {
		outcome, err := f.Invoke("roll", nil)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		v, ok := outcome.Payload.Value.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T", outcome.Payload.Value)
		}
		if v < 1.0 || v > 6.0 {
			t.Fatalf("expected 1.0 <= v <= 6.0, got %f", v)
		}
	}
}

package time_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/time"
)

func TestTimeModule_VMEndToEnd_Import(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := time.Register(sb); err != nil {
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

	if err := f.LoadSource(`import nowUnix from "time"
import format from "time"
fun formatNow(): string { return format(nowUnix(), "2006-01-02") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("formatNow", nil)
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
	if len(s) != 10 {
		t.Fatalf("expected YYYY-MM-DD format (len 10), got %q (len %d)", s, len(s))
	}
}

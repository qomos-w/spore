package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
)

func nativeBenchInc(x int) (int, error) {
	return x + 1, nil
}

func BenchmarkVMEvaluatorNativeBridgeInc(b *testing.B) {
	prog, err := frontend.ParseModuleForTest(`fun run(n: int): int {
  var x: int = 0
  for (var i: int = 0; i < n; i = i + 1) { x = tool.inc(x) }
  return x
}`)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFreeFunction("inc", nativeBenchInc); err != nil {
		b.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		b.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		b.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	eval.SetNativeBinding(sb)
	if err := eval.CompileProgram(prog); err != nil {
		b.Fatalf("compile: %v", err)
	}
	if _, err := eval.Evaluate("run", binding.InvocationStageUnary, []any{100}); err != nil {
		b.Fatalf("warmup: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eval.Evaluate("run", binding.InvocationStageUnary, []any{100}); err != nil {
			b.Fatalf("evaluate: %v", err)
		}
	}
}

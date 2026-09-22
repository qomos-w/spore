package benchmarks

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/invoke"
)

// Go->VM map-boxing micro-benchmark. Mirrors the barcraft perception-snapshot
// regime: per-frame host code pushes a ~10-key map<string, any> with mixed
// string/float/int/bool leaves, nested maps and small arrays into a script
// callable. The callable body is a constant so the measured cost is dominated
// by toVMArgs/anyToVMValueAtPath/stringMapToVMMap and the temporary-root churn
// of the conversion, not by script execution.
const mapBoxingSource = `export fun perceive(snap: any): int { return 42 }`

func newMapBoxingRunner(b *testing.B) *bytecode.VMEvaluator {
	b.Helper()
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		b.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	if err := f.LoadSource(mapBoxingSource); err != nil {
		b.Fatalf("LoadSource: %v", err)
	}
	return vmEval
}

// perceptionSnapshot builds the typical per-pawn perception payload: 10 keys,
// mixed scalar leaves, three nested maps and two small arrays.
func perceptionSnapshot() map[string]any {
	return map[string]any{
		"id":     42,
		"name":   "pawn-alpha",
		"hp":     87.5,
		"alive":  true,
		"pos":    map[string]any{"x": 12.25, "y": -3.5, "zone": 3},
		"vel":    map[string]any{"dx": 0.5, "dy": -1.25},
		"target": map[string]any{"id": 7, "kind": "cover", "dist": 18.75},
		"allies": []any{3, 5, 8},
		"tags":   []any{"fast", "armed"},
		"score":  1234,
	}
}

func BenchmarkGoToVMMapBoxing_PerceptionSnapshot10Keys(b *testing.B) {
	vmEval := newMapBoxingRunner(b)
	snap := perceptionSnapshot()
	args := []any{snap}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := vmEval.Evaluate("perceive", invoke.InvocationStageUnary, args)
		if err != nil {
			b.Fatalf("perceive: %v", err)
		}
		if n, ok := out.(int); !ok || n != 42 {
			b.Fatalf("perceive = %v, want 42", out)
		}
	}
}

// Nested-map stress: same shape but the hot loop rebuilds the snapshot each
// call (host-side construction + boxing together, as in a real tick).
func BenchmarkGoToVMMapBoxing_PerceptionSnapshotRebuilt(b *testing.B) {
	vmEval := newMapBoxingRunner(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := perceptionSnapshot()
		out, err := vmEval.Evaluate("perceive", invoke.InvocationStageUnary, []any{snap})
		if err != nil {
			b.Fatalf("perceive: %v", err)
		}
		if n, ok := out.(int); !ok || n != 42 {
			b.Fatalf("perceive = %v, want 42", out)
		}
	}
}

package integration_test

import (
	"testing"

	"github.com/qomos-w/spore/script"
)

// Hot-path script invocation benchmarks: same callable, same arg shape,
// repeated. This is the regime game systems use (per-entity Think calls)
// and surfaces per-invoke overhead: descriptor lookups, argument
// marshaling (toVMArgs/stringMapToVMMap), interpreter frames, and VM
// lifecycle — none of which the ECS batch benchmarks above stress.

const scriptHotpathSource = `export fun tick(): int { return 1 }

export fun ping(x: int): int { return x + 1 }

export fun sum(m: map<string, int>): int { return m["a"] + m["b"] }`

func hotpathSetup(b *testing.B) *script.Runtime {
	b.Helper()
	rt, err := script.NewRuntime()
	if err != nil {
		b.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("hotpath", scriptHotpathSource); err != nil {
		b.Fatalf("LoadSource: %v", err)
	}
	return rt
}

func benchCall(b *testing.B, name string, args ...any) int {
	b.Helper()
	rt := hotpathSetup(b)
	b.ReportAllocs()
	b.ResetTimer()
	var out int
	for i := 0; i < b.N; i++ {
		res, err := rt.Call(name, args...)
		if err != nil {
			b.Fatalf("%s: %v", name, err)
		}
		n, err := res.AsInt()
		if err != nil {
			b.Fatalf("%s: %v", name, err)
		}
		out = n
	}
	return out
}

func BenchmarkScriptCall_NoArgs(b *testing.B) {
	if got := benchCall(b, "tick"); got != 1 {
		b.Fatalf("tick = %d, want 1", got)
	}
}

func BenchmarkScriptCall_IntArg(b *testing.B) {
	if got := benchCall(b, "ping", 41); got != 42 {
		b.Fatalf("ping = %d, want 42", got)
	}
}

func BenchmarkScriptCall_MapArg(b *testing.B) {
	args := map[string]int{"a": 1, "b": 2}
	if got := benchCall(b, "sum", args); got != 3 {
		b.Fatalf("sum = %d, want 3", got)
	}
}

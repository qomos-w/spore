package bytecode

import "testing"

// TestValueCodec_BuiltinEncodeZeroAlloc guards the codec's hot-path contract:
// encoding the built-in primitive types must not allocate, so keeping the
// extension seam in the fall-through branch (rather than probing every value
// up front) preserves the previous per-value cost.
func TestValueCodec_BuiltinEncodeZeroAlloc(t *testing.T) {
	eval := NewVMEvaluator()
	cases := []struct {
		name string
		v    any
	}{
		{"int32", int32(7)},
		{"int", int(7)},
		{"bool", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(1000, func() {
				_, _ = anyToVMValueAtPath(nil, eval.vm_, tc.v, "vm/test/alloc")
			})
			if allocs != 0 {
				t.Fatalf("built-in %s encode allocated %v objects per run, want 0", tc.name, allocs)
			}
		})
	}
}

// TestValueCodec_AnyMapBoxingSteadyStateZeroAlloc guards the conversion-loop
// contract for the dominant host->VM shape: after the string pool and the
// root-scope pool are warm, boxing a nested map[string]any snapshot must not
// allocate on the Go side (VM-heap slots are the VM GC's business).
func TestValueCodec_AnyMapBoxingSteadyStateZeroAlloc(t *testing.T) {
	eval := NewVMEvaluator()
	snapshot := map[string]any{
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
	// Warm up: interns every key/leaf string and grows the scope/value pools.
	for i := 0; i < 50; i++ {
		if _, err := anyToVMValueAtPath(nil, eval.vm_, snapshot, "vm/test/alloc"); err != nil {
			t.Fatalf("convert: %v", err)
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = anyToVMValueAtPath(nil, eval.vm_, snapshot, "vm/test/alloc")
	})
	if allocs != 0 {
		t.Fatalf("map boxing allocated %v objects per run in steady state, want 0", allocs)
	}
}

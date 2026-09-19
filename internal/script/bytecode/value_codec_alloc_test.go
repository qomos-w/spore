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

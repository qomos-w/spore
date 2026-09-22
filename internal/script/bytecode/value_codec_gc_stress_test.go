package bytecode

import (
	"testing"
)

// TestValueCodec_NestedConversionSurvivesGC stress-tests the temporary-root
// discipline of the Go->VM conversion loops: with a tiny VM heap the GC runs
// repeatedly mid-conversion, and every pending (not yet stored) map/array
// handle must survive until it is reachable from its parent container.
// Regression guard for the batched rootScope rework of the boxing path.
func TestValueCodec_NestedConversionSurvivesGC(t *testing.T) {
	eval := NewVMEvaluatorWith(16<<10, 64) // 16 KiB heap: forces constant GC

	snapshot := func(depth int) map[string]any {
		// Linear chain: each level references only its child, so the live
		// footprint is proportional to depth, not depth^2.
		m := map[string]any{
			"level": depth,
			"x":     float64(depth) * 1.5,
			"names": []any{"a", "b", "c"},
		}
		for i := depth - 1; i >= 0; i-- {
			m = map[string]any{
				"name":   "pawn",
				"hp":     87.5,
				"alive":  true,
				"ids":    []any{int32(1), int32(2), int32(3)},
				"tags":   []any{"fast", "armed"},
				"nested": m,
			}
		}
		return m
	}

	for _, depth := range []int{0, 1, 4, 16} {
		for round := 0; round < 200; round++ {
			val, err := anyToVMValueAtPath(nil, eval.vm_, snapshot(depth), "vm/test/gc-stress")
			if err != nil {
				t.Fatalf("depth=%d round=%d: convert: %v", depth, round, err)
			}
			// Round-trip decode must see the same leaves; a swept handle would
			// surface as a missing key or corrupted value.
			decoded, ok := vmValueToAny(eval.vm_, val).(map[string]any)
			if !ok {
				t.Fatalf("depth=%d round=%d: decode: got %T", depth, round, vmValueToAny(eval.vm_, val))
			}
			if depth == 0 {
				// The depth-0 snapshot is the leaf itself.
				if decoded["names"].([]any)[0] != "a" {
					t.Fatalf("depth=0 round=%d: leaf corrupted: %#v", round, decoded["names"])
				}
				continue
			}
			if decoded["name"] != "pawn" {
				t.Fatalf("depth=%d round=%d: name leaf corrupted: %#v", depth, round, decoded["name"])
			}
			hp, ok := decoded["hp"].(float64)
			if !ok || hp != 87.5 {
				t.Fatalf("depth=%d round=%d: hp leaf corrupted: %#v", depth, round, decoded["hp"])
			}
			ids, ok := decoded["ids"].([]any)
			if !ok || len(ids) != 3 || ids[2].(int) != 3 {
				t.Fatalf("depth=%d round=%d: ids leaf corrupted: %#v", depth, round, decoded["ids"])
			}
		}
	}
	if eval.vm_.GCCollections() <= 0 {
		t.Fatalf("stress test never triggered a VM GC; heap too large to validate rooting")
	}
}

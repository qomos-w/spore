package script_test

import (
	"fmt"
	"testing"

	"github.com/qomos-w/spore/script"
)

func TestReproNestedMapArg(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	src := `
export fun think(pawn_id: long, snap: map<string, any>, dt: double): map<string, any> {
    var threat: any = snap["threat"]
    if threat != null {
        var m: map<string, any> = threat as map<string, any>
        var x: double = m["x"] as double
        return {"job": 17, "x": x}
    }
    return null
}
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	snap := map[string]any{
		"current_job": 3.0,
		"has_job":     true,
		"threat":      map[string]any{"x": 3.0, "y": 4.0, "z": 0.0},
	}
	r, err := rt.Call("think", int64(1), snap, 1.0)
	if err != nil {
		t.Fatalf("Call think: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("think errored: %v", r.Error)
	}
	m, ok := r.Value.(map[string]any)
	if !ok || m["job"] != 17 {
		t.Fatalf("expected {job:17}, got %#v", r.Value)
	}
}

func TestReproManyKeyMapArg(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	src := `
export fun probe(snap: map<string, any>): double {
    return snap["key_39"] as double
}
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	snap := map[string]any{}
	for i := 0; i < 40; i++ {
		snap[fmt.Sprintf("key_%02d", i)] = float64(i)
	}
	r, err := rt.Call("probe", snap)
	if err != nil {
		t.Fatalf("Call probe: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("probe errored: %v", r.Error)
	}
	if r.Value != float64(39) {
		t.Fatalf("expected 39 (last of 40 keys), got %#v", r.Value)
	}
}

func TestReproNilValueInMapArg(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	src := `
export fun think(snap: map<string, any>): map<string, any> {
    var threat: any = snap["threat"]
    if threat != null {
        return {"job": 17}
    }
    return null
}
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	snap := map[string]any{
		"current_job": 3.0,
		"has_job":     true,
		"threat":      nil,
	}
	r, err := rt.Call("think", snap)
	if err != nil {
		t.Fatalf("Call think: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("think errored: %v", r.Error)
	}
	if r.Value != nil {
		t.Fatalf("expected nil (threat==null path), got %#v", r.Value)
	}
}

// Repro (barcraft scriptai crash, "map is full (should not happen)"):
// stringMapToVMMap did not root the outer map handle while converting
// nested values. An explicit 512 KiB budget (65536 slots) trips its 3/4 GC
// threshold mid-conversion; collect() frees the unrooted map, its memory is
// reused by later nested allocations, and the next MapSet writes into a
// foreign object — surfacing as "map is full" or invalid handle panics.
func TestReproNestedMapArgUnderGCPressure(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{VMHeapBytes: 524288, VMHeapSlots: 256})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	src := `
export fun probe(snap: map<string, any>): double {
    var last: any = snap["k699"]
    var m: map<string, any> = last as map<string, any>
    return m["v"] as double
}
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	snap := map[string]any{}
	for i := 0; i < 700; i++ {
		snap[fmt.Sprintf("k%d", i)] = map[string]any{
			"v":    float64(i),
			"tag":  "tag",
			"name": "name",
		}
	}
	r, err := rt.Call("probe", snap)
	if err != nil {
		t.Fatalf("Call probe: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("probe errored: %v", r.Error)
	}
	if r.Value != float64(699) {
		t.Fatalf("expected 699, got %#v", r.Value)
	}
}

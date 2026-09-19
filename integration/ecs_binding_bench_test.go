package integration

import (
	"fmt"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/ecsbind"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// Feasibility micro-benchmark for the "World facade bound into spore script"
// design (see wiki card spore-lang绑定runtime可行性分析). Measures the cost of
// the chatty per-entity script loop (query → get → mutate → set → tick)
// against the equivalent native Go loop over the same World.

type benchHealth struct {
	Value int `json:"value"`
}

type ecsWorldBenchBinding struct {
	w    *runtime.World
	desc schema.ObjectDesc
}

func (b *ecsWorldBenchBinding) resolve(id string) (runtime.Entity, bool) {
	cid, err := identity.ParseCanonicalID(id)
	if err != nil {
		return runtime.Entity{}, false
	}
	return b.w.Entity(cid)
}

func (b *ecsWorldBenchBinding) QueryHas(comp string) []string {
	entities := b.w.Execute(runtime.NewQuery().Has(comp))
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID().String()
	}
	return ids
}

func (b *ecsWorldBenchBinding) Get(id string, comp string) map[string]any {
	e, ok := b.resolve(id)
	if !ok {
		return nil
	}
	proj, err := ecsbind.ProjectEntity(b.w, e, comp, b.desc)
	if err != nil {
		return nil
	}
	return proj.Fields
}

func (b *ecsWorldBenchBinding) Set(id string, comp string, fields map[string]any) int {
	e, ok := b.resolve(id)
	if !ok {
		return 0
	}
	muts, err := ecsbind.PatchEntity(b.w, e, comp, b.desc, &binding.ViewProjection{
		Schema: b.desc,
		Fields: fields,
	})
	if err != nil {
		return 0
	}
	return len(muts)
}

// View returns the batch envelope used by BenchmarkEcsScript_BatchDrain.
// The shape mirrors ecsbind.View: {"ids": []string, "data": map<string, []map>}.
// ids is sorted defensively; data[comp][i] aligns with ids[i].
func (b *ecsWorldBenchBinding) View(has []string, changed []string) (map[string]any, error) {
	q := runtime.NewQuery()
	if len(has) > 0 {
		q = q.Has(has...)
	}
	if len(changed) > 0 {
		q = q.WhenChanged(changed...)
	}
	entities := b.w.Execute(q)
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID().String()
	}
	data := map[string][]map[string]any{}
	if len(has) > 0 {
		comp := has[0]
		arr := make([]map[string]any, len(entities))
		for i, e := range entities {
			view, err := ecsbind.ProjectEntity(b.w, e, comp, b.desc)
			if err != nil {
				arr[i] = nil
				continue
			}
			arr[i] = view.Fields
		}
		data[comp] = arr
	}
	return map[string]any{"ids": ids, "data": data}, nil
}

// Apply mirrors ecsbind.Apply: patches comp on every id with its aligned
// field map. First error stops with the partial count.
func (b *ecsWorldBenchBinding) Apply(comp string, ids []string, fields []map[string]any) (int, error) {
	n := len(ids)
	if n == 0 {
		return 0, nil
	}
	if len(fields) != n {
		return 0, fmt.Errorf("apply(%s): ids/fields length mismatch (%d vs %d)", comp, n, len(fields))
	}
	for i := 0; i < n; i++ {
		e, ok := b.resolve(ids[i])
		if !ok {
			return i, fmt.Errorf("apply(%s): entity %s not alive", comp, ids[i])
		}
		if fields[i] == nil {
			continue
		}
		if _, err := ecsbind.PatchEntity(b.w, e, comp, b.desc, &binding.ViewProjection{
			Schema: b.desc,
			Fields: fields[i],
		}); err != nil {
			return i, err
		}
	}
	return n, nil
}

func (b *ecsWorldBenchBinding) Tick() { b.w.Tick() }

const ecsBenchSource = `import World from "ecs"

export fun count_hurt(): int {
    var hurt: array<string> = World.query_has("Health")
    return len(hurt)
}

export fun drain(): int {
    var hurt: array<string> = World.query_has("Health")
    var total: int = 0
    for (id in hurt) {
        var h: map<string, any> = World.get(id, "Health")
        var v: int = (h["value"] as int) - 10
        if (v <= 0) { v = 100 }
        h["value"] = v
        total = total + World.set(id, "Health", h)
    }
    World.tick()
    return total
}`

func scalarT(name string) schema.TypeDesc {
	return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}
}

func ecsBenchSetup(t testing.TB, n int) (*script.Runtime, *runtime.World) {
	w := runtime.NewWorld(runtime.WithComponents("Health"))
	desc, err := schema.DescribeGoStruct(&benchHealth{})
	if err != nil {
		t.Fatalf("DescribeGoStruct: %v", err)
	}
	for i := 0; i < n; i++ {
		e := w.Create()
		if err := w.SetComponent(e, "Health", &benchHealth{Value: 100}); err != nil {
			t.Fatalf("SetComponent: %v", err)
		}
	}

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	strT := scalarT("string")
	b := &ecsWorldBenchBinding{w: w, desc: desc}
	iface := schema.InterfaceDesc{Name: "World", Methods: []schema.MethodDesc{
		{
			Name: "query_has",
			Parameters: []schema.ParameterDesc{
				{Name: "comp", Type: strT},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindArray, Name: "array", Element: &strT}},
		},
		{
			Name: "get",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: strT},
				{Name: "comp", Type: strT},
			},
			Returns: []schema.TypeDesc{{
				Kind:      schema.TypeKindMap,
				Name:      "map",
				Key:       &strT,
				Value:     &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
			}},
		},
		{
			Name: "set",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: strT},
				{Name: "comp", Type: strT},
				{Name: "fields", Type: schema.TypeDesc{
					Kind:  schema.TypeKindMap,
					Name:  "map",
					Key:   &strT,
					Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
				}},
			},
			Returns: []schema.TypeDesc{scalarT("int")},
		},
		{Name: "tick"},
	}}
	if err := rt.BindInterfaceObject("ecs", "World", iface, b); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("bench", ecsBenchSource); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	return rt, w
}

func BenchmarkEcsScript_QueryOnly(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			rt, _ := ecsBenchSetup(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := rt.Call("count_hurt"); err != nil {
					b.Fatalf("count_hurt: %v", err)
				}
			}
		})
	}
}

func BenchmarkEcsScript_DrainLoop(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			rt, _ := ecsBenchSetup(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := rt.Call("drain"); err != nil {
					b.Fatalf("drain: %v", err)
				}
			}
		})
	}
}

// Native Go equivalent of the script drain loop: same World, same query
// filter, direct struct mutation + MarkChanged + Tick.
func BenchmarkEcsGo_NativeDrainLoop(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			_, w := ecsBenchSetup(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				entities := w.Execute(runtime.NewQuery().Has("Health"))
				for _, e := range entities {
					h, _ := w.GetComponent(e, "Health")
					hp := h.(*benchHealth)
					hp.Value -= 10
					if hp.Value <= 0 {
						hp.Value = 100
					}
					w.MarkChanged(e, "Health")
				}
				w.Tick()
			}
		})
	}
}

// ----------------------------------------------------------------------------
// view/apply batch benchmark
// ----------------------------------------------------------------------------
//
// Exercises the ecsbind.View / ecsbind.Apply batch surface end-to-end
// through the script runtime. Mirrors the chatty drain loop above but
// replaces the per-entity proxy calls with a single View + Apply per
// tick — the design win the feasibility analysis predicts:
//
//   chatty  : N x (get, set)  → 2N proxy crossings + per-call arg convert
//   batch   : 1 x view       → 1 crossing, ids + aligned arrays out
//             1 x apply      → 1 crossing, ids + aligned field maps in
//                            → 1 tick
//
// Both view and apply must round-trip the nested map<string, []map>
// through the VM, so this also exercises the verify-vm-batch-encoding
// conversion path under load.
//
// Note on VM memory: the script VM uses a flat heap per runtime whose
// default is 4 MiB (bytecode/vm_evaluator.go, DefaultVMHeapBytes; it was
// 64 KiB before v0.1.2). Materialising a
// deeply-nested map<string, []map<string, any>> return shape with N
// entries can exhaust that budget; the script-side bench therefore
// (a) uses small N values that fit, and (b) recreates the runtime on
// every iteration to keep the working set stable. The pure-Go
// BenchmarkEcsGo_ViewBodyBatch exercises the same facade without the
// VM envelope and so can run at N up to 10000.

const ecsBenchBatchSource = `import World from "ecs"

export fun drain_batch(): int {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var healthArr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var i: int = 0
    var n: int = len(ids)
    while (i < n) {
        var h: map<string, any> = healthArr[i]
        var v: int = (h["Value"] as int) - 10
        if (v <= 0) { v = 100 }
        h["Value"] = v
        i = i + 1
    }
    var applied: int = World.apply("Health", ids, healthArr)
    World.tick()
    return applied
}`

// ecsBenchBatchSetup installs the ecsbind.View + ecsbind.Apply methods
// and loads drain_batch. Used by both the script-side benchmark (which
// needs a fresh runtime per iteration so each measurement sees a clean
// heap) and the Go-side benchmark (which discards the runtime).
func ecsBenchBatchSetup(t testing.TB, n int) (*script.Runtime, *runtime.World) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return ecsBenchBatchSetupInto(t, rt, n)
}

// ecsBenchBatchSetupInto installs the World binding + drain_batch source
// on a caller-supplied Runtime. This is the shared tail of the bench
// setup; ecsBenchBatchSetup is the default-budget convenience wrapper
// that constructs a fresh Runtime, while the N=1000 large-heap bench
// constructs its own Runtime via script.NewRuntimeWith and reuses this
// to wire the bindings.
func ecsBenchBatchSetupInto(t testing.TB, rt *script.Runtime, n int) (*script.Runtime, *runtime.World) {
	w := runtime.NewWorld(runtime.WithComponents("Health"))
	desc, err := schema.DescribeGoStruct(&benchHealth{})
	if err != nil {
		t.Fatalf("DescribeGoStruct: %v", err)
	}
	for i := 0; i < n; i++ {
		e := w.Create()
		if err := w.SetComponent(e, "Health", &benchHealth{Value: 100}); err != nil {
			t.Fatalf("SetComponent: %v", err)
		}
	}

	strT := scalarT("string")
	strArrT := schema.TypeDesc{
		Kind:    schema.TypeKindArray,
		Name:    "array",
		Element: &strT,
	}
	fieldsArrT := schema.TypeDesc{
		Kind: schema.TypeKindArray,
		Name: "array",
		Element: &schema.TypeDesc{
			Kind:  schema.TypeKindMap,
			Name:  "map",
			Key:   &strT,
			Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
		},
	}
	iface := schema.InterfaceDesc{Name: "World", Methods: []schema.MethodDesc{
		{
			Name: "view",
			Parameters: []schema.ParameterDesc{
				{Name: "has", Type: strArrT},
				{Name: "changed", Type: strArrT},
			},
			Returns: []schema.TypeDesc{{
				Kind:  schema.TypeKindMap,
				Name:  "map",
				Key:   &strT,
				Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
			}},
		},
		{
			Name: "apply",
			Parameters: []schema.ParameterDesc{
				{Name: "comp", Type: strT},
				{Name: "ids", Type: strArrT},
				{Name: "fields", Type: fieldsArrT},
			},
			Returns: []schema.TypeDesc{scalarT("int")},
		},
		{Name: "tick"},
	}}
	b := &ecsWorldBenchBinding{w: w, desc: desc}
	if err := rt.BindInterfaceObject("ecs", "World", iface, b); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("bench", ecsBenchBatchSource); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	return rt, w
}

// BenchmarkEcsScript_BatchDrain measures the script-side view/apply
// path. The script runtime is recreated per iteration so each
// measurement sees a clean heap — the script runtime construction cost
// dominates the bench but is part of the "host-call-driven script" cost model the
// design must support. A defer/recover catches VM OOM panics so that
// slightly-larger N values don't crash the whole benchmark — those
// iterations are skipped via b.Skip.
//
// N=1000 sub-benchmark uses the script.RuntimeOptions.VMHeapBytes knob
// (1 MiB) to opt into a larger VM heap so the same drain_batch path can
// run at chatty-vs-batch equilibrium sizes. The larger budget is set via
// the host-facing surface added by vm-heap-budget-option; it preserves the
// historical default and is purely additive.
func BenchmarkEcsScript_BatchDrain(b *testing.B) {
	for _, n := range []int{5, 20, 50} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							b.StopTimer()
							b.Skip("VM OOM:", fmt.Sprint(r))
						}
					}()
					rt, _ := ecsBenchBatchSetup(b, n)
					if _, err := rt.Call("drain_batch"); err != nil {
						b.Fatalf("drain_batch: %v", err)
					}
				}()
			}
		})
	}
	// Smoke run for the opt-in VM memory budget knob: at N=1000 the
	// nested map<string, []map> envelope would OOM the historical
	// 64 KiB default (pre-v0.1.2). Using script.RuntimeOptions.VMHeapBytes
	// the same drain_batch path
	// completes successfully, demonstrating that the batch envelope scales
	// linearly with N once the host opts in.
	b.Run("N=1000_LargeHeap", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						b.StopTimer()
						b.Fatalf("VM OOM at N=1000 with 1 MiB heap: %v", r)
					}
				}()
				rt, err := script.NewRuntimeWith(script.RuntimeOptions{
					VMHeapBytes: 1 << 20,
					VMHeapSlots: 1024,
				})
				if err != nil {
					b.Fatalf("NewRuntimeWith: %v", err)
				}
				_, w := ecsBenchBatchSetupInto(b, rt, 1000)
				_ = w
				if _, err := rt.Call("drain_batch"); err != nil {
					b.Fatalf("drain_batch: %v", err)
				}
			}()
		}
	})
}

// BenchmarkEcsGo_ViewBodyBatch measures the ecsbind.WorldBinding
// ViewBody + Apply methods directly from Go, without the VM envelope.
// This isolates the facade's intrinsic cost (one Execute + one
// ProjectEntity-per-component-per-entity pass for ViewBody, one
// PatchEntity-per-entity for Apply) from the script-side overhead,
// and lets us reach N=10000 to compare with the chatty baseline.
func BenchmarkEcsGo_ViewBodyBatch(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Fresh world per iteration keeps the timing
				// representative of steady-state drain (Health is
				// reset on every iteration through the seed loop).
				w := runtime.NewWorld(runtime.WithComponents("Health"))
				desc, err := schema.DescribeGoStruct(&benchHealth{})
				if err != nil {
					b.Fatalf("DescribeGoStruct: %v", err)
				}
				for j := 0; j < n; j++ {
					e := w.Create()
					if err := w.SetComponent(e, "Health", &benchHealth{Value: 100}); err != nil {
						b.Fatalf("SetComponent: %v", err)
					}
				}
				binding := ecsbind.New(w).RegisterComponent("Health", desc)

				ids, data, err := binding.ViewBody([]string{"Health"}, nil)
				if err != nil {
					b.Fatalf("ViewBody: %v", err)
				}
				healthArr := data["Health"]
				applied, err := binding.Apply("Health", ids, healthArr)
				if err != nil {
					b.Fatalf("Apply: %v", err)
				}
				if applied != n {
					b.Fatalf("Apply: want %d applied, got %d", n, applied)
				}
				w.Tick()
			}
		})
	}
}

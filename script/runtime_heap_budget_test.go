package script_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/script"
)

// --- Runtime VM-memory-budget knob ---
//
// The default script.Runtime budget is DefaultVMHeapBytes (4 MiB since
// v0.1.2; 64 KiB before). Exceeding the budget after GC is a VM-internal
// invariant violation, and since the error-model convergence the evaluator
// boundary reports it as a structured runtime error with code
// "vm_internal_panic" instead of letting the VM's panic escape into the
// host — Call never panics. Budget-sensitive hosts must still set
// RuntimeOptions.VMHeapBytes / VMHeapSlots explicitly and treat
// vm_internal_panic as a hard engine-side failure. These tests pin the
// default's numeric value and exercise the explicit-budget path end-to-end
// through the public embedding surface.

// buildLargeNestedBatchEnvelope mirrors the ecsbind.World.View return shape
// ("ids": []string, "data": map<string, []map<string, any>>) at large N.
// N=4000 reliably overflows an explicit 64 KiB heap (byte budget: 8 KiB
// slots) and fits comfortably in the 8 MiB budget.
func buildLargeNestedBatchEnvelope(n int) map[string]any {
	ids := make([]string, n)
	entries := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		ids[i] = "e" + strings.Repeat("x", 8) + intToDec(i)
		entries[i] = map[string]any{
			"Value": 100,
			"Tag":   "tag-" + intToDec(i),
		}
	}
	return map[string]any{
		"ids":  ids,
		"data": map[string]any{"Health": entries},
	}
}

func intToDec(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestRuntime_VMInternalPanicConvertedToRuntimeError pins the boundary
// contract introduced by the #29 error-model convergence: a Runtime whose VM
// heap is too small for the deeply-nested ecsbind.World.View envelope makes
// the VM raise its internal "out of memory" panic, and Call must report that
// as a structured runtime error (Diagnostic.Code "vm_internal_panic") rather
// than letting the panic escape into the caller. Before the convergence this
// call panicked, which is exactly why sporemind needed a per-invoke recover.
//
// It sets an explicit small budget (the historical 64 KiB default) rather
// than relying on DefaultVMHeapBytes so the contract test stays
// deterministic regardless of the documented default's size.
func TestRuntime_VMInternalPanicConvertedToRuntimeError(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{VMHeapBytes: 65536, VMHeapSlots: 256})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("host", "ids", func() []string {
		return buildLargeNestedBatchEnvelope(4000)["ids"].([]string)
	}); err != nil {
		t.Fatalf("BindFunc host.ids: %v", err)
	}
	if err := rt.BindFunc("host", "data", func() map[string]any {
		return buildLargeNestedBatchEnvelope(4000)["data"].(map[string]any)
	}); err != nil {
		t.Fatalf("BindFunc host.data: %v", err)
	}
	const src = `import { ids, data } from "host"
export fun count(): int {
	var m: map<string, any> = {"ids": ids(), "data": data()}
	var arr: array<any> = (m["ids"] as array<any>)
	return len(arr)
}`
	if err := rt.LoadSource("budget_default", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("count")
	if err != nil {
		t.Fatalf("Call reported a host-level failure %v, want a structured runtime error", err)
	}
	if result.Error == nil {
		t.Fatalf("expected a structured runtime error for the VM OOM, got value %#v", result.Value)
	}
	if !script.IsRuntimeError(result.Error) {
		t.Fatalf("expected script.RuntimeError, got %T", result.Error)
	}
	if got := result.Error.Diagnostic.Code; got != "vm_internal_panic" {
		t.Fatalf("diagnostic code = %q, want vm_internal_panic", got)
	}
	if msg := result.Error.Diagnostic.Message; !strings.Contains(msg, "out of memory") {
		t.Fatalf("diagnostic message %q should keep the VM's panic text", msg)
	}
}

// TestRuntime_LargeBudgetMaterialisesNestedReturn confirms that opting into
// a larger heap via RuntimeOptions.VMHeapBytes lets the script runtime
// materialise the same nested envelope end-to-end.
func TestRuntime_LargeBudgetMaterialisesNestedReturn(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{
		VMHeapBytes: 8 << 20,
		VMHeapSlots: 1024,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWith: %v", err)
	}
	if err := rt.BindFunc("host", "ids", func() []string {
		return buildLargeNestedBatchEnvelope(4000)["ids"].([]string)
	}); err != nil {
		t.Fatalf("BindFunc host.ids: %v", err)
	}
	if err := rt.BindFunc("host", "data", func() map[string]any {
		return buildLargeNestedBatchEnvelope(4000)["data"].(map[string]any)
	}); err != nil {
		t.Fatalf("BindFunc host.data: %v", err)
	}
	const src = `import { ids, data } from "host"
export fun count(): int {
	var m: map<string, any> = {"ids": ids(), "data": data()}
	var arr: array<any> = (m["ids"] as array<any>)
	return len(arr)
}`
	if err := rt.LoadSource("budget_large", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("count")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("runtime error: %v", result.Error)
	}
	var got int
	if err := result.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != 4000 {
		t.Fatalf("expected count=4000, got %d", got)
	}
}

// TestRuntime_LargeBudgetPersistsAcrossReset asserts that the opt-in budget
// survives Reset() — a Runtime configured with VMHeapBytes must rebuild its
// evaluator with the same cap after a Reset, otherwise the host has to
// remember to re-apply the knob every reload.
func TestRuntime_LargeBudgetPersistsAcrossReset(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{
		VMHeapBytes: 8 << 20,
		VMHeapSlots: 1024,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWith: %v", err)
	}
	if err := rt.BindFunc("host", "ids", func() []string {
		return buildLargeNestedBatchEnvelope(4000)["ids"].([]string)
	}); err != nil {
		t.Fatalf("BindFunc host.ids: %v", err)
	}
	if err := rt.BindFunc("host", "data", func() map[string]any {
		return buildLargeNestedBatchEnvelope(4000)["data"].(map[string]any)
	}); err != nil {
		t.Fatalf("BindFunc host.data: %v", err)
	}
	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// After Reset, the large-budget Runtime must still materialise the
	// deeply-nested shape. If the budget regressed to 64 KiB on Reset this
	// call would OOM the same way the default-budget test above does.
	const src = `import { ids, data } from "host"
export fun count(): int {
	var m: map<string, any> = {"ids": ids(), "data": data()}
	var arr: array<any> = (m["ids"] as array<any>)
	return len(arr)
}`
	if err := rt.LoadSource("after_reset", src); err != nil {
		t.Fatalf("LoadSource after_reset: %v", err)
	}
	result, err := rt.Call("count")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("runtime error: %v", result.Error)
	}
	var got int
	if err := result.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != 4000 {
		t.Fatalf("expected post-reset count=4000, got %d", got)
	}
}

// TestRuntime_LargeBudgetPersistsAcrossClone asserts the same persistence
// for Clone(): the cloned Runtime must inherit the opt-in budget.
func TestRuntime_LargeBudgetPersistsAcrossClone(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{
		VMHeapBytes: 8 << 20,
		VMHeapSlots: 1024,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWith: %v", err)
	}
	if err := rt.BindFunc("host", "ids", func() []string {
		return buildLargeNestedBatchEnvelope(4000)["ids"].([]string)
	}); err != nil {
		t.Fatalf("BindFunc host.ids: %v", err)
	}
	if err := rt.BindFunc("host", "data", func() map[string]any {
		return buildLargeNestedBatchEnvelope(4000)["data"].(map[string]any)
	}); err != nil {
		t.Fatalf("BindFunc host.data: %v", err)
	}

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	const src = `import { ids, data } from "host"
export fun count(): int {
	var m: map<string, any> = {"ids": ids(), "data": data()}
	var arr: array<any> = (m["ids"] as array<any>)
	return len(arr)
}`
	if err := cloned.LoadSource("cloned", src); err != nil {
		t.Fatalf("LoadSource cloned: %v", err)
	}
	result, err := cloned.Call("count")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("runtime error: %v", result.Error)
	}
	var got int
	if err := result.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != 4000 {
		t.Fatalf("expected post-clone count=4000, got %d", got)
	}
}
package script_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/script"
)

// --- Runtime VM-memory-budget knob ---
//
// The default script.Runtime keeps the historical 64 KiB / 256-slot VM budget.
// Hosts that materialise deeply-nested values (e.g. ecsbind.World.View's
// map<string, []map<string, any>> envelope at large N) can opt into a larger
// budget via RuntimeOptions.VMHeapBytes / VMHeapSlots without touching the
// default. These tests pin the default behaviour and exercise the opt-in
// path end-to-end through the public embedding surface.

// buildLargeNestedBatchEnvelope mirrors the ecsbind.World.View return shape
// ("ids": []string, "data": map<string, []map<string, any>>) at large N.
// N=4000 reliably overflows the default 64 KiB heap and fits comfortably in
// the 1 MiB opt-in budget.
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

// TestRuntime_SmallBudgetPanicsOnLargeNestedReturn pins the panic-on-OOM
// contract at the Runtime level: a Runtime whose VM heap is too small for
// the deeply-nested ecsbind.World.View envelope panics during Call. It
// sets an explicit small budget (the historical 64 KiB default) rather
// than relying on DefaultVMHeapBytes so the contract test stays
// deterministic regardless of the documented default's size.
func TestRuntime_SmallBudgetPanicsOnLargeNestedReturn(t *testing.T) {
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

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected VM OOM panic on small-budget Runtime, got none")
		}
	}()
	result, err := rt.Call("count")
	if err != nil {
		// Defensive: anyToVMValue currently propagates VM panics as panics,
		// not as script errors; this branch keeps the test honest if that
		// ever changes.
		t.Fatalf("unexpected error before OOM: %v (result=%+v)", err, result)
	}
}

// TestRuntime_LargeBudgetMaterialisesNestedReturn confirms that opting into
// a larger heap via RuntimeOptions.VMHeapBytes lets the script runtime
// materialise the same nested envelope end-to-end.
func TestRuntime_LargeBudgetMaterialisesNestedReturn(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{
		VMHeapBytes: 1 << 20,
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
		VMHeapBytes: 1 << 20,
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
		VMHeapBytes: 1 << 20,
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
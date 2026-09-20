package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
)

// --- VM memory budget knob ---
//
// The default VM is 4 MiB of flat heap plus 256 call-stack slots; hosts
// can opt into a different size via NewVMEvaluatorWith (and embedders via
// RuntimeOptions.VMHeapBytes). These tests pin the default and exercise
// the opt-in path, including the raw codec's panic-on-OOM behaviour (the
// evaluator boundary converts that panic into a structured vm_internal_panic
// error for every real caller — see doc.go and panic_recovery_test.go).

func TestVMEvaluator_DefaultVMBudgetPinned(t *testing.T) {
	eval := NewVMEvaluator()
	heap, slots := eval.VMHeapBudget()
	if heap != DefaultVMHeapBytes {
		t.Fatalf("default heap bytes: want %d, got %d", DefaultVMHeapBytes, heap)
	}
	if slots != DefaultVMHeapSlots {
		t.Fatalf("default call-stack slots: want %d, got %d", DefaultVMHeapSlots, slots)
	}
	if DefaultVMHeapBytes != 4<<20 || DefaultVMHeapSlots != 256 {
		t.Fatalf("documented defaults regressed: heap=%d slots=%d", DefaultVMHeapBytes, DefaultVMHeapSlots)
	}
}

func TestVMEvaluator_NewWithZeroFallsBackToDefaults(t *testing.T) {
	eval := NewVMEvaluatorWith(0, 0)
	heap, slots := eval.VMHeapBudget()
	if heap != DefaultVMHeapBytes || slots != DefaultVMHeapSlots {
		t.Fatalf("zero budget should resolve to defaults, got heap=%d slots=%d", heap, slots)
	}
}

func TestVMEvaluator_NewWithNegativeFallsBackToDefaults(t *testing.T) {
	// Defensive: a host that subtracts by mistake shouldn't get a 0-byte VM.
	eval := NewVMEvaluatorWith(-1, -1)
	heap, slots := eval.VMHeapBudget()
	if heap != DefaultVMHeapBytes || slots != DefaultVMHeapSlots {
		t.Fatalf("negative budget should resolve to defaults, got heap=%d slots=%d", heap, slots)
	}
}

func TestVMEvaluator_NewWithAppliesConfiguredBudget(t *testing.T) {
	eval := NewVMEvaluatorWith(1<<20, 512)
	heap, slots := eval.VMHeapBudget()
	if heap != 1<<20 {
		t.Fatalf("configured heap bytes: want %d, got %d", 1<<20, heap)
	}
	if slots != 512 {
		t.Fatalf("configured call-stack slots: want %d, got %d", 512, slots)
	}
}

// buildLargeNestedBatchEnvelope returns a map<string, any> shaped like the
// ecsbind.World.View return: top-level keys "ids" and "data", where data
// carries one component named "Health" whose value is []map<string, any>
// with N entries each holding two fields.
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

// TestVMEvaluator_SmallBudgetOOMsOnLargeNestedEnvelope pins the codec-level
// panic-on-OOM behaviour: a VM whose heap is too small to materialise the
// deeply-nested ecsbind.World.View envelope panics with "out of memory"
// after GC. It calls the raw conversion helper (anyToVMValue) directly, below
// the evaluator boundary, which is why the panic is observable here at all;
// any call through an evaluator entry point reports the same condition as a
// structured vm_internal_panic RuntimeError (see
// TestVMEvaluator_OOMConvertedToStructuredErrorAndEvaluationContinues). It
// uses an explicit small budget (the historical 64 KiB default) rather than
// DefaultVMHeapBytes so the contract test stays deterministic regardless of
// the documented default's size.
//
// N is chosen so that even with a (somewhat) aggressive VM garbage collector
// the test reliably OOMs; smaller N values may succeed on a small budget
// and are exercised by the historical unit tests in
// vm_evaluator_boundary_test.go.
func TestVMEvaluator_SmallBudgetOOMsOnLargeNestedEnvelope(t *testing.T) {
	eval := NewVMEvaluatorWith(65536, 256)
	payload := buildLargeNestedBatchEnvelope(4000)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected VM OOM panic at N=4000 with a 65536-byte heap, got none")
		}
		msg := toStringAny(r)
		if !strings.Contains(strings.ToLower(msg), "out of memory") {
			t.Fatalf("expected OOM panic mentioning memory, got: %v", r)
		}
	}()
	if _, err := anyToVMValue(eval.vm_, payload); err != nil {
		// anyToVMValue currently lets VM panics propagate as panics, not
		// errors; this branch is a defensive sanity check.
		t.Fatalf("unexpected error before OOM: %v", err)
	}
}

// toStringAny is a tiny helper used only by the OOM-message sanity check
// above; it avoids importing fmt just to render an interface{}.
func toStringAny(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "<non-string panic>"
}

// TestVMEvaluator_LargeBudgetMaterialisesNestedEnvelope confirms the
// opt-in knob relaxes small budgets: the same payload that panics the
// small-budget evaluator round-trips successfully with NewVMEvaluatorWith.
func TestVMEvaluator_LargeBudgetMaterialisesNestedEnvelope(t *testing.T) {
	eval := NewVMEvaluatorWith(1<<20, 1024)
	v, err := anyToVMValue(eval.vm_, buildLargeNestedBatchEnvelope(200))
	if err != nil {
		t.Fatalf("expected large-budget materialisation success, got %v", err)
	}
	root, ok := vmValueToAny(eval.vm_, v).(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any root, got %T", vmValueToAny(eval.vm_, v))
	}
	ids, ok := root["ids"].([]any)
	if !ok {
		t.Fatalf("expected ids []any, got %T", root["ids"])
	}
	if len(ids) != 200 {
		t.Fatalf("expected 200 ids, got %d", len(ids))
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map[string]any, got %T", root["data"])
	}
	health, ok := data["Health"].([]any)
	if !ok {
		t.Fatalf("expected data.Health []any, got %T", data["Health"])
	}
	if len(health) != 200 {
		t.Fatalf("expected 200 health entries, got %d", len(health))
	}
	first := health[0].(map[string]any)
	if first["Value"].(int) != 100 {
		t.Fatalf("expected first entry Value=100, got %v", first["Value"])
	}
}

// TestVMEvaluator_LargeBudgetRoundTripThroughScriptFunction exercises the
// same shape end-to-end: the host returns a nested batch envelope to script,
// script reads it back. With a large heap this completes; the default budget
// would OOM at N=200 and panic inside Evaluate.
func TestVMEvaluator_LargeBudgetRoundTripThroughScriptFunction(t *testing.T) {
	eval := NewVMEvaluatorWith(1<<20, 1024)
	prog, err := frontend.ParseModuleForTest(`fun count(v: any): int {
		var m: map<string, any> = (v as map<string, any>)
		var ids: array<any> = (m["ids"] as array<any>)
		return len(ids)
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("count", binding.InvocationStageUnary, []any{buildLargeNestedBatchEnvelope(200)})
	if err != nil {
		t.Fatalf("evaluate with large heap: %v", err)
	}
	if result.(int) != 200 {
		t.Fatalf("expected script to report 200, got %v", result)
	}
}

// TestVMEvaluator_LargeBudgetSurvivesRecompile asserts that the configured
// budget is honoured on every CompileLoweredProgram pass, not just the first
// construction. CompileLoweredProgram rebuilds the underlying VM internally
// (it copies registered native structs/class metadata into a fresh heap);
// a budget stored only on the Go struct but not re-read at recompile time
// would silently regress here.
func TestVMEvaluator_LargeBudgetSurvivesRecompile(t *testing.T) {
	eval := NewVMEvaluatorWith(1<<20, 1024)

	prog1, err := frontend.ParseModuleForTest(`fun first(): int { return 1 }`)
	if err != nil {
		t.Fatalf("parse 1: %v", err)
	}
	if err := eval.CompileProgram(prog1); err != nil {
		t.Fatalf("compile 1: %v", err)
	}
	heap, slots := eval.VMHeapBudget()
	if heap != 1<<20 || slots != 1024 {
		t.Fatalf("budget lost after first compile: got heap=%d slots=%d", heap, slots)
	}

	prog2, err := frontend.ParseModuleForTest(`fun second(n: int): int { return n + n }`)
	if err != nil {
		t.Fatalf("parse 2: %v", err)
	}
	if err := eval.CompileProgram(prog2); err != nil {
		t.Fatalf("compile 2: %v", err)
	}
	heap, slots = eval.VMHeapBudget()
	if heap != 1<<20 || slots != 1024 {
		t.Fatalf("budget lost after second compile: got heap=%d slots=%d", heap, slots)
	}

	// And the recompiled VM can still materialise the large nested shape.
	v, err := anyToVMValue(eval.vm_, buildLargeNestedBatchEnvelope(200))
	if err != nil {
		t.Fatalf("post-recompile materialisation: %v", err)
	}
	root := vmValueToAny(eval.vm_, v).(map[string]any)
	if len(root["ids"].([]any)) != 200 {
		t.Fatalf("post-recompile ids: want 200, got %d", len(root["ids"].([]any)))
	}
}

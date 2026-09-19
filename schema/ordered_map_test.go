package schema

import "testing"

func TestOrderedMap_SetAndEntriesPreserveInsertionOrder(t *testing.T) {
	m := NewOrderedMap[string, int]()
	if got := m.Set("alpha", 1); got != SetInserted {
		t.Fatalf("expected inserted result, got %q", got)
	}
	m.Set("beta", 2)
	m.Set("gamma", 3)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != "alpha" || entries[1].Key != "beta" || entries[2].Key != "gamma" {
		t.Fatalf("unexpected entry order: %#v", entries)
	}
}

func TestOrderedMap_SetExistingKeyReplacesWithoutReordering(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("alpha", 1)
	m.Set("beta", 2)

	if got := m.Set("alpha", 10); got != SetReplaced {
		t.Fatalf("expected replaced result, got %q", got)
	}
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "alpha" || entries[0].Value != 10 {
		t.Fatalf("unexpected first entry after replace: %#v", entries[0])
	}
	if entries[1].Key != "beta" || entries[1].Value != 2 {
		t.Fatalf("unexpected second entry after replace: %#v", entries[1])
	}
}

func TestOrderedMap_DeleteReportsRemovedAndMissing(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("alpha", 1)

	if got := m.Delete("alpha"); got != DeleteRemoved {
		t.Fatalf("expected removed result, got %q", got)
	}
	if got := m.Delete("alpha"); got != DeleteMissing {
		t.Fatalf("expected missing result, got %q", got)
	}
	if m.Len() != 0 {
		t.Fatalf("expected map to be empty, got len %d", m.Len())
	}
}

func TestOrderedMap_DeleteAndReinsertMovesKeyToTail(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("alpha", 1)
	m.Set("beta", 2)
	m.Set("gamma", 3)

	m.Delete("beta")
	if got := m.Set("beta", 20); got != SetInserted {
		t.Fatalf("expected reinsert to be inserted, got %q", got)
	}

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != "alpha" || entries[1].Key != "gamma" || entries[2].Key != "beta" {
		t.Fatalf("unexpected entry order after reinsert: %#v", entries)
	}
}

func TestOrderedMap_EntriesReturnsCopy(t *testing.T) {
	m := NewOrderedMap[string, int]()
	m.Set("alpha", 1)
	m.Set("beta", 2)

	entries := m.Entries()
	entries[0].Value = 99
	entries[1].Key = "changed"

	again := m.Entries()
	if again[0].Key != "alpha" || again[0].Value != 1 {
		t.Fatalf("expected first entry to remain unchanged, got %#v", again[0])
	}
	if again[1].Key != "beta" || again[1].Value != 2 {
		t.Fatalf("expected second entry to remain unchanged, got %#v", again[1])
	}
}

func TestOrderedMap_GetHasAndLenStayConsistent(t *testing.T) {
	m := NewOrderedMap[string, int]()
	if m.Len() != 0 {
		t.Fatalf("expected empty map len, got %d", m.Len())
	}
	if m.Has("alpha") {
		t.Fatal("expected alpha to be missing")
	}
	if _, ok := m.Get("alpha"); ok {
		t.Fatal("expected get on missing key to fail")
	}

	if got := m.Set("alpha", 1); got != SetInserted {
		t.Fatalf("expected inserted result, got %q", got)
	}
	if got := m.Set("beta", 2); got != SetInserted {
		t.Fatalf("expected inserted result, got %q", got)
	}
	if m.Len() != 2 {
		t.Fatalf("expected len 2, got %d", m.Len())
	}
	if !m.Has("alpha") || !m.Has("beta") {
		t.Fatal("expected both keys to exist")
	}
	if value, ok := m.Get("beta"); !ok || value != 2 {
		t.Fatalf("unexpected get result: value=%d ok=%v", value, ok)
	}
}

func TestOrderedMap_NilReceiverBehavesLikeEmptyMap(t *testing.T) {
	var m *OrderedMap[string, int]
	if m.Len() != 0 {
		t.Fatalf("expected nil map len 0, got %d", m.Len())
	}
	if m.Has("alpha") {
		t.Fatal("expected nil map to report missing key")
	}
	if value, ok := m.Get("alpha"); ok || value != 0 {
		t.Fatalf("expected nil map get to return zero false, got value=%d ok=%v", value, ok)
	}
	if got := m.Delete("alpha"); got != DeleteMissing {
		t.Fatalf("expected nil map delete to report missing, got %q", got)
	}
	if entries := m.Entries(); entries != nil {
		t.Fatalf("expected nil map entries to be nil, got %#v", entries)
	}
}

func TestOrderedMapFromMap_SortsStringKeys(t *testing.T) {
	src := map[string]int{
		"gamma": 3,
		"alpha": 1,
		"beta":  2,
	}

	ordered := OrderedMapFromMap(src)
	entries := ordered.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != "alpha" || entries[1].Key != "beta" || entries[2].Key != "gamma" {
		t.Fatalf("unexpected sorted entry order: %#v", entries)
	}
}

func TestOrderedMapFromMap_SortsIntKeys(t *testing.T) {
	src := map[int]string{
		20: "twenty",
		5:  "five",
		11: "eleven",
	}

	ordered := OrderedMapFromMap(src)
	entries := ordered.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != 5 || entries[1].Key != 11 || entries[2].Key != 20 {
		t.Fatalf("unexpected sorted int key order: %#v", entries)
	}
}

func TestOrderedMapFromMap_NilMapReturnsEmptyOrderedMap(t *testing.T) {
	ordered := OrderedMapFromMap(map[string]int(nil))
	if ordered == nil {
		t.Fatal("expected non-nil ordered map")
	}
	if ordered.Len() != 0 {
		t.Fatalf("expected empty ordered map, got len %d", ordered.Len())
	}
	if entries := ordered.Entries(); entries != nil {
		t.Fatalf("expected nil entries for empty map, got %#v", entries)
	}
}

func TestOrderedMapFromMap_PreservesOrderedMapInsertionSemanticsAfterAdaptation(t *testing.T) {
	src := map[string]int{
		"gamma": 3,
		"alpha": 1,
	}

	ordered := OrderedMapFromMap(src)
	if got := ordered.Set("beta", 2); got != SetInserted {
		t.Fatalf("expected inserted result, got %q", got)
	}
	entries := ordered.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != "alpha" || entries[1].Key != "gamma" || entries[2].Key != "beta" {
		t.Fatalf("unexpected order after append to adapted map: %#v", entries)
	}
}

func TestOrderedMapFromMap_RepeatedConstructionIsStable(t *testing.T) {
	src := map[string]int{
		"gamma": 3,
		"alpha": 1,
		"beta":  2,
	}

	first := OrderedMapFromMap(src).Entries()
	second := OrderedMapFromMap(src).Entries()
	if len(first) != len(second) {
		t.Fatalf("expected same length, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("expected stable entries, got %#v and %#v", first, second)
		}
	}
}

// --- SortedOrderedMap contract tests ---

func TestSortedOrderedMap_InsertionOrderIsSorted(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("gamma", 3)
	m.Set("alpha", 1)
	m.Set("beta", 2)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Keys must appear in sorted (alpha, beta, gamma) order, NOT insertion order
	if entries[0].Key != "alpha" || entries[1].Key != "beta" || entries[2].Key != "gamma" {
		t.Fatalf("unexpected entry order — expected sorted, got: %#v", entries)
	}
}

func TestSortedOrderedMap_OverwritePreservesSortedPosition(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	m.Set("gamma", 3)
	m.Set("beta", 2)

	if got := m.Set("alpha", 10); got != SetReplaced {
		t.Fatalf("expected replaced result, got %q", got)
	}

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// alpha stays in its sorted position, not moved to end
	if entries[0].Key != "alpha" || entries[0].Value != 10 {
		t.Fatalf("unexpected first entry after replace: %#v", entries[0])
	}
	if entries[1].Key != "beta" || entries[2].Key != "gamma" {
		t.Fatalf("unexpected order after replace: %#v", entries)
	}
}

func TestSortedOrderedMap_DeleteAndReinsertGoesToSortedPosition(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	m.Set("gamma", 3)
	m.Set("beta", 2)

	m.Delete("beta")
	if got := m.Set("beta", 20); got != SetInserted {
		t.Fatalf("expected inserted result, got %q", got)
	}

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// beta goes back to its sorted position (between alpha and gamma),
	// NOT to the tail as in insertion-order OrderedMap
	if entries[0].Key != "alpha" || entries[1].Key != "beta" || entries[2].Key != "gamma" {
		t.Fatalf("unexpected order after reinsert — expected sorted: %#v", entries)
	}
	if entries[1].Value != 20 {
		t.Fatalf("unexpected value for beta: %d", entries[1].Value)
	}
}

func TestSortedOrderedMap_EntriesIsCanonicalView(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("gamma", 3)
	m.Set("alpha", 1)
	m.Set("beta", 2)

	entries := m.Entries()
	// Same Entry shape as OrderedMap
	if entries[0].Key != "alpha" || entries[0].Value != 1 {
		t.Fatalf("unexpected entry shape: %#v", entries[0])
	}
}

func TestSortedOrderedMap_SetResultSemantics(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	if got := m.Set("alpha", 1); got != SetInserted {
		t.Fatalf("expected inserted, got %q", got)
	}
	if got := m.Set("alpha", 10); got != SetReplaced {
		t.Fatalf("expected replaced, got %q", got)
	}
}

func TestSortedOrderedMap_DeleteResultSemantics(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	if got := m.Delete("alpha"); got != DeleteRemoved {
		t.Fatalf("expected removed, got %q", got)
	}
	if got := m.Delete("alpha"); got != DeleteMissing {
		t.Fatalf("expected missing, got %q", got)
	}
	if m.Len() != 0 {
		t.Fatalf("expected empty map, got len %d", m.Len())
	}
}

func TestSortedOrderedMap_DefaultInsertionOrderNotAffected(t *testing.T) {
	// Adding SortedOrderedMap does NOT change OrderedMap's insertion-order contract
	m := NewOrderedMap[string, int]()
	m.Set("gamma", 3)
	m.Set("alpha", 1)
	m.Set("beta", 2)

	entries := m.Entries()
	// OrderedMap still preserves insertion order: gamma, alpha, beta
	if entries[0].Key != "gamma" || entries[1].Key != "alpha" || entries[2].Key != "beta" {
		t.Fatalf("OrderedMap insertion order broken: %#v", entries)
	}
}

func TestSortedOrderedMap_NilReceiverBehavesLikeEmptyMap(t *testing.T) {
	var m *SortedOrderedMap[string, int]
	if m.Len() != 0 {
		t.Fatalf("expected nil map len 0, got %d", m.Len())
	}
	if m.Has("alpha") {
		t.Fatal("expected nil map to report missing key")
	}
	if value, ok := m.Get("alpha"); ok || value != 0 {
		t.Fatalf("expected nil map get to return zero false, got value=%d ok=%v", value, ok)
	}
	if got := m.Delete("alpha"); got != DeleteMissing {
		t.Fatalf("expected nil map delete to report missing, got %q", got)
	}
	if entries := m.Entries(); entries != nil {
		t.Fatalf("expected nil map entries to be nil, got %#v", entries)
	}
}

func TestSortedOrderedMap_HasMissingKey(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	if m.Has("missing") {
		t.Fatal("expected Has(missing) to be false")
	}
	if !m.Has("alpha") {
		t.Fatal("expected Has(alpha) to be true")
	}
}

func TestSortedOrderedMap_GetMissingKey(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	if value, ok := m.Get("missing"); ok || value != 0 {
		t.Fatalf("expected Get(missing) to return zero false, got value=%d ok=%v", value, ok)
	}
	if value, ok := m.Get("alpha"); !ok || value != 1 {
		t.Fatalf("expected Get(alpha) to return 1 true, got value=%d ok=%v", value, ok)
	}
}

func TestSortedOrderedMap_IntKeysSorted(t *testing.T) {
	m := NewSortedOrderedMap[int, string](NaturalOrder[int]())
	m.Set(30, "thirty")
	m.Set(10, "ten")
	m.Set(20, "twenty")

	entries := m.Entries()
	if entries[0].Key != 10 || entries[1].Key != 20 || entries[2].Key != 30 {
		t.Fatalf("unexpected int key order: %#v", entries)
	}
}

func TestSortedOrderedMap_CustomComparator(t *testing.T) {
	// Reverse order comparator
	reverseCmp := func(a, b string) int {
		if a < b {
			return 1
		}
		if a > b {
			return -1
		}
		return 0
	}
	m := NewSortedOrderedMap[string, int](reverseCmp)
	m.Set("alpha", 1)
	m.Set("gamma", 3)
	m.Set("beta", 2)

	entries := m.Entries()
	// Reverse sorted: gamma, beta, alpha
	if entries[0].Key != "gamma" || entries[1].Key != "beta" || entries[2].Key != "alpha" {
		t.Fatalf("unexpected reverse order: %#v", entries)
	}
}

func TestSortedOrderedMap_EntriesReturnsCopy(t *testing.T) {
	m := NewSortedOrderedMap[string, int](NaturalOrder[string]())
	m.Set("alpha", 1)
	m.Set("beta", 2)

	entries := m.Entries()
	entries[0].Value = 99
	entries[1].Key = "changed"

	again := m.Entries()
	if again[0].Key != "alpha" || again[0].Value != 1 {
		t.Fatalf("expected first entry to remain unchanged, got %#v", again[0])
	}
	if again[1].Key != "beta" || again[1].Value != 2 {
		t.Fatalf("expected second entry to remain unchanged, got %#v", again[1])
	}
}

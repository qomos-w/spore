package transport

import (
	"reflect"
	"testing"
)

type entryStruct struct {
	Key   string
	Value int
}

type unexportedEntry struct {
	key   string
	value int
}

func TestEntryFromProjectedValue(t *testing.T) {
	// map[string]any entry
	k, v, ok := entryFromProjectedValue(map[string]any{"Key": "a", "Value": 1})
	if !ok || k != "a" || v != 1 {
		t.Fatalf("expected map entry to work, got k=%v v=%v ok=%v", k, v, ok)
	}

	// map[string]any missing Key
	_, _, ok = entryFromProjectedValue(map[string]any{"Value": 1})
	if ok {
		t.Fatal("expected missing Key to fail")
	}

	// map[string]any missing Value
	_, _, ok = entryFromProjectedValue(map[string]any{"Key": "a"})
	if ok {
		t.Fatal("expected missing Value to fail")
	}

	// struct entry
	k, v, ok = entryFromProjectedValue(entryStruct{Key: "b", Value: 2})
	if !ok || k != "b" || v != 2 {
		t.Fatalf("expected struct entry to work, got k=%v v=%v ok=%v", k, v, ok)
	}

	// pointer to struct entry
	k, v, ok = entryFromProjectedValue(&entryStruct{Key: "c", Value: 3})
	if !ok || k != "c" || v != 3 {
		t.Fatalf("expected pointer struct entry to work, got k=%v v=%v ok=%v", k, v, ok)
	}

	// nil pointer
	_, _, ok = entryFromProjectedValue((*entryStruct)(nil))
	if ok {
		t.Fatal("expected nil pointer to fail")
	}

	// non-struct
	_, _, ok = entryFromProjectedValue(42)
	if ok {
		t.Fatal("expected non-struct to fail")
	}

	// struct without Key/Value fields
	_, _, ok = entryFromProjectedValue(struct{ X int }{X: 1})
	if ok {
		t.Fatal("expected struct without Key/Value to fail")
	}

	// struct with unexported Key/Value fields
	_, _, ok = entryFromProjectedValue(unexportedEntry{key: "d", value: 4})
	if ok {
		t.Fatal("expected unexported fields to fail")
	}
}

func TestLooksLikeEntrySequence(t *testing.T) {
	// invalid value
	if looksLikeEntrySequence(reflect.Value{}) {
		t.Fatal("expected invalid value to be false")
	}

	// non-slice
	if looksLikeEntrySequence(reflect.ValueOf(42)) {
		t.Fatal("expected int to be false")
	}

	// empty slice
	if !looksLikeEntrySequence(reflect.ValueOf([]entryStruct{})) {
		t.Fatal("expected empty slice to be true")
	}

	// slice of valid entries
	if !looksLikeEntrySequence(reflect.ValueOf([]entryStruct{{Key: "a", Value: 1}})) {
		t.Fatal("expected slice of entries to be true")
	}

	// slice of invalid entries
	if looksLikeEntrySequence(reflect.ValueOf([]int{1, 2, 3})) {
		t.Fatal("expected slice of ints to be false")
	}

	// array of valid entries
	if !looksLikeEntrySequence(reflect.ValueOf([1]entryStruct{{Key: "a", Value: 1}})) {
		t.Fatal("expected array of entries to be true")
	}
}

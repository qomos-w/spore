package transport

import (
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

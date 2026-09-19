package bytes_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/bytes"
)

func invokeBytes(t *testing.T, sb *binding.ScriptBinding, callable string, args ...any) any {
	t.Helper()
	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: callable, Stage: binding.InvocationStageUnary, Args: args})
	if err != nil {
		t.Fatalf("%s invoke: %v", callable, err)
	}
	if outcome.Payload == nil {
		t.Fatalf("%s: expected payload", callable)
	}
	return outcome.Payload.Value
}

func newBytesBinding(t *testing.T) *binding.ScriptBinding {
	t.Helper()
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return sb
}

func TestBytesModule_Slice(t *testing.T) {
	sb := newBytesBinding(t)
	got, ok := invokeBytes(t, sb, "bytes.slice", []byte("hello"), 1, 4).([]byte)
	if !ok || string(got) != "ell" {
		t.Fatalf("expected ell, got %v", got)
	}
	// Full-range and empty slices.
	if v, _ := invokeBytes(t, sb, "bytes.slice", []byte("hi"), 0, 2).([]byte); string(v) != "hi" {
		t.Fatalf("full-range slice: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.slice", []byte("hi"), 1, 1).([]byte); len(v) != 0 {
		t.Fatalf("empty slice: %q", v)
	}
	// Out-of-range and inverted bounds must surface as invocation error
	// descriptors (outcome.Error), not Go-level Invoke errors.
	for _, args := range [][]any{
		{[]byte("hi"), -1, 1},
		{[]byte("hi"), 0, 3},
		{[]byte("hi"), 2, 1},
	} {
		outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "bytes.slice", Stage: binding.InvocationStageUnary, Args: args})
		if err != nil {
			t.Fatalf("unexpected invoke error for args %v: %v", args, err)
		}
		if outcome.Result.Error == nil {
			t.Fatalf("expected error descriptor for args %v", args)
		}
	}
}

func TestBytesModule_CompareContainsPrefixSuffix(t *testing.T) {
	sb := newBytesBinding(t)
	if got := invokeBytes(t, sb, "bytes.compare", []byte("abc"), []byte("abd")); got != -1 {
		t.Fatalf("compare abc/abd = %v, want -1", got)
	}
	if got := invokeBytes(t, sb, "bytes.compare", []byte("abd"), []byte("abc")); got != 1 {
		t.Fatalf("compare abd/abc = %v, want 1", got)
	}
	if got := invokeBytes(t, sb, "bytes.compare", []byte("abc"), []byte("abc")); got != 0 {
		t.Fatalf("compare equal = %v, want 0", got)
	}
	if got := invokeBytes(t, sb, "bytes.contains", []byte("seafood"), []byte("foo")); got != true {
		t.Fatalf("contains foo = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.contains", []byte("seafood"), []byte("bar")); got != false {
		t.Fatalf("contains bar = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasPrefix", []byte("gopher"), []byte("go")); got != true {
		t.Fatalf("hasPrefix go = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasPrefix", []byte("gopher"), []byte("x")); got != false {
		t.Fatalf("hasPrefix x = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasSuffix", []byte("gopher"), []byte("er")); got != true {
		t.Fatalf("hasSuffix er = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasSuffix", []byte("gopher"), []byte("go")); got != false {
		t.Fatalf("hasSuffix go = %v", got)
	}
}

func TestBytesModule_RepeatReplace(t *testing.T) {
	sb := newBytesBinding(t)
	if v, _ := invokeBytes(t, sb, "bytes.repeat", []byte("na"), 4).([]byte); string(v) != "nananana" {
		t.Fatalf("repeat: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.repeat", []byte("na"), 0).([]byte); len(v) != 0 {
		t.Fatalf("repeat 0: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.replace", []byte("a-b-c"), []byte("-"), []byte("+"), 1).([]byte); string(v) != "a+b-c" {
		t.Fatalf("replace n=1: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.replace", []byte("a-b-c"), []byte("-"), []byte("+"), -1).([]byte); string(v) != "a+b+c" {
		t.Fatalf("replace all: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.replace", []byte("abc"), []byte("x"), []byte("y"), -1).([]byte); string(v) != "abc" {
		t.Fatalf("replace absent: %q", v)
	}
}

func TestBytesModule_CaseTrim(t *testing.T) {
	sb := newBytesBinding(t)
	if v, _ := invokeBytes(t, sb, "bytes.toLower", []byte("GoPhEr!")).([]byte); string(v) != "gopher!" {
		t.Fatalf("toLower: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.toUpper", []byte("GoPhEr!")).([]byte); string(v) != "GOPHER!" {
		t.Fatalf("toUpper: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.trimSpace", []byte("  hi \t\n")).([]byte); string(v) != "hi" {
		t.Fatalf("trimSpace: %q", v)
	}
	if v, _ := invokeBytes(t, sb, "bytes.trimSpace", []byte("hi")).([]byte); string(v) != "hi" {
		t.Fatalf("trimSpace no-op: %q", v)
	}
}

func TestBytesModule_EmptyAndNilInputs(t *testing.T) {
	sb := newBytesBinding(t)
	if got := invokeBytes(t, sb, "bytes.length", []byte(nil)); got != 0 {
		t.Fatalf("length nil = %v", got)
	}
	if v, _ := invokeBytes(t, sb, "bytes.concat", []byte(nil), []byte(nil)).([]byte); len(v) != 0 {
		t.Fatalf("concat nils: %q", v)
	}
	if got := invokeBytes(t, sb, "bytes.contains", []byte("x"), []byte(nil)); got != true {
		t.Fatalf("contains empty = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.index", []byte("x"), []byte(nil)); got != 0 {
		t.Fatalf("index empty = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasPrefix", []byte("x"), []byte(nil)); got != true {
		t.Fatalf("hasPrefix empty = %v", got)
	}
	if got := invokeBytes(t, sb, "bytes.hasSuffix", []byte("x"), []byte(nil)); got != true {
		t.Fatalf("hasSuffix empty = %v", got)
	}
}

func TestBytesModule_SliceAndConcatRoundTrip(t *testing.T) {
	sb := newBytesBinding(t)
	orig := []byte("abcdef")
	part, _ := invokeBytes(t, sb, "bytes.slice", orig, 2, 5).([]byte)
	joined, _ := invokeBytes(t, sb, "bytes.concat", orig[:2], part).([]byte)
	if string(joined) != "abcde" {
		t.Fatalf("round-trip: %q", joined)
	}
	// Large concat stays deterministic.
	big := make([]byte, 1024)
	for i := range big {
		big[i] = byte(i % 251)
	}
	if v, _ := invokeBytes(t, sb, "bytes.concat", big, big).([]byte); len(v) != 2048 {
		t.Fatalf("large concat len = %d", len(v))
	}
}

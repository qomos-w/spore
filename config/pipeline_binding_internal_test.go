package config

import (
	"reflect"
	"testing"

	"github.com/qomos-w/spore/binding"
)

func TestPipelineValueToAnyKinds(t *testing.T) {
	var diags []Diagnostic
	if v := valueToAny(Value{Kind: ValueInt, IntVal: 7}, &diags); v != int64(7) {
		t.Fatalf("int: %v", v)
	}
	if v := valueToAny(Value{Kind: ValueFloat, FloatVal: 1.5}, &diags); v != 1.5 {
		t.Fatalf("float: %v", v)
	}
	if v := valueToAny(Value{Kind: ValueString, StrVal: "s"}, &diags); v != "s" {
		t.Fatalf("string: %v", v)
	}
	if v := valueToAny(Value{Kind: ValueBool, BoolVal: true}, &diags); v != true {
		t.Fatalf("bool: %v", v)
	}
	if v := valueToAny(Value{Kind: ValueNull}, &diags); v != nil {
		t.Fatalf("null: %v", v)
	}
	arr := valueToAny(Value{Kind: ValueArray, Elements: []Value{{Kind: ValueInt, IntVal: 1}, {Kind: ValueString, StrVal: "a"}}}, &diags)
	if !reflect.DeepEqual(arr, []any{int64(1), "a"}) {
		t.Fatalf("array: %#v", arr)
	}
	m := valueToAny(Value{Kind: ValueMap, Entries: []KeyValue{{Key: "k", Value: Value{Kind: ValueInt, IntVal: 2}}}}, &diags)
	if !reflect.DeepEqual(m, map[string]any{"k": int64(2)}) {
		t.Fatalf("map: %#v", m)
	}
	st := valueToAny(Value{Kind: ValueStruct, TypeName: "P", Fields: []KeyValue{{Key: "f", Value: Value{Kind: ValueInt, IntVal: 3}}}}, &diags)
	if !reflect.DeepEqual(st, map[string]any{"__struct__": "P", "f": int64(3)}) {
		t.Fatalf("struct: %#v", st)
	}
	ref := valueToAny(Value{Kind: ValueRef, StrVal: "$a.b"}, &diags)
	if pr, ok := ref.(binding.PipelineRef); !ok || pr.Expr != "$a.b" {
		t.Fatalf("ref: %#v", ref)
	}
	before := len(diags)
	if v := valueToAny(Value{Kind: ValueKind(99)}, &diags); v != nil {
		t.Fatal("unsupported kind should be nil")
	}
	if len(diags) != before+1 {
		t.Fatal("unsupported kind should append a diagnostic")
	}
}

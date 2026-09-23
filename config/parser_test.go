package config

import (
	"testing"
)

func TestParse_Scalars(t *testing.T) {
	cfg, err := Parse(`
int_val: 42
float_val: 3.14
str_val: "hello"
bool_val: true
null_val: null
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Values) != 5 {
		t.Fatalf("expected 5 values, got %d", len(cfg.Values))
	}
	assertValue(t, cfg, "int_val", ValueInt, func(v Value) {
		if v.IntVal != 42 {
			t.Errorf("expected 42, got %d", v.IntVal)
		}
	})
	assertValue(t, cfg, "float_val", ValueFloat, func(v Value) {
		if v.FloatVal != 3.14 {
			t.Errorf("expected 3.14, got %v", v.FloatVal)
		}
	})
	assertValue(t, cfg, "str_val", ValueString, func(v Value) {
		if v.StrVal != "hello" {
			t.Errorf("expected hello, got %q", v.StrVal)
		}
	})
	assertValue(t, cfg, "bool_val", ValueBool, func(v Value) {
		if !v.BoolVal {
			t.Errorf("expected true")
		}
	})
	assertValue(t, cfg, "null_val", ValueNull, func(v Value) {})
}

func TestParse_Array(t *testing.T) {
	cfg, err := Parse(`items: [1, 2, 3]`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("items")
	if !ok || v.Kind != ValueArray || len(v.Elements) != 3 {
		t.Fatalf("unexpected: %+v %v", v, ok)
	}
	if v.Elements[0].IntVal != 1 || v.Elements[2].IntVal != 3 {
		t.Errorf("unexpected array values: %+v", v.Elements)
	}
}

func TestParse_Map(t *testing.T) {
	cfg, err := Parse(`m: {a: 1, b: "two"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("m")
	if !ok || v.Kind != ValueMap || len(v.Entries) != 2 {
		t.Fatalf("unexpected: %+v %v", v, ok)
	}
	if v.Entries[0].Key != "a" || v.Entries[0].Value.IntVal != 1 {
		t.Errorf("unexpected first entry: %+v", v.Entries[0])
	}
	if v.Entries[1].Key != "b" || v.Entries[1].Value.StrVal != "two" {
		t.Errorf("unexpected second entry: %+v", v.Entries[1])
	}
}

func TestParse_BlockSyntax(t *testing.T) {
	cfg, err := Parse(`
database {
    host: "localhost"
    port: 5432
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("database")
	if !ok || v.Kind != ValueMap {
		t.Fatalf("unexpected: %+v %v", v, ok)
	}
	host, _ := findEntry(v.Entries, "host")
	port, _ := findEntry(v.Entries, "port")
	if host.StrVal != "localhost" {
		t.Errorf("expected host localhost, got %q", host.StrVal)
	}
	if port.IntVal != 5432 {
		t.Errorf("expected port 5432, got %d", port.IntVal)
	}
}

func TestParse_StructLiteral(t *testing.T) {
	cfg, err := Parse(`
struct Point {
    x: double
    y: double
}

origin: Point{x: 0.0, y: 0.0}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Types) != 1 || cfg.Types[0].Name != "Point" {
		t.Fatalf("unexpected types: %+v", cfg.Types)
	}
	if len(cfg.Types[0].Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(cfg.Types[0].Fields))
	}
	v, ok := cfg.Get("origin")
	if !ok || v.Kind != ValueStruct || v.TypeName != "Point" {
		t.Fatalf("unexpected: %+v %v", v, ok)
	}
	x, _ := findEntry(v.Fields, "x")
	if x.FloatVal != 0.0 {
		t.Errorf("expected x=0.0, got %v", x.FloatVal)
	}
}

func TestParse_NestedBlocks(t *testing.T) {
	cfg, err := Parse(`
db {
    pool {
        min: 5
        max: 20
    }
    name: "spore"
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	db, ok := cfg.Get("db")
	if !ok || db.Kind != ValueMap {
		t.Fatalf("unexpected db: %+v", db)
	}
	pool, _ := findEntry(db.Entries, "pool")
	if pool.Kind != ValueMap {
		t.Fatalf("unexpected pool: %+v", pool)
	}
	min, _ := findEntry(pool.Entries, "min")
	max, _ := findEntry(pool.Entries, "max")
	if min.IntVal != 5 || max.IntVal != 20 {
		t.Errorf("unexpected pool values: min=%d, max=%d", min.IntVal, max.IntVal)
	}
}

func TestParse_ArrayMultiline(t *testing.T) {
	cfg, err := Parse(`
features: [
    "alpha"
    "beta"
    "gamma"
]
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("features")
	if !ok || v.Kind != ValueArray || len(v.Elements) != 3 {
		t.Fatalf("unexpected: %+v", v)
	}
	if v.Elements[0].StrVal != "alpha" || v.Elements[2].StrVal != "gamma" {
		t.Errorf("unexpected values: %+v", v.Elements)
	}
}

func TestParse_MapQuotedKeys(t *testing.T) {
	cfg, err := Parse(`m: {"key-a": 1, "key-b": 2}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("m")
	if !ok || v.Kind != ValueMap {
		t.Fatalf("unexpected: %+v", v)
	}
	if v.Entries[0].Key != "key-a" || v.Entries[1].Key != "key-b" {
		t.Errorf("unexpected keys: %+v", v.Entries)
	}
}

func TestParse_StringEscapes(t *testing.T) {
	cfg, err := Parse(`msg: "hello\nworld"`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	v, ok := cfg.Get("msg")
	if !ok || v.StrVal != "hello\nworld" {
		t.Errorf("expected escaped newline, got %q", v.StrVal)
	}
}

func TestParse_StructDefWithGenerics(t *testing.T) {
	cfg, err := Parse(`
struct Config {
    tags: array<string>
    counts: map<string, int>
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(cfg.Types))
	}
	td := cfg.Types[0]
	if len(td.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(td.Fields))
	}
	if td.Fields[0].TypeText != "array<string>" {
		t.Errorf("expected array<string>, got %q", td.Fields[0].TypeText)
	}
	if td.Fields[1].TypeText != "map<string, int>" {
		t.Errorf("expected map<string, int>, got %q", td.Fields[1].TypeText)
	}
}

func TestParse_Comments(t *testing.T) {
	cfg, err := Parse(`
// This is a comment
x: 1
/* block comment */
y: 2
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(cfg.Values))
	}
}

func TestParse_Empty(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Values) != 0 || len(cfg.Types) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestParse_ErrorUnterminatedString(t *testing.T) {
	_, err := Parse(`x: "unterminated`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
}

func TestParse_ErrorExpectedColon(t *testing.T) {
	_, err := Parse(`x 1`)
	if err == nil {
		t.Fatal("expected error for missing colon")
	}
}

func TestConfig_Keys(t *testing.T) {
	cfg, _ := Parse(`
b: 2
a: 1
c: 3
`)
	keys := cfg.Keys()
	if len(keys) != 3 || keys[0] != "b" || keys[1] != "a" || keys[2] != "c" {
		t.Errorf("expected ordered keys [b a c], got %v", keys)
	}
}

func TestConfig_Get(t *testing.T) {
	cfg, _ := Parse(`x: 42`)
	_, ok := cfg.Get("missing")
	if ok {
		t.Error("expected false for missing key")
	}
}

func assertValue(t *testing.T, cfg *Config, key string, kind ValueKind, check func(Value)) {
	t.Helper()
	v, ok := cfg.Get(key)
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	if v.Kind != kind {
		t.Fatalf("key %q: expected kind %d, got %d", key, kind, v.Kind)
	}
	check(v)
}

func findEntry(entries []KeyValue, key string) (Value, bool) {
	for _, kv := range entries {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return Value{}, false
}

func TestParse_NegativeNumbers(t *testing.T) {
	cfg, err := Parse(`
neg_int: -42
neg_float: -3.14
int64_min: -9223372036854775808
neg_in_array: [1, -2.5]
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	assertValue(t, cfg, "neg_int", ValueInt, func(v Value) {
		if v.IntVal != -42 {
			t.Errorf("expected -42, got %d", v.IntVal)
		}
	})
	assertValue(t, cfg, "neg_float", ValueFloat, func(v Value) {
		if v.FloatVal != -3.14 {
			t.Errorf("expected -3.14, got %v", v.FloatVal)
		}
	})
	assertValue(t, cfg, "int64_min", ValueInt, func(v Value) {
		if v.IntVal != -9223372036854775808 {
			t.Errorf("expected int64 min, got %d", v.IntVal)
		}
	})
	v, ok := cfg.Get("neg_in_array")
	if !ok || len(v.Elements) != 2 || v.Elements[1].FloatVal != -2.5 {
		t.Errorf("expected [1, -2.5], got %+v", v)
	}
}

func TestParse_NegativeNumberErrors(t *testing.T) {
	if _, err := Parse("x: -"); err == nil {
		t.Error("lone '-' must be a parse error")
	}
	if _, err := Parse("x: -a"); err == nil {
		t.Error("'-' not followed by a digit must be a parse error")
	}
	if _, err := Parse("x: -9223372036854775809"); err == nil {
		t.Error("literal below int64 min must be a parse error")
	}
}

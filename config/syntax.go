package config

import (
	"fmt"
	"strings"
)

// ValueKind classifies a configuration value.
type ValueKind int

const (
	ValueInt ValueKind = iota
	ValueFloat
	ValueString
	ValueBool
	ValueNull
	ValueArray
	ValueMap
	ValueStruct
	ValueRef
)

// Value represents a parsed configuration value with position info.
type Value struct {
	Kind     ValueKind
	IntVal   int64
	FloatVal float64
	StrVal   string
	BoolVal  bool
	Elements []Value   // ValueArray
	Entries  []KeyValue // ValueMap
	TypeName string     // ValueStruct
	Fields   []KeyValue // ValueStruct
	Raw      string
	Line     int
}

// KeyValue is a named entry in a config map or struct literal.
type KeyValue struct {
	Key   string
	Value Value
	Line  int
}

// TypeDef represents an inline struct type definition.
type TypeDef struct {
	Name   string
	Fields []FieldDef
	Line   int
}

// FieldDef describes a field in a type definition.
type FieldDef struct {
	Name     string
	TypeText string
	Line     int
}

// Config represents a parsed configuration file.
type Config struct {
	Values    []KeyValue
	Types     []TypeDef
	Pipelines []PipelineAST
}

// Get retrieves a top-level value by key.
func (c *Config) Get(key string) (Value, bool) {
	for _, kv := range c.Values {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return Value{}, false
}

// Keys returns all top-level keys in order.
func (c *Config) Keys() []string {
	keys := make([]string, len(c.Values))
	for i, kv := range c.Values {
		keys[i] = kv.Key
	}
	return keys
}

// PipelineRef is a host-visible marker for a reference expression value.
type PipelineRef struct {
	Expr string
}

// GoValue converts a config Value to a Go value (any).
func (v Value) GoValue() any {
	switch v.Kind {
	case ValueInt:
		return v.IntVal
	case ValueFloat:
		return v.FloatVal
	case ValueString:
		return v.StrVal
	case ValueBool:
		return v.BoolVal
	case ValueNull:
		return nil
	case ValueArray:
		arr := make([]any, len(v.Elements))
		for i, e := range v.Elements {
			arr[i] = e.GoValue()
		}
		return arr
	case ValueMap:
		m := make(map[string]any, len(v.Entries))
		for _, kv := range v.Entries {
			m[kv.Key] = kv.Value.GoValue()
		}
		return m
	case ValueStruct:
		m := make(map[string]any, len(v.Fields)+1)
		m["__struct__"] = v.TypeName
		for _, kv := range v.Fields {
			m[kv.Key] = kv.Value.GoValue()
		}
		return m
	case ValueRef:
		return PipelineRef{Expr: v.StrVal}
	default:
		return nil
	}
}

func (v Value) String() string {
	switch v.Kind {
	case ValueInt:
		return fmt.Sprintf("%d", v.IntVal)
	case ValueFloat:
		return fmt.Sprintf("%v", v.FloatVal)
	case ValueString:
		return fmt.Sprintf("%q", v.StrVal)
	case ValueBool:
		return fmt.Sprintf("%v", v.BoolVal)
	case ValueNull:
		return "null"
	case ValueArray:
		elems := make([]string, len(v.Elements))
		for i, e := range v.Elements {
			elems[i] = e.String()
		}
		return "[" + strings.Join(elems, ", ") + "]"
	case ValueMap:
		entries := make([]string, len(v.Entries))
		for i, kv := range v.Entries {
			entries[i] = fmt.Sprintf("%s: %s", kv.Key, kv.Value.String())
		}
		return "{" + strings.Join(entries, ", ") + "}"
	case ValueStruct:
		fields := make([]string, len(v.Fields))
		for i, kv := range v.Fields {
			fields[i] = fmt.Sprintf("%s: %s", kv.Key, kv.Value.String())
		}
		return fmt.Sprintf("%s{%s}", v.TypeName, strings.Join(fields, ", "))
	case ValueRef:
		return fmt.Sprintf("ref(%s)", v.StrVal)
	default:
		return "<unknown>"
	}
}

// parseError creates a formatted parse error.
type parseError struct {
	Message string
	Line    int
	Col     int
}

func (e *parseError) Error() string {
	return fmt.Sprintf("config parse error at line %d: %s", e.Line, e.Message)
}

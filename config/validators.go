package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// ValidateNoUnknownFields returns a validator that checks config keys against
// a Go struct's field names. Unknown keys produce warnings with spell suggestions.
func ValidateNoUnknownFields() Validator {
	return func(cfg *Config, target any) []Diagnostic {
		if target == nil {
			return nil
		}
		desc, err := schema.DescribeGoStruct(target)
		if err != nil {
			return nil
		}
		known := make(map[string]bool, len(desc.Fields))
		for _, f := range desc.Fields {
			known[strings.ToLower(f.Name)] = true
		}
		var diags []Diagnostic
		for _, kv := range cfg.Values {
			lower := strings.ToLower(kv.Key)
			if !known[lower] {
				suggestions := suggestSimilar(lower, known)
				d := Diagnostic{
					Code:        "config_unknown_field",
					Category:    "validate",
					Severity:    "warning",
					Message:     fmt.Sprintf("unknown config key %q", kv.Key),
					Hint:        "remove the key or check spelling",
					Field:       kv.Key,
					Line:        kv.Line,
					Actual:      kv.Key,
					Suggestions: suggestions,
				}
				if len(suggestions) > 0 {
					d.Hint = fmt.Sprintf("did you mean %s?", strings.Join(suggestions, " or "))
				}
				diags = append(diags, d)
			}
		}
		return diags
	}
}

// ValidateRequiredFields returns a validator that checks all Go struct fields
// have corresponding config entries. Missing fields produce errors.
func ValidateRequiredFields() Validator {
	return func(cfg *Config, target any) []Diagnostic {
		if target == nil {
			return nil
		}
		desc, err := schema.DescribeGoStruct(target)
		if err != nil {
			return nil
		}
		cfgKeys := make(map[string]bool, len(cfg.Values))
		for _, kv := range cfg.Values {
			cfgKeys[strings.ToLower(kv.Key)] = true
		}
		var diags []Diagnostic
		for _, f := range desc.Fields {
			if !cfgKeys[strings.ToLower(f.Name)] {
				diags = append(diags, Diagnostic{
					Code:     "config_missing_field",
					Category: "validate",
					Severity: "error",
					Message:  fmt.Sprintf("missing required field %q", f.Name),
					Hint:     fmt.Sprintf("add %q to the config", f.Name),
					Field:    f.Name,
					Expected: f.Type.String(),
				})
			}
		}
		return diags
	}
}

// ValidateTypes returns a validator that checks config value types against
// Go struct field types. Mismatches produce errors with expected/actual.
func ValidateTypes() Validator {
	return func(cfg *Config, target any) []Diagnostic {
		if target == nil {
			return nil
		}
		rv := reflect.ValueOf(target)
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			return nil
		}
		rt := rv.Type()
		cfgMap := make(map[string]KeyValue, len(cfg.Values))
		for _, kv := range cfg.Values {
			cfgMap[strings.ToLower(kv.Key)] = kv
		}

		var diags []Diagnostic
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			kv, ok := cfgMap[strings.ToLower(field.Name)]
			if !ok {
				continue
			}
			expected := typeKindName(field.Type)
			actual := valueKindName(kv.Value)
			if !typeCompatible(expected, actual, field.Type, kv.Value) {
				diags = append(diags, Diagnostic{
					Code:     "config_type_mismatch",
					Category: "validate",
					Severity: "error",
					Message:  fmt.Sprintf("field %q: expected %s, got %s", field.Name, expected, actual),
					Hint:     fmt.Sprintf("change the value to type %s", expected),
					Field:    field.Name,
					Line:     kv.Line,
					Expected: expected,
					Actual:   actual,
				})
			}
		}
		return diags
	}
}

func typeKindName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "double"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Slice:
		return "array"
	case reflect.Map:
		return "map"
	case reflect.Struct:
		return "struct"
	case reflect.Pointer:
		return typeKindName(t.Elem())
	default:
		return t.Kind().String()
	}
}

func valueKindName(v Value) string {
	switch v.Kind {
	case ValueInt:
		return "int"
	case ValueFloat:
		return "double"
	case ValueString:
		return "string"
	case ValueBool:
		return "bool"
	case ValueNull:
		return "null"
	case ValueArray:
		return "array"
	case ValueMap:
		return "map"
	case ValueStruct:
		return "struct:" + v.TypeName
	default:
		return "unknown"
	}
}

func typeCompatible(expected string, actual string, goType reflect.Type, v Value) bool {
	// null is compatible with pointer/interface types
	if actual == "null" {
		return goType.Kind() == reflect.Pointer || goType.Kind() == reflect.Interface
	}
	// int is compatible with any integer Go type
	if expected == "int" && actual == "int" {
		return true
	}
	if expected == "double" && (actual == "double" || actual == "int") {
		return true
	}
	// map/struct config values are compatible with Go struct type
	if goType.Kind() == reflect.Struct && (actual == "map" || strings.HasPrefix(actual, "struct:")) {
		return true
	}
	return expected == actual
}

// suggestSimilar returns known field names that are close to the given key.
func suggestSimilar(key string, known map[string]bool) []string {
	type scored struct {
		name  string
		score int
	}
	var candidates []scored
	for k := range known {
		d := levenshtein(key, k)
		if d <= 3 && d < len(key) {
			candidates = append(candidates, scored{k, d})
		}
		if strings.HasPrefix(k, key) || strings.HasPrefix(key, k) {
			candidates = append(candidates, scored{k, 0})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Deduplicate and sort by score
	seen := make(map[string]bool)
	var best []string
	for _, c := range candidates {
		if seen[c.name] {
			continue
		}
		seen[c.name] = true
		best = append(best, c.name)
		if len(best) >= 3 {
			break
		}
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

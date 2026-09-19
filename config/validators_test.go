package config

import (
	"strings"
	"testing"
)

type validatorHost struct {
	Host    string
	Port    int
	Debug   bool
	Timeout float64
	Tags    []string
	Meta    map[string]string
}

func TestValidateNoUnknownFields(t *testing.T) {
	cfg, err := Parse("host: \"localhost\"\nport: 5432\nhotz: \"oops\"")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateNoUnknownFields()
	diags := v(cfg, &validatorHost{})
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for unknown field")
	}
	d := diags[0]
	if d.Code != "config_unknown_field" {
		t.Errorf("expected code config_unknown_field, got %q", d.Code)
	}
	if d.Field != "hotz" {
		t.Errorf("expected field hotz, got %q", d.Field)
	}
	// Should suggest "host" via Levenshtein
	found := false
	for _, s := range d.Suggestions {
		if s == "host" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected suggestion 'host', got %v", d.Suggestions)
	}
}

func TestValidateNoUnknownFields_AllKnown(t *testing.T) {
	cfg, err := Parse("host: \"localhost\"\nport: 5432")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateNoUnknownFields()
	diags := v(cfg, &validatorHost{})
	if len(diags) != 0 {
		t.Fatalf("expected no diags for known fields, got %+v", diags)
	}
}

func TestValidateNoUnknownFields_NilTarget(t *testing.T) {
	cfg, err := Parse("host: \"localhost\"")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateNoUnknownFields()
	diags := v(cfg, nil)
	if len(diags) != 0 {
		t.Fatalf("expected no diags for nil target, got %+v", diags)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	cfg, err := Parse("host: \"localhost\"")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateRequiredFields()
	diags := v(cfg, &validatorHost{})
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for missing fields")
	}
	// Should report Port, Debug, Timeout at minimum
	fields := make(map[string]bool)
	for _, d := range diags {
		if d.Code != "config_missing_field" {
			t.Errorf("expected code config_missing_field, got %q", d.Code)
		}
		fields[d.Field] = true
	}
	if !fields["Port"] {
		t.Error("expected missing Port")
	}
	if !fields["Debug"] {
		t.Error("expected missing Debug")
	}
}

func TestValidateRequiredFields_AllPresent(t *testing.T) {
	input := strings.Join([]string{
		"host: \"localhost\"",
		"port: 5432",
		"debug: true",
		"timeout: 30.0",
		"tags: [\"a\"]",
		"meta: {\"k\": \"v\"}",
	}, "\n")
	cfg, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateRequiredFields()
	diags := v(cfg, &validatorHost{})
	if len(diags) != 0 {
		t.Fatalf("expected no diags when all fields present, got %+v", diags)
	}
}

func TestValidateTypes_IntOk(t *testing.T) {
	cfg, err := Parse("host: \"localhost\"\nport: 5432\ndebug: false\ntimeout: 1.0")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateTypes()
	diags := v(cfg, &validatorHost{})
	// Should have no type mismatches
	for _, d := range diags {
		if d.Code == "config_type_mismatch" {
			t.Errorf("unexpected type mismatch: %+v", d)
		}
	}
}

func TestValidateTypes_Mismatch(t *testing.T) {
	cfg, err := Parse("port: \"not_a_number\"")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateTypes()
	diags := v(cfg, &validatorHost{})
	if len(diags) == 0 {
		t.Fatal("expected type mismatch diag")
	}
	d := diags[0]
	if d.Code != "config_type_mismatch" {
		t.Errorf("expected config_type_mismatch, got %q", d.Code)
	}
	if d.Expected != "int" {
		t.Errorf("expected 'int', got %q", d.Expected)
	}
	if d.Actual != "string" {
		t.Errorf("expected 'string', got %q", d.Actual)
	}
}

func TestValidateTypes_DoubleAcceptsInt(t *testing.T) {
	cfg, err := Parse("timeout: 30")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateTypes()
	diags := v(cfg, &validatorHost{})
	for _, d := range diags {
		if d.Code == "config_type_mismatch" && d.Field == "timeout" {
			t.Error("int should be compatible with float64 field")
		}
	}
}

func TestValidateTypes_NilTarget(t *testing.T) {
	cfg, err := Parse("host: 42")
	if err != nil {
		t.Fatal(err)
	}
	v := ValidateTypes()
	diags := v(cfg, nil)
	if len(diags) != 0 {
		t.Fatalf("expected no diags for nil target, got %+v", diags)
	}
}

func TestSuggestSimilar(t *testing.T) {
	known := map[string]bool{
		"host":    true,
		"port":    true,
		"debug":   true,
		"timeout": true,
	}
	sugs := suggestSimilar("hot", known)
	found := false
	for _, s := range sugs {
		if s == "host" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'host' suggestion for 'hot', got %v", sugs)
	}
}

func TestSuggestSimilar_NoMatch(t *testing.T) {
	known := map[string]bool{"host": true}
	sugs := suggestSimilar("zzzzzzzz", known)
	if len(sugs) != 0 {
		t.Errorf("expected no suggestions for very different key, got %v", sugs)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct{ a, b string; want int }{
		{"", "", 0},
		{"abc", "abc", 0},
		{"host", "host", 0},
		{"hot", "host", 1},
		{"abc", "", 3},
		{"", "xyz", 3},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

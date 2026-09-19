package config_test

import (
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

func TestConfig_AllDiagnosticCodesRegistered(t *testing.T) {
	codes := []string{
		// parser corrections
		"config_parse_error",
		"config_int_overflow",
		"config_float_overflow",
		"config_markdown_block",
		"config_json_quotes",
		"config_json_trailing_comma",
		"config_equals_colon",
		"config_json_brackets",
		"config_unknown_field",
		"config_missing_field",
		"config_type_mismatch",
		"config_format_rejected",
		"config_too_many_corrections",
		// pipeline diagnostic codes
		"pipeline_duplicate_step",
		"pipeline_missing_invoke",
		"pipeline_invalid_invoke_ref",
		"pipeline_unknown_dependency",
		"pipeline_dependency_cycle",
		"pipeline_invalid_timeout",
		// pipeline registry-aware diagnostic codes
		"pipeline_unknown_schema",
		"pipeline_unknown_callable",
		"pipeline_input_mismatch",
	}
	for _, code := range codes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("expected config diagnostic code %q to be registered", code)
		}
		if info.Category != diagnostics.CategorySchema {
			t.Fatalf("code %q registered under category %q, want CategorySchema", code, info.Category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", code)
		}
	}
}

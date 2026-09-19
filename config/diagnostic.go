package config

import (
	"github.com/qomos-w/spore/diagnostics"
)

// Diagnostic is a structured configuration diagnostic for LLM/tooling consumption.
type Diagnostic struct {
	Code        string
	Category    string   // "correct" or "validate"
	Severity    string   // "error", "warning", "info"
	Message     string
	Hint        string
	Line        int
	Col         int
	Field       string
	Expected    string
	Actual      string
	Suggestions []string
}

func (d Diagnostic) String() string {
	s := d.Severity + ": " + d.Message
	if d.Code != "" {
		s += " [" + d.Code + "]"
	}
	if d.Hint != "" {
		s += " — " + d.Hint
	}
	return s
}

func init() {
	cat := diagnostics.CategorySchema
	for _, info := range []diagnostics.CodeInfo{
		{Code: "config_parse_error", Category: cat, Description: "config parse error", Hint: "check syntax near the reported line"},
		{Code: "config_markdown_block", Category: cat, Description: "config wrapped in markdown code block", Hint: "auto-stripped markdown fencing"},
		{Code: "config_json_quotes", Category: cat, Description: "JSON-style quoted map keys", Hint: "auto-removed quotes from map keys; use bare identifiers"},
		{Code: "config_json_trailing_comma", Category: cat, Description: "JSON-style trailing comma before } or ]", Hint: "auto-removed trailing commas"},
		{Code: "config_equals_colon", Category: cat, Description: "key = value instead of key: value", Hint: "auto-normalized '=' to ':'"},
		{Code: "config_json_brackets", Category: cat, Description: "JSON-style outer braces", Hint: "auto-stripped outer { } braces"},
		{Code: "config_unknown_field", Category: cat, Description: "config key not found in target struct", Hint: "remove the unknown key or check spelling"},
		{Code: "config_missing_field", Category: cat, Description: "required struct field missing from config", Hint: "add the missing key to config"},
		{Code: "config_type_mismatch", Category: cat, Description: "config value type does not match struct field type", Hint: "use the expected type shown in the diagnostic"},
		{Code: "config_format_rejected", Category: cat, Description: "input format not recognized as config or JSON", Hint: "use Spore config, JSON, or key=value syntax"},
		{Code: "config_too_many_corrections", Category: cat, Description: "too many corrections applied, input may be too far from valid syntax", Hint: "rewrite the config in Spore config or JSON syntax and retry"},
		// Pipeline diagnostic codes
		{Code: "pipeline_duplicate_step", Category: cat, Description: "duplicate step name within a pipeline", Hint: "rename the step or remove the duplicate"},
		{Code: "pipeline_missing_invoke", Category: cat, Description: "pipeline step is missing required invoke field", Hint: "add invoke: \"<schemaID>:<callableName>\" to the step"},
		{Code: "pipeline_invalid_invoke_ref", Category: cat, Description: "pipeline step invoke ref has invalid format", Hint: "use format \"<schemaID>:<callableName>\" or \"tool:<toolName>\""},
		{Code: "pipeline_unknown_dependency", Category: cat, Description: "pipeline step depends on a non-existent step", Hint: "ensure the referenced step exists in the pipeline"},
		{Code: "pipeline_dependency_cycle", Category: cat, Description: "pipeline contains a circular dependency", Hint: "remove circular dependencies so steps form a DAG"},
		{Code: "pipeline_invalid_timeout", Category: cat, Description: "pipeline step timeout is not a valid duration", Hint: "use a valid Go duration string like \"10m\", \"30s\", \"1h30m\""},
		// Pipeline registry-aware diagnostic codes
		{Code: "pipeline_unknown_schema", Category: cat, Description: "pipeline step references a schema that does not exist in the registry", Hint: "ensure the capability schema is registered"},
		{Code: "pipeline_unknown_callable", Category: cat, Description: "pipeline step references a callable that does not exist in the schema", Hint: "ensure the callable is declared in the capability schema"},
		{Code: "pipeline_input_mismatch", Category: cat, Description: "pipeline step input keys do not match callable parameters", Hint: "check input keys against the callable's parameter schema"},
	} {
		diagnostics.RegisterCode(info)
	}
}

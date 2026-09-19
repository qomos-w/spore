package config

import (
	"strings"
	"testing"
)

func TestStripMarkdownCodeBlock(t *testing.T) {
	c := StripMarkdownCodeBlock()
	input := "```json\nhost: \"localhost\"\nport: 5432\n```"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_markdown_block" {
		t.Fatalf("expected markdown diag, got %+v", diags)
	}
	if strings.TrimSpace(cleaned) != strings.TrimSpace("host: \"localhost\"\nport: 5432") {
		t.Errorf("unexpected cleaned: %q", cleaned)
	}
}

func TestStripMarkdownCodeBlock_NoFencing(t *testing.T) {
	c := StripMarkdownCodeBlock()
	input := "host: \"localhost\"\nport: 5432"
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("should not modify non-fenced input")
	}
}

func TestStripJSONQuotes(t *testing.T) {
	c := StripJSONQuotes()
	input := "{\n  \"host\": \"localhost\",\n  \"port\": 5432\n}"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_json_quotes" {
		t.Fatalf("expected json_quotes diag, got %+v", diags)
	}
	if strings.Contains(cleaned, `"host"`) {
		t.Errorf("should have removed quotes from host: %q", cleaned)
	}
	if !strings.Contains(cleaned, `host:`) {
		t.Errorf("expected bare host key: %q", cleaned)
	}
}

func TestStripJSONQuotes_BareKeys(t *testing.T) {
	c := StripJSONQuotes()
	input := "host: \"localhost\"\nport: 5432"
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags for already-bare keys, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("should not modify bare keys")
	}
}

func TestStripJSONTrailingCommas(t *testing.T) {
	c := StripJSONTrailingCommas()
	input := "host: \"localhost\",\nport: 5432,"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_json_trailing_comma" {
		t.Fatalf("expected trailing_comma diag, got %+v", diags)
	}
	if strings.Contains(cleaned, ",\n") {
		t.Errorf("should have removed trailing commas: %q", cleaned)
	}
}

func TestStripJSONTrailingCommas_ArrayTrailingComma(t *testing.T) {
	c := StripJSONTrailingCommas()
	input := "items: [1, 2, 3,]"
	cleaned, diags := c(input)
	if len(diags) != 1 {
		t.Fatalf("expected diag, got %+v", diags)
	}
	if strings.Contains(cleaned, ",]") {
		t.Errorf("should have removed array trailing comma: %q", cleaned)
	}
}

func TestNormalizeEquals(t *testing.T) {
	c := NormalizeEquals()
	input := "host = localhost\nport = 5432"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_equals_colon" {
		t.Fatalf("expected equals_colon diag, got %+v", diags)
	}
	if strings.Contains(cleaned, "=") {
		t.Errorf("should have replaced = with :: %q", cleaned)
	}
	if !strings.Contains(cleaned, "host:") || !strings.Contains(cleaned, "port:") {
		t.Errorf("expected colon syntax: %q", cleaned)
	}
}

func TestNormalizeEquals_ColonAlreadyPresent(t *testing.T) {
	c := NormalizeEquals()
	input := "host: localhost\nport: 5432"
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("should not modify already-colon syntax")
	}
}

func TestStripJSONBrackets(t *testing.T) {
	c := StripJSONBrackets()
	input := "{\n  host: \"localhost\"\n  port: 5432\n}"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_json_brackets" {
		t.Fatalf("expected json_brackets diag, got %+v", diags)
	}
	if strings.HasPrefix(strings.TrimSpace(cleaned), "{") {
		t.Errorf("should have stripped outer braces: %q", cleaned)
	}
}

func TestStripJSONBrackets_NoBraces(t *testing.T) {
	c := StripJSONBrackets()
	input := "host: \"localhost\"\nport: 5432"
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("should not modify input without outer braces")
	}
}

func TestStripJSONBrackets_NestedBraces(t *testing.T) {
	c := StripJSONBrackets()
	// Input with balanced inner braces should NOT be stripped
	input := "{ host: { nested: true } }"
	cleaned, diags := c(input)
	if len(diags) != 1 {
		t.Fatalf("expected diag (outer braces), got %+v", diags)
	}
	// Inner braces should be preserved
	if !strings.Contains(cleaned, "nested") {
		t.Errorf("should preserve inner content: %q", cleaned)
	}
}

func TestFormatGuard_ConfigPass(t *testing.T) {
	c := FormatGuard()
	_, diags := c("host: \"localhost\"\nport: 5432")
	if len(diags) != 0 {
		t.Fatalf("config syntax should pass guard, got %+v", diags)
	}
}

func TestFormatGuard_BlockPass(t *testing.T) {
	c := FormatGuard()
	_, diags := c("server {\n  host: \"localhost\"\n}")
	if len(diags) != 0 {
		t.Fatalf("block syntax should pass guard, got %+v", diags)
	}
}

func TestFormatGuard_JSONPass(t *testing.T) {
	c := FormatGuard()
	_, diags := c("{\"host\": \"localhost\"}")
	if len(diags) != 0 {
		t.Fatalf("JSON syntax should pass guard, got %+v", diags)
	}
}

func TestFormatGuard_EqualsPass(t *testing.T) {
	c := FormatGuard()
	_, diags := c("host = localhost\nport = 5432")
	if len(diags) != 0 {
		t.Fatalf("equals syntax should pass guard, got %+v", diags)
	}
}

func TestFormatGuard_EmptyRejected(t *testing.T) {
	c := FormatGuard()
	_, diags := c("")
	if len(diags) == 0 {
		t.Fatal("empty input should be rejected")
	}
	if diags[0].Code != "config_format_rejected" {
		t.Errorf("expected config_format_rejected, got %q", diags[0].Code)
	}
	if diags[0].Severity != "error" {
		t.Errorf("expected error severity, got %q", diags[0].Severity)
	}
}

func TestFormatGuard_WhitespaceRejected(t *testing.T) {
	c := FormatGuard()
	_, diags := c("   \n\t  \n  ")
	if len(diags) == 0 {
		t.Fatal("whitespace-only input should be rejected")
	}
	if diags[0].Code != "config_format_rejected" {
		t.Errorf("expected config_format_rejected, got %q", diags[0].Code)
	}
}

func TestFormatGuard_XMLRejected(t *testing.T) {
	c := FormatGuard()
	_, diags := c("<config><host>localhost</host><port>5432</port></config>")
	if len(diags) == 0 {
		t.Fatal("XML should be rejected")
	}
	if diags[0].Code != "config_format_rejected" {
		t.Errorf("expected config_format_rejected, got %q", diags[0].Code)
	}
}

func TestFormatGuard_PlainTextRejected(t *testing.T) {
	c := FormatGuard()
	_, diags := c("The database should connect to localhost on port 5432")
	if len(diags) == 0 {
		t.Fatal("natural language should be rejected")
	}
	if diags[0].Code != "config_format_rejected" {
		t.Errorf("expected config_format_rejected, got %q", diags[0].Code)
	}
}

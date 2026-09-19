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

// The rewriting correctors must never touch text that lives inside a string
// literal. Each case below contains a regex-like pattern that the corresponding
// corrector would otherwise rewrite, corrupting the value.

func TestStripJSONTrailingCommas_NotInsideString(t *testing.T) {
	c := StripJSONTrailingCommas()
	inputs := []string{
		`msg: "value,]"`,               // ,] inside a value
		`pattern: "array [1, 2,]"`,     // ,] inside a value
		`note: "brace,}"`,              // ,} inside a value
		`msg: "a \"b, ] c"`,            // escaped quote, then , ]
		"msg: \"line1,\nline2\"",       // ,\n inside a multi-line value
		"msg: \"unterminated, ]",       // unterminated literal: stay conservative
	}
	for _, input := range inputs {
		cleaned, diags := c(input)
		if len(diags) != 0 {
			t.Errorf("%q: expected no diags, got %+v", input, diags)
		}
		if cleaned != input {
			t.Errorf("%q: string content must not be rewritten, got %q", input, cleaned)
		}
	}
}

func TestStripJSONTrailingCommas_StillRewritesOutsideStrings(t *testing.T) {
	c := StripJSONTrailingCommas()
	// One trailing comma outside any string, one comma that is string content.
	input := "items: [1, 2,]\nmsg: \"a, b,\""
	cleaned, diags := c(input)
	if len(diags) != 1 {
		t.Fatalf("expected one diag, got %+v", diags)
	}
	if strings.Contains(cleaned, ",]") {
		t.Errorf("array trailing comma should be removed: %q", cleaned)
	}
	if !strings.Contains(cleaned, `msg: "a, b,"`) {
		t.Errorf("string value must be preserved verbatim: %q", cleaned)
	}
}

func TestNormalizeEquals_NotInsideString(t *testing.T) {
	c := NormalizeEquals()
	// A multi-line string literal whose line looks like key = value.
	input := "msg: \"first\nkey = value\nlast\""
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("string content must not be rewritten, got %q", cleaned)
	}
}

func TestNormalizeEquals_RewritesOutsideStringsOnly(t *testing.T) {
	c := NormalizeEquals()
	input := "msg: \"first\nkey = value\nlast\"\nport = 5432"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_equals_colon" {
		t.Fatalf("expected one equals_colon diag, got %+v", diags)
	}
	if !strings.Contains(cleaned, "key = value") {
		t.Errorf("in-string `key = value` must be preserved: %q", cleaned)
	}
	if !strings.Contains(cleaned, "port: 5432") {
		t.Errorf("real `port = 5432` should be normalized: %q", cleaned)
	}
}

func TestStripJSONQuotes_NotInsideString(t *testing.T) {
	c := StripJSONQuotes()
	// Escaped quotes plus a colon inside a value: a quoted-key match must not
	// fire on literal content.
	input := `note: "see \"host\": here"`
	cleaned, diags := c(input)
	if len(diags) != 0 {
		t.Fatalf("expected no diags, got %+v", diags)
	}
	if cleaned != input {
		t.Errorf("string content must not be rewritten, got %q", cleaned)
	}
}

func TestStripJSONQuotes_StillRewritesMapKeys(t *testing.T) {
	c := StripJSONQuotes()
	// A quoted key at the start of a line and one after `{` must both survive
	// the string-literal gate.
	input := "{\n  \"host\": \"localhost\",\n  \"port\": 5432\n}"
	cleaned, diags := c(input)
	if len(diags) != 1 || diags[0].Code != "config_json_quotes" {
		t.Fatalf("expected one json_quotes diag, got %+v", diags)
	}
	if strings.Contains(cleaned, `"host"`) || strings.Contains(cleaned, `"port"`) {
		t.Errorf("map keys should be unquoted: %q", cleaned)
	}
	if !strings.Contains(cleaned, `"localhost"`) {
		t.Errorf("string value must be preserved: %q", cleaned)
	}
}

func TestStripJSONBrackets_StringWithBrace(t *testing.T) {
	c := StripJSONBrackets()
	// Braces inside a string are content, not nesting: the outer JSON object
	// braces must still be stripped.
	for _, input := range []string{
		`{ msg: "closing } brace" }`,
		`{ msg: "opening { brace" }`,
		`{ msg: "both { and } inside" }`,
	} {
		cleaned, diags := c(input)
		if len(diags) != 1 || diags[0].Code != "config_json_brackets" {
			t.Fatalf("%q: expected one json_brackets diag, got %+v", input, diags)
		}
		if strings.HasPrefix(strings.TrimSpace(cleaned), "{") {
			t.Errorf("%q: outer braces should be stripped: %q", input, cleaned)
		}
	}
}

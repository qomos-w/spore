package config

import (
	"regexp"
	"strings"
)

// StripMarkdownCodeBlock returns a corrector that removes markdown code
// fencing (```json ... ``` or ``` ... ```).
func StripMarkdownCodeBlock() Corrector {
	re := regexp.MustCompile("(?s)^\\s*```[a-zA-Z]*\\n(.*?)\\n\\s*```\\s*$")
	return func(source string) (string, []Diagnostic) {
		m := re.FindStringSubmatch(source)
		if m == nil {
			return source, nil
		}
		return m[1], []Diagnostic{{
			Code:     "config_markdown_block",
			Category: "correct",
			Severity: "info",
			Message:  "stripped markdown code block fencing",
			Hint:     "config was wrapped in ```...```, auto-removed",
		}}
	}
}

// StripJSONQuotes returns a corrector that removes unnecessary quotes around
// map keys when the key is a valid identifier.
func StripJSONQuotes() Corrector {
	// Match "key": at the start of a line (with optional leading whitespace)
	// or after {/,(whitespace. The key must be a valid identifier inside the quotes.
	re := regexp.MustCompile(`(?m)(^\s*|[{,]\s*)"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:`)
	return func(source string) (string, []Diagnostic) {
		if !strings.Contains(source, `"`) {
			return source, nil
		}
		count := 0
		cleaned := re.ReplaceAllStringFunc(source, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			// sub[1] is prefix (start-of-line or {, + space), sub[2] is the key
			if sub[2] == "true" || sub[2] == "false" || sub[2] == "null" {
				return match
			}
			count++
			return sub[1] + sub[2] + ":"
		})
		if count == 0 {
			return source, nil
		}
		return cleaned, []Diagnostic{{
			Code:     "config_json_quotes",
			Category: "correct",
			Severity: "info",
			Message:  "removed JSON-style quotes from map keys",
			Hint:     "Spore config uses bare identifiers as map keys, not quoted strings",
			Actual:   "quoted keys",
			Expected: "bare identifiers",
		}}
	}
}

// StripJSONTrailingCommas returns a corrector that removes trailing commas
// before } or ], at end of lines, or at end of input.
func StripJSONTrailingCommas() Corrector {
	reBracket := regexp.MustCompile(`,\s*([}\]])`)
	reEOL := regexp.MustCompile(`,\s*\n`)
	reEOF := regexp.MustCompile(`,\s*$`)
	return func(source string) (string, []Diagnostic) {
		if !strings.Contains(source, ",") {
			return source, nil
		}
		cleaned := reBracket.ReplaceAllString(source, "$1")
		cleaned = reEOL.ReplaceAllString(cleaned, "\n")
		cleaned = reEOF.ReplaceAllString(cleaned, "")
		if cleaned == source {
			return source, nil
		}
		return cleaned, []Diagnostic{{
			Code:     "config_json_trailing_comma",
			Category: "correct",
			Severity: "info",
			Message:  "removed JSON-style trailing commas",
			Hint:     "trailing commas before } or ] are not needed in Spore config",
		}}
	}
}

// NormalizeEquals returns a corrector that converts `key = value` to
// `key: value` (common in INI/TOML-style LLM output).
func NormalizeEquals() Corrector {
	// Match identifier followed by = (not == or !=) at key position
	re := regexp.MustCompile(`(?m)^(\s*[a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*`)
	return func(source string) (string, []Diagnostic) {
		if !strings.Contains(source, "=") {
			return source, nil
		}
		// Don't match inside strings — simple heuristic: if the line has no : before =
		cleaned := re.ReplaceAllStringFunc(source, func(match string) string {
			// Already has : before =? skip
			if strings.Contains(match, ":") {
				return match
			}
			return re.ReplaceAllString(match, "$1: ")
		})
		if cleaned == source {
			return source, nil
		}
		return cleaned, []Diagnostic{{
			Code:     "config_equals_colon",
			Category: "correct",
			Severity: "info",
			Message:  "normalized '=' to ':' for key-value separation",
			Hint:     "Spore config uses key: value, not key = value",
		}}
	}
}

// StripJSONBrackets returns a corrector that strips outer { } braces when
// the entire source is a JSON-style object.
func StripJSONBrackets() Corrector {
	return func(source string) (string, []Diagnostic) {
		trimmed := strings.TrimSpace(source)
		if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
			return source, nil
		}
		// Check it's not a block syntax (contains only one top-level brace pair)
		inner := trimmed[1 : len(trimmed)-1]
		depth := 0
		for _, ch := range inner {
			switch ch {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth != 0 {
			return source, nil
		}
		return inner, []Diagnostic{{
			Code:     "config_json_brackets",
			Category: "correct",
			Severity: "info",
			Message:  "stripped outer JSON object braces",
			Hint:     "Spore config does not need outer { } braces",
		}}
	}
}

// FormatGuard returns a corrector that rejects input which does not resemble
// config, JSON, or key=value syntax. It should be placed at the head of the
// corrector chain to prevent downstream correctors from mangling unrelated
// formats (XML, natural language, etc.).
func FormatGuard() Corrector {
	reColon := regexp.MustCompile(`(?m)^\s*[a-zA-Z_]\w*\s*:`)
	reBlock := regexp.MustCompile(`(?m)^\s*[a-zA-Z_]\w*\s*\{`)
	reJSONKey := regexp.MustCompile(`(?m)^\s*"[a-zA-Z_]\w*"\s*:`)
	reEquals := regexp.MustCompile(`(?m)^\s*[a-zA-Z_]\w*\s*=`)
	return func(source string) (string, []Diagnostic) {
		trimmed := strings.TrimSpace(source)
		if len(trimmed) == 0 {
			return source, []Diagnostic{{
				Code:     "config_format_rejected",
				Category: "correct",
				Severity: "error",
				Message:  "input is empty",
				Hint:     "provide config in Spore config, JSON, or key=value syntax",
			}}
		}
		if reColon.MatchString(trimmed) || reBlock.MatchString(trimmed) {
			return source, nil
		}
		if reJSONKey.MatchString(trimmed) || strings.HasPrefix(trimmed, "{") {
			return source, nil
		}
		if reEquals.MatchString(trimmed) {
			return source, nil
		}
		return source, []Diagnostic{{
			Code:     "config_format_rejected",
			Category: "correct",
			Severity: "error",
			Message:  "input format not recognized",
			Hint:     "use Spore config (key: value), JSON ({\"key\": \"value\"}), or key=value syntax",
		}}
	}
}

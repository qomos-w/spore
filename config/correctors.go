package config

import (
	"regexp"
	"strings"
)

// The regex-driven correctors below rewrite key syntax (bare map keys, trailing
// commas, `=` separators) on the raw source text. A regex cannot tell a value
// string from surrounding syntax, so a pattern that looks like a rewrite target
// but lives *inside* a string literal — `msg: "array [1, 2,]"` or a multi-line
// string holding `key = value` — used to be rewritten, silently corrupting the
// value. Every rewriting corrector therefore runs through
// replaceOutsideStrings, which skips matches that overlap a string literal.

// stringLiteralSpans returns the byte ranges [start,end) of every string
// literal in src, opening and closing quotes included. It walks the config
// lexer's token stream so the correctors and the parser agree on what counts as
// a string:
//
//   - an escaped quote (\" ) does not terminate a literal;
//   - a quote inside a // or /* */ comment is not a literal (the lexer skips
//     comments before recognising strings);
//   - a literal may span raw newlines;
//   - an unterminated literal is treated as reaching the end of input, so a
//     malformed trailing quote can never re-open the text to rewriting.
//
// Byte offsets come from the lexer's own cursor, so they index directly into
// src.
func stringLiteralSpans(src string) [][2]int {
	l := newLexer(src)
	var spans [][2]int
	for {
		tok := l.nextToken()
		if tok.typ == tokEOF {
			return spans
		}
		switch {
		case tok.typ == tokStringLit:
			spans = append(spans, [2]int{l.start, l.pos})
		case tok.typ == tokError && l.start < len(src) && src[l.start] == '"':
			// The lexer's only error whose token starts on a quote is an
			// unterminated string; it consumes to EOF, so treat the remainder
			// as literal.
			spans = append(spans, [2]int{l.start, len(src)})
		}
	}
}

// insideLiteral reports whether byte offset off is strictly inside a string
// literal — past its opening quote. An offset exactly at a literal's opening
// quote counts as outside: that is the structural key position StripJSONQuotes
// targets (the quotes around a top-level map key), so the correctors must be
// allowed to rewrite there.
func insideLiteral(spans [][2]int, off int) bool {
	for _, s := range spans {
		if off > s[0] && off < s[1] {
			return true
		}
	}
	return false
}

// replaceOutsideStrings applies re to src, but only to matches that do not
// begin inside a string literal. A match whose start lies past a literal's
// opening quote (e.g. `,]` in `msg: "value,]"`, or a `key = value` line inside
// a multi-line string) is left byte-for-byte unchanged, so callers can never
// rewrite inside a string. repl receives the matched text and returns its
// replacement; returning the match unchanged counts as no replacement. The
// returned count is the number of matches actually rewritten.
func replaceOutsideStrings(src string, re *regexp.Regexp, repl func(match string) string) (string, int) {
	matches := re.FindAllStringIndex(src, -1)
	if len(matches) == 0 {
		return src, 0
	}
	spans := stringLiteralSpans(src)
	var b strings.Builder
	last := 0
	count := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if insideLiteral(spans, start) {
			continue
		}
		match := src[start:end]
		replaced := repl(match)
		if replaced == match {
			continue
		}
		b.WriteString(src[last:start])
		b.WriteString(replaced)
		last = end
		count++
	}
	if count == 0 {
		return src, 0
	}
	b.WriteString(src[last:])
	return b.String(), count
}

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
		cleaned, count := replaceOutsideStrings(source, re, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			// sub[1] is prefix (start-of-line or {, + space), sub[2] is the key
			if sub[2] == "true" || sub[2] == "false" || sub[2] == "null" {
				return match
			}
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
		cleaned, _ := replaceOutsideStrings(source, reBracket, func(match string) string {
			sub := reBracket.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			return sub[1]
		})
		cleaned, _ = replaceOutsideStrings(cleaned, reEOL, func(string) string { return "\n" })
		cleaned, _ = replaceOutsideStrings(cleaned, reEOF, func(string) string { return "" })
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
		// Don't match inside strings — replaceOutsideStrings enforces it on the
		// lexer's string-literal spans; the `:` check keeps the historical
		// guard for matches that still somehow carry a colon.
		cleaned, count := replaceOutsideStrings(source, re, func(match string) string {
			// Already has : before =? skip
			if strings.Contains(match, ":") {
				return match
			}
			return re.ReplaceAllString(match, "$1: ")
		})
		if count == 0 {
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
		// Check it's not a block syntax (contains only one top-level brace pair).
		// Braces inside string literals are content, not nesting, so skip them.
		inner := trimmed[1 : len(trimmed)-1]
		spans := stringLiteralSpans(inner)
		depth := 0
		for i := 0; i < len(inner); i++ {
			if insideLiteral(spans, i) {
				continue
			}
			switch inner[i] {
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

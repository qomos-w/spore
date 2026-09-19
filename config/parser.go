package config

import (
	"fmt"
	"os"
	"strings"
)

// Parse parses a config source string into a Config.
//
// A source with syntax errors reports every error it can in one pass instead of
// stopping at the first: the result is a single *SyntaxError, or an aggregate
// that SyntaxDiagnostics flattens into one Diagnostic per error. Recovery is
// deliberately shallow — a damaged statement is dropped up to the end of its
// line and parsing resumes at the next statement — so the parser stops as soon
// as it reaches a construct it cannot resume from.
func Parse(source string) (*Config, error) {
	l := newLexer(source)
	p := &parser{l: l}
	p.nextToken()
	return p.parseConfig()
}

// ParseFile reads and parses a config file from disk.
func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(string(data))
}

type parser struct {
	l   *lexer
	cur token
	// errs collects every syntax error reported during the parse. The parser
	// recovers from a broken statement by dropping the rest of its line and
	// resuming at the next statement, so one pass reports every reportable
	// error instead of stopping at the first.
	errs []*SyntaxError
}

func (p *parser) nextToken() {
	p.cur = p.l.nextToken()
}

// curSyntaxError turns the current token into a syntax error. A lexer token
// that carries its own diagnostic (an out-of-range numeric literal, for
// example) keeps that diagnostic instead of being restated generically.
func (p *parser) curSyntaxError() *SyntaxError {
	if d := p.cur.diag; d != nil {
		return &SyntaxError{
			Code:     d.Code,
			Message:  d.Message,
			Hint:     d.Hint,
			Line:     d.Line,
			Col:      d.Col,
			Expected: d.Expected,
			Actual:   d.Actual,
		}
	}
	return &SyntaxError{
		Code:    CodeConfigParseError,
		Message: p.cur.lexeme,
		Line:    p.cur.line,
		Col:     p.cur.col,
	}
}

// unexpected builds the standard "wrong token here" error, preferring the
// lexer's own diagnostic when the offending token carries one.
func (p *parser) unexpected(expected string) *SyntaxError {
	if p.cur.diag != nil {
		return p.curSyntaxError()
	}
	return &SyntaxError{
		Code:     CodeConfigParseError,
		Message:  fmt.Sprintf("expected %s, got %q", expected, p.cur.lexeme),
		Line:     p.cur.line,
		Col:      p.cur.col,
		Expected: expected,
		Actual:   p.cur.lexeme,
	}
}

// recordError collects a reported error. Nested parses return plain
// *SyntaxError values, so the collected list stays flat.
func (p *parser) recordError(err error) {
	if err == nil {
		return
	}
	if e, ok := err.(*SyntaxError); ok {
		p.errs = append(p.errs, e)
		return
	}
	p.errs = append(p.errs, &SyntaxError{Code: CodeConfigParseError, Message: err.Error()})
}

// syncStatement recovers after a reported error: it discards tokens up to the
// next statement boundary (a newline at brace depth zero, or EOF). There is no
// attempt at full recovery — once a construct is damaged, the rest of its line
// is dropped and parsing resumes at the next statement.
func (p *parser) syncStatement() {
	if p.cur.typ == tokNewline || p.cur.typ == tokEOF {
		return
	}
	depth := 0
	for p.cur.typ != tokEOF {
		switch p.cur.typ {
		case tokLBrace, tokLBracket:
			depth++
		case tokRBrace, tokRBracket:
			if depth > 0 {
				depth--
			}
		case tokNewline:
			if depth == 0 {
				p.nextToken()
				return
			}
		}
		p.nextToken()
	}
}

// parseResult returns the parse outcome: the config plus the aggregated syntax
// errors, or the config alone when the source was clean.
func (p *parser) parseResult(cfg *Config) (*Config, error) {
	if len(p.errs) == 0 {
		return cfg, nil
	}
	return nil, newSyntaxErrors(p.errs)
}

func (p *parser) parseConfig() (*Config, error) {
	cfg := &Config{}
	for p.cur.typ != tokEOF {
		if p.cur.typ == tokError {
			p.recordError(p.curSyntaxError())
			p.nextToken()
			continue
		}
		if p.cur.typ == tokNewline {
			p.nextToken()
			continue
		}
		if p.cur.typ != tokIdent {
			// A closing brace left over from a construct already reported is
			// residue, not a second defect: drop it silently so one damaged
			// statement yields one error.
			if len(p.errs) > 0 && (p.cur.typ == tokRBrace || p.cur.typ == tokRBracket) {
				p.nextToken()
				continue
			}
			p.recordError(p.unexpected("identifier"))
			p.syncStatement()
			continue
		}

		var stmtErr error
		switch p.cur.lexeme {
		case "struct":
			var td TypeDef
			td, stmtErr = p.parseStructDef()
			if stmtErr == nil {
				cfg.Types = append(cfg.Types, td)
			}
		case "pipeline":
			var pl PipelineAST
			pl, stmtErr = p.parsePipelineBlock()
			if stmtErr == nil {
				cfg.Pipelines = append(cfg.Pipelines, pl)
			}
		default:
			var kv KeyValue
			kv, stmtErr = p.parseKeyValueOrBlock()
			if stmtErr == nil {
				cfg.Values = append(cfg.Values, kv)
			}
		}
		if stmtErr != nil {
			p.recordError(stmtErr)
			// Resume at the next statement so the rest of the source still
			// gets parsed and can report its own errors.
			p.syncStatement()
			continue
		}
		p.skipNewlines()
	}
	return p.parseResult(cfg)
}

func (p *parser) parseStructDef() (TypeDef, error) {
	p.nextToken() // consume "struct"
	if p.cur.typ != tokIdent {
		return TypeDef{}, p.unexpected("struct name")
	}
	td := TypeDef{Name: p.cur.lexeme, Line: p.cur.line}
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return TypeDef{}, p.unexpected("'{'")
	}
	p.nextToken()
	p.skipNewlines()

	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return TypeDef{}, p.unexpected("field name")
		}
		name := p.cur.lexeme
		line := p.cur.line
		p.nextToken()
		if p.cur.typ != tokColon {
			return TypeDef{}, p.unexpected("':' after field name")
		}
		p.nextToken()
		typeText, err := p.parseTypeExpr()
		if err != nil {
			return TypeDef{}, err
		}
		td.Fields = append(td.Fields, FieldDef{Name: name, TypeText: typeText, Line: line})
		if p.cur.typ == tokComma {
			p.nextToken()
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return TypeDef{}, &SyntaxError{
			Message: "unterminated struct definition",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return td, nil
}

func (p *parser) parseTypeExpr() (string, error) {
	if p.cur.typ != tokIdent {
		return "", p.unexpected("type name")
	}
	name := p.cur.lexeme
	p.nextToken()

	if p.cur.typ != tokLT {
		return name, nil
	}
	// Generic: array<T>, map<K, V>
	var b strings.Builder
	b.WriteString(name)
	b.WriteString("<")
	p.nextToken()

	for {
		p.skipNewlines()
		inner, err := p.parseTypeExpr()
		if err != nil {
			return "", err
		}
		b.WriteString(inner)
		if p.cur.typ == tokComma {
			b.WriteString(", ")
			p.nextToken()
			continue
		}
		break
	}
	if p.cur.typ != tokGT {
		return "", p.unexpected("'>' in type expression")
	}
	b.WriteString(">")
	p.nextToken()
	return b.String(), nil
}

func (p *parser) parseKeyValueOrBlock() (KeyValue, error) {
	key := p.cur.lexeme
	line := p.cur.line
	p.nextToken()

	if p.cur.typ == tokLBrace {
		p.nextToken()
		p.skipNewlines()
		val, err := p.parseMapBody()
		if err != nil {
			return KeyValue{}, err
		}
		return KeyValue{Key: key, Value: val, Line: line}, nil
	}

	if p.cur.typ == tokError {
		// The lexer rejected the lexeme that followed the key.
		return KeyValue{}, p.curSyntaxError()
	}
	if p.cur.typ != tokColon {
		return KeyValue{}, p.unexpected("':' or '{'")
	}
	p.nextToken()

	val, err := p.parseValue()
	if err != nil {
		return KeyValue{}, err
	}
	return KeyValue{Key: key, Value: val, Line: line}, nil
}

func (p *parser) parseValue() (Value, error) {
	switch p.cur.typ {
	case tokIntLit:
		v := Value{Kind: ValueInt, IntVal: p.cur.intVal, Raw: p.cur.lexeme, Line: p.cur.line}
		p.nextToken()
		return v, nil
	case tokFloatLit:
		v := Value{Kind: ValueFloat, FloatVal: p.cur.floatVal, Raw: p.cur.lexeme, Line: p.cur.line}
		p.nextToken()
		return v, nil
	case tokStringLit:
		s := unquoteString(p.cur.lexeme)
		v := Value{Kind: ValueString, StrVal: s, Raw: p.cur.lexeme, Line: p.cur.line}
		p.nextToken()
		return v, nil
	case tokIdent:
		switch p.cur.lexeme {
		case "true":
			v := Value{Kind: ValueBool, BoolVal: true, Raw: "true", Line: p.cur.line}
			p.nextToken()
			return v, nil
		case "false":
			v := Value{Kind: ValueBool, BoolVal: false, Raw: "false", Line: p.cur.line}
			p.nextToken()
			return v, nil
		case "null":
			v := Value{Kind: ValueNull, Raw: "null", Line: p.cur.line}
			p.nextToken()
			return v, nil
		default:
			name := p.cur.lexeme
			line := p.cur.line
			p.nextToken()
			if p.cur.typ == tokLBrace {
				p.nextToken()
				p.skipNewlines()
				return p.parseStructLiteralBody(name, line)
			}
			if p.cur.typ == tokDot {
				// Reference expression: name.field.subfield
				var parts []string
				parts = append(parts, name)
				for p.cur.typ == tokDot {
					p.nextToken() // consume "."
					if p.cur.typ != tokIdent {
						return Value{}, p.unexpected("identifier after '.'")
					}
					parts = append(parts, p.cur.lexeme)
					p.nextToken()
				}
				raw := strings.Join(parts, ".")
				return Value{Kind: ValueRef, StrVal: raw, Raw: raw, Line: line}, nil
			}
			return Value{}, &SyntaxError{
				Message: fmt.Sprintf("unexpected identifier %q in value position", name),
				Line:    line, Col: p.cur.col,
			}
		}
	case tokLBracket:
		return p.parseArray()
	case tokLBrace:
		p.nextToken()
		p.skipNewlines()
		return p.parseMapBody()
	case tokError:
		// The lexer rejected the literal itself (an out-of-range number, for
		// example); report its diagnostic rather than restating it as a
		// generic "expected value".
		return Value{}, p.curSyntaxError()
	default:
		return Value{}, p.unexpected("value")
	}
}

func (p *parser) parseArray() (Value, error) {
	line := p.cur.line
	p.nextToken()
	p.skipNewlines()

	var elems []Value
	for p.cur.typ != tokRBracket && p.cur.typ != tokEOF {
		v, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		elems = append(elems, v)
		if p.cur.typ == tokComma {
			p.nextToken()
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBracket {
		return Value{}, &SyntaxError{
			Message: "unterminated array",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return Value{Kind: ValueArray, Elements: elems, Line: line}, nil
}

func (p *parser) parseMapBody() (Value, error) {
	line := p.cur.line
	var entries []KeyValue
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		p.skipNewlines()
		if p.cur.typ == tokRBrace {
			break
		}

		var key string
		var keyLine int
		switch p.cur.typ {
		case tokIdent:
			key = p.cur.lexeme
			keyLine = p.cur.line
			p.nextToken()
		case tokStringLit:
			key = unquoteString(p.cur.lexeme)
			keyLine = p.cur.line
			p.nextToken()
		default:
			return Value{}, p.unexpected("map key")
		}

		if p.cur.typ == tokLBrace {
			p.nextToken()
			p.skipNewlines()
			val, err := p.parseMapBody()
			if err != nil {
				return Value{}, err
			}
			entries = append(entries, KeyValue{Key: key, Value: val, Line: keyLine})
			if p.cur.typ == tokComma {
				p.nextToken()
			}
			p.skipNewlines()
			continue
		}

		if p.cur.typ != tokColon {
			return Value{}, &SyntaxError{
				Message: fmt.Sprintf("expected ':' or '{' after key %q, got %q", key, p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		p.nextToken()

		val, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		entries = append(entries, KeyValue{Key: key, Value: val, Line: keyLine})

		if p.cur.typ == tokComma {
			p.nextToken()
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return Value{}, &SyntaxError{
			Message: "unterminated map",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return Value{Kind: ValueMap, Entries: entries, Line: line}, nil
}

func (p *parser) parseStructLiteralBody(typeName string, line int) (Value, error) {
	var fields []KeyValue
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		p.skipNewlines()
		if p.cur.typ == tokRBrace {
			break
		}

		if p.cur.typ != tokIdent {
			return Value{}, p.unexpected("field name")
		}
		fieldName := p.cur.lexeme
		fieldLine := p.cur.line
		p.nextToken()

		if p.cur.typ != tokColon {
			return Value{}, &SyntaxError{
				Message: fmt.Sprintf("expected ':' after field %q, got %q", fieldName, p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		p.nextToken()

		val, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		fields = append(fields, KeyValue{Key: fieldName, Value: val, Line: fieldLine})

		if p.cur.typ == tokComma {
			p.nextToken()
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return Value{}, &SyntaxError{
			Message: fmt.Sprintf("unterminated struct literal %s", typeName),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return Value{Kind: ValueStruct, TypeName: typeName, Fields: fields, Line: line}, nil
}

func (p *parser) skipNewlines() {
	for p.cur.typ == tokNewline {
		p.nextToken()
	}
}

// parsePipelineBlock parses a pipeline declaration.
// pipeline_block = "pipeline" IDENT "{" { step_block | parallel_block } "}"
func (p *parser) parsePipelineBlock() (PipelineAST, error) {
	p.nextToken() // consume "pipeline"
	if p.cur.typ != tokIdent {
		return PipelineAST{}, p.unexpected("pipeline name")
	}
	name := p.cur.lexeme
	line := p.cur.line
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return PipelineAST{}, p.unexpected("'{' after pipeline name")
	}
	p.nextToken()
	p.skipNewlines()

	var steps []PipelineStepAST
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return PipelineAST{}, p.unexpected("'step' or 'parallel'")
		}
		switch p.cur.lexeme {
		case "step":
			step, err := p.parsePipelineStep()
			if err != nil {
				return PipelineAST{}, err
			}
			steps = append(steps, step)
		case "parallel":
			parallelStep, err := p.parseParallelBlock()
			if err != nil {
				return PipelineAST{}, err
			}
			steps = append(steps, parallelStep)
		default:
			return PipelineAST{}, p.unexpected("'step' or 'parallel'")
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineAST{}, &SyntaxError{
			Message: "unterminated pipeline block",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return PipelineAST{Name: name, Steps: steps, Line: line}, nil
}

// parsePipelineStep parses a single step declaration.
// step_block = "step" IDENT "{" { step_field } "}"
func (p *parser) parsePipelineStep() (PipelineStepAST, error) {
	p.nextToken() // consume "step"
	if p.cur.typ != tokIdent {
		return PipelineStepAST{}, p.unexpected("step name")
	}
	name := p.cur.lexeme
	line := p.cur.line
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return PipelineStepAST{}, p.unexpected("'{' after step name")
	}
	p.nextToken()
	p.skipNewlines()

	step := PipelineStepAST{Name: name, Line: line}
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return PipelineStepAST{}, p.unexpected("step field name")
		}
		fieldName := p.cur.lexeme
		fieldLine := p.cur.line
		p.nextToken()
		if p.cur.typ != tokColon {
			return PipelineStepAST{}, &SyntaxError{
				Code:    CodeConfigParseError,
				Message: fmt.Sprintf("expected ':' after field %q, got %q", fieldName, p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
				Actual: p.cur.lexeme,
			}
		}
		p.nextToken()

		spec, known := pipelineStepField(fieldName)
		if !known {
			// Report the bad field and keep parsing the rest of the step: the
			// step boundary stays intact, so a following step is still parsed
			// and can report its own problems.
			p.recordError(p.unknownStepField(fieldName, fieldLine))
			p.syncStatement()
			continue
		}

		if err := p.parseStepFieldValue(&step, spec); err != nil {
			p.recordError(err)
			p.syncStatement()
			continue
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineStepAST{}, &SyntaxError{
			Code:    CodeConfigParseError,
			Message: fmt.Sprintf("unterminated step block %q", name),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return step, nil
}

// unknownStepField reports a field name that is not in the canonical step-field
// table, enumerating the accepted fields so the reader (typically an LLM) can
// repair the source without a second lookup.
func (p *parser) unknownStepField(fieldName string, line int) *SyntaxError {
	suggestions := suggestStepFields(fieldName)
	message := fmt.Sprintf("unknown step field %q: valid fields are %s", fieldName, pipelineStepFieldList())
	if len(suggestions) > 0 {
		message += fmt.Sprintf(" (did you mean %s?)", strings.Join(suggestions, " or "))
	}
	return &SyntaxError{
		Code:        CodeConfigParseError,
		Message:     message,
		Hint:        "valid fields are " + pipelineStepFieldList(),
		Line:        line,
		Col:         p.cur.col,
		Expected:    pipelineStepFieldList(),
		Actual:      fieldName,
		Suggestions: suggestions,
	}
}

// parseStepFieldValue parses the value of one step field and stores it on the
// step. The field table's ValueForm picks the grammar, so the parser and the
// validators agree on a field's shape by construction rather than by
// convention.
func (p *parser) parseStepFieldValue(step *PipelineStepAST, spec pipelineStepFieldSpec) error {
	switch spec.ValueForm {
	case stepFormString:
		if p.cur.typ == tokError {
			return p.curSyntaxError()
		}
		if p.cur.typ != tokStringLit {
			return &SyntaxError{
				Code:    CodeConfigParseError,
				Message: fmt.Sprintf("expected string for %s, got %q", spec.Name, p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
				Expected: "string",
				Actual:   p.cur.lexeme,
			}
		}
		value := unquoteString(p.cur.lexeme)
		p.nextToken()
		switch spec.Name {
		case PipelineStepFieldInvoke:
			step.Invoke = value
		case PipelineStepFieldTimeout:
			step.Timeout = value
		default:
			// A string-valued field added to the table without a destination
			// in the step AST must fail loudly instead of dropping its value.
			return &SyntaxError{
				Code:    CodeConfigParseError,
				Message: fmt.Sprintf("step field %q has no destination in the step AST", spec.Name),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
	case stepFormStringArray:
		deps, err := p.parseStringArray()
		if err != nil {
			return err
		}
		step.DependsOn = deps
	case stepFormRef:
		ref, err := p.parseRefExpr()
		if err != nil {
			return err
		}
		step.When = ref
	case stepFormValue:
		value, err := p.parseValue()
		if err != nil {
			return err
		}
		step.Input = value
	default:
		return &SyntaxError{
			Code:    CodeConfigParseError,
			Message: fmt.Sprintf("step field %q declares unknown value form %q", spec.Name, spec.ValueForm),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	return nil
}

// parseParallelBlock parses a parallel block as a synthetic step.
// parallel_block = "parallel" "{" { step_block } "}"
func (p *parser) parseParallelBlock() (PipelineStepAST, error) {
	p.nextToken() // consume "parallel"
	line := p.cur.line
	if p.cur.typ != tokLBrace {
		return PipelineStepAST{}, p.unexpected("'{' after parallel")
	}
	p.nextToken()
	p.skipNewlines()

	var children []PipelineStepAST
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent || p.cur.lexeme != "step" {
			return PipelineStepAST{}, p.unexpected("'step' in parallel block")
		}
		step, err := p.parsePipelineStep()
		if err != nil {
			return PipelineStepAST{}, err
		}
		children = append(children, step)
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineStepAST{}, &SyntaxError{
			Message: "unterminated parallel block",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return PipelineStepAST{Parallel: children, Line: line}, nil
}

// parseStringArray parses ["a", "b", ...] into []string.
func (p *parser) parseStringArray() ([]string, error) {
	if p.cur.typ == tokError {
		return nil, p.curSyntaxError()
	}
	if p.cur.typ != tokLBracket {
		return nil, p.unexpected("'['")
	}
	p.nextToken()
	p.skipNewlines()

	var items []string
	for p.cur.typ != tokRBracket && p.cur.typ != tokEOF {
		if p.cur.typ == tokError {
			return nil, p.curSyntaxError()
		}
		if p.cur.typ != tokIdent && p.cur.typ != tokStringLit {
			return nil, p.unexpected("identifier or string in array")
		}
		var s string
		if p.cur.typ == tokStringLit {
			s = unquoteString(p.cur.lexeme)
		} else {
			s = p.cur.lexeme
		}
		items = append(items, s)
		p.nextToken()
		if p.cur.typ == tokComma {
			p.nextToken()
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBracket {
		return nil, &SyntaxError{
			Message: "unterminated array",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return items, nil
}

// parseRefExpr parses a reference expression like stepName.field.subfield.
// ref_expr = IDENT "." IDENT { "." IDENT }
func (p *parser) parseRefExpr() (*RefExpr, error) {
	if p.cur.typ != tokIdent {
		return nil, p.unexpected("identifier in ref expression")
	}
	stepName := p.cur.lexeme
	line := p.cur.line
	var rawParts []string
	rawParts = append(rawParts, stepName)
	p.nextToken()

	for p.cur.typ == tokDot {
		p.nextToken() // consume "."
		if p.cur.typ != tokIdent {
			return nil, p.unexpected("identifier after '.'")
		}
		rawParts = append(rawParts, p.cur.lexeme)
		p.nextToken()
	}

	if len(rawParts) < 2 {
		return nil, &SyntaxError{
			Message: "ref expression must have at least two parts (step.field)",
			Line:    line, Col: p.cur.col,
		}
	}

	return &RefExpr{
		StepName: stepName,
		Path:     rawParts[1:],
		Raw:      strings.Join(rawParts, "."),
		Line:     line,
	}, nil
}

func unquoteString(raw string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		if raw[i] == '\\' && i+1 < len(raw) {
			switch raw[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteByte(raw[i+1])
			}
			i += 2
		} else {
			b.WriteByte(raw[i])
			i++
		}
	}
	return b.String()
}

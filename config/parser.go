package config

import (
	"fmt"
	"os"
	"strings"
)

// Parse parses a config source string into a Config.
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
}

func (p *parser) nextToken() {
	p.cur = p.l.nextToken()
}

func (p *parser) parseConfig() (*Config, error) {
	cfg := &Config{}
	for p.cur.typ != tokEOF {
		if p.cur.typ == tokError {
			return nil, &parseError{Message: p.cur.lexeme, Line: p.cur.line, Col: p.cur.col}
		}
		if p.cur.typ == tokNewline {
			p.nextToken()
			continue
		}
		if p.cur.typ != tokIdent {
			return nil, &parseError{
				Message: fmt.Sprintf("expected identifier, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}

		if p.cur.lexeme == "struct" {
			td, err := p.parseStructDef()
			if err != nil {
				return nil, err
			}
			cfg.Types = append(cfg.Types, td)
			p.skipNewlines()
			continue
		}

		if p.cur.lexeme == "pipeline" {
			pl, err := p.parsePipelineBlock()
			if err != nil {
				return nil, err
			}
			cfg.Pipelines = append(cfg.Pipelines, pl)
			p.skipNewlines()
			continue
		}

		kv, err := p.parseKeyValueOrBlock()
		if err != nil {
			return nil, err
		}
		cfg.Values = append(cfg.Values, kv)
		p.skipNewlines()
	}
	return cfg, nil
}

func (p *parser) parseStructDef() (TypeDef, error) {
	p.nextToken() // consume "struct"
	if p.cur.typ != tokIdent {
		return TypeDef{}, &parseError{
			Message: fmt.Sprintf("expected struct name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	td := TypeDef{Name: p.cur.lexeme, Line: p.cur.line}
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return TypeDef{}, &parseError{
			Message: fmt.Sprintf("expected '{', got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	p.skipNewlines()

	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return TypeDef{}, &parseError{
				Message: fmt.Sprintf("expected field name, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		name := p.cur.lexeme
		line := p.cur.line
		p.nextToken()
		if p.cur.typ != tokColon {
			return TypeDef{}, &parseError{
				Message: fmt.Sprintf("expected ':' after field name, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
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
		return TypeDef{}, &parseError{
			Message: "unterminated struct definition",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return td, nil
}

func (p *parser) parseTypeExpr() (string, error) {
	if p.cur.typ != tokIdent {
		return "", &parseError{
			Message: fmt.Sprintf("expected type name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
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
		return "", &parseError{
			Message: fmt.Sprintf("expected '>' in type expression, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
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

	if p.cur.typ != tokColon {
		return KeyValue{}, &parseError{
			Message: fmt.Sprintf("expected ':' or '{', got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
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
						return Value{}, &parseError{
							Message: fmt.Sprintf("expected identifier after '.', got %q", p.cur.lexeme),
							Line:    p.cur.line, Col: p.cur.col,
						}
					}
					parts = append(parts, p.cur.lexeme)
					p.nextToken()
				}
				raw := strings.Join(parts, ".")
				return Value{Kind: ValueRef, StrVal: raw, Raw: raw, Line: line}, nil
			}
			return Value{}, &parseError{
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
	default:
		return Value{}, &parseError{
			Message: fmt.Sprintf("expected value, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
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
		return Value{}, &parseError{
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
			return Value{}, &parseError{
				Message: fmt.Sprintf("expected map key, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
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
			return Value{}, &parseError{
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
		return Value{}, &parseError{
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
			return Value{}, &parseError{
				Message: fmt.Sprintf("expected field name, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		fieldName := p.cur.lexeme
		fieldLine := p.cur.line
		p.nextToken()

		if p.cur.typ != tokColon {
			return Value{}, &parseError{
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
		return Value{}, &parseError{
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
		return PipelineAST{}, &parseError{
			Message: fmt.Sprintf("expected pipeline name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	name := p.cur.lexeme
	line := p.cur.line
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return PipelineAST{}, &parseError{
			Message: fmt.Sprintf("expected '{' after pipeline name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	p.skipNewlines()

	var steps []PipelineStepAST
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return PipelineAST{}, &parseError{
				Message: fmt.Sprintf("expected 'step' or 'parallel', got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
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
			return PipelineAST{}, &parseError{
				Message: fmt.Sprintf("expected 'step' or 'parallel', got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineAST{}, &parseError{
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
		return PipelineStepAST{}, &parseError{
			Message: fmt.Sprintf("expected step name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	name := p.cur.lexeme
	line := p.cur.line
	p.nextToken()
	if p.cur.typ != tokLBrace {
		return PipelineStepAST{}, &parseError{
			Message: fmt.Sprintf("expected '{' after step name, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	p.skipNewlines()

	step := PipelineStepAST{Name: name, Line: line}
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent {
			return PipelineStepAST{}, &parseError{
				Message: fmt.Sprintf("expected step field name, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		fieldName := p.cur.lexeme
		fieldLine := p.cur.line
		p.nextToken()
		if p.cur.typ != tokColon {
			return PipelineStepAST{}, &parseError{
				Message: fmt.Sprintf("expected ':' after field %q, got %q", fieldName, p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		p.nextToken()

		switch fieldName {
		case "invoke":
			if p.cur.typ != tokStringLit {
				return PipelineStepAST{}, &parseError{
					Message: fmt.Sprintf("expected string for invoke, got %q", p.cur.lexeme),
					Line:    p.cur.line, Col: p.cur.col,
				}
			}
			step.Invoke = unquoteString(p.cur.lexeme)
			p.nextToken()
		case "input":
			val, err := p.parseValue()
			if err != nil {
				return PipelineStepAST{}, err
			}
			step.Input = val
		case "depends_on":
			deps, err := p.parseStringArray()
			if err != nil {
				return PipelineStepAST{}, err
			}
			step.DependsOn = deps
		case "timeout":
			if p.cur.typ != tokStringLit {
				return PipelineStepAST{}, &parseError{
					Message: fmt.Sprintf("expected string for timeout, got %q", p.cur.lexeme),
					Line:    p.cur.line, Col: p.cur.col,
				}
			}
			step.Timeout = unquoteString(p.cur.lexeme)
			p.nextToken()
		case "when":
			ref, err := p.parseRefExpr()
			if err != nil {
				return PipelineStepAST{}, err
			}
			step.When = ref
		default:
			return PipelineStepAST{}, &parseError{
				Message: fmt.Sprintf("unknown step field %q", fieldName),
				Line:    fieldLine, Col: p.cur.col,
			}
		}
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineStepAST{}, &parseError{
			Message: fmt.Sprintf("unterminated step block %q", name),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return step, nil
}

// parseParallelBlock parses a parallel block as a synthetic step.
// parallel_block = "parallel" "{" { step_block } "}"
func (p *parser) parseParallelBlock() (PipelineStepAST, error) {
	p.nextToken() // consume "parallel"
	line := p.cur.line
	if p.cur.typ != tokLBrace {
		return PipelineStepAST{}, &parseError{
			Message: fmt.Sprintf("expected '{' after parallel, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	p.skipNewlines()

	var children []PipelineStepAST
	for p.cur.typ != tokRBrace && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent || p.cur.lexeme != "step" {
			return PipelineStepAST{}, &parseError{
				Message: fmt.Sprintf("expected 'step' in parallel block, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		step, err := p.parsePipelineStep()
		if err != nil {
			return PipelineStepAST{}, err
		}
		children = append(children, step)
		p.skipNewlines()
	}

	if p.cur.typ != tokRBrace {
		return PipelineStepAST{}, &parseError{
			Message: "unterminated parallel block",
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	return PipelineStepAST{Parallel: children, Line: line}, nil
}

// parseStringArray parses ["a", "b", ...] into []string.
func (p *parser) parseStringArray() ([]string, error) {
	if p.cur.typ != tokLBracket {
		return nil, &parseError{
			Message: fmt.Sprintf("expected '[', got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	p.nextToken()
	p.skipNewlines()

	var items []string
	for p.cur.typ != tokRBracket && p.cur.typ != tokEOF {
		if p.cur.typ != tokIdent && p.cur.typ != tokStringLit {
			return nil, &parseError{
				Message: fmt.Sprintf("expected identifier or string in array, got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
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
		return nil, &parseError{
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
		return nil, &parseError{
			Message: fmt.Sprintf("expected identifier in ref expression, got %q", p.cur.lexeme),
			Line:    p.cur.line, Col: p.cur.col,
		}
	}
	stepName := p.cur.lexeme
	line := p.cur.line
	var rawParts []string
	rawParts = append(rawParts, stepName)
	p.nextToken()

	for p.cur.typ == tokDot {
		p.nextToken() // consume "."
		if p.cur.typ != tokIdent {
			return nil, &parseError{
				Message: fmt.Sprintf("expected identifier after '.', got %q", p.cur.lexeme),
				Line:    p.cur.line, Col: p.cur.col,
			}
		}
		rawParts = append(rawParts, p.cur.lexeme)
		p.nextToken()
	}

	if len(rawParts) < 2 {
		return nil, &parseError{
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

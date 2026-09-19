package frontend

import (
	"fmt"
)

// ParseValue parses a Spore value literal string into a Go value.
// Supported literals:
//   - scalars: 42, 3.14, "hello", true, false, null
//   - arrays: [1, 2, 3]
//   - maps: {"a": 1, "b": 2}
//   - struct literals: Point{x: 1, y: 2}
//
// Struct literals return a map[string]any with the struct name accessible
// via the special key "__struct__".
func ParseValue(source string) (any, error) {
	l := newLexer(source)
	p := newParser(l, source)
	// Use the parser's expression parser directly via a dummy statement
	val, err := parseValueFromParser(p)
	if err != nil {
		return nil, err
	}
	if !p.curIs(tokEOF) {
		return nil, fmt.Errorf("unexpected token %q after value", p.cur.lexeme)
	}
	return val, nil
}

// parseValueFromParser uses the parser's Pratt expression parser to parse
// a single value, then evaluates it to a Go value.
func parseValueFromParser(p *parser) (any, error) {
	expr := p.parseExpression()
	if expr == nil {
		return nil, fmt.Errorf("expected value, got %q", p.cur.lexeme)
	}
	return evalExpression(expr)
}

// evalExpression converts an AST expression node into a Go value.
func evalExpression(expr expression) (any, error) {
	switch e := expr.(type) {
	case *intLiteral:
		return e.Value, nil
	case *floatLiteral:
		return e.Value, nil
	case *stringLiteral:
		return e.Value, nil
	case *boolLiteral:
		return e.Value, nil
	case *nullLiteral:
		return nil, nil
	case *arrayLiteral:
		result := make([]any, len(e.Elements))
		for i, elem := range e.Elements {
			v, err := evalExpression(elem)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	case *mapLiteral:
		result := make(map[string]any)
		for _, pair := range e.Pairs {
			key, err := evalExpression(pair.Key)
			if err != nil {
				return nil, err
			}
			keyStr, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("map key must be string, got %T", key)
			}
			val, err := evalExpression(pair.Value)
			if err != nil {
				return nil, err
			}
			result[keyStr] = val
		}
		return result, nil
	case *structLiteral:
		result := make(map[string]any)
		result["__struct__"] = e.TypeName
		for _, field := range e.Fields {
			val, err := evalExpression(field.Value)
			if err != nil {
				return nil, err
			}
			result[field.Name] = val
		}
		return result, nil
	case *identExpr:
		// Allow bare identifiers for enum-like values (resolve to string)
		return e.Value, nil
	case *unaryExpr:
		val, err := evalExpression(e.Right)
		if err != nil {
			return nil, err
		}
		switch e.Operator {
		case "-":
			switch v := val.(type) {
			case int64:
				return -v, nil
			case float64:
				return -v, nil
			default:
				return nil, fmt.Errorf("cannot negate %T", v)
			}
		case "!":
			v, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("cannot apply ! to %T", val)
			}
			return !v, nil
		default:
			return nil, fmt.Errorf("unsupported unary operator %q", e.Operator)
		}
	default:
		return nil, fmt.Errorf("unsupported expression type %T", expr)
	}
}

// ParseArgs parses a comma-separated list of Spore value literals.
// Example: `1, "hello", Point{x: 1, y: 2}` → []any{1, "hello", map[string]any{...}}
func ParseArgs(source string) ([]any, error) {
	// Wrap in a synthetic function call to leverage the parser's arg-parsing
	wrapped := "__parse(" + source + ")"
	l := newLexer(wrapped)
	p := newParser(l, wrapped)
	// Parse as expression (function call)
	expr := p.parseExpression()
	if expr == nil {
		return nil, fmt.Errorf("expected argument list")
	}
	call, ok := expr.(*callExpr)
	if !ok {
		return nil, fmt.Errorf("expected argument list, got %T", expr)
	}
	result := make([]any, len(call.Arguments))
	for i, arg := range call.Arguments {
		v, err := evalExpression(arg)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i+1, err)
		}
		result[i] = v
	}
	return result, nil
}

// Value is a convenience that parses a single Spore value and returns it
// wrapped in a []any slice suitable for Frontend.Invoke.
func Value(source string) []any {
	v, err := ParseValue(source)
	if err != nil {
		panic(fmt.Sprintf("ParseValue: %v", err))
	}
	return []any{v}
}

// MustParseValue parses a Spore value literal and panics on error.
func MustParseValue(source string) any {
	v, err := ParseValue(source)
	if err != nil {
		panic(err)
	}
	return v
}

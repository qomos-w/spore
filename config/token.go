package config

type tokenType int

const (
	tokEOF tokenType = iota
	tokError
	tokIntLit
	tokFloatLit
	tokStringLit
	tokIdent
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokColon
	tokComma
	tokLT
	tokGT
	tokDot
	tokNewline
)

type token struct {
	typ      tokenType
	lexeme   string
	line     int
	col      int
	intVal   int64
	floatVal float64
	// diag carries the structured reason a token is invalid when the lexeme
	// itself is well-formed but unusable — an integer literal outside int64
	// range, for example. A tokError with a diag keeps its specific code
	// (config_int_overflow) instead of degrading to a generic parse error.
	diag *Diagnostic
}

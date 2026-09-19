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
}

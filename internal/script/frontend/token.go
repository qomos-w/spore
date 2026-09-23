package frontend

// tokenType classifies a single lexical unit produced by the lexer.
// All token types are internal to the frontend package.
type tokenType int

const (
	tokEOF tokenType = iota
	tokError

	// Literals
	tokIntLit
	tokFloatLit
	tokStringLit
	tokIdent

	// Keywords — SYNTAX.md kept constructs
	tokFun
	tokExport
	tokClass
	tokStruct
	tokEnum
	tokType
	tokVar
	tokPackage
	tokImport
	tokVoid
	tokAny
	tokBool
	tokByte
	tokShort
	tokUShort
	tokUint
	tokLong
	tokUlong
	tokDouble
	tokBytes
	tokArray
	tokMap
	tokInt
	tokFloat
	tokString
	tokMedia
	tokReturn
	tokIf
	tokElse
	tokFor
	tokWhile
	tokIn
	tokWhen
	tokCase
	tokBreak
	tokContinue
	tokIs
	tokAs
	tokFrom
	tokThis
	tokNew
	tokTrue
	tokFalse
	tokNull

	// Keywords — Spore-reserved script surface
	tokSuper
	tokConstructor
	tokOpen
	tokOverride
	tokInterface
	tokPublic
	tokPrivate
	tokStatic
	tokStream   // stream fun — explicit streaming callable declaration
	tokYield    // yield — legal inside stream fun
	tokAsync    // reserved — compiler rejects
	tokAwait    // reserved — compiler rejects
	tokOptional // optional — struct/class field modifier marking nullable wire fields
	tokKey      // key — struct field modifier marking the @data table key field
	tokTry      // try — error-capturing block statement
	tokCatch    // catch — handler clause of a try statement
	tokDefer    // defer — deferred execution block statement

	// Operators
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPercent
	tokEq
	tokEqEq
	tokBangEq
	tokLT
	tokLTEq
	tokGT
	tokGTEq
	tokAnd
	tokOr
	tokBang
	tokQuestion
	tokQuestionQuestion // ??
	tokQuestionDot      // ?.
	tokColon
	tokDot
	tokDotDot // ".."
	tokComma
	tokSemicolon
	tokArrow    // ->
	tokFatArrow // =>
	tokAt       // @

	// Delimiters
	tokLParen
	tokRParen
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
)

// token represents a single lexed unit with position information.
type token struct {
	typ      tokenType
	lexeme   string
	line     int
	col      int
	intVal   int64   // parsed integer value (for tokIntLit)
	floatVal float64 // parsed float value (for tokFloatLit)
}

// keywords maps identifier text to token types for the Spore-kept syntax profile.
var keywords = map[string]tokenType{
	"fun":         tokFun,
	"export":      tokExport,
	"class":       tokClass,
	"struct":      tokStruct,
	"enum":        tokEnum,
	"type":        tokType,
	"var":         tokVar,
	"package":     tokPackage,
	"import":      tokImport,
	"void":        tokVoid,
	"any":         tokAny,
	"bool":        tokBool,
	"byte":        tokByte,
	"short":       tokShort,
	"ushort":      tokUShort,
	"uint":        tokUint,
	"long":        tokLong,
	"ulong":       tokUlong,
	"double":      tokDouble,
	"bytes":       tokBytes,
	"array":       tokArray,
	"map":         tokMap,
	"int":         tokInt,
	"float":       tokFloat,
	"string":      tokString,
	"media":       tokMedia,
	"return":      tokReturn,
	"if":          tokIf,
	"else":        tokElse,
	"for":         tokFor,
	"while":       tokWhile,
	"in":          tokIn,
	"when":        tokWhen,
	"case":        tokCase,
	"break":       tokBreak,
	"continue":    tokContinue,
	"is":          tokIs,
	"as":          tokAs,
	"from":        tokFrom,
	"this":        tokThis,
	"new":         tokNew,
	"true":        tokTrue,
	"false":       tokFalse,
	"null":        tokNull,
	"super":       tokSuper,
	"constructor": tokConstructor,
	"open":        tokOpen,
	"override":    tokOverride,
	"interface":   tokInterface,
	"public":      tokPublic,
	"private":     tokPrivate,
	"static":      tokStatic,
	"stream":      tokStream,
	"yield":       tokYield,
	"async":       tokAsync,
	"await":       tokAwait,
	"optional":    tokOptional,
	"key":         tokKey,
	"try":         tokTry,
	"catch":       tokCatch,
	"defer":       tokDefer,
}

func (t tokenType) String() string {
	switch t {
	case tokEOF:
		return "EOF"
	case tokError:
		return "Error"
	case tokIntLit:
		return "Int"
	case tokFloatLit:
		return "Float"
	case tokStringLit:
		return "String"
	case tokIdent:
		return "Ident"
	case tokFun:
		return "fun"
	case tokExport:
		return "export"
	case tokClass:
		return "class"
	case tokStruct:
		return "struct"
	case tokEnum:
		return "enum"
	case tokType:
		return "type"
	case tokVar:
		return "var"
	case tokPackage:
		return "package"
	case tokImport:
		return "import"
	case tokVoid:
		return "void"
	case tokAny:
		return "any"
	case tokBool:
		return "bool"
	case tokByte:
		return "byte"
	case tokShort:
		return "short"
	case tokUShort:
		return "ushort"
	case tokUint:
		return "uint"
	case tokLong:
		return "long"
	case tokUlong:
		return "ulong"
	case tokDouble:
		return "double"
	case tokBytes:
		return "bytes"
	case tokInt:
		return "int"
	case tokFloat:
		return "float"
	case tokString:
		return "string"
	case tokMedia:
		return "media"
	case tokArray:
		return "array"
	case tokMap:
		return "map"
	case tokReturn:
		return "return"
	case tokIf:
		return "if"
	case tokElse:
		return "else"
	case tokFor:
		return "for"
	case tokWhile:
		return "while"
	case tokIn:
		return "in"
	case tokWhen:
		return "when"
	case tokCase:
		return "case"
	case tokBreak:
		return "break"
	case tokContinue:
		return "continue"
	case tokIs:
		return "is"
	case tokAs:
		return "as"
	case tokFrom:
		return "from"
	case tokThis:
		return "this"
	case tokNew:
		return "new"
	case tokTrue:
		return "true"
	case tokFalse:
		return "false"
	case tokNull:
		return "null"
	case tokSuper:
		return "super"
	case tokConstructor:
		return "constructor"
	case tokOpen:
		return "open"
	case tokOverride:
		return "override"
	case tokInterface:
		return "interface"
	case tokPublic:
		return "public"
	case tokPrivate:
		return "private"
	case tokStatic:
		return "static"
	case tokStream:
		return "stream"
	case tokYield:
		return "yield"
	case tokAsync:
		return "async"
	case tokAwait:
		return "await"
	case tokOptional:
		return "optional"
	case tokKey:
		return "key"
	case tokTry:
		return "try"
	case tokCatch:
		return "catch"
	case tokDefer:
		return "defer"
	case tokPlus:
		return "+"
	case tokMinus:
		return "-"
	case tokStar:
		return "*"
	case tokSlash:
		return "/"
	case tokPercent:
		return "%"
	case tokEq:
		return "="
	case tokEqEq:
		return "=="
	case tokBangEq:
		return "!="
	case tokLT:
		return "<"
	case tokLTEq:
		return "<="
	case tokGT:
		return ">"
	case tokGTEq:
		return ">="
	case tokAnd:
		return "&&"
	case tokOr:
		return "||"
	case tokBang:
		return "!"
	case tokQuestion:
		return "?"
	case tokQuestionQuestion:
		return "??"
	case tokQuestionDot:
		return "?."
	case tokColon:
		return ":"
	case tokDot:
		return "."
	case tokDotDot:
		return ".."
	case tokComma:
		return ","
	case tokSemicolon:
		return ";"
	case tokArrow:
		return "->"
	case tokFatArrow:
		return "=>"
	case tokAt:
		return "@"
	case tokLParen:
		return "("
	case tokRParen:
		return ")"
	case tokLBrace:
		return "{"
	case tokRBrace:
		return "}"
	case tokLBracket:
		return "["
	case tokRBracket:
		return "]"
	default:
		return "Unknown"
	}
}

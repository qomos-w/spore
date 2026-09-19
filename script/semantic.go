package script

import (
	"github.com/qomos-w/spore/internal/script/frontend"
)

// SemanticTokenType encodes a VS Code SemanticTokensLegend token type.
// Integer values match the VS Code protocol so consumers can use them directly.
type SemanticTokenType int

const (
	SemanticTokenNamespace SemanticTokenType = iota // 0
	SemanticTokenTypeType                           // 1
	SemanticTokenTypeClass                          // 2
	SemanticTokenTypeEnum                           // 3 — reserved
	SemanticTokenTypeInterface                      // 4
	SemanticTokenTypeStruct                         // 5
	SemanticTokenTypeTypeParameter                  // 6
	SemanticTokenTypeParameter                      // 7
	SemanticTokenTypeVariable                       // 8
	SemanticTokenTypeProperty                       // 9
	SemanticTokenTypeEnumMember                     // 10 — reserved
	SemanticTokenTypeEvent                          // 11 — reserved
	SemanticTokenTypeFunction                       // 12
	SemanticTokenTypeMethod                         // 13
	SemanticTokenTypeMacro                          // 14 — reserved
	SemanticTokenTypeKeyword                        // 15
	SemanticTokenTypeModifier                       // 16
	SemanticTokenTypeComment                        // 17 — reserved
	SemanticTokenTypeString                         // 18
	SemanticTokenTypeNumber                         // 19
	SemanticTokenTypeRegexp                         // 20 — reserved
	SemanticTokenTypeOperator                       // 21
	SemanticTokenTypeDecorator                      // 22 — reserved
)

// SemanticTokenModifier is a bitmask of VS Code SemanticTokensLegend modifiers.
type SemanticTokenModifier int

const (
	SemanticModifierDeclaration    SemanticTokenModifier = 1 << 0
	SemanticModifierDefinition     SemanticTokenModifier = 1 << 1
	SemanticModifierReadonly       SemanticTokenModifier = 1 << 2
	SemanticModifierStatic         SemanticTokenModifier = 1 << 3
	SemanticModifierDeprecated     SemanticTokenModifier = 1 << 4
	SemanticModifierAbstract       SemanticTokenModifier = 1 << 5
	SemanticModifierAsync          SemanticTokenModifier = 1 << 6
	SemanticModifierModification   SemanticTokenModifier = 1 << 7
	SemanticModifierDocumentation  SemanticTokenModifier = 1 << 8
	SemanticModifierDefaultLibrary SemanticTokenModifier = 1 << 9
)

// SemanticToken represents a single semantic highlighting token.
// Line and Col are 0-based (VS Code protocol convention).
// Length is the character length of the token text.
type SemanticToken struct {
	Line      int
	Col       int
	Length    int
	Type      SemanticTokenType
	Modifiers SemanticTokenModifier
}

// Analyze returns semantic highlighting tokens for the given Spore source.
// It parses the source and walks the AST to classify each identifier and literal.
// If the source has parse errors, Analyze returns partial tokens covering the
// successfully parsed portion, along with the parse error.
// Analyze does not require a Runtime and performs no code generation.
func Analyze(source string) ([]SemanticToken, error) {
	internal, err := frontend.AnalyzeSemanticTokens(source)
	if len(internal) == 0 {
		return nil, err
	}
	tokens := make([]SemanticToken, len(internal))
	for i, t := range internal {
		tokens[i] = SemanticToken{
			Line:      t.Line,
			Col:       t.Col,
			Length:    t.Length,
			Type:      SemanticTokenType(t.TokenType),
			Modifiers: SemanticTokenModifier(t.Modifiers),
		}
	}
	return tokens, err
}

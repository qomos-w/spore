package frontend

import (
	"sort"
)

// semanticTokenResult is the internal counterpart of script.SemanticToken.
// TokenType and Modifiers use int to avoid depending on the public package.
type semanticTokenResult struct {
	Line      int
	Col       int
	Length    int
	TokenType int
	Modifiers int
}

// --- Semantic token type constants (matching VS Code SemanticTokensLegend) ---

const (
	stNamespace     = 0
	stType          = 1
	stClass         = 2
	stEnum          = 3
	stInterface     = 4
	stStruct        = 5
	stTypeParameter = 6
	stParameter     = 7
	stVariable      = 8
	stProperty      = 9
	stFunction      = 12
	stMethod        = 13
	stKeyword       = 15
	stModifier      = 16
	stString        = 18
	stNumber        = 19
	stOperator      = 21
	stEnumMember    = 22
)

const (
	modDeclaration = 1 << 0
	modStatic      = 1 << 3
)

// --- Token index: scan all tokens from source for position lookup ---

type tokenIndex struct {
	tokens []token
}

func buildTokenIndex(source string) *tokenIndex {
	l := newLexer(source)
	var tokens []token
	for {
		t := l.nextToken()
		tokens = append(tokens, t)
		if t.typ == tokEOF || t.typ == tokError {
			break
		}
	}
	return &tokenIndex{tokens: tokens}
}

func (idx *tokenIndex) nextIdentAfter(line, col int) token {
	for _, t := range idx.tokens {
		if t.typ == tokIdent && (t.line > line || (t.line == line && t.col > col)) {
			return t
		}
	}
	return token{}
}

func (idx *tokenIndex) nextIdentOrKeywordAfter(line, col int) token {
	for _, t := range idx.tokens {
		if (t.typ == tokIdent || isTypeKeyword(t.typ)) && (t.line > line || (t.line == line && t.col > col)) {
			return t
		}
	}
	return token{}
}

func isTypeKeyword(typ tokenType) bool {
	switch typ {
	case tokVoid, tokAny, tokBool, tokByte, tokShort, tokUShort, tokUint, tokLong, tokUlong, tokDouble, tokBytes, tokArray, tokMap, tokInt, tokFloat, tokString, tokMedia:
		return true
	}
	return false
}

// --- Lexer pass: keyword, operator, literal tokens ---

func lexerTokens(idx *tokenIndex) []semanticTokenResult {
	var tokens []semanticTokenResult
	for _, t := range idx.tokens {
		var tt int
		switch {
		case t.typ == tokIntLit || t.typ == tokFloatLit:
			tt = stNumber
		case t.typ == tokStringLit:
			tt = stString
		case isModifierKeyword(t.typ):
			tt = stModifier
		case isLanguageKeyword(t.typ):
			tt = stKeyword
		case isOperator(t.typ):
			tt = stOperator
		default:
			continue
		}
		tokens = append(tokens, semanticTokenResult{
			Line:      t.line - 1,
			Col:       t.col - 1,
			Length:    len(t.lexeme),
			TokenType: tt,
		})
	}
	return tokens
}

func isModifierKeyword(typ tokenType) bool {
	switch typ {
	case tokExport, tokOpen, tokOverride, tokPublic, tokPrivate, tokStatic, tokStream:
		return true
	}
	return false
}

func isLanguageKeyword(typ tokenType) bool {
	switch typ {
	case tokFun, tokClass, tokStruct, tokType, tokVar, tokPackage, tokImport,
		tokReturn, tokIf, tokElse, tokFor, tokWhile, tokIn, tokWhen, tokCase,
		tokBreak, tokContinue, tokIs, tokAs, tokFrom, tokThis, tokNew,
		tokTrue, tokFalse, tokNull, tokSuper, tokConstructor, tokInterface,
		tokYield, tokVoid, tokAny, tokBool, tokByte, tokShort, tokUShort,
		tokUint, tokLong, tokUlong, tokDouble, tokBytes, tokArray, tokMap,
		tokInt, tokFloat, tokString, tokMedia, tokAsync, tokAwait:
		return true
	}
	return false
}

func isOperator(typ tokenType) bool {
	switch typ {
	case tokPlus, tokMinus, tokStar, tokSlash, tokPercent,
		tokEq, tokEqEq, tokBangEq, tokLT, tokLTEq, tokGT, tokGTEq,
		tokAnd, tokOr, tokBang, tokQuestion, tokArrow:
		return true
	}
	return false
}

// --- Declaration table with scoping ---

type declKind int

const (
	declFunction declKind = iota
	declStruct
	declClass
	declInterface
	declTypeAlias
	declVariable
	declParameter
	declField
	declMethod
	declEnum
)

type declEntry struct {
	kind declKind
}

type declTable struct {
	scopes []map[string]declEntry
}

func newDeclTable() *declTable {
	return &declTable{scopes: []map[string]declEntry{{}}}
}

func (dt *declTable) push() {
	dt.scopes = append(dt.scopes, map[string]declEntry{})
}

func (dt *declTable) pop() {
	if len(dt.scopes) > 1 {
		dt.scopes = dt.scopes[:len(dt.scopes)-1]
	}
}

func (dt *declTable) add(name string, kind declKind) {
	dt.scopes[len(dt.scopes)-1][name] = declEntry{kind: kind}
}

func (dt *declTable) lookup(name string) (declKind, bool) {
	for i := len(dt.scopes) - 1; i >= 0; i-- {
		if e, ok := dt.scopes[i][name]; ok {
			return e.kind, true
		}
	}
	return 0, false
}

// --- Declaration collection (pass 1) ---

func (dt *declTable) collectProgram(prog *program) {
	if prog == nil {
		return
	}
	// Collect top-level type names first so they resolve in later passes.
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			dt.add(s.Name.Value, declStruct)
		case *classStmt:
			dt.add(s.Name.Value, declClass)
		case *interfaceStmt:
			dt.add(s.Name.Value, declInterface)
		case *typeAliasStmt:
			dt.add(s.Name.Value, declTypeAlias)
		case *enumStmt:
			dt.add(s.Name.Value, declEnum)
		}
	}
	// Collect all other declarations.
	for _, stmt := range prog.Stmts {
		dt.collectStmt(stmt)
	}
}

func (dt *declTable) collectStmt(stmt statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *funStmt:
		dt.add(s.Name.Value, declFunction)
		dt.push()
		for _, p := range s.Params {
			dt.add(p.Name.Value, declParameter)
		}
		if s.Body != nil {
			dt.collectBlock(s.Body)
		}
		dt.pop()
	case *varStmt:
		dt.add(s.Name.Value, declVariable)
	case *structStmt:
		for _, f := range s.Fields {
			dt.add(f.Name.Value, declField)
		}
	case *classStmt:
		dt.push()
		for _, f := range s.Fields {
			dt.add(f.Name.Value, declField)
		}
		for _, m := range s.Methods {
			dt.add(m.Name.Value, declMethod)
			dt.push()
			for _, p := range m.Params {
				dt.add(p.Name.Value, declParameter)
			}
			if m.Body != nil {
				dt.collectBlock(m.Body)
			}
			dt.pop()
		}
		dt.pop()
	case *interfaceStmt:
		for _, ms := range s.Methods {
			dt.add(ms.Name.Value, declMethod)
			if ms.Body != nil {
				dt.push()
				for _, p := range ms.Params {
					dt.add(p.Name.Value, declParameter)
				}
				dt.collectBlock(ms.Body)
				dt.pop()
			}
		}
	case *ifStmt:
		if s.Consequence != nil {
			dt.push()
			dt.collectBlock(s.Consequence)
			dt.pop()
		}
		dt.collectStmt(s.Alternative)
	case *whileStmt:
		dt.push()
		if s.Body != nil {
			dt.collectBlock(s.Body)
		}
		dt.pop()
	case *forStmt:
		dt.push()
		if s.IsForIn && s.Variable != "" {
			dt.add(s.Variable, declVariable)
		}
		dt.collectStmt(s.Init)
		if s.Body != nil {
			dt.collectBlock(s.Body)
		}
		dt.pop()
	case *whenStmt:
		for _, c := range s.Cases {
			if c.Variable != "" {
				dt.add(c.Variable, declVariable)
			}
			if c.Body != nil {
				dt.push()
				dt.collectBlock(c.Body)
				dt.pop()
			}
		}
		if s.DefaultCase != nil {
			dt.push()
			dt.collectBlock(s.DefaultCase)
			dt.pop()
		}
	case *blockStmt:
		dt.push()
		dt.collectBlock(s)
		dt.pop()
	case *exprStatement:
		dt.collectExpr(s.Expr)
	}
}

func (dt *declTable) collectBlock(b *blockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		dt.collectStmt(s)
	}
}

func (dt *declTable) collectExpr(expr expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *callExpr:
		dt.collectExpr(e.Callee)
		for _, a := range e.Arguments {
			dt.collectExpr(a)
		}
	case *memberExpr:
		dt.collectExpr(e.Object)
	case *nullCoalesceExpr:
		dt.collectExpr(e.Left)
		dt.collectExpr(e.Right)
	case *optionalChainExpr:
		dt.collectExpr(e.Expr)
	case *binaryExpr:
		dt.collectExpr(e.Left)
		dt.collectExpr(e.Right)
	case *unaryExpr:
		dt.collectExpr(e.Right)
	case *assignExpr:
		dt.collectExpr(e.Target)
		dt.collectExpr(e.Value)
	case *typeCastExpr:
		dt.collectExpr(e.Left)
	case *typeCheckExpr:
		dt.collectExpr(e.Left)
	case *newExpr:
		for _, a := range e.Arguments {
			dt.collectExpr(a)
		}
	case *indexExpr:
		dt.collectExpr(e.Left)
		dt.collectExpr(e.Index)
	case *arrayLiteral:
		for _, el := range e.Elements {
			dt.collectExpr(el)
		}
	case *mapLiteral:
		for _, p := range e.Pairs {
			dt.collectExpr(p.Key)
			dt.collectExpr(p.Value)
		}
	case *structLiteral:
		for _, f := range e.Fields {
			dt.collectExpr(f.Value)
		}
	case *lambdaExpr:
		dt.push()
		for _, prm := range e.Params {
			dt.add(prm.Name.Value, declParameter)
		}
		if e.Body != nil {
			dt.collectBlock(e.Body)
		}
		dt.collectExpr(e.ExprBody)
		dt.pop()
	}
}

// --- AST token collector (pass 2) ---

type astTokenCollector struct {
	tokens    []semanticTokenResult
	tokIdx    *tokenIndex
	declTable *declTable
}

func (c *astTokenCollector) emit(line, col, length, tokenType, modifiers int) {
	c.tokens = append(c.tokens, semanticTokenResult{
		Line:      line - 1,
		Col:       col - 1,
		Length:    length,
		TokenType: tokenType,
		Modifiers: modifiers,
	})
}

func (c *astTokenCollector) emitIdent(id *ident, tokenType, modifiers int) {
	if id == nil {
		return
	}
	origLine, origCol := id.tok.line, id.tok.col
	line, col := origLine, origCol
	if t := c.tokIdx.tokenAt(line, col); t.typ != 0 && t.lexeme != id.Value {
		found := c.tokIdx.findIdent(id.Value, line, col)
		if found.typ != 0 {
			line, col = found.line, found.col
		}
	}
	if id == nil {
		return
	}
	line, col = id.tok.line, id.tok.col
	// Some parser nodes store p.cur (post-advance) instead of the actual
	// ident token. Detect this by checking whether the token at (line,col)
	// in the index matches the ident's name.
	if t := c.tokIdx.tokenAt(line, col); t.typ != 0 && t.lexeme != id.Value {
		if t := c.tokIdx.findIdent(id.Value, line, col); t.typ != 0 {
			line, col = t.line, t.col
		}
	}
	c.emit(line, col, len(id.Value), tokenType, modifiers)
}

func (idx *tokenIndex) tokenAt(line, col int) token {
	for _, t := range idx.tokens {
		if t.line == line && t.col == col {
			return t
		}
	}
	return token{}
}

func (idx *tokenIndex) findIdent(name string, nearLine, nearCol int) token {
	var best token
	for _, t := range idx.tokens {
		if (t.typ == tokIdent || isTypeKeyword(t.typ)) && t.lexeme == name {
			if best.typ == 0 || t.line < best.line || (t.line == best.line && t.col < best.col) {
				best = t
			}
		}
	}
	return best
}

func (c *astTokenCollector) emitTypeAnnotation(ta *typeAnnotation, isParam bool) {
	if ta == nil {
		return
	}
	tt := stType
	if isParam {
		tt = stTypeParameter
	}
	if ta.IsFun {
		// Function type `fun(T1, T2): R` — emit the `fun` keyword and walk
		// the parameter/return type annotations.
		c.emit(ta.tok.line, ta.tok.col, len(ta.Name), tt, 0)
		for _, p := range ta.FunParams {
			c.emitTypeAnnotation(p, true)
		}
		c.emitTypeAnnotation(ta.FunReturn, false)
		return
	}
	if isTypeKeyword(ta.tok.typ) || ta.tok.typ == tokIdent {
		c.emit(ta.tok.line, ta.tok.col, len(ta.Name), tt, 0)
	}
	for _, p := range ta.Params {
		c.emitTypeAnnotation(p, true)
	}
}

func (c *astTokenCollector) collectProgram(prog *program) {
	if prog == nil {
		return
	}
	// Import names as namespace tokens.
	for _, imp := range prog.imports {
		c.emitImportStmt(imp)
	}
	// Top-level declarations.
	for _, stmt := range prog.Stmts {
		c.collectTopLevelStmt(stmt)
	}
}

func (c *astTokenCollector) emitImportStmt(imp *importStmt) {
	if imp == nil {
		return
	}
	if imp.Name != "" {
		t := c.tokIdx.nextIdentAfter(imp.tok.line, imp.tok.col)
		if t.typ == tokIdent || isTypeKeyword(t.typ) {
			c.emit(t.line, t.col, len(t.lexeme), stNamespace, 0)
		}
	}
}

func (c *astTokenCollector) collectTopLevelStmt(stmt statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *funStmt:
		c.emitIdent(s.Name, stFunction, modDeclaration)
		for _, p := range s.Params {
			c.emitIdent(p.Name, stParameter, modDeclaration)
			c.emitTypeAnnotation(p.Type_, false)
		}
		if s.ReturnType != nil {
			c.emitTypeAnnotation(s.ReturnType, false)
		}
		c.collectBlock(s.Body)
	case *structStmt:
		c.emitIdent(s.Name, stStruct, modDeclaration)
		for _, f := range s.Fields {
			c.emitIdent(f.Name, stProperty, modDeclaration)
			c.emitTypeAnnotation(f.Type_, false)
		}
	case *classStmt:
		c.emitIdent(s.Name, stClass, modDeclaration)
		if s.Parent != nil {
			c.emitIdent(s.Parent, stClass, 0)
		}
		for _, impl := range s.Implements {
			c.emitIdent(impl, stInterface, 0)
		}
		for _, f := range s.Fields {
			c.emitIdent(f.Name, stProperty, modDeclaration)
			c.emitTypeAnnotation(f.Type_, false)
		}
		for _, m := range s.Methods {
			c.emitIdent(m.Name, stMethod, modDeclaration)
			for _, p := range m.Params {
				c.emitIdent(p.Name, stParameter, modDeclaration)
				c.emitTypeAnnotation(p.Type_, false)
			}
			if m.ReturnType != nil {
				c.emitTypeAnnotation(m.ReturnType, false)
			}
			c.collectBlock(m.Body)
		}
	case *interfaceStmt:
		c.emitIdent(s.Name, stInterface, modDeclaration)
		for _, ms := range s.Methods {
			c.emitIdent(ms.Name, stMethod, modDeclaration)
			for _, p := range ms.Params {
				c.emitIdent(p.Name, stParameter, modDeclaration)
				c.emitTypeAnnotation(p.Type_, false)
			}
			if ms.ReturnType != nil {
				c.emitTypeAnnotation(ms.ReturnType, false)
			}
			c.collectBlock(ms.Body)
		}
	case *varStmt:
		c.emitIdent(s.Name, stVariable, modDeclaration)
		c.emitTypeAnnotation(s.Type_, false)
		c.collectExpr(s.Value)
	case *typeAliasStmt:
		c.emitIdent(s.Name, stType, modDeclaration)
		c.emitTypeAnnotation(s.Alias, false)
	case *enumStmt:
		c.emitIdent(s.Name, stEnum, modDeclaration)
		for _, m := range s.Members {
			if m != nil && m.Name != nil {
				c.emitIdent(m.Name, stEnumMember, modDeclaration)
			}
		}
	}
}

func (c *astTokenCollector) collectBlock(b *blockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		c.collectStmt(s)
	}
}

func (c *astTokenCollector) collectStmt(stmt statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *varStmt:
		c.emitIdent(s.Name, stVariable, modDeclaration)
		c.emitTypeAnnotation(s.Type_, false)
		c.collectExpr(s.Value)
		c.collectExpr(s.InitExpr)
	case *ifStmt:
		c.collectExpr(s.Condition)
		c.collectBlock(s.Consequence)
		c.collectStmt(s.Alternative)
	case *whileStmt:
		c.collectExpr(s.Condition)
		c.collectBlock(s.Body)
	case *forStmt:
		c.collectStmt(s.Init)
		c.collectExpr(s.Condition)
		c.collectExpr(s.Update)
		if s.IsForIn && s.Variable != "" {
			t := c.tokIdx.nextIdentAfter(s.tok.line, s.tok.col)
			if t.typ == tokIdent {
				c.emit(t.line, t.col, len(t.lexeme), stVariable, modDeclaration)
			}
		}
		c.collectExpr(s.Iterable)
		c.collectBlock(s.Body)
	case *whenStmt:
		c.collectExpr(s.Expr)
		for _, cc := range s.Cases {
			for _, v := range cc.Values {
				c.collectExpr(v)
			}
			if cc.TypeAnnotation != nil {
				c.emitTypeAnnotation(cc.TypeAnnotation, false)
			}
			if cc.Variable != "" {
				t := c.tokIdx.nextIdentAfter(cc.tok.line, cc.tok.col)
				if t.typ == tokIdent {
					c.emit(t.line, t.col, len(t.lexeme), stVariable, modDeclaration)
				}
			}
			c.collectExpr(cc.Guard)
			c.collectBlock(cc.Body)
		}
		c.collectBlock(s.DefaultCase)
	case *returnStmt:
		c.collectExpr(s.Value)
	case *yieldStmt:
		c.collectExpr(s.Value)
	case *breakStmt, *continueStmt:
		// No tokens to emit.
	case *exprStatement:
		c.collectExpr(s.Expr)
	case *blockStmt:
		c.collectBlock(s)
	}
}

func (c *astTokenCollector) collectExpr(expr expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *identExpr:
		c.collectIdentExpr(e)
	case *callExpr:
		c.collectCallExpr(e)
	case *memberExpr:
		// Handled inside collectCallExpr for method calls; here it's a property access.
		c.emitIdent(e.Member, stProperty, 0)
		c.collectExpr(e.Object)
	case *nullCoalesceExpr:
		c.collectExpr(e.Left)
		c.collectExpr(e.Right)
	case *optionalChainExpr:
		c.collectExpr(e.Expr)
	case *binaryExpr:
		c.collectExpr(e.Left)
		c.collectExpr(e.Right)
	case *unaryExpr:
		c.collectExpr(e.Right)
	case *assignExpr:
		c.collectExpr(e.Target)
		c.collectExpr(e.Value)
	case *typeCheckExpr:
		c.collectExpr(e.Left)
		t := c.tokIdx.nextIdentOrKeywordAfter(e.tok.line, e.tok.col)
		if t.typ != 0 {
			c.emit(t.line, t.col, len(t.lexeme), stType, 0)
		}
	case *typeCastExpr:
		c.collectExpr(e.Left)
		c.emitTypeAnnotation(e.Target, false)
	case *newExpr:
		t := c.tokIdx.nextIdentOrKeywordAfter(e.tok.line, e.tok.col)
		if t.typ != 0 {
			c.emit(t.line, t.col, len(t.lexeme), stClass, 0)
		}
		for _, a := range e.Arguments {
			c.collectExpr(a)
		}
	case *structLiteral:
		// The struct literal tok is the type name token.
		c.emit(e.tok.line, e.tok.col, len(e.TypeName), stStruct, 0)
		for _, f := range e.Fields {
			c.collectExpr(f.Value)
		}
	case *indexExpr:
		c.collectExpr(e.Left)
		c.collectExpr(e.Index)
	case *arrayLiteral:
		for _, el := range e.Elements {
			c.collectExpr(el)
		}
	case *mapLiteral:
		for _, p := range e.Pairs {
			c.collectExpr(p.Key)
			c.collectExpr(p.Value)
		}
	case *thisExpr, *superExpr:
		// Handled by lexer pass (keyword).
	case *lambdaExpr:
		for _, prm := range e.Params {
			c.emitIdent(prm.Name, stParameter, modDeclaration)
			c.emitTypeAnnotation(prm.Type_, true)
		}
		c.emitTypeAnnotation(e.ReturnType, false)
		if e.Body != nil {
			c.collectBlock(e.Body)
		}
		c.collectExpr(e.ExprBody)
	case *intLiteral, *floatLiteral, *stringLiteral, *boolLiteral, *nullLiteral:
		// Handled by lexer pass (literal/keyword).
	}
}

func (c *astTokenCollector) collectIdentExpr(e *identExpr) {
	kind, ok := c.declTable.lookup(e.Value)
	if !ok {
		return // let lexer handle it as keyword or skip unknown
	}
	switch kind {
	case declFunction:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stFunction, 0)
	case declVariable:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stVariable, 0)
	case declParameter:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stParameter, 0)
	case declField, declMethod:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stProperty, 0)
	case declStruct:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stStruct, 0)
	case declClass:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stClass, 0)
	case declInterface:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stInterface, 0)
	case declTypeAlias:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stType, 0)
	case declEnum:
		c.emit(e.tok.line, e.tok.col, len(e.Value), stEnum, 0)
	}
}

func (c *astTokenCollector) collectCallExpr(e *callExpr) {
	if me, ok := e.Callee.(*memberExpr); ok {
		c.emitIdent(me.Member, stMethod, 0)
		c.collectExpr(me.Object)
	} else {
		c.collectExpr(e.Callee)
	}
	for _, a := range e.Arguments {
		c.collectExpr(a)
	}
}

// --- Merge and sort ---

type posKey struct {
	line, col int
}

func mergeTokens(lexerTokens, astTokens []semanticTokenResult) []semanticTokenResult {
	m := make(map[posKey]semanticTokenResult, len(lexerTokens)+len(astTokens))
	for _, t := range lexerTokens {
		m[posKey{t.Line, t.Col}] = t
	}
	for _, t := range astTokens {
		m[posKey{t.Line, t.Col}] = t
	}
	result := make([]semanticTokenResult, 0, len(m))
	for _, t := range m {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Col < result[j].Col
	})
	return result
}

// --- Main entry ---

func walkForSemanticTokens(source string, prog *program) []semanticTokenResult {
	idx := buildTokenIndex(source)

	dt := newDeclTable()
	dt.collectProgram(prog)

	lexTokens := lexerTokens(idx)

	collector := &astTokenCollector{
		tokIdx:    idx,
		declTable: dt,
	}
	collector.collectProgram(prog)

	return mergeTokens(lexTokens, collector.tokens)
}

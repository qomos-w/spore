// compiler_function.go compiles functions, lambdas and closures, including the capture analysis that determines closure environments.

package bytecode

import (
	"fmt"
	"sort"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// --- Function compilation ---

func (c *compiler) compileFunDecl(fn funDecl) {
	funcName := fn.name

	// Save current compiler state.
	savedChunk := c.chunk
	savedLocals := c.locals
	savedDepth := c.scopeDepth
	savedReturnHint := c.returnTypeHint
	savedStreamFun := c.currentStreamFun
	savedCapturedNames := c.capturedNames
	savedCapturedOrder := c.capturedOrder
	savedHandlerDepth := c.handlerDepth
	savedInDeferBody := c.inDeferBody

	// Create a new chunk for this function.
	c.chunk = newChunk()
	c.chunk.sourceName = funcName
	c.locals = make([]local, 0)
	c.peakLocals = 0
	c.scopeDepth = 1
	c.handlerDepth = 0
	c.inDeferBody = 0

	// Pre-compute which locals/params are captured by lambdas in the body.
	c.capturedNames, c.capturedOrder = computeFunctionCaptures(fn.paramNames, fn.body, false, nil)

	// Register parameters as locals at function scope.
	for i, name := range fn.paramNames {
		typeName := ""
		if i < len(fn.paramTypes) {
			typeName = c.resolveType(fn.paramTypes[i])
		}
		c.addLocalWithType(name, typeName)
	}
	// Parameters captured by lambdas must be boxed into capture cells at
	// entry so mutations stay shared between the function and its closures.
	c.boxCapturedParams(fn.paramNames)
	c.returnTypeHint = ""
	c.currentStreamFun = fn.isStream
	if resolved := c.resolveType(fn.returnType); resolved == "long" || resolved == "ulong" || resolved == "double" {
		c.returnTypeHint = resolved
	}

	// Compile the body.
	if len(fn.body.stmts) > 0 {
		c.compileBlock(fn.body)
	} else if fn.exprBody != nil {
		// Expression body: fun f(): int = expr → compile as return <expr>.
		prevHint := c.typeHint
		resolved := c.resolveType(fn.returnType)
		if resolved == "long" || resolved == "ulong" || resolved == "double" {
			c.typeHint = resolved
		} else {
			c.typeHint = ""
		}
		c.compileExpression(fn.exprBody)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
		// Store the function chunk.
		c.functions[funcName] = c.chunk
		c.funcInfo[funcName] = &functionInfo{
			name:       funcName,
			paramCount: len(fn.paramNames),
			paramTypes: append([]string(nil), fn.paramTypes...),
			localCount: c.peakLocals,
			returnType: fn.returnType,
		}
		c.chunk.LocalCount = c.peakLocals
		c.chunk = savedChunk
		c.locals = savedLocals
		c.scopeDepth = savedDepth
		c.returnTypeHint = savedReturnHint
		c.currentStreamFun = savedStreamFun
		c.capturedNames = savedCapturedNames
		c.capturedOrder = savedCapturedOrder
		c.handlerDepth = savedHandlerDepth
		c.inDeferBody = savedInDeferBody
		return
	}

	// Ensure there's a return at the end.
	c.emit(opReturnVoid, 0, c.curLine)
	// Store the function chunk.
	c.functions[funcName] = c.chunk
	c.funcInfo[funcName] = &functionInfo{
		name:       funcName,
		paramCount: len(fn.paramNames),
		paramTypes: append([]string(nil), fn.paramTypes...),
		localCount: c.peakLocals,
		returnType: fn.returnType,
	}
	c.chunk.LocalCount = c.peakLocals

	// Restore compiler state.
	c.chunk = savedChunk
	c.locals = savedLocals
	c.scopeDepth = savedDepth
	c.returnTypeHint = savedReturnHint
	c.currentStreamFun = savedStreamFun
	c.capturedNames = savedCapturedNames
	c.capturedOrder = savedCapturedOrder
	c.handlerDepth = savedHandlerDepth
	c.inDeferBody = savedInDeferBody
}

// boxCapturedParams wraps parameters that are captured by lambdas into
// capture cells at function entry. Must run after params were registered
// and before the body is compiled.
func (c *compiler) boxCapturedParams(paramNames []string) {
	for _, name := range paramNames {
		if !c.capturedNames[name] {
			continue
		}
		idx := c.resolveLocal(name)
		if idx < 0 {
			continue
		}
		c.emit(opLoadLocal, int32(idx), c.curLine)
		c.emit(opMakeCell, 0, c.curLine)
		c.emit(opStoreLocal, int32(idx), c.curLine)
		c.locals[idx].isCell = true
	}
}

// --- Lambda / closure compilation ---

// compileLambdaExpr compiles `fun(params): T { ... }` used as an expression.
// The lambda body becomes an internal chunk (named lambda$N, never exported);
// captured enclosing locals are shared through capture cells.
func (c *compiler) compileLambdaExpr(e *frontend.LambdaExpr) {
	lambdaName := fmt.Sprintf("lambda$%d", c.lambdaCounter)
	c.lambdaCounter++

	paramNames := make([]string, len(e.Params))
	paramTypes := make([]string, len(e.Params))
	rawParamTypes := make([]string, len(e.Params))
	for i, p := range e.Params {
		paramNames[i] = p.Name.Value
		if p.Type_ != nil {
			rawParamTypes[i] = typeAnnotationName(p.Type_)
			paramTypes[i] = c.resolveType(rawParamTypes[i])
		}
	}

	var body blockDecl
	if e.Body != nil {
		body = blockDeclFromBlock(e.Body)
	}

	// Free names of the lambda body relative to the lambda's own scope. These
	// resolve either to the enclosing frame (captures) or to this lambda's own
	// params/locals (which must then be boxed for deeper closures).
	ownDeclared := make(map[string]bool)
	for _, p := range paramNames {
		ownDeclared[p] = true
	}
	collectDeclaredNamesBlock(body, ownDeclared)
	ownFree := make(map[string]bool)
	freeNamesBlock(body, ownDeclared, ownFree)
	if e.ExprBody != nil {
		freeNamesExpr(e.ExprBody, ownDeclared, ownFree)
	}

	// Names this lambda's own frame must box: free names that are declared in
	// this lambda (captured by nested lambdas). `this` is boxed at
	// closure-creation time instead, so it is excluded.
	boxSet := make(map[string]bool)
	for name := range ownFree {
		if name != "this" && ownDeclared[name] {
			boxSet[name] = true
		}
	}

	// Captures from the enclosing frame: free names resolvable as locals at
	// the closure-creation site (or `this` inside a method).
	captureOrder := make([]string, 0, len(ownFree))
	for name := range ownFree {
		if name == "this" {
			if c.currentClassName != "" && c.resolveLocal("this") >= 0 {
				captureOrder = append(captureOrder, name)
			}
			continue
		}
		if c.resolveLocal(name) >= 0 {
			captureOrder = append(captureOrder, name)
		}
	}
	sort.Strings(captureOrder)

	// Resolve capture types from the enclosing scope before switching chunks.
	captureTypes := make([]string, len(captureOrder))
	for i, name := range captureOrder {
		captureTypes[i] = ""
		if name == "this" {
			captureTypes[i] = c.currentClassName
			continue
		}
		if idx := c.resolveLocal(name); idx >= 0 {
			captureTypes[i] = c.locals[idx].typeName
		}
	}

	// Save compiler state.
	savedChunk := c.chunk
	savedLocals := c.locals
	savedDepth := c.scopeDepth
	savedPeak := c.peakLocals
	savedReturnHint := c.returnTypeHint
	savedStreamFun := c.currentStreamFun
	savedCapturedNames := c.capturedNames
	savedCapturedOrder := c.capturedOrder
	savedCurrentClass := c.currentClassName
	savedHandlerDepth := c.handlerDepth
	savedInDeferBody := c.inDeferBody

	// Compile the lambda into its own chunk.
	c.chunk = newChunk()
	c.chunk.sourceName = lambdaName
	c.locals = make([]local, 0)
	c.peakLocals = 0
	c.scopeDepth = 1
	c.handlerDepth = 0
	c.inDeferBody = 0
	c.capturedNames = boxSet
	c.capturedOrder = nil
	// Keep the enclosing class context so `this.field` expressions inside the
	// lambda resolve field types (typed arithmetic, method resolution). `this`
	// itself still arrives as a capture cell, not as a receiver local.
	c.currentClassName = savedCurrentClass
	c.currentStreamFun = false
	c.returnTypeHint = ""
	returnTypeName := ""
	if e.ReturnType != nil {
		returnTypeName = c.resolveType(typeAnnotationName(e.ReturnType))
		if returnTypeName == "long" || returnTypeName == "ulong" || returnTypeName == "double" {
			c.returnTypeHint = returnTypeName
		}
	}

	// Layout: declared params first, then capture-cell locals. The runtime
	// appends capture cells after the declared arguments on every call.
	for i, name := range paramNames {
		c.addLocalWithType(name, paramTypes[i])
	}
	for i, name := range captureOrder {
		idx := c.addLocalWithType(name, captureTypes[i])
		c.locals[idx].isCell = true
	}
	// Parameters captured by nested lambdas are boxed into cells at entry.
	c.boxCapturedParams(paramNames)

	if len(body.stmts) > 0 {
		c.compileBlock(body)
	} else if e.ExprBody != nil {
		prevHint := c.typeHint
		if returnTypeName == "long" || returnTypeName == "ulong" || returnTypeName == "double" {
			c.typeHint = returnTypeName
		} else {
			c.typeHint = ""
		}
		c.compileExpression(e.ExprBody)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
	} else {
		c.emit(opReturnVoid, 0, c.curLine)
	}

	c.functions[lambdaName] = c.chunk
	c.funcInfo[lambdaName] = &functionInfo{
		name:       lambdaName,
		paramCount: len(paramNames) + len(captureOrder),
		paramTypes: rawParamTypes,
		localCount: c.peakLocals,
		returnType: returnTypeName,
	}
	c.chunk.LocalCount = c.peakLocals

	// Restore compiler state.
	c.chunk = savedChunk
	c.locals = savedLocals
	c.scopeDepth = savedDepth
	c.peakLocals = savedPeak
	c.returnTypeHint = savedReturnHint
	c.currentStreamFun = savedStreamFun
	c.capturedNames = savedCapturedNames
	c.capturedOrder = savedCapturedOrder
	c.currentClassName = savedCurrentClass
	c.handlerDepth = savedHandlerDepth
	c.inDeferBody = savedInDeferBody

	// Emit closure creation in the enclosing chunk: push the capture cells
	// (same order as the lambda chunk layout), then bind them to the chunk.
	funcRefID := savedChunk.addFunctionRef(lambdaName, c.functions[lambdaName])
	for _, name := range captureOrder {
		if name == "this" {
			thisIdx := c.resolveLocal("this")
			if thisIdx < 0 {
				c.addCompileError("undefined_variable", "bytecode/scope/closure", fmt.Sprintf("lambda captures 'this' outside of a method"))
				return
			}
			c.emit(opLoadLocal, int32(thisIdx), c.curLine)
			c.emit(opMakeCell, 0, c.curLine)
			continue
		}
		idx := c.resolveLocal(name)
		if idx < 0 {
			c.addCompileError("undefined_variable", "bytecode/scope/closure", fmt.Sprintf("lambda captures variable %q before its declaration", name))
			return
		}
		c.emit(opLoadLocal, int32(idx), c.curLine)
		if !c.locals[idx].isCell {
			// Capture analysis said this name is captured, but the enclosing
			// local was not boxed (e.g. `this`). Box the raw value now.
			c.emit(opMakeCell, 0, c.curLine)
		}
	}
	operand := int32(uint32(funcRefID)&0xFFFF | ((uint32(len(captureOrder)) & 0xFFFF) << 16))
	c.emit(opMakeClosure, operand, c.curLine)
}

// --- Capture analysis ---

// computeFunctionCaptures analyzes a function body and returns the set (and a
// deterministic order) of names that lambdas inside the body capture from the
// enclosing function scope. Params and body locals are candidates; names that
// only resolve to globals/functions are not captured. extraCaptures lists
// names already known to be captures (e.g. the enclosing function's captured
// set, or `this` inside methods); those are captured too when referenced.
func computeFunctionCaptures(params []string, body blockDecl, isMethod bool, extraCaptures map[string]bool) (map[string]bool, []string) {
	declared := make(map[string]bool)
	for _, p := range params {
		declared[p] = true
	}
	if isMethod {
		declared["this"] = true
	}
	collectDeclaredNamesBlock(body, declared)

	free := make(map[string]bool)
	freeNamesStmt(stmtDecl{kind: stmtKindBlock, blk: body}, declared, free)

	captured := make(map[string]bool)
	for name := range free {
		// `this` never becomes a capture cell in the enclosing scope; it is
		// boxed at closure-creation time instead.
		if name == "this" {
			continue
		}
		if declared[name] {
			captured[name] = true
		}
	}
	for name := range extraCaptures {
		if free[name] {
			captured[name] = true
		}
	}
	order := make([]string, 0, len(captured))
	for name := range captured {
		order = append(order, name)
	}
	sort.Strings(order)
	return captured, order
}

// collectDeclaredNamesBlock adds every name bound inside the block (vars,
// for-loop variables, when-case variables) to out.
func collectDeclaredNamesBlock(block blockDecl, out map[string]bool) {
	for _, s := range block.stmts {
		collectDeclaredNames(s, out)
	}
}

func collectDeclaredNames(stmt stmtDecl, out map[string]bool) {
	switch stmt.kind {
	case stmtKindVar:
		if stmt.vr.name != "" {
			out[stmt.vr.name] = true
		}
	case stmtKindFor:
		if stmt.for_.isForIn {
			if stmt.for_.variable != "" {
				out[stmt.for_.variable] = true
			}
		} else if stmt.for_.init != nil {
			collectDeclaredNames(*stmt.for_.init, out)
		}
	case stmtKindWhen:
		// when-case type-match variables are not real locals at runtime
		// (never bound by compileWhenStmt); skip them.
	case stmtKindIf:
		collectDeclaredNamesBlock(stmt.if_.consequence, out)
		if stmt.if_.alternativeBlk != nil {
			collectDeclaredNamesBlock(*stmt.if_.alternativeBlk, out)
		}
		if stmt.if_.alternativeIf != nil {
			alt := stmtDecl{kind: stmtKindIf, if_: *stmt.if_.alternativeIf}
			collectDeclaredNames(alt, out)
		}
	case stmtKindWhile:
		collectDeclaredNamesBlock(stmt.whl.body, out)
	case stmtKindBlock:
		collectDeclaredNamesBlock(stmt.blk, out)
	case stmtKindTry:
		collectDeclaredNamesBlock(stmt.try_.body, out)
		if stmt.try_.catchVar != "" {
			out[stmt.try_.catchVar] = true
		}
		collectDeclaredNamesBlock(stmt.try_.catchBody, out)
	case stmtKindDefer:
		collectDeclaredNamesBlock(stmt.dfr.body, out)
	}
}

// freeNamesStmt collects free identifier names (reads or writes) reachable
// from stmt into out, ignoring names in bound.
func freeNamesStmt(stmt stmtDecl, bound map[string]bool, out map[string]bool) {
	switch stmt.kind {
	case stmtKindVar:
		freeNamesExpr(stmt.vr.value, bound, out)
	case stmtKindReturn:
		freeNamesExpr(stmt.ret.value, bound, out)
	case stmtKindYield:
		freeNamesExpr(stmt.yld.value, bound, out)
	case stmtKindExpr:
		freeNamesExpr(stmt.expr, bound, out)
	case stmtKindIf:
		freeNamesExpr(stmt.if_.condition, bound, out)
		freeNamesBlock(stmt.if_.consequence, bound, out)
		if stmt.if_.alternativeBlk != nil {
			freeNamesBlock(*stmt.if_.alternativeBlk, bound, out)
		}
		if stmt.if_.alternativeIf != nil {
			freeNamesStmt(stmtDecl{kind: stmtKindIf, if_: *stmt.if_.alternativeIf}, bound, out)
		}
	case stmtKindWhile:
		freeNamesExpr(stmt.whl.condition, bound, out)
		freeNamesBlock(stmt.whl.body, bound, out)
	case stmtKindFor:
		if stmt.for_.init != nil {
			freeNamesStmt(*stmt.for_.init, bound, out)
		}
		freeNamesExpr(stmt.for_.condition, bound, out)
		freeNamesExpr(stmt.for_.update, bound, out)
		freeNamesExpr(stmt.for_.iterable, bound, out)
		freeNamesBlock(stmt.for_.body, bound, out)
	case stmtKindWhen:
		freeNamesExpr(stmt.when.expr, bound, out)
		for _, cc := range stmt.when.cases {
			for _, val := range cc.values {
				freeNamesExpr(val, bound, out)
			}
			freeNamesExpr(cc.guard, bound, out)
			freeNamesBlock(cc.body, bound, out)
		}
		if stmt.when.defaultCase != nil {
			freeNamesBlock(*stmt.when.defaultCase, bound, out)
		}
	case stmtKindBlock:
		freeNamesBlock(stmt.blk, bound, out)
	case stmtKindTry:
		freeNamesBlock(stmt.try_.body, bound, out)
		freeNamesBlock(stmt.try_.catchBody, bound, out)
	case stmtKindDefer:
		freeNamesBlock(stmt.dfr.body, bound, out)
	case stmtKindBreak, stmtKindContinue:
		// nothing
	}
}

func freeNamesBlock(block blockDecl, bound map[string]bool, out map[string]bool) {
	for _, s := range block.stmts {
		freeNamesStmt(s, bound, out)
	}
}

// freeNamesExpr collects free identifier names from expr into out. Lambda
// sub-expressions are analyzed against their own parameter/local scope, so
// only names truly free inside the lambda propagate to out.
func freeNamesExpr(expr frontend.Expression, bound map[string]bool, out map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *frontend.IdentExpr:
		if !bound[e.Value] {
			out[e.Value] = true
		}
	case *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral,
		*frontend.BoolLiteral, *frontend.NullLiteral:
		// no free names
	case *frontend.ThisExpr:
		// `this` inside a lambda body is resolved via captures; freeNamesLambda
		// deliberately leaves "this" out of the lambda's bound set so it
		// propagates to the enclosing scope analysis.
		if !bound["this"] {
			out["this"] = true
		}
	case *frontend.BinaryExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Right, bound, out)
	case *frontend.UnaryExpr:
		freeNamesExpr(e.Right, bound, out)
	case *frontend.CallExpr:
		freeNamesExpr(e.Callee, bound, out)
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.MemberExpr:
		freeNamesExpr(e.Object, bound, out)
	case *frontend.NullCoalesceExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Right, bound, out)
	case *frontend.OptionalChainExpr:
		freeNamesExpr(e.Expr, bound, out)
	case *frontend.IndexExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Index, bound, out)
	case *frontend.AssignExpr:
		freeNamesExpr(e.Target, bound, out)
		freeNamesExpr(e.Value, bound, out)
	case *frontend.TypeCheckExpr:
		freeNamesExpr(e.Left, bound, out)
	case *frontend.TypeCastExpr:
		freeNamesExpr(e.Left, bound, out)
	case *frontend.ArrayLiteral:
		for _, el := range e.Elements {
			freeNamesExpr(el, bound, out)
		}
	case *frontend.MapLiteral:
		for _, p := range e.Pairs {
			freeNamesExpr(p.Key, bound, out)
			freeNamesExpr(p.Value, bound, out)
		}
	case *frontend.StructLiteral:
		for _, f := range e.Fields {
			freeNamesExpr(f.Value, bound, out)
		}
	case *frontend.NewExpr:
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.SuperExpr:
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.LambdaExpr:
		freeNamesLambda(e, bound, out)
	}
}

// freeNamesLambda analyzes a lambda's body against its own scope only (params
// + body locals). Enclosing-scope names intentionally stay free so they
// propagate up to the enclosing function's capture analysis. `this` is never
// part of the lambda's own scope, so method-receiver capture propagates too.
func freeNamesLambda(e *frontend.LambdaExpr, bound map[string]bool, out map[string]bool) {
	lambdaBound := make(map[string]bool, len(e.Params)+4)
	for _, p := range e.Params {
		lambdaBound[p.Name.Value] = true
	}
	if e.Body != nil {
		body := blockDeclFromBlock(e.Body)
		collectDeclaredNamesBlock(body, lambdaBound)
		freeNamesBlock(body, lambdaBound, out)
	}
	if e.ExprBody != nil {
		freeNamesExpr(e.ExprBody, lambdaBound, out)
	}
}

// compiler_function.go compiles functions, lambdas and closures, including the capture analysis that determines closure environments.

package bytecode

import (
	"fmt"
	"sort"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// --- Function compilation ---

func (c *compiler) compileFunDecl(fn *frontend.FunStmt) {
	funcName := fn.Name.Value
	paramNames := funParamNames(fn)
	paramTypes := funParamTypes(fn)
	returnType := funReturnType(fn)

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
	c.capturedNames, c.capturedOrder = computeFunctionCaptures(paramNames, fn.Body, false, nil)

	// Register parameters as locals at function scope.
	for i, name := range paramNames {
		typeName := ""
		if i < len(paramTypes) {
			typeName = c.resolveType(paramTypes[i])
		}
		c.addLocalWithType(name, typeName)
	}
	// Parameters captured by lambdas must be boxed into capture cells at
	// entry so mutations stay shared between the function and its closures.
	c.boxCapturedParams(paramNames)
	c.returnTypeHint = ""
	c.currentStreamFun = fn.IsStream
	if resolved := c.resolveType(returnType); resolved == "long" || resolved == "ulong" || resolved == "double" {
		c.returnTypeHint = resolved
	}

	// Compile the body.
	if fn.Body != nil && len(fn.Body.Stmts) > 0 {
		c.compileBlock(fn.Body)
	} else if fn.ExprBody != nil {
		// Expression body: fun f(): int = expr → compile as return <expr>.
		prevHint := c.typeHint
		resolved := c.resolveType(returnType)
		if resolved == "long" || resolved == "ulong" || resolved == "double" {
			c.typeHint = resolved
		} else {
			c.typeHint = ""
		}
		c.compileExpression(fn.ExprBody)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
		// Store the function chunk.
		c.functions[funcName] = c.chunk
		c.funcInfo[funcName] = &functionInfo{
			name:       funcName,
			paramCount: len(paramNames),
			paramTypes: append([]string(nil), paramTypes...),
			localCount: c.peakLocals,
			returnType: returnType,
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
		paramCount: len(paramNames),
		paramTypes: append([]string(nil), paramTypes...),
		localCount: c.peakLocals,
		returnType: returnType,
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

	// Free names of the lambda body relative to the lambda's own scope. These
	// resolve either to the enclosing frame (captures) or to this lambda's own
	// params/locals (which must then be boxed for deeper closures).
	ownDeclared := make(map[string]bool)
	for _, p := range paramNames {
		ownDeclared[p] = true
	}
	collectDeclaredNamesBlock(e.Body, ownDeclared)
	ownFree := make(map[string]bool)
	freeNamesBlock(e.Body, ownDeclared, ownFree)
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

	if e.Body != nil && len(e.Body.Stmts) > 0 {
		c.compileBlock(e.Body)
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
func computeFunctionCaptures(params []string, body *frontend.BlockStmt, isMethod bool, extraCaptures map[string]bool) (map[string]bool, []string) {
	declared := make(map[string]bool)
	for _, p := range params {
		declared[p] = true
	}
	if isMethod {
		declared["this"] = true
	}
	collectDeclaredNamesBlock(body, declared)

	free := make(map[string]bool)
	freeNamesBlock(body, declared, free)

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
// for-loop variables, catch variables) to out.
func collectDeclaredNamesBlock(block *frontend.BlockStmt, out map[string]bool) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		collectDeclaredNames(s, out)
	}
}

func collectDeclaredNames(stmt frontend.Statement, out map[string]bool) {
	switch s := stmt.(type) {
	case *frontend.VarStmt:
		if s.Name.Value != "" {
			out[s.Name.Value] = true
		}
	case *frontend.ForStmt:
		if s.IsForIn {
			if s.Variable != "" {
				out[s.Variable] = true
			}
		} else if s.Init != nil {
			collectDeclaredNames(s.Init, out)
		}
	case *frontend.WhenStmt:
		// when-case type-match variables are not real locals at runtime
		// (never bound by compileWhenStmt); skip them.
	case *frontend.IfStmt:
		collectDeclaredNamesBlock(s.Consequence, out)
		if s.Alternative != nil {
			collectDeclaredNames(s.Alternative, out)
		}
	case *frontend.WhileStmt:
		collectDeclaredNamesBlock(s.Body, out)
	case *frontend.BlockStmt:
		collectDeclaredNamesBlock(s, out)
	case *frontend.TryStmt:
		collectDeclaredNamesBlock(s.Body, out)
		if s.CatchVar != nil && s.CatchVar.Value != "" {
			out[s.CatchVar.Value] = true
		}
		collectDeclaredNamesBlock(s.CatchBody, out)
	case *frontend.DeferStmt:
		collectDeclaredNamesBlock(s.Body, out)
	}
}

// freeNamesStmt collects free identifier names (reads or writes) reachable
// from stmt into out, ignoring names in bound.
func freeNamesStmt(stmt frontend.Statement, bound map[string]bool, out map[string]bool) {
	switch s := stmt.(type) {
	case *frontend.VarStmt:
		freeNamesExpr(s.Value, bound, out)
	case *frontend.ReturnStmt:
		freeNamesExpr(s.Value, bound, out)
	case *frontend.YieldStmt:
		freeNamesExpr(s.Value, bound, out)
	case *frontend.ExprStatement:
		freeNamesExpr(s.Expr, bound, out)
	case *frontend.IfStmt:
		freeNamesExpr(s.Condition, bound, out)
		freeNamesBlock(s.Consequence, bound, out)
		if s.Alternative != nil {
			freeNamesStmt(s.Alternative, bound, out)
		}
	case *frontend.WhileStmt:
		freeNamesExpr(s.Condition, bound, out)
		freeNamesBlock(s.Body, bound, out)
	case *frontend.ForStmt:
		if s.Init != nil {
			freeNamesStmt(s.Init, bound, out)
		}
		freeNamesExpr(s.Condition, bound, out)
		freeNamesExpr(s.Update, bound, out)
		freeNamesExpr(s.Iterable, bound, out)
		freeNamesBlock(s.Body, bound, out)
	case *frontend.WhenStmt:
		freeNamesExpr(s.Expr, bound, out)
		for _, cc := range s.Cases {
			for _, val := range cc.Values {
				freeNamesExpr(val, bound, out)
			}
			freeNamesExpr(cc.Guard, bound, out)
			freeNamesBlock(cc.Body, bound, out)
		}
		freeNamesBlock(s.DefaultCase, bound, out)
	case *frontend.BlockStmt:
		freeNamesBlock(s, bound, out)
	case *frontend.TryStmt:
		freeNamesBlock(s.Body, bound, out)
		freeNamesBlock(s.CatchBody, bound, out)
	case *frontend.DeferStmt:
		freeNamesBlock(s.Body, bound, out)
	case *frontend.BreakStmt, *frontend.ContinueStmt:
		// nothing
	}
}

func freeNamesBlock(block *frontend.BlockStmt, bound map[string]bool, out map[string]bool) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
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
		collectDeclaredNamesBlock(e.Body, lambdaBound)
		freeNamesBlock(e.Body, lambdaBound, out)
	}
	if e.ExprBody != nil {
		freeNamesExpr(e.ExprBody, lambdaBound, out)
	}
}

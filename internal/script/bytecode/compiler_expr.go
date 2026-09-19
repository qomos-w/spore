// compiler_expr.go compiles expression forms: literals, operators, calls, member and optional-chain access, enum members and native callables.

package bytecode

import (
	"fmt"
	"github.com/qomos-w/spore/invoke"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// --- Expression compilation ---

func (c *compiler) compileExpression(expr frontend.Expression) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *frontend.IntLiteral:
		switch c.typeHint {
		case "long":
			idx := c.chunk.addConstant(e.Value) // int64
			c.emit(opPushLong, int32(idx), c.curLine)
		case "ulong":
			idx := c.chunk.addConstant(uint64(e.Value)) // uint64
			c.emit(opPushULong, int32(idx), c.curLine)
		default:
			idx := c.chunk.addConstant(int32(e.Value)) // int32 (default)
			c.emit(opPushInt, int32(idx), c.curLine)
		}
	case *frontend.FloatLiteral:
		if c.typeHint == "double" {
			idx := c.chunk.addConstant(e.Value) // float64
			c.emit(opPushDouble, int32(idx), c.curLine)
		} else {
			idx := c.chunk.addConstant(float32(e.Value)) // float32 (default)
			c.emit(opPushFloat, int32(idx), c.curLine)
		}
	case *frontend.StringLiteral:
		idx := c.chunk.addConstant(e.Value)
		c.emit(opPushString, int32(idx), c.curLine)
	case *frontend.BoolLiteral:
		if e.Value {
			c.emit(opPushTrue, 0, c.curLine)
		} else {
			c.emit(opPushFalse, 0, c.curLine)
		}
	case *frontend.NullLiteral:
		c.emit(opPushNull, 0, c.curLine)
	case *frontend.IdentExpr:
		c.compileIdentExpr(e)
	case *frontend.BinaryExpr:
		c.compileBinaryExpr(e)
	case *frontend.UnaryExpr:
		c.compileUnaryExpr(e)
	case *frontend.CallExpr:
		c.compileCallExpr(e)
	case *frontend.MemberExpr:
		c.compileMemberExpr(e)
	case *frontend.NullCoalesceExpr:
		c.compileNullCoalesceExpr(e)
	case *frontend.OptionalChainExpr:
		c.compileOptionalChainExpr(e)
	case *frontend.IndexExpr:
		c.compileIndexExpr(e)
	case *frontend.AssignExpr:
		c.compileAssignExpr(e)
	case *frontend.TypeCheckExpr:
		c.compileTypeCheckExpr(e)
	case *frontend.TypeCastExpr:
		c.compileTypeCastExpr(e)
	case *frontend.ThisExpr:
		c.compileThisExpr()
	case *frontend.ArrayLiteral:
		c.compileArrayLiteral(e)
	case *frontend.MapLiteral:
		c.compileMapLiteral(e)
	case *frontend.StructLiteral:
		c.compileStructLiteral(e)
	case *frontend.NewExpr:
		c.compileNewExpr(e)
	case *frontend.SuperExpr:
		c.compileSuperExpr(e)
	case *frontend.LambdaExpr:
		c.compileLambdaExpr(e)
	default:
		c.addError(fmt.Sprintf("unsupported expression type: %T", e))
	}
}

func qualifiedModuleCallableName(path, name string) string {
	return "__module_" + sanitizeModulePath(path) + "__" + name
}

func qualifiedModuleGlobalName(path, name string) string {
	return "__module_global_" + sanitizeModulePath(path) + "__" + name
}

func sanitizeModulePath(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ".", "_", "-", "_")
	return replacer.Replace(path)
}

func (c *compiler) compileIdentExpr(e *frontend.IdentExpr) {
	if localIdx := c.resolveLocal(e.Value); localIdx >= 0 {
		if c.locals[localIdx].isCell {
			c.emit(opLoadCell, int32(localIdx), c.curLine)
		} else {
			c.emit(opLoadLocal, int32(localIdx), c.curLine)
		}
	} else if _, ok := c.nativeValues[e.Value]; ok {
		slot, exists := c.nativeValueSlots[e.Value]
		if !exists {
			c.addCompileError("undefined_imported_variable", "bytecode/scope/import", fmt.Sprintf("undefined imported native value: %s", e.Value))
			return
		}
		c.emit(opLoadNativeValue, int32(slot), c.curLine)
	} else if target, ok := c.importedGlobals[e.Value]; ok {
		globalIdx, exists := c.globals[target]
		if !exists {
			c.addCompileError("undefined_imported_variable", "bytecode/scope/import", fmt.Sprintf("undefined imported variable: %s", e.Value))
			return
		}
		c.emit(opLoadGlobal, int32(globalIdx), c.curLine)
	} else if globalIdx, ok := c.globals[e.Value]; ok {
		c.emit(opLoadGlobal, int32(globalIdx), c.curLine)
	} else {
		c.addCompileError("undefined_variable", "bytecode/scope/identifier", fmt.Sprintf("undefined variable: %s", e.Value))
	}
}

func (c *compiler) compileBinaryExpr(e *frontend.BinaryExpr) {
	// Short-circuit evaluation for && and ||.
	if e.Operator == "&&" {
		c.compileExpression(e.Left)
		jumpIfFalse := c.emit(opJumpIfFalse, 0, c.curLine)
		c.compileExpression(e.Right)
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
		c.emit(opPushFalse, 0, c.curLine)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
		return
	}
	if e.Operator == "||" {
		c.compileExpression(e.Left)
		jumpIfTrue := c.emit(opJumpIfTrue, 0, c.curLine)
		c.compileExpression(e.Right)
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfTrue, c.chunk.size())
		c.emit(opPushTrue, 0, c.curLine)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
		return
	}

	c.compileExpression(e.Left)
	c.compileExpression(e.Right)

	// Try type-specialization first: when both operands' static types
	// are known to match a registered scalar (int / double), emit the
	// typed opcode and skip the runtime tag dispatch in the generic op.
	if op, ok := c.typedBinaryOp(e); ok {
		c.emit(op, 0, c.curLine)
		return
	}

	switch e.Operator {
	case "+":
		c.emit(opAdd, 0, c.curLine)
	case "-":
		c.emit(opSub, 0, c.curLine)
	case "*":
		c.emit(opMul, 0, c.curLine)
	case "/":
		c.emit(opDiv, 0, c.curLine)
	case "%":
		c.emit(opMod, 0, c.curLine)
	case "==":
		c.emit(opEq, 0, c.curLine)
	case "!=":
		c.emit(opNe, 0, c.curLine)
	case "<":
		c.emit(opLt, 0, c.curLine)
	case "<=":
		c.emit(opLe, 0, c.curLine)
	case ">":
		c.emit(opGt, 0, c.curLine)
	case ">=":
		c.emit(opGe, 0, c.curLine)
	default:
		c.addError(fmt.Sprintf("unsupported binary operator: %s", e.Operator))
	}
}

// typedBinaryOp returns a type-specialized opcode for the given binary
// expression when both operands' inferred static types match a registered
// scalar specialization. Returns (0, false) for unknown / unsupported
// combinations so the caller falls through to the generic dispatch ops.
func (c *compiler) typedBinaryOp(e *frontend.BinaryExpr) (opcode, bool) {
	leftT := c.inferScalarType(e.Left)
	if leftT == "" {
		return 0, false
	}
	rightT := c.inferScalarType(e.Right)
	if leftT != rightT {
		return 0, false
	}
	switch leftT {
	case "int":
		switch e.Operator {
		case "+":
			return opAddInt, true
		case "-":
			return opSubInt, true
		case "*":
			return opMulInt, true
		case "/":
			return opDivInt, true
		case "%":
			return opModInt, true
		case "==":
			return opEqInt, true
		case "!=":
			return opNeInt, true
		case "<":
			return opLtInt, true
		case "<=":
			return opLeInt, true
		case ">":
			return opGtInt, true
		case ">=":
			return opGeInt, true
		}
	case "double":
		switch e.Operator {
		case "+":
			return opAddDouble, true
		case "-":
			return opSubDouble, true
		case "*":
			return opMulDouble, true
		case "/":
			return opDivDouble, true
		case "==":
			return opEqDouble, true
		case "!=":
			return opNeDouble, true
		case "<":
			return opLtDouble, true
		case "<=":
			return opLeDouble, true
		case ">":
			return opGtDouble, true
		case ">=":
			return opGeDouble, true
		}
	case "long":
		switch e.Operator {
		case "+":
			return opAddLong, true
		case "-":
			return opSubLong, true
		case "*":
			return opMulLong, true
		case "/":
			return opDivLong, true
		case "%":
			return opModLong, true
		case "==":
			return opEqLong, true
		case "!=":
			return opNeLong, true
		case "<":
			return opLtLong, true
		case "<=":
			return opLeLong, true
		case ">":
			return opGtLong, true
		case ">=":
			return opGeLong, true
		}
	case "ulong":
		switch e.Operator {
		case "+":
			return opAddULong, true
		case "-":
			return opSubULong, true
		case "*":
			return opMulULong, true
		case "/":
			return opDivULong, true
		case "%":
			return opModULong, true
		case "==":
			return opEqULong, true
		case "!=":
			return opNeULong, true
		case "<":
			return opLtULong, true
		case "<=":
			return opLeULong, true
		case ">":
			return opGtULong, true
		case ">=":
			return opGeULong, true
		}
	case "float":
		switch e.Operator {
		case "+":
			return opAddFloat, true
		case "-":
			return opSubFloat, true
		case "*":
			return opMulFloat, true
		case "/":
			return opDivFloat, true
		case "==":
			return opEqFloat, true
		case "!=":
			return opNeFloat, true
		case "<":
			return opLtFloat, true
		case "<=":
			return opLeFloat, true
		case ">":
			return opGtFloat, true
		case ">=":
			return opGeFloat, true
		}
	case "string":
		switch e.Operator {
		case "+":
			return opAddString, true
		}
	}
	return 0, false
}

// inferScalarType returns the static scalar type name of an expression
// when the compiler can prove it without running the program. Returns
// "" when the type is unknown or not a single scalar (so the caller
// must fall back to the generic runtime-tagged dispatch).
//
// The function is conservative: false negatives (missed specializations)
// are fine; false positives (wrong type) would corrupt the bytecode.
func (c *compiler) inferScalarType(expr frontend.Expression) string {
	switch e := expr.(type) {
	case *frontend.IntLiteral:
		switch c.typeHint {
		case "long", "ulong":
			return c.typeHint
		}
		return "int"
	case *frontend.FloatLiteral:
		if c.typeHint == "double" {
			return "double"
		}
		return "float"
	case *frontend.BoolLiteral:
		return "bool"
	case *frontend.StringLiteral:
		return "string"
	case *frontend.IdentExpr:
		return c.localOrGlobalTypeName(e.Value)
	case *frontend.MemberExpr:
		return c.memberExprTypeName(e)
	case *frontend.NullCoalesceExpr:
		// `a ?? b` has the type of the non-null branch when both sides
		// agree; otherwise the type is unknown.
		lt := c.inferScalarType(e.Left)
		if lt == "" {
			return ""
		}
		if rt := c.inferScalarType(e.Right); lt == rt {
			return lt
		}
		return ""
	case *frontend.OptionalChainExpr:
		// The chain yields the inner type on the non-null path.
		return c.inferScalarType(e.Expr)
	case *frontend.CallExpr:
		return c.callExpressionReturnType(e)
	case *frontend.BinaryExpr:
		switch e.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return "bool"
		case "+", "-", "*", "/", "%":
			lt := c.inferScalarType(e.Left)
			if lt == "" {
				return ""
			}
			rt := c.inferScalarType(e.Right)
			if lt == rt {
				return lt
			}
		}
		return ""
	case *frontend.UnaryExpr:
		switch e.Operator {
		case "!":
			return "bool"
		case "-":
			return c.inferScalarType(e.Right)
		}
		return ""
	}
	return ""
}

func (c *compiler) compileUnaryExpr(e *frontend.UnaryExpr) {
	c.compileExpression(e.Right)
	switch e.Operator {
	case "!":
		c.emit(opNot, 0, c.curLine)
	case "-":
		c.emit(opNeg, 0, c.curLine)
	default:
		c.addError(fmt.Sprintf("unsupported unary operator: %s", e.Operator))
	}
}

func (c *compiler) compileCallExpr(e *frontend.CallExpr) {
	if nativeName, ok := c.nativeCallableNameForCall(e); ok {
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		nameIdx := c.chunk.addConstant(nativeName)
		operand := int32(len(e.Arguments))<<16 | int32(nameIdx)
		c.emit(opCall, operand, c.curLine)
		return
	}
	// Check if the callee is a simple identifier (direct function call).
	if ident, ok := e.Callee.(*frontend.IdentExpr); ok {
		if ident.Value == "len" {
			if len(e.Arguments) != 1 {
				c.addError("len expects exactly 1 argument")
				return
			}
			c.compileExpression(e.Arguments[0])
			if c.isMapExpressionInContext(e.Arguments[0]) {
				c.emit(opMapLen, 0, c.curLine)
			} else {
				c.emit(opArrayLen, 0, c.curLine)
			}
			return
		}
		if ident.Value == "push" {
			if len(e.Arguments) != 2 {
				c.addError("push expects exactly 2 arguments")
				return
			}
			// Fast path: push(localArray, "literal") -> fused opcode
			if arr, ok := e.Arguments[0].(*frontend.IdentExpr); ok {
				if lit, ok := e.Arguments[1].(*frontend.StringLiteral); ok {
					if localIdx := c.resolveLocal(arr.Value); localIdx >= 0 {
						constIdx := c.chunk.addConstant(lit.Value)
						operand, ok := packLocalPair(localIdx, constIdx)
						if ok {
							c.emit(opArrayPushLocalConstString, operand, c.curLine)
							return
						}
					}
				}
			}
			c.compileExpression(e.Arguments[0]) // array
			c.compileExpression(e.Arguments[1]) // value
			c.emit(opArrayPush, 0, c.curLine)
			return
		}
		if ident.Value == "delete" {
			if len(e.Arguments) != 2 {
				c.addError("delete expects exactly 2 arguments")
				return
			}
			c.compileExpression(e.Arguments[0]) // map
			c.compileExpression(e.Arguments[1]) // key
			c.emit(opMapDelete, 0, c.curLine)
			return
		}
		callName := ident.Value
		// Value-call path: if the callee identifier resolves to a local variable
		// or a global var (not a function), the callee is a runtime value —
		// typically a lambda/closure stored in that slot. opCallValue expects
		// args first and the callee pushed last (on top of the stack).
		if c.resolveLocal(ident.Value) >= 0 {
			// Function-typed variables validate call arguments against
			// their declared signature at compile time.
			c.checkFunTypeCallArgs(ident.Value, e.Arguments)
			for _, arg := range e.Arguments {
				c.compileExpression(arg)
			}
			c.compileExpression(e.Callee)
			c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
			return
		}
		if _, isGlobal := c.globals[ident.Value]; isGlobal {
			if _, importedCallable := c.importedCallables[ident.Value]; !importedCallable {
				if _, importedNative := c.importedNatives[ident.Value]; !importedNative {
					if _, isFn := c.functions[ident.Value]; !isFn {
						c.checkFunTypeCallArgs(ident.Value, e.Arguments)
						for _, arg := range e.Arguments {
							c.compileExpression(arg)
						}
						c.compileExpression(e.Callee)
						c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
						return
					}
				}
			}
		}
		if target, ok := c.importedCallables[ident.Value]; ok {
			callName = target
		} else if target, ok := c.importedNatives[ident.Value]; ok {
			callName = target
		}
		// Compile arguments.
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		if calleeChunk, ok := c.functions[callName]; ok {
			// Lambda arguments are validated against function-typed
			// parameters of the callee (higher-order callables).
			if info, ok := c.funcInfo[callName]; ok && len(info.paramTypes) > 0 {
				c.checkDirectCallFunTypeParams(callName, info.paramTypes, e.Arguments)
			}
			funcID := c.chunk.addFunctionRef(callName, calleeChunk)
			operand := int32(len(e.Arguments))<<16 | int32(funcID)
			c.emit(opCallDirect, operand, c.curLine)
			return
		}
		// Emit call by name.
		nameIdx := c.chunk.addConstant(callName)
		operand := int32(len(e.Arguments))<<16 | int32(nameIdx)
		c.emit(opCall, operand, c.curLine)
		return
	}

	// Check if it's a method call (obj.method(args)).
	if member, ok := e.Callee.(*frontend.MemberExpr); ok {
		if object, ok := member.Object.(*frontend.IdentExpr); ok {
			if _, importedNativeValue := c.nativeValues[object.Value]; importedNativeValue {
				c.compileObjectForChain(member)
				for _, arg := range e.Arguments {
					c.compileExpression(arg)
				}
				methodNameIdx := c.chunk.addConstant(member.Member.Value)
				operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
				c.checkMethodAccess(member.Object, member.Member.Value)
				c.emit(opCallMethod, operand, c.curLine)
				return
			}
			if _, knownNamespace := c.nativeCapabilities[object.Value]; knownNamespace {
				if c.resolveLocal(object.Value) < 0 {
					if _, ok := c.globals[object.Value]; !ok {
						if !member.Optional {
							idx := c.chunk.addConstant(object.Value)
							c.emit(opPushString, int32(idx), c.curLine)
							for _, arg := range e.Arguments {
								c.compileExpression(arg)
							}
							methodNameIdx := c.chunk.addConstant(member.Member.Value)
							operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
							c.emit(opCallMethod, operand, c.curLine)
							return
						}
					}
				}
			}
		}
		c.compileObjectForChain(member)
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		methodNameIdx := c.chunk.addConstant(member.Member.Value)
		operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
		c.checkMethodAccess(member.Object, member.Member.Value)
		c.emit(opCallMethod, operand, c.curLine)
		return
	}

	// General case: value call. opCallValue expects args first and the
	// callee pushed last (on top of the stack). A callee spine that still
	// carries an un-wrapped optional link (e.g. `x?.m(y)(z)`) would emit a
	// null short-circuit jump to the outer chain end with the already
	// pushed arguments stranded below — reject instead of corrupting the
	// operand stack.
	if directOptionalLink(e.Callee) {
		c.addError("cannot call the result of an optional chain directly; assign it to a variable first")
		return
	}
	for _, arg := range e.Arguments {
		c.compileExpression(arg)
	}
	c.compileExpression(e.Callee)
	c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
}

// directOptionalLink reports whether compiling expr as a value can emit a
// null short-circuit jump that targets an enclosing optional-chain end
// (rather than an inner, self-balanced wrapper). Such jumps must not run
// while caller-owned values sit below on the operand stack.
func directOptionalLink(expr frontend.Expression) bool {
	switch e := expr.(type) {
	case *frontend.OptionalChainExpr:
		return false // inner wrapper isolates and patches its own jumps
	case *frontend.MemberExpr:
		return e.Optional
	case *frontend.CallExpr:
		return directOptionalLink(e.Callee)
	case *frontend.IndexExpr:
		return directOptionalLink(e.Left)
	case *frontend.NullCoalesceExpr:
		return false // balanced: both branches leave exactly one value
	}
	return false
}

func (c *compiler) isMapExpressionInContext(expr frontend.Expression) bool {
	currentClass := c.currentClassName
	if currentClass == "" && c.chunk != nil && strings.Contains(c.chunk.sourceName, ".") {
		if idx := strings.Index(c.chunk.sourceName, "."); idx > 0 {
			currentClass = c.chunk.sourceName[:idx]
		}
	}
	if currentClass == "" {
		return c.isMapExpression(expr)
	}
	shadow := *c
	shadow.currentClassName = currentClass
	return shadow.isMapExpression(expr)
}

func (c *compiler) compileMemberExpr(e *frontend.MemberExpr) {
	// Enum member access (`Color.Red`): when the object identifier names an
	// enum (declared or imported) and does not shadow a local/global/native
	// binding, the member must be one of the enum's members and compiles to
	// a tagged enum-value constant.
	if id, ok := e.Object.(*frontend.IdentExpr); ok {
		if enumName, enumOK := c.resolveEnumName(id.Value); enumOK && !c.nameShadowsEnum(id.Value) {
			c.compileEnumMember(enumName, e.Member)
			return
		}
	}
	c.compileObjectForChain(e)
	c.checkFieldAccess(e.Object, e.Member.Value, "read")
	fieldNameIdx := c.chunk.addConstant(e.Member.Value)
	c.emit(opGetField, int32(fieldNameIdx), c.curLine)
}

// compileObjectForChain compiles the receiver of a memberExpr and, when the
// member is optional (`obj?.member`), emits opJumpIfNull. The jump is
// recorded on the compiler so compileOptionalChainExpr can patch it to the
// end of the enclosing chain; until then the operand is a placeholder that
// is always patched before the chunk is finalized. On the null path the
// null value itself stays on the stack and becomes the chain's result.
func (c *compiler) compileObjectForChain(member *frontend.MemberExpr) {
	c.compileExpression(member.Object)
	if member.Optional {
		jump := c.emit(opJumpIfNull, 0, c.curLine)
		c.optionalChainJumps = append(c.optionalChainJumps, jump)
	}
}

// compileOptionalChainExpr compiles a postfix chain containing at least one
// `?.` link. Each optional link emits opJumpIfNull targeting the end of the
// whole chain, so a null receiver anywhere skips every remaining link —
// including argument evaluation of subsequent calls — and leaves null as
// the chain's value.
func (c *compiler) compileOptionalChainExpr(e *frontend.OptionalChainExpr) {
	saved := c.optionalChainJumps
	c.optionalChainJumps = nil
	c.compileExpression(e.Expr)
	end := c.chunk.size()
	for _, jump := range c.optionalChainJumps {
		c.chunk.patchJump(jump, end)
	}
	c.optionalChainJumps = saved
}

// compileNullCoalesceExpr compiles `left ?? right`: when left is not null it
// is kept on the stack as the result and right is skipped; when left is
// null it is popped and right becomes the result.
func (c *compiler) compileNullCoalesceExpr(e *frontend.NullCoalesceExpr) {
	c.compileExpression(e.Left)
	jumpEnd := c.emit(opJumpIfNotNull, 0, c.curLine)
	c.emit(opPop, 0, c.curLine)
	c.compileExpression(e.Right)
	c.chunk.patchJump(jumpEnd, c.chunk.size())
}

// resolveEnumName maps a source name to a declared enum name, following
// import aliases. The second return is false when the name is not an enum.
func (c *compiler) resolveEnumName(name string) (string, bool) {
	if _, ok := c.enums[name]; ok {
		return name, true
	}
	if target, ok := c.importedEnums[name]; ok {
		return target, true
	}
	return "", false
}

// nameShadowsEnum reports whether the given source name is bound to a
// local, global, imported global, or native value, in which case those
// bindings take precedence over enum member resolution.
func (c *compiler) nameShadowsEnum(name string) bool {
	if c.resolveLocal(name) >= 0 {
		return true
	}
	if _, ok := c.globals[name]; ok {
		return true
	}
	if _, ok := c.importedGlobals[name]; ok {
		return true
	}
	if _, ok := c.nativeValues[name]; ok {
		return true
	}
	if _, ok := c.importedNatives[name]; ok {
		return true
	}
	return false
}

// compileEnumMember emits an opEnumValue constant for `EnumName.member`.
// Unknown members are compile-time errors (closed set).
func (c *compiler) compileEnumMember(enumName string, member *frontend.Ident) {
	info, ok := c.enums[enumName]
	if !ok {
		c.addCompileErrorWithTypes("unknown_enum", "bytecode/enum", fmt.Sprintf("enum %q is not declared", enumName), enumName, "")
		return
	}
	value, ok := info.memberValue(member.Value)
	if !ok {
		c.addCompileErrorWithTypes("unknown_enum_member", "bytecode/enum", fmt.Sprintf("enum %s has no member %q", enumName, member.Value), enumName+"."+member.Value, "")
		return
	}
	constIdx := c.chunk.addConstant(enumConst{enum: enumName, value: value})
	c.emit(opEnumValue, int32(constIdx), c.curLine)
}

func (c *compiler) nativeCallableNameForCall(e *frontend.CallExpr) (string, bool) {
	member, ok := e.Callee.(*frontend.MemberExpr)
	if !ok {
		return "", false
	}
	// Optional chains must compile the receiver and emit the null check,
	// so the qualified-name fast path (which never materializes the
	// receiver) does not apply.
	if member.Optional {
		return "", false
	}
	object, ok := member.Object.(*frontend.IdentExpr)
	if !ok {
		return "", false
	}
	callables, knownNamespace := c.nativeCapabilities[object.Value]
	if c.resolveLocal(object.Value) >= 0 {
		return "", false
	}
	if _, ok := c.globals[object.Value]; ok {
		return "", false
	}
	if !knownNamespace {
		return "", false
	}
	if _, ok := callables[member.Member.Value]; !ok {
		return "", false
	}
	return object.Value + "." + member.Member.Value, true
}

func (c *compiler) registerNativeCapability(desc invoke.CapabilityDesc) {
	if desc.Name == "" {
		return
	}
	callables := make(map[string]struct{}, len(desc.Callables))
	for _, callable := range desc.Callables {
		callables[callable.Name] = struct{}{}
	}
	c.nativeCapabilities[desc.Name] = callables
}

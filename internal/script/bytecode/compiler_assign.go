// compiler_assign.go lowers assignments and value-producing literals, type checks/casts, zero values, new and super calls.

package bytecode

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

func (c *compiler) compileAssignExpr(e *frontend.AssignExpr) {
	// Determine the target.
	switch target := e.Target.(type) {
	case *frontend.IdentExpr:
		if _, imported := c.importedGlobals[target.Value]; imported {
			c.addCompileError("assign_imported_variable", "bytecode/scope/import", fmt.Sprintf("cannot assign imported variable: %s", target.Value))
			return
		}
		if _, imported := c.nativeValues[target.Value]; imported {
			c.addCompileError("assign_imported_variable", "bytecode/scope/import", fmt.Sprintf("cannot assign imported variable: %s", target.Value))
			return
		}
		// Function-typed targets validate the assigned value at compile time.
		if targetType := c.localOrGlobalTypeName(target.Value); isFunTypeName(targetType) {
			c.checkFunTypeValueAssign(targetType, target.Value, e.Value)
		}
		// Capture-cell locals bypass all fused local mutators below: those
		// opcodes operate on raw local slots, not cell contents.
		if cellIdx := c.resolveLocal(target.Value); cellIdx >= 0 && c.locals[cellIdx].isCell {
			c.compileExpression(e.Value)
			c.emit(opStoreCell, int32(cellIdx), c.curLine)
			return
		}
		// Fast path: x = x + 1  →  INC_LOCAL
		//            x = x - 1  →  DEC_LOCAL
		// opIncLocal/opDecLocal rewrite the slot as a tagged int, so only
		// int-typed locals qualify: fusing a long counter would re-tag the
		// slot as int and later typed accesses (e.g. opLtLong) would fail
		// to decode it.
		if inc, ok := isIncDecPattern(target.Value, e.Value); ok {
			if localIdx := c.resolveLocal(target.Value); localIdx >= 0 && c.localTypeName(target.Value) == "int" {
				if inc {
					c.emit(opIncLocal, int32(localIdx), c.curLine)
				} else {
					c.emit(opDecLocal, int32(localIdx), c.curLine)
				}
				return
			}
		}
		if c.tryCompileAddLocalIntAssign(target.Value, e.Value) {
			return
		}
		if c.tryCompileConcatLocalConstStringAssign(target.Value, e.Value) {
			return
		}
		c.compileExpression(e.Value)
		if localIdx := c.resolveLocal(target.Value); localIdx >= 0 {
			c.emit(opStoreLocal, int32(localIdx), c.curLine)
		} else if globalIdx, ok := c.globals[target.Value]; ok {
			c.emit(opStoreGlobal, int32(globalIdx), c.curLine)
		} else {
			c.addCompileError("undefined_variable", "bytecode/scope/identifier", fmt.Sprintf("undefined variable: %s", target.Value))
		}
	case *frontend.MemberExpr:
		if target.Optional {
			c.addError("optional chaining cannot be used as an assignment target")
			return
		}
		c.compileExpression(target.Object)
		c.compileExpression(e.Value)
		fieldNameIdx := c.chunk.addConstant(target.Member.Value)
		c.checkFieldAccess(target.Object, target.Member.Value, "write")
		c.emit(opSetField, int32(fieldNameIdx), c.curLine)
	case *frontend.OptionalChainExpr:
		c.addError("optional chaining cannot be used as an assignment target")
	case *frontend.IndexExpr:
		if c.tryCompileMapSetString(target, e.Value) {
			return
		}
		if c.tryCompileArraySetInt(target, e.Value) {
			return
		}
		c.compileExpression(target.Left)
		c.compileExpression(target.Index)
		c.compileExpression(e.Value)
		c.emit(opSetElement, 0, c.curLine)
	default:
		c.addError("invalid assignment target")
	}
}

func (c *compiler) compileTypeCheckExpr(e *frontend.TypeCheckExpr) {
	c.compileExpression(e.Left)
	typeNameIdx := c.chunk.addConstant(e.TypeName)
	c.emit(opIs, int32(typeNameIdx), c.curLine)
}

func (c *compiler) compileTypeCastExpr(e *frontend.TypeCastExpr) {
	c.compileExpression(e.Left)
	typeNameIdx := c.chunk.addConstant(e.Target.Name)
	c.emit(opAs, int32(typeNameIdx), c.curLine)
}

func (c *compiler) compileThisExpr() {
	localIdx := c.resolveLocal("this")
	if localIdx >= 0 {
		if c.locals[localIdx].isCell {
			// `this` captured by a lambda arrives as a capture cell.
			c.emit(opLoadCell, int32(localIdx), c.curLine)
		} else {
			c.emit(opLoadLocal, int32(localIdx), c.curLine)
		}
	} else {
		c.emit(opPushNull, 0, c.curLine)
	}
}

func (c *compiler) compileArrayLiteral(e *frontend.ArrayLiteral) {
	for _, elem := range e.Elements {
		c.compileExpression(elem)
	}
	c.emit(opNewArray, int32(len(e.Elements)), c.curLine)
}

func (c *compiler) compileMapLiteral(e *frontend.MapLiteral) {
	// Create empty map on stack.
	c.emit(opNewMap, 0, c.curLine)
	for _, pair := range e.Pairs {
		// Duplicate map reference for opMapSet.
		c.emit(opDup, 0, c.curLine)
		// Stack: [map, map]
		c.compileExpression(pair.Key)
		// Stack: [map, map, key]
		c.compileExpression(pair.Value)
		// Stack: [map, map, key, value]
		c.emit(opMapSet, 0, c.curLine)
		// Stack: [map]
	}
}

func (c *compiler) compileStructLiteral(e *frontend.StructLiteral) {
	// Look up struct in registry.
	structName := e.TypeName
	structInfo, ok := c.structs[structName]
	if !ok {
		c.addCompileError("unknown_struct_type", "bytecode/struct/literal", fmt.Sprintf("unknown struct type: %s", structName))
		return
	}

	// Build field name list for the struct.
	fieldCount := len(structInfo.fields)

	// Push all field values in declaration order.
	fieldMap := make(map[string]frontend.Expression)
	for _, f := range e.Fields {
		if _, exists := fieldMap[f.Name]; exists {
			c.addCompileError("duplicate_struct_field", "bytecode/struct/literal", fmt.Sprintf("duplicate field %q for struct %q", f.Name, structName))
			return
		}
		found := false
		for _, declared := range structInfo.fields {
			if f.Name == declared.name {
				found = true
				break
			}
		}
		if !found {
			c.addCompileError("unknown_struct_field", "bytecode/struct/literal", fmt.Sprintf("unknown field %q for struct %q", f.Name, structName))
			return
		}
		fieldMap[f.Name] = f.Value
	}
	for i := 0; i < fieldCount; i++ {
		field := structInfo.fields[i]
		if valExpr, found := fieldMap[field.name]; found {
			if !structFieldValueMatchesType(valExpr, field.typeName) {
				actualType := exprTypeName(valExpr)
				c.addCompileErrorWithTypes("struct_field_type_mismatch", "bytecode/struct/literal", fmt.Sprintf("field %q for struct %q expects %q", field.name, structName, field.typeName), field.typeName, actualType)
				return
			}
			prevHint := c.typeHint
			resolved := c.resolveType(field.typeName)
			if resolved == "long" || resolved == "ulong" || resolved == "double" {
				c.typeHint = resolved
			} else {
				c.typeHint = ""
			}
			c.compileExpression(valExpr)
			c.typeHint = prevHint
		} else if e.HasZeroFill {
			c.emitZeroValue(field.typeName)
		} else {
			c.addCompileError("missing_struct_field", "bytecode/struct/literal", fmt.Sprintf("missing required field %q for struct %q", field.name, structName))
			return
		}
	}

	// Create struct instance.
	nameIdx := c.chunk.addConstant(structName)
	c.emit(opNewStructInstance, int32(nameIdx), c.curLine)
}

// emitZeroValue pushes the typed zero value for the given declared field type.
// Used by zero-fill struct literal spread (e.g. `Point{x: 1, ..}`) to fill
// fields the author chose to leave at their type's default. Container types
// produce empty containers; struct/class/null/unknown references produce a
// null handle.
func (c *compiler) emitZeroValue(typeName string) {
	resolved := c.resolveType(typeName)
	switch resolved {
	case "int":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "long":
		idx := c.chunk.addConstant(int64(0))
		c.emit(opPushLong, int32(idx), c.curLine)
	case "ulong":
		idx := c.chunk.addConstant(uint64(0))
		c.emit(opPushULong, int32(idx), c.curLine)
	case "uint":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "short", "byte", "ushort":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "float":
		idx := c.chunk.addConstant(float32(0))
		c.emit(opPushFloat, int32(idx), c.curLine)
	case "double":
		idx := c.chunk.addConstant(float64(0))
		c.emit(opPushDouble, int32(idx), c.curLine)
	case "bool":
		c.emit(opPushFalse, 0, c.curLine)
	case "string", "bytes":
		idx := c.chunk.addConstant("")
		c.emit(opPushString, int32(idx), c.curLine)
	default:
		if strings.HasPrefix(resolved, "array<") {
			c.emit(opNewArray, 0, c.curLine)
			return
		}
		if strings.HasPrefix(resolved, "map<") {
			c.emit(opNewMap, 0, c.curLine)
			return
		}
		// struct/class/unknown ref/null → null handle
		c.emit(opPushNull, 0, c.curLine)
	}
}
func exprTypeName(expr frontend.Expression) string {
	switch expr.(type) {
	case *frontend.IntLiteral:
		return "int"
	case *frontend.FloatLiteral:
		return "float"
	case *frontend.StringLiteral:
		return "string"
	case *frontend.BoolLiteral:
		return "bool"
	case *frontend.NullLiteral:
		return "null"
	case *frontend.ArrayLiteral:
		return "array"
	case *frontend.MapLiteral:
		return "map"
	case *frontend.StructLiteral:
		return "struct"
	default:
		return "unknown"
	}
}

func structFieldValueMatchesType(expr frontend.Expression, typeName string) bool {
	// Only perform strict literal type checking for literal expressions.
	// Runtime expressions (variables, calls, etc.) pass through.
	switch expr.(type) {
	case *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral,
		*frontend.BoolLiteral, *frontend.NullLiteral, *frontend.ArrayLiteral,
		*frontend.MapLiteral, *frontend.StructLiteral:
		// literal — continue to type check
	default:
		return true
	}

	switch typeName {
	case "", "any":
		return true
	case "int", "long", "ulong", "byte", "short", "ushort", "uint":
		_, ok := expr.(*frontend.IntLiteral)
		return ok
	case "float", "double":
		switch expr.(type) {
		case *frontend.FloatLiteral, *frontend.IntLiteral:
			return true
		}
		return false
	case "string":
		_, ok := expr.(*frontend.StringLiteral)
		return ok
	case "bool":
		_, ok := expr.(*frontend.BoolLiteral)
		return ok
	}
	return true
}

func (c *compiler) compileNewExpr(e *frontend.NewExpr) {
	classNameIdx := c.chunk.addConstant(e.ClassName)
	c.emit(opNewObject, int32(classNameIdx), c.curLine)

	// Two-step: allocate + call constructor when one is declared.
	if c.classHasConstructor(e.ClassName) {
		c.emit(opDup, 0, c.curLine)
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		ctorNameIdx := c.chunk.addConstant(e.ClassName)
		operand := int32(len(e.Arguments))<<16 | int32(ctorNameIdx)
		c.emit(opCallMethod, operand, c.curLine)
		c.emit(opPop, 0, c.curLine) // discard constructor return
	}
}

func (c *compiler) classHasConstructor(className string) bool {
	info, ok := c.classes[className]
	if !ok {
		return false
	}
	for _, method := range info.methods {
		if method.Name.Value == className {
			return true
		}
	}
	return false
}

func (c *compiler) compileSuperExpr(e *frontend.SuperExpr) {
	// Load "this" (local 0 in method scope).
	c.emit(opLoadLocal, 0, c.curLine)

	// Compile arguments.
	for _, arg := range e.Arguments {
		c.compileExpression(arg)
	}

	if e.Method == nil {
		// super() — call parent constructor.
		// Constructor method name equals the parent class name.
		classInfo, ok := c.classes[c.currentClassName]
		if !ok || classInfo.parent == "" {
			return
		}
		ctorNameIdx := c.chunk.addConstant(classInfo.parent)
		operand := int32(len(e.Arguments))<<16 | int32(ctorNameIdx)
		c.emit(opCallSuperMethod, operand, c.curLine)
		return
	}

	methodNameIdx := c.chunk.addConstant(e.Method.Value)
	operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
	c.emit(opCallSuperMethod, operand, c.curLine)
}

func (c *compiler) compileGlobalAssign(stmt *frontend.ExprStatement) {
	if stmt == nil {
		return
	}
	assign, ok := stmt.Expr.(*frontend.AssignExpr)
	if !ok {
		c.addError("expected assignment at top level")
		return
	}
	c.compileAssignExpr(assign)
}

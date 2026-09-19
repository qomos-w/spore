// compiler_lookup.go resolves fields, methods and static types used by expression compilation, and lowers typed index access.

package bytecode

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// checkFieldAccess checks if accessing the named field from the current
// compilation context would violate private access rules.
func (c *compiler) checkFieldAccess(object frontend.Expression, fieldName string, mode string) {
	lookup, ok := c.lookupFieldOnExpr(object, fieldName)
	if !ok {
		return
	}
	if lookup.access == accessPrivate && c.currentClassName != lookup.owner {
		c.addCompileError("private_field_access_denied", "bytecode/access/field", fmt.Sprintf("cannot %s private field '%s' of class '%s'", mode, fieldName, lookup.owner))
	}
}

// checkMethodAccess checks if calling the named method from the current
// compilation context would violate private access rules.
func (c *compiler) checkMethodAccess(object frontend.Expression, methodName string) {
	lookup, ok := c.lookupMethodOnExpr(object, methodName)
	if !ok {
		return
	}
	if lookup.access == accessPrivate && c.currentClassName != lookup.owner {
		c.addCompileError("private_method_access_denied", "bytecode/access/method", fmt.Sprintf("cannot call private method '%s' of class '%s'", methodName, lookup.owner))
	}
}

func (c *compiler) lookupFieldOnExpr(object frontend.Expression, fieldName string) (memberLookup, bool) {
	return c.lookupFieldOnType(c.expressionTypeName(object), fieldName)
}

func (c *compiler) lookupMethodOnExpr(object frontend.Expression, methodName string) (memberLookup, bool) {
	return c.lookupMethodOnType(c.expressionTypeName(object), methodName)
}

func (c *compiler) lookupFieldOnType(typeName string, fieldName string) (memberLookup, bool) {
	className := c.classNameFromType(typeName)
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, f := range cls.fields {
			if f.name == fieldName {
				return memberLookup{owner: cls.name, access: f.access, typeName: c.resolveType(f.typeName), fieldInfo: f}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) lookupMethodOnType(typeName string, methodName string) (memberLookup, bool) {
	className := c.classNameFromType(typeName)
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, m := range cls.methods {
			if m.name == methodName {
				return memberLookup{owner: cls.name, access: m.access, returnType: c.resolveType(m.returnType), methodDecl: m}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) lookupMethodOnAncestor(className string, methodName string) (memberLookup, bool) {
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, m := range cls.methods {
			if m.name == methodName {
				return memberLookup{owner: cls.name, access: m.access, returnType: c.resolveType(m.returnType), methodDecl: m}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) classNameFromType(typeName string) string {
	typeName = c.resolveType(typeName)
	if strings.HasPrefix(typeName, "array<") {
		return c.resolveType(elementTypeName(typeName))
	}
	if strings.HasPrefix(typeName, "map<") {
		return c.resolveType(mapValueTypeName(typeName))
	}
	if _, ok := c.classes[typeName]; ok {
		return typeName
	}
	return ""
}

func (c *compiler) isMapExpression(expr frontend.Expression) bool {
	switch e := expr.(type) {
	case *frontend.MapLiteral:
		return true
	case *frontend.IdentExpr:
		return c.localOrGlobalIsMap(e.Value)
	case *frontend.MemberExpr:
		return c.memberExpressionIsMap(e)
	case *frontend.IndexExpr:
		return c.indexExpressionIsMap(e)
	case *frontend.CallExpr:
		return c.callExpressionReturnsMap(e)
	case *frontend.OptionalChainExpr:
		return c.isMapExpression(e.Expr)
	case *frontend.NullCoalesceExpr:
		return c.isMapExpression(e.Left) || c.isMapExpression(e.Right)
	default:
		return false
	}
}

func (c *compiler) memberExpressionIsMap(expr *frontend.MemberExpr) bool {
	if expr == nil {
		return false
	}
	typeName := c.expressionTypeName(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) localOrGlobalIsMap(name string) bool {
	typeName := c.localOrGlobalTypeName(name)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) localOrGlobalTypeName(name string) string {
	if typeName := c.localTypeName(name); typeName != "" {
		return typeName
	}
	if target, ok := c.importedGlobals[name]; ok {
		return c.globalTypes[target]
	}
	return c.globalTypes[name]
}

func (c *compiler) localTypeName(name string) string {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return c.locals[i].typeName
		}
	}
	return ""
}

// isCellLocal reports whether the innermost local named name holds a capture
// cell. Fused local opcodes must not be used on cell locals.
func (c *compiler) isCellLocal(name string) bool {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return c.locals[i].isCell
		}
	}
	return false
}

func (c *compiler) memberExprTypeName(expr *frontend.MemberExpr) string {
	if expr == nil {
		return ""
	}
	objectType := c.expressionTypeName(expr.Object)
	return c.memberOnObjectType(objectType, expr.Member.Value)
}

func (c *compiler) indexExpressionIsMap(expr *frontend.IndexExpr) bool {
	typeName := c.indexExpressionTypeName(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) indexExpressionTypeName(expr *frontend.IndexExpr) string {
	if expr == nil {
		return ""
	}
	leftType := c.resolveType(c.expressionTypeName(expr.Left))
	if strings.HasPrefix(leftType, "array<") {
		return c.resolveType(elementTypeName(leftType))
	}
	if strings.HasPrefix(leftType, "map<") {
		return c.resolveType(mapValueTypeName(leftType))
	}
	if leftType == "array" || leftType == "map" {
		return ""
	}
	return ""
}

func (c *compiler) callExpressionReturnsMap(expr *frontend.CallExpr) bool {
	typeName := c.callExpressionReturnType(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) callExpressionReturnType(expr *frontend.CallExpr) string {
	if expr == nil {
		return ""
	}
	if ident, ok := expr.Callee.(*frontend.IdentExpr); ok {
		if fn, ok := c.funcInfo[ident.Value]; ok {
			return fn.returnType
		}
	}
	if member, ok := expr.Callee.(*frontend.MemberExpr); ok {
		lookup, ok := c.lookupMethodOnExpr(member.Object, member.Member.Value)
		if ok {
			return lookup.returnType
		}
	}
	return ""
}

func (c *compiler) expressionTypeName(expr frontend.Expression) string {
	switch e := expr.(type) {
	case *frontend.IdentExpr:
		if e.Value == "this" {
			return c.currentClassName
		}
		return c.localOrGlobalTypeName(e.Value)
	case *frontend.ThisExpr:
		return c.currentClassName
	case *frontend.MemberExpr:
		return c.memberExprTypeName(e)
	case *frontend.IndexExpr:
		return c.indexExpressionTypeName(e)
	case *frontend.CallExpr:
		return c.callExpressionReturnType(e)
	case *frontend.NullCoalesceExpr:
		// `a ?? b` has the type of the non-null branch when both sides
		// agree; otherwise the type is unknown.
		lt := c.expressionTypeName(e.Left)
		if lt == "" {
			return ""
		}
		if rt := c.expressionTypeName(e.Right); lt == rt {
			return lt
		}
		return ""
	case *frontend.OptionalChainExpr:
		// The chain yields the inner type on the non-null path.
		return c.expressionTypeName(e.Expr)
	case *frontend.MapLiteral:
		return "map"
	case *frontend.ArrayLiteral:
		return "array"
	case *frontend.IntLiteral:
		return "int"
	default:
		return ""
	}
}

func elementTypeName(typeName string) string {
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return ""
	}
	inner := typeName[start+1 : end]
	depth := 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[:i])
			}
		}
	}
	return strings.TrimSpace(inner)
}

func mapValueTypeName(typeName string) string {
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return ""
	}
	inner := typeName[start+1 : end]
	depth := 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[i+1:])
			}
		}
	}
	return ""
}

func (c *compiler) memberOnObjectType(objectTypeName, fieldName string) string {
	lookup, ok := c.lookupFieldOnType(objectTypeName, fieldName)
	if !ok {
		return ""
	}
	return lookup.typeName
}

func (c *compiler) compileIndexExpr(e *frontend.IndexExpr) {
	if c.tryCompileMapGetString(e) {
		return
	}
	if c.tryCompileArrayGetInt(e) {
		return
	}
	c.compileExpression(e.Left)
	c.compileExpression(e.Index)
	c.emit(opGetElement, 0, c.curLine)
}

func (c *compiler) tryCompileArrayGetInt(e *frontend.IndexExpr) bool {
	if !indexReceiverIsTypedArray(c.expressionTypeName(e.Left)) {
		return false
	}
	idx, ok := e.Index.(*frontend.IntLiteral)
	if !ok || idx.Value < 0 || idx.Value > 0xFFFF {
		return false
	}
	c.compileExpression(e.Left)
	c.emit(opArrayGetInt, int32(idx.Value), c.curLine)
	return true
}

func (c *compiler) tryCompileMapGetString(e *frontend.IndexExpr) bool {
	if !indexReceiverIsTypedMap(c.expressionTypeName(e.Left)) {
		return false
	}
	key, ok := e.Index.(*frontend.StringLiteral)
	if !ok {
		return false
	}
	c.compileExpression(e.Left)
	keyIdx := c.chunk.addConstant(key.Value)
	c.emit(opMapGetString, int32(keyIdx), c.curLine)
	return true
}

func (c *compiler) tryCompileMapSetString(target *frontend.IndexExpr, value frontend.Expression) bool {
	if !indexReceiverIsTypedMap(c.expressionTypeName(target.Left)) {
		return false
	}
	key, ok := target.Index.(*frontend.StringLiteral)
	if !ok {
		return false
	}
	c.compileExpression(target.Left)
	c.compileExpression(value)
	keyIdx := c.chunk.addConstant(key.Value)
	c.emit(opMapSetString, int32(keyIdx), c.curLine)
	return true
}

func (c *compiler) tryCompileArraySetInt(target *frontend.IndexExpr, value frontend.Expression) bool {
	if !indexReceiverIsTypedArray(c.expressionTypeName(target.Left)) {
		return false
	}
	idx, ok := target.Index.(*frontend.IntLiteral)
	if !ok || idx.Value < 0 || idx.Value > 0xFFFF {
		return false
	}
	c.compileExpression(target.Left)
	c.compileExpression(value)
	c.emit(opArraySetInt, int32(idx.Value), c.curLine)
	return true
}

func indexReceiverIsTypedMap(typeName string) bool {
	return strings.HasPrefix(typeName, "map<")
}

func indexReceiverIsTypedArray(typeName string) bool {
	return strings.HasPrefix(typeName, "array<")
}

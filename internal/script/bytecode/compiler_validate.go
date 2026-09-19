// compiler_validate.go validates declaration-level rules: class relationships, field shadowing and super usage.

package bytecode

import (
	"fmt"

	"github.com/qomos-w/spore/internal/script/frontend"
)

func (c *compiler) validateSuperOutsideClass(fn funDecl) {
	if len(fn.body.stmts) == 0 && fn.exprBody == nil {
		return
	}
	method := methodDecl{name: fn.name, body: fn.body, exprBody: fn.exprBody}
	c.validateSuperUsage(classInfo{name: fn.name, parent: ""}, method)
}

func (c *compiler) validateClassDecl(info classInfo) {
	c.validateFieldShadowing(info)
	for _, method := range info.methods {
		if method.isOverride && info.parent == "" {
			c.addCompileError("override_without_parent", "bytecode/oop/override", fmt.Sprintf("method %q in class %q is marked override but class has no parent", method.name, info.name))
		}
		if method.isOverride && method.access == accessPrivate {
			c.addCompileError("override_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q cannot override with private visibility", method.name, info.name))
		}
		if !method.isOverride {
			if ancestor, ok := c.lookupMethodOnAncestor(info.parent, method.name); ok && method.access == accessPrivate {
				c.addCompileError("method_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q shadows ancestor method from class %q with private visibility", method.name, info.name, ancestor.owner))
			}
		}
		if info.parent != "" {
			parentInfo, ok := c.classes[info.parent]
			if !ok {
				if _, isIface := c.interfaces[info.parent]; !isIface {
					c.addCompileError("parent_class_not_found", "bytecode/oop/inheritance", fmt.Sprintf("parent class %q not found for %q", info.parent, info.name))
				}
			} else {
				for _, parentMethod := range parentInfo.methods {
					if parentMethod.name != method.name {
						continue
					}
					if method.isOverride {
						if len(parentMethod.paramNames) != len(method.paramNames) {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parameter count %d does not match parent count %d", method.name, len(method.paramNames), len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(method.paramNames)))
						} else if !methodReturnsCompatible(method, parentMethod) {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: return type %q does not match parent return type %q", method.name, method.returnTypeName(), parentMethod.returnTypeName()), parentMethod.returnTypeName(), method.returnTypeName())
						}
						if !parentMethod.isOpen {
							c.addCompileError("override_parent_method_not_open", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parent method in class %q is not open", method.name, info.parent))
						}
					}
				}
			}
		}
		if method.exprBody == nil && method.body.stmts == nil {
			continue
		}
		c.validateSuperUsage(info, method)
	}
	for _, ifaceName := range info.implements {
		iface, ok := c.interfaces[ifaceName]
		if !ok {
			c.addCompileError("unknown_interface", "bytecode/oop/interface", fmt.Sprintf("class %q implements unknown interface %q", info.name, ifaceName))
			continue
		}
		for _, ifaceMethod := range iface.methods {
			found := false
			for _, classMethod := range info.methods {
				if classMethod.name == ifaceMethod.name {
					found = true
					if len(classMethod.paramNames) != ifaceMethod.paramCount {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q has parameter count %d, expected %d", info.name, ifaceName, ifaceMethod.name, len(classMethod.paramNames), ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", len(classMethod.paramNames)))
					}
					if ifaceMethod.returnType != "" && classMethod.returnType != ifaceMethod.returnType {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q returns %q, expected %q", info.name, ifaceName, ifaceMethod.name, classMethod.returnType, ifaceMethod.returnType), ifaceMethod.returnType, classMethod.returnType)
					}
					break
				}
			}
			if found {
				continue
			}
			if info.parent != "" {
				if parentInfo, ok := c.classes[info.parent]; ok {
					for _, parentMethod := range parentInfo.methods {
						if parentMethod.name != ifaceMethod.name {
							continue
						}
						found = true
						if len(parentMethod.paramNames) != ifaceMethod.paramCount {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q with parameter count %d, expected %d for interface %q", info.name, ifaceMethod.name, info.parent, len(parentMethod.paramNames), ifaceMethod.paramCount, ifaceName), fmt.Sprintf("%d parameter(s)", ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", len(parentMethod.paramNames)))
						}
						if ifaceMethod.returnType != "" && parentMethod.returnType != ifaceMethod.returnType {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q returning %q, expected %q for interface %q", info.name, ifaceMethod.name, info.parent, parentMethod.returnType, ifaceMethod.returnType, ifaceName), ifaceMethod.returnType, parentMethod.returnType)
						}
						break
					}
				}
			}
			if !found {
				c.addCompileError("interface_method_missing", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but is missing method %q", info.name, ifaceName, ifaceMethod.name))
			}
		}
	}
}

func (c *compiler) validateFieldShadowing(info classInfo) {
	if info.parent == "" {
		return
	}
	for _, field := range info.fields {
		className := info.parent
		for className != "" {
			parent, ok := c.classes[className]
			if !ok {
				break
			}
			for _, parentField := range parent.fields {
				if parentField.name == field.name {
					c.addCompileError("field_shadowing_disallowed", "bytecode/oop/field", fmt.Sprintf("field %q in class %q shadows field from ancestor class %q", field.name, info.name, parent.name))
					return
				}
			}
			className = parent.parent
		}
	}
}

func (c *compiler) validateSuperUsage(info classInfo, method methodDecl) {
	walkExpression := func(expr frontend.Expression, visit func(frontend.Expression)) {}
	var walk func(frontend.Expression)
	walk = func(expr frontend.Expression) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case *frontend.SuperExpr:
			if info.parent == "" {
				c.addCompileError("super_without_parent", "bytecode/oop/super", fmt.Sprintf("super can only be used in class %q when a parent class exists", info.name))
				return
			}
			if e.Method == nil {
				return
			}
		case *frontend.BinaryExpr:
			walk(e.Left)
			walk(e.Right)
		case *frontend.UnaryExpr:
			walk(e.Right)
		case *frontend.CallExpr:
			walk(e.Callee)
			for _, arg := range e.Arguments {
				walk(arg)
			}
		case *frontend.MemberExpr:
			walk(e.Object)
		case *frontend.NullCoalesceExpr:
			walk(e.Left)
			walk(e.Right)
		case *frontend.OptionalChainExpr:
			walk(e.Expr)
		case *frontend.IndexExpr:
			walk(e.Left)
			walk(e.Index)
		case *frontend.AssignExpr:
			walk(e.Target)
			walk(e.Value)
		case *frontend.TypeCheckExpr:
			walk(e.Left)
		case *frontend.TypeCastExpr:
			walk(e.Left)
		case *frontend.ArrayLiteral:
			for _, elem := range e.Elements {
				walk(elem)
			}
		case *frontend.MapLiteral:
			for _, pair := range e.Pairs {
				walk(pair.Key)
				walk(pair.Value)
			}
		case *frontend.NewExpr:
			for _, arg := range e.Arguments {
				walk(arg)
			}
		case *frontend.StructLiteral:
			for _, field := range e.Fields {
				walk(field.Value)
			}
		case *frontend.ThisExpr, *frontend.IdentExpr, *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral, *frontend.BoolLiteral, *frontend.NullLiteral:
			return
		}
	}
	var walkStmt func(stmt stmtDecl)
	walkStmt = func(stmt stmtDecl) {
		switch stmt.kind {
		case stmtKindVar:
			walk(stmt.vr.value)
		case stmtKindReturn:
			walk(stmt.ret.value)
		case stmtKindYield:
			walk(stmt.yld.value)
		case stmtKindIf:
			walk(stmt.if_.condition)
			for _, nested := range stmt.if_.consequence.stmts {
				walkStmt(nested)
			}
			if stmt.if_.alternativeIf != nil {
				for _, nested := range stmt.if_.alternativeIf.consequence.stmts {
					walkStmt(nested)
				}
			}
			if stmt.if_.alternativeBlk != nil {
				for _, nested := range stmt.if_.alternativeBlk.stmts {
					walkStmt(nested)
				}
			}
		case stmtKindWhile:
			walk(stmt.whl.condition)
			for _, nested := range stmt.whl.body.stmts {
				walkStmt(nested)
			}
		case stmtKindFor:
			if stmt.for_.init != nil {
				walkStmt(*stmt.for_.init)
			}
			walk(stmt.for_.condition)
			walk(stmt.for_.update)
			walk(stmt.for_.iterable)
			for _, nested := range stmt.for_.body.stmts {
				walkStmt(nested)
			}
		case stmtKindWhen:
			walk(stmt.when.expr)
			for _, cc := range stmt.when.cases {
				for _, value := range cc.values {
					walk(value)
				}
				for _, nested := range cc.body.stmts {
					walkStmt(nested)
				}
			}
			if stmt.when.defaultCase != nil {
				for _, nested := range stmt.when.defaultCase.stmts {
					walkStmt(nested)
				}
			}
		case stmtKindBlock:
			for _, nested := range stmt.blk.stmts {
				walkStmt(nested)
			}
		case stmtKindExpr:
			walk(stmt.expr)
		case stmtKindTry:
			for _, nested := range stmt.try_.body.stmts {
				walkStmt(nested)
			}
			for _, nested := range stmt.try_.catchBody.stmts {
				walkStmt(nested)
			}
		case stmtKindDefer:
			for _, nested := range stmt.dfr.body.stmts {
				walkStmt(nested)
			}
		}
	}
	_ = walkExpression
	walk(method.exprBody)
	for _, stmt := range method.body.stmts {
		walkStmt(stmt)
	}
}

// compiler_validate.go validates declaration-level rules: class relationships, field shadowing and super usage.

package bytecode

import (
	"fmt"

	"github.com/qomos-w/spore/internal/script/frontend"
)

func (c *compiler) validateSuperOutsideClass(fn *frontend.FunStmt) {
	if fn == nil {
		return
	}
	if (fn.Body == nil || len(fn.Body.Stmts) == 0) && fn.ExprBody == nil {
		return
	}
	c.validateSuperUsage(classInfo{name: fn.Name.Value, parent: ""}, fn)
}

func (c *compiler) validateClassDecl(info classInfo) {
	c.validateFieldShadowing(info)
	for _, method := range info.methods {
		methodName := method.Name.Value
		paramCount := len(method.Params)
		if method.IsOverride && info.parent == "" {
			c.addCompileError("override_without_parent", "bytecode/oop/override", fmt.Sprintf("method %q in class %q is marked override but class has no parent", methodName, info.name))
		}
		if method.IsOverride && methodAccess(method) == accessPrivate {
			c.addCompileError("override_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q cannot override with private visibility", methodName, info.name))
		}
		if !method.IsOverride {
			if ancestor, ok := c.lookupMethodOnAncestor(info.parent, methodName); ok && methodAccess(method) == accessPrivate {
				c.addCompileError("method_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q shadows ancestor method from class %q with private visibility", methodName, info.name, ancestor.owner))
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
					if parentMethod.Name.Value != methodName {
						continue
					}
					if method.IsOverride {
						parentParamCount := len(parentMethod.Params)
						if parentParamCount != paramCount {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parameter count %d does not match parent count %d", methodName, paramCount, parentParamCount), fmt.Sprintf("%d parameter(s)", parentParamCount), fmt.Sprintf("%d parameter(s)", paramCount))
						} else if !methodReturnsCompatible(method, parentMethod) {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: return type %q does not match parent return type %q", methodName, funReturnType(method), funReturnType(parentMethod)), funReturnType(parentMethod), funReturnType(method))
						}
						if !parentMethod.IsOpen {
							c.addCompileError("override_parent_method_not_open", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parent method in class %q is not open", methodName, info.parent))
						}
					}
				}
			}
		}
		if method.ExprBody == nil && (method.Body == nil || method.Body.Stmts == nil) {
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
			ifaceMethodName := ifaceMethod.Name.Value
			expectedParams := len(ifaceMethod.Params)
			ifaceReturn := interfaceMethodReturnType(ifaceMethod)
			for _, classMethod := range info.methods {
				if classMethod.Name.Value == ifaceMethodName {
					found = true
					if params := len(classMethod.Params); params != expectedParams {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q has parameter count %d, expected %d", info.name, ifaceName, ifaceMethodName, params, expectedParams), fmt.Sprintf("%d parameter(s)", expectedParams), fmt.Sprintf("%d parameter(s)", params))
					}
					if ifaceReturn != "" && funReturnType(classMethod) != ifaceReturn {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q returns %q, expected %q", info.name, ifaceName, ifaceMethodName, funReturnType(classMethod), ifaceReturn), ifaceReturn, funReturnType(classMethod))
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
						if parentMethod.Name.Value != ifaceMethodName {
							continue
						}
						found = true
						if params := len(parentMethod.Params); params != expectedParams {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q with parameter count %d, expected %d for interface %q", info.name, ifaceMethodName, info.parent, params, expectedParams, ifaceName), fmt.Sprintf("%d parameter(s)", expectedParams), fmt.Sprintf("%d parameter(s)", params))
						}
						if ifaceReturn != "" && funReturnType(parentMethod) != ifaceReturn {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q returning %q, expected %q for interface %q", info.name, ifaceMethodName, info.parent, funReturnType(parentMethod), ifaceReturn, ifaceName), ifaceReturn, funReturnType(parentMethod))
						}
						break
					}
				}
			}
			if !found {
				c.addCompileError("interface_method_missing", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but is missing method %q", info.name, ifaceName, ifaceMethodName))
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

func (c *compiler) validateSuperUsage(info classInfo, method *frontend.FunStmt) {
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
	var walkStmtBlock func(block *frontend.BlockStmt)
	var walkStmt func(stmt frontend.Statement)
	walkStmtBlock = func(block *frontend.BlockStmt) {
		if block == nil {
			return
		}
		for _, nested := range block.Stmts {
			walkStmt(nested)
		}
	}
	walkStmt = func(stmt frontend.Statement) {
		switch s := stmt.(type) {
		case *frontend.VarStmt:
			walk(s.Value)
		case *frontend.ReturnStmt:
			walk(s.Value)
		case *frontend.YieldStmt:
			walk(s.Value)
		case *frontend.IfStmt:
			walk(s.Condition)
			walkStmtBlock(s.Consequence)
			// Mirror the historical traversal shape: an else-if chain is
			// inspected one alternative level deep.
			if altIf, ok := s.Alternative.(*frontend.IfStmt); ok {
				walkStmtBlock(altIf.Consequence)
			}
			if altBlk, ok := s.Alternative.(*frontend.BlockStmt); ok {
				walkStmtBlock(altBlk)
			}
		case *frontend.WhileStmt:
			walk(s.Condition)
			walkStmtBlock(s.Body)
		case *frontend.ForStmt:
			if s.Init != nil {
				walkStmt(s.Init)
			}
			walk(s.Condition)
			walk(s.Update)
			walk(s.Iterable)
			walkStmtBlock(s.Body)
		case *frontend.WhenStmt:
			walk(s.Expr)
			for _, cc := range s.Cases {
				for _, value := range cc.Values {
					walk(value)
				}
				walkStmtBlock(cc.Body)
			}
			walkStmtBlock(s.DefaultCase)
		case *frontend.BlockStmt:
			walkStmtBlock(s)
		case *frontend.ExprStatement:
			walk(s.Expr)
		case *frontend.TryStmt:
			walkStmtBlock(s.Body)
			walkStmtBlock(s.CatchBody)
		case *frontend.DeferStmt:
			walkStmtBlock(s.Body)
		}
	}
	walk(method.ExprBody)
	walkStmtBlock(method.Body)
}

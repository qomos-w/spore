// compiler_typedecl.go compiles struct, class, interface and variable declarations.

package bytecode

import "github.com/qomos-w/spore/internal/script/frontend"

// --- Struct compilation ---

func (c *compiler) compileStructDecl(info structInfo) {
	c.structs[info.name] = info
}

// --- Class compilation ---

func (c *compiler) compileClassDecl(info classInfo) {
	c.classes[info.name] = info

	// Compile methods as separate function chunks.
	for _, method := range info.methods {
		methodKey := info.name + "." + method.Name.Value
		paramNames := funParamNames(method)
		paramTypes := funParamTypes(method)
		returnType := funReturnType(method)

		savedChunk := c.chunk
		savedLocals := c.locals
		savedDepth := c.scopeDepth
		savedClass := c.currentClassName
		savedReturnHint := c.returnTypeHint
		savedStreamFun := c.currentStreamFun
		savedCapturedNames := c.capturedNames
		savedCapturedOrder := c.capturedOrder
		savedHandlerDepth := c.handlerDepth
		savedInDeferBody := c.inDeferBody

		c.chunk = newChunk()
		c.chunk.sourceName = methodKey
		c.locals = make([]local, 0)
		c.peakLocals = 0
		c.scopeDepth = 0
		c.currentClassName = info.name
		c.handlerDepth = 0
		c.inDeferBody = 0

		// Pre-compute captures for the method body (`this` included).
		c.capturedNames, c.capturedOrder = computeFunctionCaptures(paramNames, method.Body, true, nil)

		// "this" as local 0.
		c.addLocalWithType("this", info.name)
		for i, name := range paramNames {
			typeName := ""
			if i < len(paramTypes) {
				typeName = c.resolveType(paramTypes[i])
			}
			c.addLocalWithType(name, typeName)
		}
		// Parameters captured by lambdas are boxed into cells at entry.
		// `this` is never boxed; it is boxed at closure-creation time.
		c.boxCapturedParams(paramNames)
		c.returnTypeHint = ""
		c.currentStreamFun = false
		if resolved := c.resolveType(returnType); resolved == "long" || resolved == "ulong" || resolved == "double" {
			c.returnTypeHint = resolved
		}

		if method.Body != nil && len(method.Body.Stmts) > 0 {
			c.compileBlock(method.Body)
		} else if method.ExprBody != nil {
			c.compileExpression(method.ExprBody)
			c.emit(opReturn, 0, c.curLine)
			c.functions[methodKey] = c.chunk
			c.funcInfo[methodKey] = &functionInfo{
				name:       methodKey,
				paramCount: len(paramNames) + 1,
				paramTypes: append([]string(nil), paramTypes...),
				localCount: c.peakLocals,
				returnType: returnType,
			}
			c.chunk.LocalCount = c.peakLocals
			c.chunk = savedChunk
			c.locals = savedLocals
			c.scopeDepth = savedDepth
			c.currentClassName = savedClass
			c.returnTypeHint = savedReturnHint
			c.currentStreamFun = savedStreamFun
			c.capturedNames = savedCapturedNames
			c.capturedOrder = savedCapturedOrder
			c.handlerDepth = savedHandlerDepth
			c.inDeferBody = savedInDeferBody
			continue
		}
		c.emit(opReturnVoid, 0, c.curLine)

		c.functions[methodKey] = c.chunk
		c.funcInfo[methodKey] = &functionInfo{
			name:       methodKey,
			paramCount: len(paramNames) + 1, // +1 for this
			paramTypes: append([]string(nil), paramTypes...),
			localCount: c.peakLocals,
			returnType: returnType,
		}
		c.chunk.LocalCount = c.peakLocals

		c.chunk = savedChunk
		c.locals = savedLocals
		c.scopeDepth = savedDepth
		c.currentClassName = savedClass
		c.returnTypeHint = savedReturnHint
		c.currentStreamFun = savedStreamFun
		c.capturedNames = savedCapturedNames
		c.capturedOrder = savedCapturedOrder
		c.handlerDepth = savedHandlerDepth
		c.inDeferBody = savedInDeferBody
	}
}

// --- Interface compilation ---

func (c *compiler) compileInterfaceDecl(info interfaceInfo) {
	c.interfaces[info.name] = info
}

// synthesizeInterfaceDefaultMethods appends default method bodies declared on
// implemented interfaces to a class's own method list when neither the class
// nor any ancestor provides the method. The synthesized methods are marked
// open so subclasses may override the inherited default. This makes interface
// defaults flow through ordinary method compilation, vtable construction, and
// name-based dispatch like regular class methods.
func (c *compiler) synthesizeInterfaceDefaultMethods(className string) {
	info, ok := c.classes[className]
	if !ok {
		return
	}
	alreadySynthesized := make(map[string]bool)
	for _, ifaceName := range info.implements {
		iface, ok := c.interfaces[ifaceName]
		if !ok {
			continue // unknown_interface is reported by contract validation
		}
		for i := range iface.methods {
			m := iface.methods[i]
			if m.Body == nil {
				continue
			}
			if _, provided := c.lookupMethodOnAncestor(info.name, m.Name.Value); provided {
				continue // class or an ancestor already provides the method
			}
			if alreadySynthesized[m.Name.Value] {
				continue // an earlier interface already contributed this default
			}
			alreadySynthesized[m.Name.Value] = true
			// Synthesize a class-method view of the interface default: the
			// frontend signature's own AST nodes, marked open so subclasses
			// may override the inherited default.
			info.methods = append(info.methods, &frontend.FunStmt{
				Name:       m.Name,
				Params:     m.Params,
				ReturnType: m.ReturnType,
				Body:       m.Body,
				IsOpen:     true,
			})
		}
	}
	c.classes[className] = info
}

// --- Var compilation ---

func (c *compiler) compileVarDecl(v *frontend.VarStmt) {
	name := v.Name.Value
	if qualified, ok := c.moduleGlobalNames[v]; ok {
		name = qualified
	}
	c.compileVarDeclWithName(v, name)
}

func (c *compiler) compileVarDeclWithName(v *frontend.VarStmt, name string) {
	typeName := typeAnnotationName(v.Type_)
	// Set type hint so literal compilation picks the right encoding.
	prevHint := c.typeHint
	resolved := c.resolveType(typeName)
	// Function-typed slots validate their initializer at compile time.
	c.checkFunTypeValueAssign(resolved, name, v.Value)
	if resolved == "long" || resolved == "ulong" || resolved == "double" {
		c.typeHint = resolved
	} else if typeName == "long" || typeName == "ulong" || typeName == "double" {
		c.typeHint = typeName
	} else {
		c.typeHint = ""
	}

	if c.scopeDepth == 0 {
		// Global variable.
		idx := len(c.globals)
		c.globals[name] = idx
		if v.Value != nil {
			c.compileExpression(v.Value)
			c.emit(opStoreGlobal, int32(idx), c.curLine)
		}
	} else {
		// Local variable.
		c.addLocalWithType(name, resolved)
		if c.capturedNames[name] {
			// Box captured locals into cells so closures share the slot.
			if v.Value != nil {
				c.compileExpression(v.Value)
				c.emit(opMakeCell, 0, c.curLine)
				localIdx := c.resolveLocal(name)
				c.emit(opStoreLocal, int32(localIdx), c.curLine)
			} else {
				c.emitZeroValue(resolved)
				c.emit(opMakeCell, 0, c.curLine)
				localIdx := c.resolveLocal(name)
				c.emit(opStoreLocal, int32(localIdx), c.curLine)
			}
			if localIdx := c.resolveLocal(name); localIdx >= 0 {
				c.locals[localIdx].isCell = true
			}
		} else if v.Value != nil {
			c.compileExpression(v.Value)
			localIdx := c.resolveLocal(name)
			c.emit(opStoreLocal, int32(localIdx), c.curLine)
		}
	}
	c.typeHint = prevHint
}

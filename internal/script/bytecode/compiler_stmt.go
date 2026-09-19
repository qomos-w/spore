// compiler_stmt.go compiles statement forms: blocks, control flow, returns,
// yields, loops, try/defer and expression statements. compileStatement is the
// single dispatch point for every body statement form.

package bytecode

import "github.com/qomos-w/spore/internal/script/frontend"

// --- Block compilation ---

func (c *compiler) compileBlock(block *frontend.BlockStmt) {
	c.scopeDepth++
	if block != nil {
		for _, stmt := range block.Stmts {
			c.compileStatement(stmt)
		}
	}
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

// compileStatement dispatches one body statement to its lowering. This is the
// only place a statement form is wired into the compiler: adding a new statement
// means adding one case here (plus its frontend node and parser), instead of
// touching an adapter type, two conversion switches and a private statement IR.
func (c *compiler) compileStatement(stmt frontend.Statement) {
	switch s := stmt.(type) {
	case *frontend.VarStmt:
		c.compileVarDecl(s)
	case *frontend.ReturnStmt:
		c.compileReturnStmt(s)
	case *frontend.YieldStmt:
		c.compileYieldStmt(s)
	case *frontend.IfStmt:
		c.compileIfStmt(s)
	case *frontend.WhileStmt:
		c.compileWhileStmt(s)
	case *frontend.ForStmt:
		c.compileForStmt(s)
	case *frontend.WhenStmt:
		c.compileWhenStmt(s)
	case *frontend.BreakStmt:
		c.compileBreakStmt()
	case *frontend.ContinueStmt:
		c.compileContinueStmt()
	case *frontend.BlockStmt:
		c.compileBlock(s)
	case *frontend.ExprStatement:
		if s.Expr != nil {
			c.compileExpression(s.Expr)
			c.emitPopAfterExpression()
		}
	case *frontend.TryStmt:
		c.compileTryStmt(s)
	case *frontend.DeferStmt:
		c.compileDeferStmt(s)
	}
}

// emitPopAfterExpression emits a POP unless the last instruction was a store,
// in which case it fuses the store into a store-and-pop opcode.
func (c *compiler) emitPopAfterExpression() {
	if len(c.chunk.code) > 0 {
		lastIdx := len(c.chunk.code) - 1
		last := c.chunk.code[lastIdx]
		switch last.op {
		case opStoreLocal:
			c.chunk.code[lastIdx].op = opStoreLocalPop
		case opStoreGlobal:
			c.chunk.code[lastIdx].op = opStoreGlobalPop
		case opStoreCell:
			c.chunk.code[lastIdx].op = opStoreCellPop
		case opIncLocal, opDecLocal, opAddLocalInt, opConcatLocalConstString, opArrayPushLocalConstString, opMapDelete:
			// fused local mutators and map delete don't push a value; nothing to pop
		default:
			c.emit(opPop, 0, c.curLine)
		}
	} else {
		c.emit(opPop, 0, c.curLine)
	}
}

func (c *compiler) compileReturnStmt(r *frontend.ReturnStmt) {
	if c.inDeferBody > 0 {
		c.addCompileError("return_in_defer", "bytecode/control/defer", "return is not allowed inside a defer body")
		return
	}
	if r.Value != nil {
		prevHint := c.typeHint
		if c.returnTypeHint == "long" || c.returnTypeHint == "ulong" || c.returnTypeHint == "double" {
			c.typeHint = c.returnTypeHint
		} else {
			c.typeHint = ""
		}
		c.compileExpression(r.Value)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
	} else {
		c.emit(opReturnVoid, 0, c.curLine)
	}
}

func (c *compiler) compileYieldStmt(y *frontend.YieldStmt) {
	if c.inDeferBody > 0 {
		c.addCompileError("yield_in_defer", "bytecode/control/defer", "yield is not allowed inside a defer body")
		return
	}
	if !c.currentStreamFun {
		c.addCompileError("yield_requires_stream_fun", "bytecode/callable/stream", "yield is only allowed inside stream fun")
		return
	}
	if c.handlerDepth > 0 {
		c.addCompileError("yield_disallowed_in_try", "bytecode/callable/stream", "yield is not allowed inside a try block: the catch context cannot be preserved across a stream suspension; move the error-prone segment into a helper fun and yield its result")
		return
	}
	if y.Value == nil {
		c.addCompileError("yield_value_required", "bytecode/callable/stream", "yield requires a value")
		return
	}
	c.compileExpression(y.Value)
	c.emit(opYield, 0, c.curLine)
}

func (c *compiler) compileIfStmt(s *frontend.IfStmt) {
	c.compileExpression(s.Condition)
	jumpIfFalse := c.emit(opJumpIfFalse, 0, c.curLine)

	c.compileBlock(s.Consequence)

	switch alt := s.Alternative.(type) {
	case *frontend.IfStmt:
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
		c.compileIfStmt(alt)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
	case *frontend.BlockStmt:
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
		c.compileBlock(alt)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
	default:
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
	}
}

func (c *compiler) compileWhileStmt(s *frontend.WhileStmt) {
	// Optimized layout: jump over body to condition on first entry, then
	// condition at bottom with JUMP_IF_TRUE back to body. This eliminates
	// one unconditional jump per iteration.
	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	c.compileBlock(s.Body)

	checkPos := c.chunk.size()
	// Patch continue jumps to the condition check.
	for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
		c.chunk.patchJump(jump, checkPos)
	}

	if !c.tryEmitLocalLocalIntLtLoopBranch(s.Condition, loopStart) {
		c.compileExpression(s.Condition)
		c.emit(opJumpIfTrue, int32(loopStart), c.curLine)
	}

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
}

func (c *compiler) compileForStmt(s *frontend.ForStmt) {
	if s.IsForIn {
		c.compileForIn(s)
		return
	}

	// C-style for: for (init; cond; update) { body }
	// Optimized layout: init; JUMP check; loopStart: body; update;
	// check: condition; JUMP_IF_TRUE loopStart; exit:
	// This eliminates one unconditional jump per iteration.
	c.scopeDepth++
	if s.Init != nil {
		c.compileStatement(s.Init)
	}

	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	c.compileBlock(s.Body)

	// Update expression (if any). Continue jumps target here.
	if s.Update != nil {
		updatePos := c.chunk.size()
		for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
			c.chunk.patchJump(jump, updatePos)
		}
		c.compileExpression(s.Update)
		c.emitPopAfterExpression()
	}

	// Condition check at bottom.
	checkPos := c.chunk.size()
	if s.Condition != nil {
		if !c.tryEmitLocalLocalIntLtLoopBranch(s.Condition, loopStart) {
			c.compileExpression(s.Condition)
			c.emit(opJumpIfTrue, int32(loopStart), c.curLine)
		}
	} else {
		// Infinite loop.
		c.emit(opJump, int32(loopStart), c.curLine)
	}

	// Patch continue jumps that had no update target to the check.
	if s.Update == nil {
		for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
			c.chunk.patchJump(jump, checkPos)
		}
	}

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

func (c *compiler) compileForIn(s *frontend.ForStmt) {
	c.scopeDepth++

	// Store iterable in a hidden local.
	collLocal := c.addLocal("__iter_coll__")
	c.compileExpression(s.Iterable)
	c.emit(opStoreLocal, int32(collLocal), c.curLine)

	// Initialize index to 0.
	idxLocal := c.addLocal("__iter_idx__")
	zeroIdx := c.chunk.addConstant(int32(0))
	c.emit(opPushInt, int32(zeroIdx), c.curLine)
	c.emit(opStoreLocal, int32(idxLocal), c.curLine)

	// Jump over body to condition on first entry.
	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	// Body: var item = coll[idx]; <body>.
	itemLocal := c.addLocal(s.Variable)
	c.emit(opLoadLocal, int32(collLocal), c.curLine)
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	c.emit(opIterItem, 0, c.curLine)
	if c.capturedNames[s.Variable] {
		// Loop variable captured by a closure: store through a fresh cell
		// each iteration (per-iteration binding semantics).
		c.emit(opMakeCell, 0, c.curLine)
		c.locals[itemLocal].isCell = true
	}
	c.emit(opStoreLocal, int32(itemLocal), c.curLine)

	c.compileBlock(s.Body)

	// Increment: idx++ (continue target).
	incrementPos := c.chunk.size()
	for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
		c.chunk.patchJump(jump, incrementPos)
	}
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	oneIdx := c.chunk.addConstant(int32(1))
	c.emit(opPushInt, int32(oneIdx), c.curLine)
	c.emit(opAdd, 0, c.curLine)
	c.emit(opStoreLocal, int32(idxLocal), c.curLine)

	// Condition: idx < len(coll).
	checkPos := c.chunk.size()
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	c.emit(opLoadLocal, int32(collLocal), c.curLine)
	c.emit(opIterLen, 0, c.curLine)
	c.emit(opLt, 0, c.curLine)
	c.emit(opJumpIfTrue, int32(loopStart), c.curLine)

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

func (c *compiler) compileWhenStmt(s *frontend.WhenStmt) {
	// Compile when as a chain of if-else blocks.
	c.compileExpression(s.Expr)
	subjectLocal := c.addLocal("__when__")
	c.emit(opStoreLocal, int32(subjectLocal), c.curLine)

	var endJumps []int
	for _, cc := range s.Cases {
		for _, val := range cc.Values {
			c.emit(opLoadLocal, int32(subjectLocal), c.curLine)
			c.compileExpression(val)
			c.emit(opEq, 0, c.curLine)
			matchJump := c.emit(opJumpIfFalse, 0, c.curLine)
			var guardJump int
			haveGuard := cc.Guard != nil
			if haveGuard {
				c.compileExpression(cc.Guard)
				guardJump = c.emit(opJumpIfFalse, 0, c.curLine)
			}
			c.compileBlock(cc.Body)
			endJumps = append(endJumps, c.emit(opJump, 0, c.curLine))
			afterBody := c.chunk.size()
			c.chunk.patchJump(matchJump, afterBody)
			if haveGuard {
				c.chunk.patchJump(guardJump, afterBody)
			}
		}
		if cc.Variable != "" && cc.TypeAnnotation != nil {
			c.emit(opLoadLocal, int32(subjectLocal), c.curLine)
			typeNameIdx := c.chunk.addConstant(cc.TypeAnnotation.Name)
			c.emit(opIs, int32(typeNameIdx), c.curLine)
			matchJump := c.emit(opJumpIfFalse, 0, c.curLine)
			var guardJump int
			haveGuard := cc.Guard != nil
			if haveGuard {
				c.compileExpression(cc.Guard)
				guardJump = c.emit(opJumpIfFalse, 0, c.curLine)
			}
			c.compileBlock(cc.Body)
			endJumps = append(endJumps, c.emit(opJump, 0, c.curLine))
			afterBody := c.chunk.size()
			c.chunk.patchJump(matchJump, afterBody)
			if haveGuard {
				c.chunk.patchJump(guardJump, afterBody)
			}
		}
	}

	if s.DefaultCase != nil {
		c.compileBlock(s.DefaultCase)
	}

	endPos := c.chunk.size()
	for _, jump := range endJumps {
		c.chunk.patchJump(jump, endPos)
	}
}

func (c *compiler) compileBreakStmt() {
	if len(c.loopStack) == 0 {
		c.addCompileError("break_outside_loop", "bytecode/control/loop", "break outside of loop")
		return
	}
	if c.inDeferBody > 0 {
		c.addCompileError("break_in_defer", "bytecode/control/defer", "break is not allowed inside a defer body")
		return
	}
	// Leaving the loop discards try handlers opened inside the loop body.
	lc := &c.loopStack[len(c.loopStack)-1]
	for i := lc.handlerDepth; i < c.handlerDepth; i++ {
		c.emit(opPopHandler, 0, c.curLine)
	}
	jump := c.emit(opJump, 0, c.curLine)
	lc.breakJumps = append(lc.breakJumps, jump)
}

func (c *compiler) compileContinueStmt() {
	if len(c.loopStack) == 0 {
		c.addCompileError("continue_outside_loop", "bytecode/control/loop", "continue outside of loop")
		return
	}
	if c.inDeferBody > 0 {
		c.addCompileError("continue_in_defer", "bytecode/control/defer", "continue is not allowed inside a defer body")
		return
	}
	lc := &c.loopStack[len(c.loopStack)-1]
	// Jumping to the next iteration discards try handlers opened inside the
	// current loop body; handlers opened before the loop stay active.
	for i := lc.handlerDepth; i < c.handlerDepth; i++ {
		c.emit(opPopHandler, 0, c.curLine)
	}
	if lc.continuePos != 0 {
		// Target already resolved (e.g., for-in loop).
		c.emit(opJump, int32(lc.continuePos), c.curLine)
	} else {
		// Target not yet known (C-style for loop); record jump to patch later.
		jump := c.emit(opJump, 0, c.curLine)
		lc.continueJumps = append(lc.continueJumps, jump)
	}
}

// compileTryStmt compiles try { body } catch (e) { catchBody } as:
//
//	PUSH_HANDLER catch          ; try start
//	<body>
//	POP_HANDLER
//	JUMP end
//	catch:  STORE_LOCAL_POP e   ; catch start (handler target); stack holds error value
//	<catchBody>
//	end:
//
// The handler records the frame state at PUSH time; the VM restores it when
// dispatching to the catch block, so partial operands from the failed body are
// discarded. The catch variable is a map<string, any> holding the error
// attributes (code/category/message/callable/line/...).
func (c *compiler) compileTryStmt(s *frontend.TryStmt) {
	pushIP := c.emit(opPushHandler, 0, c.curLine)
	c.handlerDepth++
	c.compileBlock(s.Body)
	c.handlerDepth--
	c.emit(opPopHandler, 0, c.curLine)
	jumpEnd := c.emit(opJump, 0, c.curLine)

	catchStart := c.chunk.size()
	c.chunk.patchJump(pushIP, catchStart)

	// Bind the error value (top of stack) to the catch variable.
	catchVar := ""
	if s.CatchVar != nil {
		catchVar = s.CatchVar.Value
	}
	if catchVar == "" {
		catchVar = "__err__"
	}
	c.scopeDepth++
	catchLocal := c.addLocalWithType(catchVar, "map")
	if c.capturedNames[catchVar] {
		// Captured by a closure inside the catch body: bind through a cell so
		// the closure observes the caught error value.
		c.emit(opMakeCell, 0, c.curLine)
		c.locals[catchLocal].isCell = true
	}
	c.emit(opStoreLocalPop, int32(catchLocal), c.curLine)

	c.compileBlock(s.CatchBody)
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)

	c.chunk.patchJump(jumpEnd, c.chunk.size())
}

// compileDeferStmt compiles defer { body } as:
//
//	PUSH_DEFER bodyStart
//	JUMP after
//	bodyStart: <body>
//	END_DEFER
//	after:
//
// At runtime PUSH_DEFER registers the body address on the VM defer stack;
// when the enclosing function returns (or unwinds on an uncaught error) the
// VM runs registered bodies in reverse registration order. The body is skipped
// during normal control flow via the jump.
func (c *compiler) compileDeferStmt(s *frontend.DeferStmt) {
	if c.inDeferBody > 0 {
		c.addCompileError("defer_in_defer", "bytecode/control/defer", "defer cannot be nested inside a defer body")
		return
	}
	pushIP := c.emit(opPushDefer, 0, c.curLine)
	jumpOver := c.emit(opJump, 0, c.curLine)
	bodyStart := c.chunk.size()
	c.chunk.patchJump(pushIP, bodyStart)

	c.inDeferBody++
	c.compileBlock(s.Body)
	c.inDeferBody--
	c.emit(opEndDefer, 0, c.curLine)

	c.chunk.patchJump(jumpOver, c.chunk.size())
}

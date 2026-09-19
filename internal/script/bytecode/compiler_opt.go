// compiler_opt.go emits specialized instruction fast paths for common patterns such as increments, packed local operations and fused add/concat assignments.

package bytecode

import (
	"github.com/qomos-w/spore/internal/script/frontend"
)

// isIncDecPattern detects `x = x + 1` and `x = x - 1`.
func isIncDecPattern(target string, val frontend.Expression) (inc bool, ok bool) {
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok {
		return false, false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || left.Value != target {
		return false, false
	}
	lit, ok := bin.Right.(*frontend.IntLiteral)
	if !ok || lit.Value != 1 {
		return false, false
	}
	switch bin.Operator {
	case "+":
		return true, true
	case "-":
		return false, true
	}
	return false, false
}

func packLocalPair(dst, src int) (int32, bool) {
	if dst < 0 || src < 0 || dst > 0xFFFF || src > 0xFFFF {
		return 0, false
	}
	return int32(uint32(dst)&0xFFFF | (uint32(src)&0xFFFF)<<16), true
}

func unpackLocalPair(operand int32) (dst, src int) {
	raw := uint32(operand)
	return int(raw & 0xFFFF), int((raw >> 16) & 0xFFFF)
}

func packLocalLocalTarget(left, right, target int) (int32, bool) {
	if left < 0 || right < 0 || target < 0 || left > 0xFF || right > 0xFF || target > 0xFFFF {
		return 0, false
	}
	return int32(uint32(left)&0xFF | (uint32(right)&0xFF)<<8 | (uint32(target)&0xFFFF)<<16), true
}

func unpackLocalLocalTarget(operand int32) (left, right, target int) {
	raw := uint32(operand)
	return int(raw & 0xFF), int((raw >> 8) & 0xFF), int((raw >> 16) & 0xFFFF)
}

func (c *compiler) tryEmitLocalLocalIntLtLoopBranch(condition frontend.Expression, target int) bool {
	bin, ok := condition.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "<" {
		return false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || c.localTypeName(left.Value) != "int" {
		return false
	}
	right, ok := bin.Right.(*frontend.IdentExpr)
	if !ok || c.localTypeName(right.Value) != "int" {
		return false
	}
	if c.isCellLocal(left.Value) || c.isCellLocal(right.Value) {
		return false
	}
	leftIdx := c.resolveLocal(left.Value)
	rightIdx := c.resolveLocal(right.Value)
	operand, ok := packLocalLocalTarget(leftIdx, rightIdx, target)
	if !ok {
		return false
	}
	c.emit(opJumpLocalLtInt, operand, c.curLine)
	return true
}

func (c *compiler) tryCompileAddLocalIntAssign(target string, val frontend.Expression) bool {
	if c.localTypeName(target) != "int" {
		return false
	}
	if c.isCellLocal(target) {
		return false
	}
	dst := c.resolveLocal(target)
	if dst < 0 {
		return false
	}
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "+" {
		return false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || left.Value != target {
		return false
	}
	right, ok := bin.Right.(*frontend.IdentExpr)
	if !ok || c.localTypeName(right.Value) != "int" {
		return false
	}
	src := c.resolveLocal(right.Value)
	if src < 0 {
		return false
	}
	operand, ok := packLocalPair(dst, src)
	if !ok {
		return false
	}
	c.emit(opAddLocalInt, operand, c.curLine)
	return true
}

func (c *compiler) tryCompileConcatLocalConstStringAssign(target string, val frontend.Expression) bool {
	if c.localTypeName(target) != "string" {
		return false
	}
	if c.isCellLocal(target) {
		return false
	}
	dst := c.resolveLocal(target)
	if dst < 0 {
		return false
	}
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "+" {
		return false
	}
	// target + "literal" or "literal" + target
	var constIdx int
	found := false
	if left, ok := bin.Left.(*frontend.IdentExpr); ok && left.Value == target {
		if lit, ok := bin.Right.(*frontend.StringLiteral); ok {
			constIdx = c.chunk.addConstant(lit.Value)
			found = true
		}
	}
	if !found {
		if right, ok := bin.Right.(*frontend.IdentExpr); ok && right.Value == target {
			if lit, ok := bin.Left.(*frontend.StringLiteral); ok {
				constIdx = c.chunk.addConstant(lit.Value)
				found = true
			}
		}
	}
	if !found {
		return false
	}
	operand, ok := packLocalPair(dst, constIdx)
	if !ok {
		return false
	}
	c.emit(opConcatLocalConstString, operand, c.curLine)
	return true
}

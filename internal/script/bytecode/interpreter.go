package bytecode

import (
	"context"
	"github.com/qomos-w/spore/invoke"

	"github.com/qomos-w/spore/internal/script/vm"
)

const maxCallDepth = 1000

type nativeValueResolver interface {
	ResolveImportedNativeValue(slot int) (vm.Value, error)
}

type nativeInvoker interface {
	InvokeVMNative(ctx context.Context, callable string, args []vm.Value) (vm.Value, bool, error)
}

// callFrame tracks one activation record on the call stack.
type callFrame struct {
	chunk       *chunk
	ip          int
	stackBase   int
	localsBase  int
	localsCount int
	function    string
}

// closureInstance is one runtime closure: a compiled lambda chunk bound to
// the capture cells it closed over at creation time.
type closureInstance struct {
	chunk    *chunk
	captures []vm.Value // capture-cell values, in the order the lambda body expects
}

// vmHandler is one active try/catch error handler on the handler stack. It
// captures the frame state at opPushHandler time so a RuntimeError raised
// anywhere inside the protected body (including nested calls) can unwind
// back to the catch block with a consistent frame.
type vmHandler struct {
	catchIP     int
	sp          int
	stackBase   int
	localsBase  int
	localsLen   int
	localsCount int
	frameDepth  int
	chunk       *chunk
	function    string
}

// deferEntry is one registered defer body on the defer stack. startIP points
// at the first instruction of the deferred block.
type deferEntry struct {
	startIP int
}

// Interpreter executes compiled bytecode chunks on a VM.
type Interpreter struct {
	vm_            *vm.VM
	ctx            context.Context
	budget         invoke.ExecutionBudget
	execution      *invoke.ExecutionState
	rootProviderID int
	chunk          *chunk
	functions      map[string]*chunk
	ip             int
	stack          []vm.Value
	sp             int
	locals         []vm.Value
	globals        []vm.Value
	nativeValues   []vm.Value
	nativeResolver nativeValueResolver
	callFrames     []callFrame
	stackBase      int
	localsBase     int
	localsCount    int
	function       string
	nativeInvoker  nativeInvoker
	callArgs       [8]vm.Value
	cells          []vm.Value        // capture cells (index = cell id, value = current content)
	closures       []closureInstance // closure table (index = closure id)
	handlerStack   []vmHandler       // active try/catch handlers (innermost last)
	deferStack     []deferEntry      // registered defer bodies (execution order: innermost first)
}

// newInterpreter creates an interpreter bound to the given VM.
func newInterpreter(v *vm.VM) *Interpreter {
	interp := &Interpreter{
		vm_:        v,
		stack:      make([]vm.Value, 1024),
		locals:     make([]vm.Value, 0, 256),
		globals:    make([]vm.Value, 256),
		callFrames: make([]callFrame, 0, 64),
	}
	interp.rootProviderID = v.AddRootProvider(func(visit func(vm.Value)) {
		for i := 0; i < interp.sp; i++ {
			visit(interp.stack[i])
		}
		for _, val := range interp.locals {
			visit(val)
		}
		for _, val := range interp.globals {
			visit(val)
		}
		for _, val := range interp.nativeValues {
			visit(val)
		}
		// Capture cells are interpreter-owned; their contents must stay
		// reachable even when no live local slot references them anymore.
		for _, val := range interp.cells {
			visit(val)
		}
	})
	return interp
}

// newCell allocates a capture cell holding val and returns its cell value.
func (interp *Interpreter) newCell(val vm.Value) vm.Value {
	interp.cells = append(interp.cells, val)
	return vm.EncodeCellIndex(uint32(len(interp.cells) - 1))
}

// cellValue returns the current content of the cell referenced by cell.
func (interp *Interpreter) cellValue(cell vm.Value) (vm.Value, bool) {
	if !vm.IsCell(cell) {
		return vm.EncodeInt(0), false
	}
	idx := int(vm.DecodeCellIndex(cell))
	if idx < 0 || idx >= len(interp.cells) {
		return vm.EncodeInt(0), false
	}
	return interp.cells[idx], true
}

// setCellValue stores val into the cell referenced by cell.
func (interp *Interpreter) setCellValue(cell vm.Value, val vm.Value) bool {
	if !vm.IsCell(cell) {
		return false
	}
	idx := int(vm.DecodeCellIndex(cell))
	if idx < 0 || idx >= len(interp.cells) {
		return false
	}
	interp.cells[idx] = val
	return true
}

// newClosure allocates a closure binding fnChunk to the given capture cells
// and returns the first-class closure value.
func (interp *Interpreter) newClosure(fnChunk *chunk, captures []vm.Value) vm.Value {
	interp.closures = append(interp.closures, closureInstance{chunk: fnChunk, captures: captures})
	return vm.EncodeClosureIndex(uint32(len(interp.closures) - 1))
}

func (interp *Interpreter) setNativeValues(values []vm.Value) {
	if interp == nil {
		return
	}
	interp.nativeValues = values
}

func (interp *Interpreter) setNativeValueResolver(resolver nativeValueResolver) {
	if interp == nil {
		return
	}
	interp.nativeResolver = resolver
}

// --- Stack helpers ---

func (interp *Interpreter) push(val vm.Value) {
	if interp.sp >= len(interp.stack) {
		interp.stack = append(interp.stack, make([]vm.Value, 256)...)
	}
	interp.stack[interp.sp] = val
	interp.sp++
}

func (interp *Interpreter) pop() vm.Value {
	if interp.sp == 0 {
		panic("stack underflow")
	}
	interp.sp--
	return interp.stack[interp.sp]
}

func (interp *Interpreter) peek() vm.Value {
	if interp.sp == 0 {
		panic("stack underflow")
	}
	return interp.stack[interp.sp-1]
}

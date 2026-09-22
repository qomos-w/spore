package vm

import (
	"fmt"
	"strings"
)

const (
	maxInlineLong   = int64(1<<47 - 1)
	minInlineLong   = -1 << 47
	maxInlineULong  = uint64(1<<48 - 1)
	longHeapMarker  = uint64(0xFFFFFFFE00000001)
	uLongHeapMarker = uint64(0xFFFFFFFE00000002)
)

type rootProvider func(func(value))

// vm is the virtual machine core.
// It holds the unified memory pool, registries, and the operand-stack
// capacity consumed by the interpreter's single execution stack.
type vm struct {
	// Unified memory pool
	memory   []uint64
	memTop   int
	freeList []freeSlot

	// Operand-stack capacity for the single execution stack.
	//
	// The VM no longer owns an operand stack of its own: the bytecode
	// interpreter is the sole owner (see bytecode/interpreter.go) and sizes
	// its stack from this value. It lives here because NewVM is the
	// configuration entry point (script.RuntimeOptions.VMHeapSlots ->
	// NewVMEvaluatorWith -> NewVM).
	stackCapacity int

	// Additional GC roots contributed by higher runtime layers.
	rootProviders      map[int]rootProvider
	rootProviderOrder  []int
	nextRootProviderID int

	// Temporary-root machinery: slot table + pooled scopes. Registering a
	// temporary root must not allocate in steady state (conversion loops
	// acquire/release per element), so slots are recycled through a free
	// list and scope objects (with their backing value buffers) through a
	// pool instead of the persistent-provider map above.
	rootSlots     []rootProvider
	freeRootSlots []int
	scopePool     []*rootScope
	scopeSeq      int64

	// Direct pointers: handle payload is the memory pool index.

	// String pool
	stringPool *stringPool

	// Garbage collector
	gc *gc

	// Class registry
	classRegistry *classRegistry

	// Interface registry
	ifaceRegistry *interfaceRegistry

	// Struct registry
	structRegistry *structRegistry

	// Enum registry
	enumRegistry *enumRegistry

	// Function registry
	funcReg *functionRegistry

	// Call stack (for error reporting)
	callStack *callStack

	// Source file tracking
	sourceFiles   []string
	sourceFileMap map[string]int
}

// newVM creates a new VM instance. stackCapacity is the operand-stack
// capacity handed to the interpreter's single execution stack (see
// bytecode/interpreter.go); the VM itself owns no operand stack.
func newVM(memorySize, stackCapacity int) *vm {
	v := &vm{
		memory:             make([]uint64, memorySize),
		memTop:             1,
		freeList:           make([]freeSlot, 0),
		stackCapacity:      stackCapacity,
		rootProviders:      make(map[int]rootProvider),
		rootProviderOrder:  make([]int, 0),
		nextRootProviderID: 1,
	}

	v.stringPool = newStringPool(v)
	v.gc = newGC(v)
	v.classRegistry = newClassRegistry()
	v.ifaceRegistry = newInterfaceRegistry()
	v.structRegistry = newStructRegistry()
	v.enumRegistry = newEnumRegistry()
	v.funcReg = newFunctionRegistry()
	v.callStack = newCallStack(1024)
	v.sourceFiles = make([]string, 0, 16)
	v.sourceFileMap = make(map[string]int)
	return v
}

func (v *vm) addRootProvider(provider rootProvider) int {
	id := v.nextRootProviderID
	v.nextRootProviderID++
	v.rootProviders[id] = provider
	v.rootProviderOrder = append(v.rootProviderOrder, id)
	return id
}

func (v *vm) removeRootProvider(id int) {
	delete(v.rootProviders, id)
	for i, existing := range v.rootProviderOrder {
		if existing != id {
			continue
		}
		v.rootProviderOrder = append(v.rootProviderOrder[:i], v.rootProviderOrder[i+1:]...)
		break
	}
}

// rootScope is a batched temporary GC root: one registered provider whose
// marked set grows via Add and shrinks via Trim. Scopes and their backing
// buffers are pooled per VM, so hot conversion loops (map/array/struct
// boxing) root pending values without heap allocation. Releases must happen
// in LIFO order relative to other scopes.
type rootScope struct {
	vm     *vm
	slot   int
	id     int64
	values []value
	mark   func(visit func(value))
}

// beginRootScope acquires a pooled scope and registers it as a root provider.
func (v *vm) beginRootScope() *rootScope {
	var s *rootScope
	if n := len(v.scopePool); n > 0 {
		s = v.scopePool[n-1]
		v.scopePool = v.scopePool[:n-1]
	} else {
		s = &rootScope{vm: v}
		scoped := s
		s.mark = func(visit func(value)) {
			for i := len(scoped.values) - 1; i >= 0; i-- {
				visit(scoped.values[i])
			}
		}
	}
	if n := len(v.freeRootSlots); n > 0 {
		s.slot = v.freeRootSlots[n-1]
		v.freeRootSlots = v.freeRootSlots[:n-1]
	} else {
		s.slot = len(v.rootSlots)
		v.rootSlots = append(v.rootSlots, nil)
	}
	v.rootSlots[s.slot] = s.mark
	v.scopeSeq++
	s.id = v.scopeSeq
	s.values = s.values[:0]
	return s
}

func (s *rootScope) add(vals ...value) {
	s.values = append(s.values, vals...)
}

// trim drops the last n values from the marked set. Callers may only trim
// values they know are now reachable through another rooted object.
func (s *rootScope) trim(n int) {
	if len(s.values) >= n {
		s.values = s.values[:len(s.values)-n]
	}
}

// end unregisters the scope and returns it (with its buffer) to the pool.
// A second end on the same acquisition is a no-op.
func (s *rootScope) end() {
	if s == nil || s.id == 0 {
		return
	}
	s.vm.rootSlots[s.slot] = nil
	s.vm.freeRootSlots = append(s.vm.freeRootSlots, s.slot)
	s.values = s.values[:0]
	s.id = 0
	s.vm.scopePool = append(s.vm.scopePool, s)
}

func (v *vm) addTemporaryRoot(values ...value) func() {
	s := v.beginRootScope()
	s.add(values...)
	id := s.id
	return func() {
		// Guard against a stale release after the scope was recycled.
		if s.id == id {
			s.end()
		}
	}
}

// --- Memory allocation ---

func (v *vm) allocMemory(size int) int {
	if idx, ok := v.allocFromFreeList(size); ok {
		for i := 0; i < size; i++ {
			v.memory[idx+i] = 0
		}
		return idx
	}

	if v.gc.shouldCollect() {
		v.gc.collect()
		if idx, ok := v.allocFromFreeList(size); ok {
			for i := 0; i < size; i++ {
				v.memory[idx+i] = 0
			}
			return idx
		}
	}

	if v.memTop+size > len(v.memory) {
		v.gc.collect()
		if idx, ok := v.allocFromFreeList(size); ok {
			for i := 0; i < size; i++ {
				v.memory[idx+i] = 0
			}
			return idx
		}
	}

	if v.memTop+size > len(v.memory) {
		vmPanic("out of memory",
			"needed", size,
			"available", len(v.memory)-v.memTop,
			"memTop", v.memTop,
			"totalMemory", len(v.memory))
	}

	idx := v.memTop
	v.memTop += size
	return idx
}

func (v *vm) allocFromFreeList(size int) (int, bool) {
	for i, slot := range v.freeList {
		if slot.size < size {
			continue
		}
		idx := slot.start
		if slot.size == size {
			v.freeList = append(v.freeList[:i], v.freeList[i+1:]...)
		} else {
			v.freeList[i].start += size
			v.freeList[i].size -= size
		}
		return idx, true
	}
	return 0, false
}

func (v *vm) freeRange(start, size int) {
	if size <= 0 {
		return
	}
	for i := 0; i < size && start+i < len(v.memory); i++ {
		v.memory[start+i] = 0
	}
	v.freeList = append(v.freeList, freeSlot{start: start, size: size})
}

func (v *vm) coalesceFreeList() {
	if len(v.freeList) < 2 {
		return
	}
	for i := 0; i < len(v.freeList)-1; i++ {
		for j := i + 1; j < len(v.freeList); j++ {
			if v.freeList[j].start < v.freeList[i].start {
				v.freeList[i], v.freeList[j] = v.freeList[j], v.freeList[i]
			}
		}
	}
	merged := v.freeList[:0]
	for _, slot := range v.freeList {
		if len(merged) == 0 {
			merged = append(merged, slot)
			continue
		}
		last := &merged[len(merged)-1]
		if last.start+last.size == slot.start {
			last.size += slot.size
			continue
		}
		merged = append(merged, slot)
	}
	v.freeList = merged
}

// --- Handle management ---

func (v *vm) createHandle(memIdx int) handle {
	if memIdx <= 0 || memIdx >= len(v.memory) {
		vmPanic("invalid memory index for handle", "memIdx", memIdx, "memorySize", len(v.memory))
	}
	return handle(memIdx)
}

func (v *vm) isInFreeList(idx int) bool {
	for _, slot := range v.freeList {
		if idx >= slot.start && idx < slot.start+slot.size {
			return true
		}
	}
	return false
}

func (v *vm) getMemoryIndex(h handle) int {
	idx := int(h)
	if h == invalidHandle || idx <= 0 || idx >= v.memTop || v.isInFreeList(idx) {
		return -1
	}
	return idx
}

func (v *vm) resolveHandle(h handle) int {
	return v.getMemoryIndex(h)
}

func encodeLongHeapHeader() uint64  { return longHeapMarker }
func encodeULongHeapHeader() uint64 { return uLongHeapMarker }
func isLongHeapHeader(header uint64) bool {
	return header == longHeapMarker
}
func isULongHeapHeader(header uint64) bool {
	return header == uLongHeapMarker
}

func (v *vm) allocNumericHeap(header uint64, payload uint64) handle {
	idx := v.allocMemory(2)
	v.memory[idx] = header
	v.memory[idx+1] = payload
	return v.createHandle(idx)
}

// --- Numeric encoding ---

func (v *vm) encodeLong(val int64) value {
	if val >= minInlineLong && val <= maxInlineLong {
		return encodeLong(val)
	}
	h := v.allocNumericHeap(encodeLongHeapHeader(), uint64(val))
	return encodeHandle(h)
}

func (v *vm) decodeLong(val value) int64 {
	if val.isLong() {
		return val.decodeLong()
	}
	if val.isPointer() {
		idx := v.getMemoryIndex(val.decodeHandle())
		if idx >= 0 && idx+1 < v.memTop && isLongHeapHeader(v.memory[idx]) {
			return int64(v.memory[idx+1])
		}
	}
	panic(fmt.Sprintf("invalid long value: %#x", uint64(val)))
}

func (v *vm) encodeULong(val uint64) value {
	if val <= maxInlineULong {
		return encodeULong(val)
	}
	h := v.allocNumericHeap(encodeULongHeapHeader(), val)
	return encodeHandle(h)
}

func (v *vm) decodeULong(val value) uint64 {
	if val.isULong() {
		return val.decodeULong()
	}
	if val.isPointer() {
		idx := v.getMemoryIndex(val.decodeHandle())
		if idx >= 0 && idx+1 < v.memTop && isULongHeapHeader(v.memory[idx]) {
			return v.memory[idx+1]
		}
	}
	panic(fmt.Sprintf("invalid ulong value: %#x", uint64(val)))
}

func (v *vm) encodeDouble(val float64) value { return encodeDouble(val) }

func (v *vm) decodeDouble(val value) float64 { return val.decodeDouble() }

// --- Stack operations ---

// Operand-stack push/pop/peek live in the interpreter (bytecode package),
// which owns the single execution stack. The VM only carries the configured
// capacity; see stackCapacity and VM.OperandStackCapacity.

// --- String operations ---

func (v *vm) encodeString(s string) value {
	return v.stringPool.intern(s)
}

func (v *vm) decodeString(val value) string {
	if val.isSmallString() {
		return string(val.decodeSmallString())
	}
	if isMediumString(val) {
		offset, length := decodeMediumString(val)
		return v.stringPool.decodeMediumString(offset, length)
	}
	if val.isPointer() {
		h := val.decodeHandle()
		return v.stringPool.decodeLargeString(h)
	}
	return ""
}

func (v *vm) concatStrings(a, b value) value {
	// Fast path: both operands are strings of any tier. Build the result
	// directly in the appropriate VM-side storage without a Go-string
	// round-trip. concatStrings is the hot path of `s = s + ...` loops, and
	// the slow Go-string path below allocates 3+ Go heap objects per call.
	if aBytes, ok := v.stringBytes(a); ok {
		if bBytes, ok := v.stringBytes(b); ok {
			total := len(aBytes) + len(bBytes)
			switch {
			case total == 0:
				return encodeSmallString(nil)
			case total <= 6:
				var buf [6]byte
				copy(buf[:len(aBytes)], aBytes)
				copy(buf[len(aBytes):], bBytes)
				return encodeSmallString(buf[:total])
			case total <= 256:
				return v.stringPool.internMediumFromTwoSources(aBytes, bBytes)
			default:
				return v.stringPool.internLargeFromTwoSources(aBytes, bBytes)
			}
		}
	}

	// Slow path: at least one operand is non-string (int/float/bool/etc.);
	// fall through to fmt-based formatting and Go-string concat.
	var strA, strB string

	if a.isString() {
		strA = v.decodeString(a)
	} else if a.isPointer() {
		h := a.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeStringHeader(v.memory[idx]) {
			strA = v.decodeString(a)
		} else if idx >= 0 && idx < v.memTop && isLongHeapHeader(v.memory[idx]) {
			strA = fmt.Sprintf("%d", v.decodeLong(a))
		} else if idx >= 0 && idx < v.memTop && isULongHeapHeader(v.memory[idx]) {
			strA = fmt.Sprintf("%d", v.decodeULong(a))
		}
	} else if a.isInt() {
		strA = fmt.Sprintf("%d", a.decodeInt())
	} else if a.isFloat() {
		strA = fmt.Sprintf("%g", a.decodeFloat())
	} else if a.isDouble() {
		strA = fmt.Sprintf("%g", v.decodeDouble(a))
	} else if a.isLong() || a.isPointer() {
		strA = fmt.Sprintf("%d", v.decodeLong(a))
	} else if a.isULong() {
		strA = fmt.Sprintf("%d", v.decodeULong(a))
	} else if a.isBool() {
		strA = fmt.Sprintf("%t", a.decodeBool())
	}

	if b.isString() {
		strB = v.decodeString(b)
	} else if b.isPointer() {
		h := b.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeStringHeader(v.memory[idx]) {
			strB = v.decodeString(b)
		} else if idx >= 0 && idx < v.memTop && isLongHeapHeader(v.memory[idx]) {
			strB = fmt.Sprintf("%d", v.decodeLong(b))
		} else if idx >= 0 && idx < v.memTop && isULongHeapHeader(v.memory[idx]) {
			strB = fmt.Sprintf("%d", v.decodeULong(b))
		}
	} else if b.isInt() {
		strB = fmt.Sprintf("%d", b.decodeInt())
	} else if b.isFloat() {
		strB = fmt.Sprintf("%g", b.decodeFloat())
	} else if b.isDouble() {
		strB = fmt.Sprintf("%g", v.decodeDouble(b))
	} else if b.isLong() || b.isPointer() {
		strB = fmt.Sprintf("%d", v.decodeLong(b))
	} else if b.isULong() {
		strB = fmt.Sprintf("%d", v.decodeULong(b))
	} else if b.isBool() {
		strB = fmt.Sprintf("%t", b.decodeBool())
	}

	return v.encodeString(strA + strB)
}

// --- Debug helpers ---

func (v *vm) dumpMemory() string {
	var b strings.Builder
	fmt.Fprintf(&b, "VM Memory (memTop=%d):\n", v.memTop)
	for i := 0; i < v.memTop; i++ {
		fmt.Fprintf(&b, "  [%04d] 0x%016x\n", i, v.memory[i])
	}
	return b.String()
}

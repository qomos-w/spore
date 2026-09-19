package vm

// gc is a mark-sweep garbage collector.
type gc struct {
	vm          *vm
	marked      []bool
	depth       int
	freed       int64
	collections int64
}

type freeSlot struct {
	start int
	size  int
}

func newGC(v *vm) *gc {
	return &gc{
		vm:     v,
		marked: make([]bool, 0),
	}
}

func (g *gc) shouldCollect() bool {
	threshold := len(g.vm.memory) * 3 / 4
	return g.vm.memTop > threshold
}

func (g *gc) collect() {
	g.depth = 0
	g.mark()
	g.sweep()
	g.collections++
}

// mark scans all registered GC roots and marks reachable objects. Roots come
// from root providers (the interpreter's operand stack/locals, session state,
// host-interface tables, ...); the VM itself owns no operand stack.
func (g *gc) mark() {
	if len(g.marked) < len(g.vm.memory) {
		g.marked = make([]bool, len(g.vm.memory))
	} else {
		for i := range g.marked {
			g.marked[i] = false
		}
	}

	for _, id := range g.vm.rootProviderOrder {
		provider := g.vm.rootProviders[id]
		if provider == nil {
			continue
		}
		provider(g.markValue)
	}
}

func (g *gc) markValue(v value) {
	if v.isPointer() {
		h := v.decodeHandle()
		if h == invalidHandle {
			return
		}
		idx := g.vm.getMemoryIndex(h)
		if idx >= 0 && idx < g.vm.memTop {
			g.markFromIndex(idx)
		}
	}
}

func (g *gc) markFromIndex(idx int) {
	if idx < 0 || idx >= g.vm.memTop {
		return
	}

	g.depth++
	if g.depth > 10000 {
		return
	}
	defer func() { g.depth-- }()

	header := g.vm.memory[idx]
	size := g.getObjectSizeAt(idx)
	if size <= 0 || idx+size > g.vm.memTop {
		return
	}

	if g.marked[idx] {
		return
	}
	for i := 0; i < size && idx+i < len(g.marked); i++ {
		g.marked[idx+i] = true
	}

	if isStructHeader(header) {
		_, fieldCount := decodeStructHeader(header)
		for i := 0; i < fieldCount; i++ {
			g.markValue(value(g.vm.memory[idx+1+i]))
		}
		return
	}
	if isArrayHeader(header) {
		storageIdx := g.vm.getArrayStorageIndex(idx)
		if storageIdx < 0 || storageIdx >= g.vm.memTop {
			return
		}
		g.markFromIndex(storageIdx)
		length := int(g.vm.memory[idx+1])
		for i := 0; i < length; i++ {
			g.markValue(value(g.vm.memory[storageIdx+1+i]))
		}
		return
	}
	if isArrayStorageHeader(header) || isLargeStringHeader(header) || isLongHeapHeader(header) || isULongHeapHeader(header) {
		return
	}
	if isMapHeader(header) {
		nodesIdx := g.vm.getMapNodesIndex(idx)
		if nodesIdx >= 0 && nodesIdx < g.vm.memTop {
			g.markFromIndex(nodesIdx)
			capacity := int(g.vm.memory[idx+1])
			for i := 0; i < capacity; i++ {
				nodeOffset := 1 + i*4
				if nodesIdx+nodeOffset+1 >= g.vm.memTop {
					break
				}
				key := value(g.vm.memory[nodesIdx+nodeOffset])
				val := value(g.vm.memory[nodesIdx+nodeOffset+1])
				if key != mapEmptyKey && key != mapTombstone {
					g.markValue(key)
					g.markValue(val)
				}
			}
		}
		return
	}
	if isMapNodesHeader(header) {
		return
	}
	if isObjectHeader(header) {
		for i := 1; i < size; i++ {
			g.markValue(value(g.vm.memory[idx+i]))
		}
		return
	}
}

// sweep frees unmarked objects without moving survivors.
func (g *gc) sweep() {
	freed := 0
	oldMemTop := g.vm.memTop
	g.vm.freeList = g.vm.freeList[:0]

	for i := 1; i < oldMemTop; {
		if i < len(g.marked) && g.marked[i] {
			i++
			continue
		}

		start := i
		for i < oldMemTop && (i >= len(g.marked) || !g.marked[i]) {
			i++
		}
		size := i - start
		if size == 1 && g.vm.memory[start] != 0 {
			continue
		}
		g.vm.freeRange(start, size)
		freed += size
	}

	g.vm.coalesceFreeList()
	g.freed += int64(freed)
}

func (g *gc) getObjectSizeAt(idx int) int {
	if idx >= g.vm.memTop {
		return 1
	}

	header := g.vm.memory[idx]
	if header == 0 {
		return 1
	}

	if isArrayHeader(header) {
		return arrayHeaderSize
	}
	if isArrayStorageHeader(header) {
		return 1 + getArrayStorageCapacity(header)
	}
	if isLargeStringHeader(header) || isLongHeapHeader(header) || isULongHeapHeader(header) {
		if idx+1 >= g.vm.memTop {
			return 1
		}
		if isLargeStringHeader(header) {
			return largeStringHeaderSize
		}
		return 2
	}
	if isMapHeader(header) {
		return mapHeaderSize
	}
	if isMapNodesHeader(header) {
		return 1 + getMapNodesCapacity(header)*4
	}
	if isObjectHeader(header) {
		size := getObjectSize(header)
		if size <= 0 || size > g.vm.memTop {
			return 1
		}
		return size
	}
	if isStructHeader(header) {
		size := getStructSize(header)
		if size <= 0 || size > g.vm.memTop {
			return 1
		}
		return size
	}

	return 1
}

func isArrayHeader(header uint64) bool {
	return (header >> 32) == 0xAAAAAAAA
}

func isMapHeader(header uint64) bool {
	return header&0xFFFFFFFF00000000 == mapHeaderMarker&0xFFFFFFFF00000000
}

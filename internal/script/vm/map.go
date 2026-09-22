package vm

import "math/bits"

const (
	mapMinCapacity   = 16
	mapLoadFactorNum = 3
	mapLoadFactorDen = 4
	mapNullPtr       = 0
	mapHeaderSize    = 6
	mapHeaderMarker  = uint64(0xFFFFFFFB00000001)
	mapNodesMarker   = uint64(0xFFFFFFFC00000001)
)

// mapEmptyKey and mapTombstone are sentinel values for hash map slots.
var (
	mapEmptyKey  value = value(0xFFFFFFFFFFFFFFFF)
	mapTombstone value = value(0xFFFFFFFFFFFFFFFE)
)

// encodeMapHeader creates a map header.
// Format: [marker:32][keyType:16][valType:16]
func encodeMapHeader(keyType, valType typeID) uint64 {
	return mapHeaderMarker | (uint64(keyType) << 16) | uint64(valType)
}

func encodeMapNodesHeader(capacity int) uint64 {
	return (mapNodesMarker & 0xFFFFFFFF00000000) | uint64(uint32(capacity))
}

func isMapNodesHeader(header uint64) bool {
	return header>>32 == mapNodesMarker>>32 && header&0xFFFFFFFF00000000 == mapNodesMarker&0xFFFFFFFF00000000
}

func getMapNodesCapacity(header uint64) int {
	return int(uint32(header & 0xFFFFFFFF))
}

func getMapKeyType(header uint64) typeID {
	return typeID((header >> 16) & 0xFFFF)
}

func getMapValueType(header uint64) typeID {
	return typeID(header & 0xFFFF)
}

func (v *vm) newMap(keyType, valType typeID, initialCapacity int) handle {
	capacity := nextPowerOf2(initialCapacity)
	if capacity < mapMinCapacity {
		capacity = mapMinCapacity
	}
	nodes := v.newMapNodes(capacity)
	scope := v.beginRootScope()
	scope.add(encodeHandle(nodes))
	idx := v.allocMemory(mapHeaderSize)
	v.memory[idx+0] = encodeMapHeader(keyType, valType)
	v.memory[idx+1] = uint64(capacity)
	v.memory[idx+2] = 0
	v.memory[idx+3] = mapNullPtr
	v.memory[idx+4] = mapNullPtr
	v.memory[idx+5] = uint64(encodeHandle(nodes))
	scope.end()
	return v.createHandle(idx)
}

func (v *vm) newMapNodes(capacity int) handle {
	if capacity < mapMinCapacity {
		capacity = mapMinCapacity
	}
	size := 1 + capacity*4
	idx := v.allocMemory(size)
	v.memory[idx] = encodeMapNodesHeader(capacity)
	for i := 0; i < capacity; i++ {
		nodeOffset := 1 + i*4
		v.memory[idx+nodeOffset+0] = uint64(mapEmptyKey)
		v.memory[idx+nodeOffset+1] = 0
		v.memory[idx+nodeOffset+2] = mapNullPtr
		v.memory[idx+nodeOffset+3] = mapNullPtr
	}
	return v.createHandle(idx)
}

func (v *vm) mapGet(m handle, key value) (value, bool) {
	mapIdx := v.getMemoryIndex(m)
	capacity := int(v.memory[mapIdx+1])
	nodesIdx := v.getMapNodesIndex(mapIdx)

	hash := hashValue(key)
	index := int(hash & uint64(capacity-1))

	for i := 0; i < capacity; i++ {
		probeIndex := (index + i) % capacity
		nodeOffset := 1 + probeIndex*4
		storedKey := value(v.memory[nodesIdx+nodeOffset])

		if storedKey == mapEmptyKey {
			return 0, false
		}
		if storedKey == mapTombstone {
			continue
		}
		if storedKey == key {
			return value(v.memory[nodesIdx+nodeOffset+1]), true
		}
	}

	return 0, false
}

func (v *vm) mapSet(m handle, key, val value) {
	if key == mapEmptyKey || key == mapTombstone {
		panic("invalid map key")
	}

	mapIdx := v.getMemoryIndex(m)
	capacity := int(v.memory[mapIdx+1])
	size := int(v.memory[mapIdx+2])

	if size*mapLoadFactorDen >= capacity*mapLoadFactorNum {
		scope := v.beginRootScope()
		scope.add(encodeHandle(m), key, val)
		v.mapGrow(m)
		scope.end()
		mapIdx = v.getMemoryIndex(m)
		capacity = int(v.memory[mapIdx+1])
		size = int(v.memory[mapIdx+2])
	}
	nodesIdx := v.getMapNodesIndex(mapIdx)

	hash := hashValue(key)
	index := int(hash & uint64(capacity-1))

	for i := 0; i < capacity; i++ {
		probeIndex := (index + i) % capacity
		nodeOffset := 1 + probeIndex*4
		storedKey := value(v.memory[nodesIdx+nodeOffset])

		if storedKey == mapEmptyKey || storedKey == mapTombstone {
			v.memory[nodesIdx+nodeOffset] = uint64(key)
			v.memory[nodesIdx+nodeOffset+1] = uint64(val)

			tail := v.memory[mapIdx+4]
			if tail == mapNullPtr {
				v.memory[mapIdx+3] = uint64(nodeOffset)
				v.memory[mapIdx+4] = uint64(nodeOffset)
				v.memory[nodesIdx+nodeOffset+2] = mapNullPtr
				v.memory[nodesIdx+nodeOffset+3] = mapNullPtr
			} else {
				v.memory[nodesIdx+int(tail)+3] = uint64(nodeOffset)
				v.memory[nodesIdx+nodeOffset+2] = tail
				v.memory[nodesIdx+nodeOffset+3] = mapNullPtr
				v.memory[mapIdx+4] = uint64(nodeOffset)
			}

			v.memory[mapIdx+2] = uint64(size + 1)
			return
		}

		if storedKey == key {
			v.memory[nodesIdx+nodeOffset+1] = uint64(val)
			return
		}
	}

	panic("map is full (should not happen)")
}

func (v *vm) mapDelete(m handle, key value) bool {
	mapIdx := v.getMemoryIndex(m)
	capacity := int(v.memory[mapIdx+1])
	nodesIdx := v.getMapNodesIndex(mapIdx)

	hash := hashValue(key)
	index := int(hash & uint64(capacity-1))

	for i := 0; i < capacity; i++ {
		probeIndex := (index + i) % capacity
		nodeOffset := 1 + probeIndex*4
		storedKey := value(v.memory[nodesIdx+nodeOffset])

		if storedKey == mapEmptyKey {
			return false
		}
		if storedKey == mapTombstone {
			continue
		}
		if storedKey == key {
			prev := v.memory[nodesIdx+nodeOffset+2]
			next := v.memory[nodesIdx+nodeOffset+3]

			if prev != mapNullPtr {
				v.memory[nodesIdx+int(prev)+3] = next
			} else {
				v.memory[mapIdx+3] = next
			}

			if next != mapNullPtr {
				v.memory[nodesIdx+int(next)+2] = prev
			} else {
				v.memory[mapIdx+4] = prev
			}

			v.memory[nodesIdx+nodeOffset] = uint64(mapTombstone)
			v.memory[nodesIdx+nodeOffset+1] = 0
			v.memory[nodesIdx+nodeOffset+2] = mapNullPtr
			v.memory[nodesIdx+nodeOffset+3] = mapNullPtr

			size := int(v.memory[mapIdx+2])
			v.memory[mapIdx+2] = uint64(size - 1)
			return true
		}
	}

	return false
}

func (v *vm) mapSize(m handle) int {
	mapIdx := v.getMemoryIndex(m)
	return int(v.memory[mapIdx+2])
}

func (v *vm) mapGrow(m handle) {
	// One batched root scope covers the map, its old nodes (still referenced
	// by the header until the swap) and the freshly allocated nodes.
	scope := v.beginRootScope()
	defer scope.end()
	scope.add(encodeHandle(m))

	mapIdx := v.getMemoryIndex(m)
	header := v.memory[mapIdx]
	oldCapacity := int(v.memory[mapIdx+1])
	keyType := getMapKeyType(header)
	valType := getMapValueType(header)
	nodesIdx := v.getMapNodesIndex(mapIdx)
	oldHead := v.memory[mapIdx+3]

	newNodes := v.newMapNodes(oldCapacity * 2)
	scope.add(encodeHandle(newNodes))
	newNodesIdx := v.getMemoryIndex(newNodes)
	newCapacity := getMapNodesCapacity(v.memory[newNodesIdx])

	v.memory[mapIdx+1] = uint64(newCapacity)
	v.memory[mapIdx+2] = 0
	v.memory[mapIdx+3] = mapNullPtr
	v.memory[mapIdx+4] = mapNullPtr
	v.memory[mapIdx+5] = uint64(encodeHandle(newNodes))

	current := oldHead
	for current != mapNullPtr {
		key := value(v.memory[nodesIdx+int(current)])
		val := value(v.memory[nodesIdx+int(current)+1])
		v.mapSet(m, key, val)
		current = v.memory[nodesIdx+int(current)+3]
	}

	_ = keyType
	_ = valType
}

func (v *vm) mapIterate(m handle, fn func(key, val value) bool) {
	mapIdx := v.getMemoryIndex(m)
	nodesIdx := v.getMapNodesIndex(mapIdx)
	current := v.memory[mapIdx+3]

	for current != mapNullPtr {
		key := value(v.memory[nodesIdx+int(current)])
		val := value(v.memory[nodesIdx+int(current)+1])
		if !fn(key, val) {
			break
		}
		current = v.memory[nodesIdx+int(current)+3]
	}
}

// mapKeyAt returns the key at the given zero-based position in the
// map's insertion-order linked list. It exists to support for-in loops
// with a simple index-based bytecode sequence.
func (v *vm) mapKeyAt(m handle, index int) (value, bool) {
	if index < 0 {
		return 0, false
	}
	mapIdx := v.getMemoryIndex(m)
	if mapIdx < 0 || mapIdx >= v.memTop {
		return 0, false
	}
	if !isMapHeader(v.memory[mapIdx]) {
		return 0, false
	}
	nodesIdx := v.getMapNodesIndex(mapIdx)
	current := v.memory[mapIdx+3]
	for i := 0; i < index; i++ {
		if current == mapNullPtr {
			return 0, false
		}
		current = v.memory[nodesIdx+int(current)+3]
	}
	if current == mapNullPtr {
		return 0, false
	}
	return value(v.memory[nodesIdx+int(current)]), true
}

// isMap checks if a handle points to a map in memory.
func (v *vm) isMap(h handle) bool {
	idx := v.getMemoryIndex(h)
	if idx < 0 || idx >= v.memTop {
		return false
	}
	header := v.memory[idx]
	if isMapHeader(header) {
		return true
	}
	if isArrayHeader(header) || isArrayStorageHeader(header) || isMapNodesHeader(header) || isStructHeader(header) {
		return false
	}
	if isObjectHeader(header) {
		return false
	}
	return false
}

func (v *vm) getMapNodesIndex(mapIdx int) int {
	nodesVal := value(v.memory[mapIdx+5])
	return v.getMemoryIndex(nodesVal.decodeHandle())
}

func hashValue(v value) uint64 {
	h := uint64(v)
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

func nextPowerOf2(n int) int {
	if n <= 0 {
		return 1
	}
	if n&(n-1) == 0 {
		return n
	}
	return 1 << bits.Len(uint(n))
}

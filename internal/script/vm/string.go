package vm

import "hash/fnv"

// stringPool manages string interning and storage.
// Three tiers: small (inline ≤6 bytes), medium (7-256 bytes in byte pool),
// large (>256 bytes in a separate flat byte pool referenced by a 3-slot
// header in VM main memory). The flat-pool layout for large strings avoids
// per-byte pack/unpack overhead vs the previous packed-uint64 layout.
type stringPool struct {
	vm     *vm
	medium []byte
	large  []byte
	index  map[uint32][]stringEntry
}

type stringEntry struct {
	offset uint32
	length uint32
	hash   uint32
}

func newStringPool(v *vm) *stringPool {
	return &stringPool{
		vm:     v,
		medium: make([]byte, 0, 4096),
		large:  make([]byte, 0, 4096),
		index:  make(map[uint32][]stringEntry),
	}
}

// internBytes adds a byte slice to the pool and returns its encoded value.
func (sp *stringPool) internBytes(data []byte) value {
	length := len(data)

	// Small bytes: inline (≤6 bytes)
	if length <= 6 {
		return encodeSmallBytes(data)
	}

	// Medium bytes: byte pool (7-256 bytes)
	if length <= 256 {
		return sp.internMediumBytes(data)
	}

	// Large bytes: flat byte pool, 3-slot header in VM main memory
	return sp.internLargeBytes(data)
}

// intern adds a string to the pool and returns its encoded value.
func (sp *stringPool) intern(s string) value {
	data := []byte(s)
	length := len(data)

	// Small string: inline (≤6 bytes)
	if length <= 6 {
		return encodeSmallString(data)
	}

	// Medium string: byte pool (7-256 bytes)
	if length <= 256 {
		return sp.internMedium(data)
	}

	// Large string: flat byte pool, 3-slot header in VM main memory
	return sp.internLarge(data)
}

func (sp *stringPool) internMedium(data []byte) value {
	hash := hashBytes(data)

	// Check if already interned.
	if entries, ok := sp.index[hash]; ok {
		for _, entry := range entries {
			if sp.stringEquals(entry, data) {
				return encodeMediumString(entry.offset, entry.length)
			}
		}
	}

	// Append to byte pool.
	offset := uint32(len(sp.medium))
	sp.medium = append(sp.medium, data...)

	entry := stringEntry{
		offset: offset,
		length: uint32(len(data)),
		hash:   hash,
	}

	sp.index[hash] = append(sp.index[hash], entry)
	return encodeMediumString(offset, uint32(len(data)))
}

// internLarge stores a large string in the flat byte pool and writes a
// 3-slot header into VM main memory.
//
// Layout in main memory: [largeStringHeader, poolOffset, length]
func (sp *stringPool) internLarge(data []byte) value {
	offset := uint64(len(sp.large))
	sp.large = append(sp.large, data...)

	idx := sp.vm.allocMemory(largeStringHeaderSize)
	sp.vm.memory[idx+0] = encodeLargeStringHeader()
	sp.vm.memory[idx+1] = offset
	sp.vm.memory[idx+2] = uint64(len(data))

	h := sp.vm.createHandle(idx)
	return encodeHandle(h)
}

// decodeMediumString returns the string stored at the given offset/length in the byte pool.
func (sp *stringPool) decodeMediumString(offset, length uint32) string {
	if int(offset+length) > len(sp.medium) {
		panic("invalid medium string offset/length")
	}
	return string(sp.medium[offset : offset+length])
}

// decodeLargeString returns the string stored at the given handle.
func (sp *stringPool) decodeLargeString(h handle) string {
	idx := sp.vm.getMemoryIndex(h)
	offset := sp.vm.memory[idx+1]
	length := sp.vm.memory[idx+2]
	return string(sp.large[offset : offset+length])
}

func (sp *stringPool) internMediumBytes(data []byte) value {
	hash := hashBytes(data)

	if entries, ok := sp.index[hash]; ok {
		for _, entry := range entries {
			if sp.stringEquals(entry, data) {
				return encodeMediumBytes(entry.offset, entry.length)
			}
		}
	}

	offset := uint32(len(sp.medium))
	sp.medium = append(sp.medium, data...)

	entry := stringEntry{
		offset: offset,
		length: uint32(len(data)),
		hash:   hash,
	}

	sp.index[hash] = append(sp.index[hash], entry)
	return encodeMediumBytes(offset, uint32(len(data)))
}

func (sp *stringPool) internLargeBytes(data []byte) value {
	offset := uint64(len(sp.large))
	sp.large = append(sp.large, data...)

	idx := sp.vm.allocMemory(largeStringHeaderSize)
	sp.vm.memory[idx+0] = encodeLargeBytesHeader()
	sp.vm.memory[idx+1] = offset
	sp.vm.memory[idx+2] = uint64(len(data))

	h := sp.vm.createHandle(idx)
	return encodeHandle(h)
}

// decodeMediumBytes returns the byte slice stored at the given offset/length in the byte pool.
func (sp *stringPool) decodeMediumBytes(offset, length uint32) []byte {
	if int(offset+length) > len(sp.medium) {
		panic("invalid medium bytes offset/length")
	}
	return sp.medium[offset : offset+length]
}

// decodeLargeBytes returns the byte slice stored at the given handle.
func (sp *stringPool) decodeLargeBytes(h handle) []byte {
	idx := sp.vm.getMemoryIndex(h)
	offset := sp.vm.memory[idx+1]
	length := sp.vm.memory[idx+2]
	return sp.large[offset : offset+length]
}

func (sp *stringPool) stringEquals(entry stringEntry, data []byte) bool {
	if entry.length != uint32(len(data)) {
		return false
	}
	for i := 0; i < len(data); i++ {
		if sp.medium[entry.offset+uint32(i)] != data[i] {
			return false
		}
	}
	return true
}

func hashBytes(data []byte) uint32 {
	h := fnv.New32a()
	h.Write(data)
	return h.Sum32()
}

// --- Medium string encoding ---
// Uses tagPointer with a marker bit at position 3.

func encodeMediumString(offset, length uint32) value {
	payload := uint64(offset) | (uint64(length) << 32)
	return makeBox(tagMediumStr, payload)
}

func decodeMediumString(v value) (offset, length uint32) {
	offset = uint32(v.payload() & 0xFFFFFFFF)
	length = uint32((v.payload() >> 32) & 0xFFFF)
	return
}

// isMediumString checks whether a value is an inline medium string.
func isMediumString(v value) bool {
	return v.isBoxed() && v.tag() == tagMediumStr
}

// largeStringHeaderSize is the fixed slot count of a large string header in
// VM main memory: [magic header, poolOffset, length].
const largeStringHeaderSize = 3

// encodeLargeStringHeader returns the magic header value for large strings.
func encodeLargeStringHeader() uint64 {
	return 0xFFFFFFFF00000001
}

func isLargeStringHeader(header uint64) bool {
	return header == encodeLargeStringHeader()
}

// encodeLargeBytesHeader returns the magic header value for large bytes.
func encodeLargeBytesHeader() uint64 {
	return 0xFFFFFFFF00000002
}

func isLargeBytesHeader(header uint64) bool {
	return header == encodeLargeBytesHeader()
}

// bytesBytes returns the byte content of a bytes value as a slice.
// For medium and large bytes the underlying byte-pool slice is returned
// directly (zero-copy); the caller must not mutate it.
// Returns (nil, false) when the value is not a recognized bytes tier.
func (v *vm) bytesBytes(s value) ([]byte, bool) {
	if s.isSmallBytes() {
		return s.decodeSmallBytes(), true
	}
	if isMediumBytes(s) {
		offset, length := decodeMediumBytes(s)
		return v.stringPool.medium[offset : offset+length], true
	}
	if s.isPointer() {
		h := s.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeBytesHeader(v.memory[idx]) {
			offset := v.memory[idx+1]
			length := v.memory[idx+2]
			return v.stringPool.large[offset : offset+length], true
		}
	}
	return nil, false
}

// bytesLength returns the length of a bytes value.
func (v *vm) bytesLength(s value) (int, bool) {
	if s.isSmallBytes() {
		return len(s.decodeSmallBytes()), true
	}
	if isMediumBytes(s) {
		_, length := decodeMediumBytes(s)
		return int(length), true
	}
	if s.isPointer() {
		h := s.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeBytesHeader(v.memory[idx]) {
			return int(v.memory[idx+2]), true
		}
	}
	return 0, false
}

// stringBytes returns the byte content of a string value as a slice. For
// small strings the bytes are decoded into a fresh ≤6-byte slice. For medium
// strings the underlying byte-pool slice is returned directly (zero-copy);
// the caller must not mutate it. For large strings the byte-pool slice is
// returned directly as well (zero-copy). Returns (nil, false) when the
// value is not a recognized string tier.
func (v *vm) stringBytes(s value) ([]byte, bool) {
	if s.isSmallString() {
		return s.decodeSmallString(), true
	}
	if isMediumString(s) {
		offset, length := decodeMediumString(s)
		return v.stringPool.medium[offset : offset+length], true
	}
	if s.isPointer() {
		h := s.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeStringHeader(v.memory[idx]) {
			offset := v.memory[idx+1]
			length := v.memory[idx+2]
			return v.stringPool.large[offset : offset+length], true
		}
	}
	return nil, false
}

func (v *vm) joinStringArray(arr handle, sep value) (value, bool) {
	arrIdx := v.getMemoryIndex(arr)
	if arrIdx < 0 || arrIdx >= v.memTop || !isArrayHeader(v.memory[arrIdx]) {
		return encodeInt(0), false
	}
	sepLen, ok := v.stringLength(sep)
	if !ok {
		return encodeInt(0), false
	}
	length := int(v.memory[arrIdx+1])
	total := 0
	for i := 0; i < length; i++ {
		partLen, ok := v.stringLength(v.getArrayElement(arr, i))
		if !ok {
			return encodeInt(0), false
		}
		total += partLen
		if i > 0 {
			total += sepLen
		}
	}

	switch {
	case total <= 6:
		var buf [6]byte
		v.copyJoinedStringArray(buf[:0], arr, length, sep)
		return encodeSmallString(buf[:total]), true
	case total <= 256:
		offset := uint32(len(v.stringPool.medium))
		v.stringPool.medium = v.copyJoinedStringArray(v.stringPool.medium, arr, length, sep)
		return encodeMediumString(offset, uint32(total)), true
	default:
		offset := uint64(len(v.stringPool.large))
		v.stringPool.large = v.copyJoinedStringArray(v.stringPool.large, arr, length, sep)
		idx := v.allocMemory(largeStringHeaderSize)
		v.memory[idx+0] = encodeLargeStringHeader()
		v.memory[idx+1] = offset
		v.memory[idx+2] = uint64(total)
		return encodeHandle(v.createHandle(idx)), true
	}
}

func (v *vm) stringLength(s value) (int, bool) {
	if s.isSmallString() {
		return s.smallStringLength(), true
	}
	if isMediumString(s) {
		_, length := decodeMediumString(s)
		return int(length), true
	}
	if s.isPointer() {
		h := s.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeStringHeader(v.memory[idx]) {
			return int(v.memory[idx+2]), true
		}
	}
	return 0, false
}

// stringByteAt returns the byte at the given index in the string value as
// a one-byte string. The runtime represents strings as UTF-8 byte sequences,
// so indexing is byte-oriented.
func (v *vm) stringByteAt(s value, index int) (value, bool) {
	b, ok := v.stringBytes(s)
	if !ok || index < 0 || index >= len(b) {
		return 0, false
	}
	return encodeSmallString(b[index : index+1]), true
}

func (v *vm) copyJoinedStringArray(dst []byte, arr handle, length int, sep value) []byte {
	for i := 0; i < length; i++ {
		if i > 0 {
			dst = v.appendStringBytes(dst, sep)
		}
		dst = v.appendStringBytes(dst, v.getArrayElement(arr, i))
	}
	return dst
}

func (v *vm) appendStringBytes(dst []byte, s value) []byte {
	if s.isSmallString() {
		payload := s.payload()
		for i := 0; i < int(payload&0xF); i++ {
			dst = append(dst, byte((payload>>(4+8*i))&0xFF))
		}
		return dst
	}
	bytes, _ := v.stringBytes(s)
	return append(dst, bytes...)
}

func (v *vm) encodeBytes(data []byte) value {
	return v.stringPool.internBytes(data)
}

// decodeBytes returns the byte slice for a bytes value.
// For medium and large bytes the underlying byte-pool slice is returned
// directly (zero-copy); the caller must not mutate it.
func (v *vm) decodeBytes(s value) []byte {
	if s.isSmallBytes() {
		return s.decodeSmallBytes()
	}
	if isMediumBytes(s) {
		offset, length := decodeMediumBytes(s)
		return v.stringPool.decodeMediumBytes(offset, length)
	}
	if s.isPointer() {
		h := s.decodeHandle()
		idx := v.getMemoryIndex(h)
		if idx >= 0 && idx < v.memTop && isLargeBytesHeader(v.memory[idx]) {
			return v.stringPool.decodeLargeBytes(h)
		}
	}
	return nil
}

// internLargeFromTwoSources stores aBytes followed by bBytes contiguously in
// the flat large-string pool and writes a 3-slot header into VM main memory.
// Equivalent to internLarge(append(aBytes, bBytes...)) but avoids the
// intermediate Go []byte allocation.
func (sp *stringPool) internLargeFromTwoSources(aBytes, bBytes []byte) value {
	offset := uint64(len(sp.large))
	sp.large = append(sp.large, aBytes...)
	sp.large = append(sp.large, bBytes...)
	length := uint64(len(aBytes) + len(bBytes))

	idx := sp.vm.allocMemory(largeStringHeaderSize)
	sp.vm.memory[idx+0] = encodeLargeStringHeader()
	sp.vm.memory[idx+1] = offset
	sp.vm.memory[idx+2] = length

	h := sp.vm.createHandle(idx)
	return encodeHandle(h)
}

// internMediumFromTwoSources appends aBytes followed by bBytes to the medium
// pool as a single string entry (no intern dedup, since concat results in hot
// loops are rarely re-encountered). Returns the medium string value.
func (sp *stringPool) internMediumFromTwoSources(aBytes, bBytes []byte) value {
	offset := uint32(len(sp.medium))
	sp.medium = append(sp.medium, aBytes...)
	sp.medium = append(sp.medium, bBytes...)
	return encodeMediumString(offset, uint32(len(aBytes)+len(bBytes)))
}

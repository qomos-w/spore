package vm

import "math"

// value represents a 64-bit VM value.
//
// Phase 2 uses a simplified NaN-boxed representation for non-double values.
// Non-double values use the top 8 bits as 0xFF, followed by a 4-bit tag in the
// next nibble and a 52-bit payload. Ordinary doubles are stored as their raw
// IEEE-754 bit pattern, except NaN values, which are canonicalized to the boxed
// double-NaN sentinel.
type value uint64

// handle is a direct index into the VM memory pool.
type handle int32

const invalidHandle handle = -1

const (
	nanBoxPrefix   uint64 = 0xFF00000000000000
	nanTagShift           = 52
	nanPayloadMask uint64 = 0x000FFFFFFFFFFFFF
)

const (
	tagNull = iota
	tagBool
	tagInt
	tagFloat
	tagLong
	tagULong
	tagPointer
	tagSmallStr
	tagMediumStr
	tagDoubleNaN
	tagBytes
	tagMediumBytes
	tagClosure
	tagCell
	tagEnum
)

func makeBox(tag uint64, payload uint64) value {
	return value(nanBoxPrefix | (tag << nanTagShift) | (payload & nanPayloadMask))
}

func (v value) isBoxed() bool { return uint64(v)>>56 == 0xFF }

func (v value) tag() uint64 {
	if !v.isBoxed() {
		return 0
	}
	return (uint64(v) >> nanTagShift) & 0xF
}

func (v value) payload() uint64 { return uint64(v) & nanPayloadMask }

func signExtend(payload uint64, bits uint) int64 {
	shift := 64 - bits
	return int64(payload<<shift) >> shift
}

func encodeSigned(payload int64, bits uint) uint64 {
	if bits == 64 {
		return uint64(payload)
	}
	return uint64(payload) & ((uint64(1) << bits) - 1)
}

// --- Type predicates ---

func (v value) isBool() bool        { return v.tag() == tagBool }
func (v value) isInt() bool         { return v.tag() == tagInt }
func (v value) isFloat() bool       { return v.tag() == tagFloat }
func (v value) isLong() bool        { return v.tag() == tagLong }
func (v value) isULong() bool       { return v.tag() == tagULong }
func (v value) isDouble() bool      { return !v.isBoxed() || v.tag() == tagDoubleNaN }
func (v value) isPointer() bool     { return v.tag() == tagPointer }
func (v value) isSmallString() bool { return v.tag() == tagSmallStr }
func (v value) isNull() bool        { return v.tag() == tagNull }

func (v value) isString() bool {
	return v.isSmallString() || isMediumString(v)
}

func (v value) isBytes() bool {
	return v.isSmallBytes() || isMediumBytes(v)
}

// --- null encoding ---

func encodeNull() value { return makeBox(tagNull, 0) }

// --- bool encoding/decoding ---

func encodeBool(b bool) value {
	if b {
		return makeBox(tagBool, 1)
	}
	return makeBox(tagBool, 0)
}

func (v value) decodeBool() bool { return v.payload() != 0 }

// --- int32 encoding/decoding ---

func encodeInt(i int32) value { return makeBox(tagInt, encodeSigned(int64(i), 32)) }

func (v value) decodeInt() int32 { return int32(signExtend(v.payload(), 32)) }

// --- uint32 encoding/decoding ---

func encodeUInt(u uint32) value { return makeBox(tagInt, uint64(u)) }

func (v value) decodeUInt() uint32 { return uint32(v.payload()) }

// --- byte/short/ushort (stored as int tag) ---

func encodeByte(b uint8) value    { return encodeUInt(uint32(b)) }
func (v value) decodeByte() uint8 { return uint8(v.decodeUInt()) }

func encodeShort(s int16) value    { return encodeInt(int32(s)) }
func (v value) decodeShort() int16 { return int16(v.decodeInt()) }

func encodeUShort(s uint16) value    { return encodeUInt(uint32(s)) }
func (v value) decodeUShort() uint16 { return uint16(v.decodeUInt()) }

// --- float32 encoding/decoding ---

func encodeFloat(f float32) value { return makeBox(tagFloat, uint64(math.Float32bits(f))) }

func (v value) decodeFloat() float32 { return math.Float32frombits(uint32(v.payload())) }

// --- double encoding/decoding ---

func encodeDouble(f float64) value {
	return value(math.Float64bits(f))
}

func (v value) decodeDouble() float64 {
	return math.Float64frombits(uint64(v))
}

// --- long/ulong encoding/decoding ---

func encodeLong(i int64) value { return makeBox(tagLong, encodeSigned(i, 52)) }

func (v value) decodeLong() int64 { return signExtend(v.payload(), 52) }

func encodeULong(u uint64) value { return makeBox(tagULong, u) }

func (v value) decodeULong() uint64 { return v.payload() }

// --- handle (pointer) encoding/decoding ---

func encodeHandle(h handle) value {
	if h == invalidHandle {
		return encodeNull()
	}
	return makeBox(tagPointer, uint64(uint32(h)))
}

func (v value) decodeHandle() handle {
	if v.isNull() {
		return invalidHandle
	}
	return handle(uint32(v.payload()))
}

// --- small string encoding/decoding (≤6 bytes, inline) ---

func encodeSmallString(data []byte) value {
	if len(data) > 6 {
		panic("string too large for small string encoding")
	}
	payload := uint64(len(data))
	for i := 0; i < len(data); i++ {
		payload |= uint64(data[i]) << (4 + 8*i)
	}
	return makeBox(tagSmallStr, payload)
}

func (v value) decodeSmallString() []byte {
	length := int(v.payload() & 0xF)
	data := make([]byte, length)
	for i := 0; i < length; i++ {
		data[i] = byte((v.payload() >> (4 + 8*i)) & 0xFF)
	}
	return data
}

func (v value) smallStringLength() int { return int(v.payload() & 0xF) }

// --- small bytes encoding/decoding (≤6 bytes, inline) ---

func encodeSmallBytes(data []byte) value {
	if len(data) > 6 {
		panic("bytes too large for small bytes encoding")
	}
	payload := uint64(len(data))
	for i := 0; i < len(data); i++ {
		payload |= uint64(data[i]) << (4 + 8*i)
	}
	return makeBox(tagBytes, payload)
}

func (v value) decodeSmallBytes() []byte {
	length := int(v.payload() & 0xF)
	data := make([]byte, length)
	for i := 0; i < length; i++ {
		data[i] = byte((v.payload() >> (4 + 8*i)) & 0xFF)
	}
	return data
}

func (v value) isSmallBytes() bool { return v.tag() == tagBytes }

// --- medium bytes encoding/decoding ---

func encodeMediumBytes(offset, length uint32) value {
	payload := uint64(offset) | (uint64(length) << 32)
	return makeBox(tagMediumBytes, payload)
}

// --- closure encoding/decoding ---
//
// A closure value carries an index into the interpreter's closure table.
// Closure values are first-class: they can be stored in variables, passed
// as arguments, and called via opCallValue.

func encodeClosureIndex(idx uint32) value {
	return makeBox(tagClosure, uint64(idx))
}

func (v value) isClosure() bool { return v.isBoxed() && v.tag() == tagClosure }

func (v value) decodeClosureIndex() uint32 { return uint32(v.payload()) }

// --- capture cell encoding/decoding ---
//
// A capture cell is an indirect slot shared between an enclosing scope and
// one or more closures, so writes through either side stay visible to all.
// Cell values never escape into user-visible script values; they only live
// in compiler-managed local slots and inside closure instances.

func encodeCellIndex(idx uint32) value {
	return makeBox(tagCell, uint64(idx))
}

func (v value) isCell() bool { return v.isBoxed() && v.tag() == tagCell }

func (v value) decodeCellIndex() uint32 { return uint32(v.payload()) }

func decodeMediumBytes(v value) (offset, length uint32) {
	offset = uint32(v.payload() & 0xFFFFFFFF)
	length = uint32((v.payload() >> 32) & 0xFFFF)
	return
}

func isMediumBytes(v value) bool {
	return v.isBoxed() && v.tag() == tagMediumBytes
}

// --- enum encoding/decoding ---
//
// An enum value is an int32 member payload tagged with the declaring enum's
// runtime ID (a 1-based index into the enum registry). Payload layout in the
// 52-bit NaN box: [enumID:20][value:32].

const (
	enumIDShift   uint64 = 32
	enumValueMask uint64 = 0xFFFFFFFF
	enumIDMax     uint64 = 0xFFFFF
)

func encodeEnumValue(enumID uint32, memberValue int32) value {
	id := uint64(enumID) & enumIDMax
	return makeBox(tagEnum, (id<<enumIDShift)|uint64(uint32(memberValue)))
}

func (v value) isEnum() bool { return v.isBoxed() && v.tag() == tagEnum }

func (v value) decodeEnumID() uint32 {
	return uint32((v.payload() >> enumIDShift) & enumIDMax)
}

func (v value) decodeEnumValue() int32 {
	return int32(uint32(v.payload() & enumValueMask))
}

package vm

import "fmt"

// typeID represents a 64-bit type identifier.
//
// Encoding (64-bit):
//
//	[63:60] - type category (4 bits):
//	          0 = basic type
//	          1 = array
//	          2 = map
//	          3 = struct
//	          4 = class
//	          5-15 = reserved
//
//	Basic type (category=0):
//	  [59:0] = basic type ID (60 bits)
//
//	array<T> (category=1):
//	  [59:56] = element type category (4 bits)
//	  [55:0]  = element type data (56 bits)
//
//	map<K,V> (category=2):
//	  [59:30] = key type (30 bits)
//	  [29:0]  = value type (30 bits)
//
//	struct (category=3):
//	  [59:0] = struct ID (60 bits)
//
//	class (category=4):
//	  [59:0] = class ID (60 bits)
type typeID uint64

// Basic type constants, matching foundation.TypeID values.
const (
	typeInvalid typeID = iota
	typeVoid
	typeBool
	typeByte
	typeShort
	typeUShort
	typeInt
	typeUInt
	typeLong
	typeULong
	typeFloat
	typeDouble
	typeString
	typeBytes
	typeObject
	typeAny
)

// Type category constants (for 64-bit encoding).
const (
	categoryBasic  = 0x0
	categoryArray  = 0x1
	categoryMap    = 0x2
	categoryStruct = 0x3
	categoryClass  = 0x4
	categoryEnum   = 0x5
)

// Generic type aliases for readability.
var (
	typeArray = makeArrayType(typeObject)
)

// --- Generic type encoding/decoding ---

func makeArrayType(elemType typeID) typeID {
	elemCategory := uint64(getTypeCategory(elemType))
	elemData := uint64(elemType) & 0x0FFFFFFFFFFFFFFF
	return typeID(categoryArray<<60) | typeID(elemCategory<<56) | typeID(elemData)
}

func makeMapType(keyType, valType typeID) typeID {
	keyBits := uint64(keyType) & 0x3FFFFFFF
	valBits := uint64(valType) & 0x3FFFFFFF
	return typeID(categoryMap<<60) | typeID(keyBits<<30) | typeID(valBits)
}

func makeClassType(classID uint32) typeID {
	return typeID(categoryClass<<60) | typeID(classID)
}

func makeEnumType(enumID uint32) typeID {
	return typeID(categoryEnum<<60) | typeID(enumID)
}

func getTypeCategory(tid typeID) uint8 {
	return uint8((tid >> 60) & 0xF)
}

func getElementType(tid typeID) typeID {
	elemCategory := uint8((tid >> 56) & 0xF)
	elemData := tid & 0x00FFFFFFFFFFFFFF
	return typeID(uint64(elemCategory)<<60) | typeID(elemData)
}

func getKeyType(tid typeID) typeID {
	return typeID((tid >> 30) & 0x3FFFFFFF)
}

func getValueType(tid typeID) typeID {
	return typeID(tid & 0x3FFFFFFF)
}

func getClassIDFromType(tid typeID) uint32 {
	return uint32(tid & 0xFFFFFFFFFFFFFFF)
}

func getEnumIDFromType(tid typeID) uint32 {
	return uint32(tid & 0xFFFFFFFFFFFFFFF)
}

// typeName returns a human-readable name for a typeID.
func typeName(tid typeID) string {
	category := getTypeCategory(tid)
	switch category {
	case categoryBasic:
		switch tid {
		case typeBool:
			return "bool"
		case typeByte:
			return "byte"
		case typeShort:
			return "short"
		case typeUShort:
			return "ushort"
		case typeInt:
			return "int"
		case typeUInt:
			return "uint"
		case typeLong:
			return "long"
		case typeULong:
			return "ulong"
		case typeFloat:
			return "float"
		case typeDouble:
			return "double"
		case typeString:
			return "string"
		case typeBytes:
			return "bytes"
		case typeObject:
			return "object"
		case typeAny:
			return "any"
		case typeVoid:
			return "void"
		case typeInvalid:
			return "invalid"
		default:
			return fmt.Sprintf("unknown(%d)", tid)
		}
	case categoryArray:
		return fmt.Sprintf("array<%s>", typeName(getElementType(tid)))
	case categoryMap:
		return fmt.Sprintf("map<%s,%s>", typeName(getKeyType(tid)), typeName(getValueType(tid)))
	case categoryStruct:
		return fmt.Sprintf("struct#%d", getClassIDFromType(tid))
	case categoryClass:
		return fmt.Sprintf("class#%d", getClassIDFromType(tid))
	case categoryEnum:
		return fmt.Sprintf("enum#%d", getEnumIDFromType(tid))
	default:
		return fmt.Sprintf("unknown_category(%d)", tid)
	}
}

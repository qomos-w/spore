package schema

// TypeID is the foundational packed type identifier consumed by schema descriptors.
// Contract: public semantic contract — stable wire-level identifier embedded in
// TypeDesc.TypeID and used by host code constructing parameter/field descriptors.
//
// The encoding is a packed 64-bit value: the high 4 bits carry a category tag
// (basic / array / map / struct / class), the remaining bits carry category-
// specific payload (element type, map key/value, class id, …). Helper
// constructors and decoders live in describe.go.
type TypeID uint64

// ParameterDef describes a named parameter at the foundational vocabulary level.
// Contract: public semantic contract — input shape for DescribeParameter.
type ParameterDef struct {
	Name   string
	TypeID TypeID
}

// FieldDef describes a named field in a foundational class shape.
// Contract: public semantic contract — input shape for Class field declarations.
type FieldDef struct {
	Name   string
	TypeID TypeID
}

// Class is the foundational class shape consumed by DescribeObject when the
// host needs to describe a class-kind object outside of Go reflection.
// Contract: public semantic contract — input shape for DescribeObject.
type Class struct {
	ID        uint64
	Name      string
	Fields    []FieldDef
	AllFields []FieldDef
}

// NewClass creates a foundational class with the given id and name.
// The third argument is reserved for future field-layout hints and is currently ignored.
func NewClass(id uint64, name string, _ any) *Class {
	return &Class{ID: id, Name: name}
}

// AddField appends a field declaration to both the declared and full field lists.
func (c *Class) AddField(name string, typeID TypeID) {
	field := FieldDef{Name: name, TypeID: typeID}
	c.Fields = append(c.Fields, field)
	c.AllFields = append(c.AllFields, field)
}

// ComputeFieldOffsets is reserved for future field-offset computation. It is a
// safe no-op today so callers can wire the call site without behavior change.
func (c *Class) ComputeFieldOffsets() {}

// Foundational type identifiers. These are the canonical wire IDs for the
// primitive scalar types and for the special invalid/void/object/any sentinels.
// Contract: public semantic contract — stable type identifier values.
const (
	TypeInvalid TypeID = 0
	TypeVoid    TypeID = 1
	TypeBool    TypeID = 2
	TypeByte    TypeID = 3
	TypeShort   TypeID = 4
	TypeUShort  TypeID = 5
	TypeInt     TypeID = 6
	TypeUInt    TypeID = 7
	TypeLong    TypeID = 8
	TypeULong   TypeID = 9
	TypeFloat   TypeID = 10
	TypeDouble  TypeID = 11
	TypeString  TypeID = 12
	TypeBytes   TypeID = 13
	TypeObject  TypeID = 14
	TypeAny     TypeID = 15
)

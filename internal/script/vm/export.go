package vm

import "fmt"

// Exported type aliases and wrappers for cross-package access.
// The vm package keeps its internal types unexported; this file exposes
// the minimal surface needed by the bytecode compiler and integration layers.

// --- Exported type aliases ---

type (
	VM                   = vm
	Value                = value
	Handle               = handle
	TypeID               = typeID
	RootProvider         func(func(Value))
	Class                = classDef
	FunctionDef          = functionDef
	ParameterDef         = paramDef
	FieldDef             = fieldDef
	FunctionBody         = functionBody
	BytecodeFunctionBody = bytecodeFunctionBody
	FunctionRegistry     = functionRegistry
	ClassRegistry        = classRegistry
	InterfaceRegistry    = interfaceRegistry
	InterfaceDef         = interfaceDef
	InterfaceMethodSig   = interfaceMethodSig
	StructRegistry       = structRegistry
	StructDef            = structDef
	EnumRegistry         = enumRegistry
	EnumDef              = enumDef
	MethodImpl           = methodImpl
)

// --- Exported constants ---

const (
	TypeInvalid   TypeID = typeInvalid
	InvalidHandle Handle = invalidHandle
)

// Basic type identifiers exported from vm/types.go.
const (
	TypeVoid   TypeID = 1
	TypeBool   TypeID = 2
	TypeByte   TypeID = 3
	TypeShort  TypeID = 4
	TypeUShort TypeID = 5
	TypeInt    TypeID = 6
	TypeUInt   TypeID = 7
	TypeLong   TypeID = 8
	TypeULong  TypeID = 9
	TypeFloat  TypeID = 10
	TypeDouble TypeID = 11
	TypeString TypeID = 12
	TypeBytes  TypeID = 13
	TypeObject TypeID = 14
	TypeAny    TypeID = 15
)

// --- Exported constructors ---

// NewVM creates a new VM instance. stackSize is the operand-stack capacity
// handed to the interpreter's single execution stack (see
// VM.OperandStackCapacity); it no longer sizes a VM-owned stack.
func NewVM(memorySize, stackSize int) *VM {
	return newVM(memorySize, stackSize)
}

// NewClass creates a new class definition.
func NewClass(id uint32, name string, parent *Class) *Class {
	return newClass(classID(id), name, parent)
}

// NewParameterDef creates a function parameter descriptor.
func NewParameterDef(name string, tid TypeID) ParameterDef {
	return paramDef{name: name, typeID: tid}
}

// NewFieldDef creates a class/struct field descriptor.
func NewFieldDef(name string, tid TypeID, offset int) FieldDef {
	return fieldDef{name: name, typeID: tid, offset: offset}
}

// Name returns the field name.
func (f FieldDef) Name() string { return f.name }

// TypeID returns the field's type identifier.
func (f FieldDef) TypeID() TypeID { return f.typeID }

// NewBytecodeFunctionBody wraps a closure-based execution function.
func NewBytecodeFunctionBody(fn func(v *VM, args []Value) Value) *BytecodeFunctionBody {
	return &bytecodeFunctionBody{executeFn: fn}
}

// NewFunctionDef creates a function definition with metadata and body.
func NewFunctionDef(name string, params []ParameterDef, retType TypeID, body FunctionBody) *FunctionDef {
	return &functionDef{name: name, parameters: params, returnType: retType, body: body}
}

// --- Exported value encoding functions ---

func EncodeInt(i int32) Value     { return encodeInt(i) }
func EncodeHandle(h Handle) Value { return encodeHandle(h) }
func EncodeBool(b bool) Value     { return encodeBool(b) }
func EncodeFloat(f float32) Value { return encodeFloat(f) }

// EncodeLong encodes an int64 value.
func EncodeLong(i int64, v *VM) Value { return v.encodeLong(i) }

// EncodeULong encodes a uint64 value.
func EncodeULong(u uint64, v *VM) Value { return v.encodeULong(u) }

// EncodeDouble encodes a float64 value.
func EncodeDouble(f float64, v *VM) Value { return v.encodeDouble(f) }

// DecodeInt decodes an integer value.
func DecodeInt(v Value) int32 { return v.decodeInt() }

// DecodeBool decodes a boolean value.
func DecodeBool(v Value) bool { return v.decodeBool() }

// DecodeFloat decodes a float value.
func DecodeFloat(v Value) float32 { return v.decodeFloat() }

// DecodeLong decodes a long (int64) value.
func (v *VM) DecodeLong(val Value) int64 { return v.decodeLong(val) }

// DecodeULong decodes a ulong (uint64) value.
func (v *VM) DecodeULong(val Value) uint64 { return v.decodeULong(val) }

// DecodeDouble decodes a double (float64) value.
func (v *VM) DecodeDouble(val Value) float64 { return v.decodeDouble(val) }

// IsNull checks if a value is null.
func IsNull(v Value) bool { return v.isNull() }

// IsInt checks if a value is an integer.
func IsInt(v Value) bool { return v.isInt() }

// IsBool checks if a value is a boolean.
func IsBool(v Value) bool { return v.isBool() }

// IsFloat checks if a value is a float.
func IsFloat(v Value) bool { return v.isFloat() }

// IsLong checks if a value is a long (int64).
func IsLong(v Value) bool { return v.isLong() }

// IsULong checks if a value is a ulong (uint64).
func IsULong(v Value) bool { return v.isULong() }

// IsDouble checks if a value is a double (float64).
func IsDouble(v Value) bool { return !v.isBoxed() }

// IsString checks if a value is a string.
func IsString(v Value) bool { return v.isString() }

// IsStringValue checks if a value is a string in this VM.
func (v *VM) IsStringValue(val Value) bool {
	_, ok := v.stringBytes(val)
	return ok
}

// IsBytesValue checks if a value is a bytes value in this VM (all tiers).
func (v *VM) IsBytesValue(val Value) bool {
	_, ok := v.bytesBytes(val)
	return ok
}

// IsArray reports whether a handle points to an array.
func (v *VM) IsArray(h Handle) bool {
	idx := v.resolveHandle(h)
	if idx < 0 || idx >= v.memTop {
		return false
	}
	return isArrayHeader(v.memory[idx])
}

// JoinStringArray joins a VM array of strings with a string separator.
func (v *VM) JoinStringArray(parts Value, sep Value) (Value, bool) {
	if !parts.isPointer() {
		return EncodeInt(0), false
	}
	return v.joinStringArray(parts.decodeHandle(), sep)
}

// IsHandle checks if a value is a handle.
func IsHandle(v Value) bool { return v.isPointer() }

// DecodeHandle decodes a handle value.
func DecodeHandle(v Value) Handle { return v.decodeHandle() }

// --- Closure / capture-cell value encoding (see value.go for details) ---

// EncodeClosureIndex encodes a closure-table index as a first-class value.
func EncodeClosureIndex(idx uint32) Value { return encodeClosureIndex(idx) }

// IsClosure reports whether a value is a closure reference.
func IsClosure(v Value) bool { return v.isClosure() }

// DecodeClosureIndex returns the closure-table index carried by a closure value.
func DecodeClosureIndex(v Value) uint32 { return v.decodeClosureIndex() }

// EncodeCellIndex encodes a capture-cell index as an internal value.
func EncodeCellIndex(idx uint32) Value { return encodeCellIndex(idx) }

// IsCell reports whether a value is a capture-cell reference.
func IsCell(v Value) bool { return v.isCell() }

// DecodeCellIndex returns the capture-cell index carried by a cell value.
func DecodeCellIndex(v Value) uint32 { return v.decodeCellIndex() }

// --- VM exported methods ---

// FuncReg returns the function registry.
func (v *VM) FuncReg() *FunctionRegistry { return v.funcReg }

// ClassReg returns the class registry.
func (v *VM) ClassReg() *ClassRegistry { return v.classRegistry }

// IfaceReg returns the interface registry.
func (v *VM) IfaceReg() *InterfaceRegistry { return v.ifaceRegistry }

// StructReg returns the struct registry.
func (v *VM) StructReg() *StructRegistry { return v.structRegistry }

// GetStruct looks up a struct definition by name.
func (sr *StructRegistry) GetStruct(name string) *StructDef {
	return sr.getByName(name)
}

// Fields returns the field definitions of a struct.
func (s *StructDef) Fields() []FieldDef {
	result := make([]FieldDef, len(s.fields))
	for i, f := range s.fields {
		result[i] = f
	}
	return result
}

// NewStructInstance creates a new struct instance with the given field values.
func (v *VM) NewStructInstance(structName string, fieldValues []Value) Handle {
	sd := v.structRegistry.getByName(structName)
	if sd == nil {
		return InvalidHandle
	}
	handle := v.newStruct(sd.id, fieldValues)
	return handle
}

// IsStruct reports whether a handle points to a struct instance.
func (v *VM) IsStruct(h Handle) bool { return v.isStruct(h) }

// GetStructFieldByIndex returns the value of a struct field by its zero-based index.
// Panics if the handle is not a struct or the index is out of range.
func (v *VM) GetStructFieldByIndex(h Handle, index int) Value {
	return v.getStructField(h, index)
}

// GetStructByID looks up a struct definition by its numeric ID.
func (sr *StructRegistry) GetStructByID(id uint32) *StructDef {
	return sr.get(structID(id))
}

// GetStructID returns the numeric ID for a registered struct name, or 0 if not found.
func (sr *StructRegistry) GetStructID(name string) uint32 {
	if id, exists := sr.nameToID[name]; exists {
		return uint32(id)
	}
	return 0
}

// --- EnumRegistry exported methods ---

// EnumReg returns the enum registry.
func (v *VM) EnumReg() *EnumRegistry { return v.enumRegistry }

// RegisterEnum registers an enum type; idempotent per name.
func (er *EnumRegistry) RegisterEnum(name string, members []EnumMemberDef) uint32 {
	return uint32(er.register(name, members))
}

// GetEnum looks up an enum definition by name.
func (er *EnumRegistry) GetEnum(name string) *EnumDef {
	return er.getByName(name)
}

// GetEnumByID looks up an enum definition by its numeric ID.
func (er *EnumRegistry) GetEnumByID(id uint32) *EnumDef {
	return er.get(enumID(id))
}

// GetEnumID returns the numeric ID for a registered enum name, or 0 if not found.
func (er *EnumRegistry) GetEnumID(name string) uint32 {
	if id, exists := er.nameToID[name]; exists {
		return uint32(id)
	}
	return 0
}

// --- Enum value encoding/decoding (see value.go for layout) ---

// EncodeEnumValue builds an enum value carrying the enum's runtime ID and the
// member's underlying int32 payload.
func EncodeEnumValue(enumID uint32, memberValue int32) Value {
	return encodeEnumValue(enumID, memberValue)
}

// IsEnum reports whether a value is an enum-tagged scalar.
func IsEnum(v Value) bool { return v.isEnum() }

// DecodeEnumID returns the enum runtime ID carried by an enum value.
func DecodeEnumID(v Value) uint32 { return v.decodeEnumID() }

// DecodeEnumValue returns the underlying int32 member value.
func DecodeEnumValue(v Value) int32 { return v.decodeEnumValue() }

// MakeEnumType builds a TypeID for a registered enum runtime ID.
func MakeEnumType(enumID uint32) TypeID { return makeEnumType(enumID) }

// GetEnumIDFromType extracts the enum runtime ID from an enum-category TypeID.
func GetEnumIDFromType(tid TypeID) uint32 { return getEnumIDFromType(tid) }

// ID returns the struct's numeric identifier.
func (s *StructDef) ID() uint32 { return uint32(s.id) }

// Name returns the struct's registered name.
func (s *StructDef) Name() string { return s.name }

// FieldCount returns the number of fields in the struct.
func (s *StructDef) FieldCount() int { return s.fieldCount }

// OperandStackCapacity reports the operand-stack capacity configured for the
// interpreter's single execution stack. The VM owns no operand stack itself;
// the interpreter (bytecode package) sizes its stack from this value.
func (v *VM) OperandStackCapacity() int { return v.stackCapacity }

// DecodeString decodes a string value.
func (v *VM) DecodeString(val Value) string { return v.decodeString(val) }

// EncodeString encodes a string value.
func (v *VM) EncodeString(s string) Value { return v.encodeString(s) }

// EncodeBytes encodes a byte slice value.
func (v *VM) EncodeBytes(data []byte) Value { return v.encodeBytes(data) }

// DecodeBytes decodes a bytes value.
func (v *VM) DecodeBytes(val Value) []byte { return v.decodeBytes(val) }

// IsBytes checks if a value is a bytes value.
func IsBytes(v Value) bool { return v.isBytes() }

// CreateObject creates a new object of the given class.
func (v *VM) CreateObject(cid uint32) Handle { return v.createObject(classID(cid)) }

// GetField retrieves a named field from an object.
func (v *VM) GetField(obj Handle, fieldName string) Value { return v.getField(obj, fieldName) }

// SetField sets a named field on an object.
func (v *VM) SetField(obj Handle, fieldName string, val Value) { v.setField(obj, fieldName, val) }

// ResolveHandle returns the memory index for a handle.
func (v *VM) ResolveHandle(h Handle) int { return v.resolveHandle(h) }

// AddRootProvider registers additional GC roots owned outside VM memory
// (e.g. the interpreter's operand stack).
func (v *VM) AddRootProvider(provider RootProvider) int {
	return v.addRootProvider(rootProvider(provider))
}

// RemoveRootProvider unregisters a previously registered GC root provider.
func (v *VM) RemoveRootProvider(id int) { v.removeRootProvider(id) }

// MemTop returns the current memory top for internal tests/bridges.
func (v *VM) MemTop() int { return v.memTop }

// MemoryAt returns the raw memory word at idx.
func (v *VM) MemoryAt(idx int) uint64 { return v.memory[idx] }

// IsLongHeapHeader reports whether a memory header is a heap long object.
func (v *VM) IsLongHeapHeader(header uint64) bool { return isLongHeapHeader(header) }

// IsULongHeapHeader reports whether a memory header is a heap ulong object.
func (v *VM) IsULongHeapHeader(header uint64) bool { return isULongHeapHeader(header) }

// IsLargeStringHeader reports whether a memory header is a large string object.
func (v *VM) IsLargeStringHeader(header uint64) bool { return isLargeStringHeader(header) }

// IsLargeBytesHeader reports whether a memory header is a large bytes object.
func (v *VM) IsLargeBytesHeader(header uint64) bool { return isLargeBytesHeader(header) }

// --- FunctionRegistry exported methods ---

// RegisterFunction registers a function definition.
func (fr *FunctionRegistry) RegisterFunction(def *FunctionDef) { fr.register(def) }

// GetFunction looks up a function by name.
func (fr *FunctionRegistry) GetFunction(name string) *FunctionDef { return fr.get(name) }

// HasFunction checks if a function is registered.
func (fr *FunctionRegistry) HasFunction(name string) bool { return fr.has(name) }

// --- FunctionDef exported methods ---

// ExecuteBody invokes the function body.
func (fd *FunctionDef) ExecuteBody(v *VM, args []Value) Value {
	return fd.body.execute(v, args)
}

// --- ClassRegistry exported methods ---

// RegisterClass registers a class and returns its ID.
func (cr *ClassRegistry) RegisterClass(class *Class) uint32 {
	return uint32(cr.register(class))
}

// GetClassByName looks up a class by name.
func (cr *ClassRegistry) GetClassByName(name string) *Class { return cr.getByName(name) }

// --- StructRegistry exported methods ---

// RegisterStruct registers a struct type.
func (sr *StructRegistry) RegisterStruct(name string, fields []FieldDef) {
	sr.register(name, fields)
}

// UpdateStructFields replaces the field definitions of an already-registered
// struct. Used when correct TypeIDs depend on other structs being registered
// first (e.g. nested struct fields).
func (sr *StructRegistry) UpdateStructFields(name string, fields []FieldDef) error {
	return sr.updateFields(name, fields)
}

// --- Class exported methods ---

// AddField adds a field to the class.
func (c *Class) AddField(name string, tid TypeID) { c.addField(name, tid) }

// AddMethod adds a method to the class.
func (c *Class) AddMethod(name string, impl MethodImpl) { c.addMethod(name, impl) }

// AddOpenMethod adds an open method that can be overridden by subclasses.
func (c *Class) AddOpenMethod(name string, impl MethodImpl) { c.addOpenMethod(name, impl) }

// OverrideMethod overrides a parent method in this class.
func (c *Class) OverrideMethod(name string, impl MethodImpl) { c.overrideMethod(name, impl) }

// SetParent sets the parent class for inheritance.
func (c *Class) SetParent(parent *Class) { c.setParent(parent) }

// ComputeFieldOffsets computes field memory offsets.
func (c *Class) ComputeFieldOffsets() { c.computeFieldOffsets() }

// BuildVTable builds the method dispatch table.
func (c *Class) BuildVTable() { c.buildVTable() }

// ID returns the class's numeric identifier.
func (c *Class) ID() uint32 { return uint32(c.id) }

// GetMethod looks up a method by name (with inheritance).
func (c *Class) GetMethod(name string) *MethodDef {
	m := c.getMethod(name)
	if m == nil {
		return nil
	}
	return &MethodDef{impl: m.impl, name: m.name, IsOpen: m.isOpen, IsOverride: m.isOverride}
}

// MethodDef is an exported wrapper for methodDef.
type MethodDef struct {
	impl       methodImpl
	name       string
	IsOpen     bool
	IsOverride bool
}

// Call invokes the method with the given receiver and arguments.
func (md *MethodDef) Call(v *VM, receiver Handle, args []Value) Value {
	return md.impl(v, receiver, args)
}

// --- Additional VM methods needed by interpreter ---

// ConcatStrings concatenates two string values.
func (v *VM) ConcatStrings(a, b Value) Value { return v.concatStrings(a, b) }

// NewArray creates a new array with the given element type and length.
func (v *VM) NewArray(elemType TypeID, length int) Handle {
	return v.newArray(elemType, length)
}

// GetArrayElement retrieves an element from an array.
func (v *VM) GetArrayElement(arr Handle, index int) Value {
	return v.getArrayElement(arr, index)
}

// SetArrayElement sets an element in an array.
func (v *VM) SetArrayElement(arr Handle, index int, val Value) {
	v.setArrayElement(arr, index, val)
}

// ArrayPush appends a value to an array.
func (v *VM) ArrayPush(arr Handle, val Value) { v.arrayPush(arr, val) }

// ArrayLength returns the number of elements in an array.
func (v *VM) ArrayLength(arr Handle) int { return v.getArrayLength(arr) }

// AddTemporaryRoot keeps values live until the returned release function is called.
func (v *VM) AddTemporaryRoot(values ...Value) func() { return v.addTemporaryRoot(values...) }

// NewMap creates a new map.
func (v *VM) NewMap(keyType, valType TypeID, initialCapacity int) Handle {
	return v.newMap(keyType, valType, initialCapacity)
}

// MapGet retrieves a value from a map.
func (v *VM) MapGet(m Handle, key Value) (Value, bool) { return v.mapGet(m, key) }

// MapSet sets a key-value pair in a map.
func (v *VM) MapSet(m Handle, key, val Value) { v.mapSet(m, key, val) }

// MapDelete removes a key from a map.
func (v *VM) MapDelete(m Handle, key Value) bool { return v.mapDelete(m, key) }

// MapSize returns the number of entries in a map.
func (v *VM) MapSize(m Handle) int { return v.mapSize(m) }

// MapIterate visits map entries in insertion order until fn returns false.
func (v *VM) MapIterate(m Handle, fn func(key, val Value) bool) { v.mapIterate(m, fn) }

// CallMethod dispatches a method call on an object.
func (v *VM) CallMethod(obj Handle, methodName string, args []Value) Value {
	cid := classID(v.getObjectClassID(obj))
	cls := v.classRegistry.getByID(cid)
	if cls == nil {
		v.Panic(fmt.Sprintf("cannot call method %q: object class not found", methodName))
		return EncodeInt(0) // unreachable
	}
	m := cls.getMethod(methodName)
	if m == nil {
		v.Panic(fmt.Sprintf("method %q not found on class %q", methodName, cls.name))
		return EncodeInt(0) // unreachable
	}
	return m.impl(v, obj, args)
}

// CallSuperMethod dispatches a method call on the parent class of the object.
func (v *VM) CallSuperMethod(obj Handle, methodName string, args []Value) Value {
	cid := classID(v.getObjectClassID(obj))
	cls := v.classRegistry.getByID(cid)
	if cls == nil {
		v.Panic(fmt.Sprintf("cannot call super method %q: object class not found", methodName))
		return EncodeInt(0) // unreachable
	}
	if cls.parent == nil {
		v.Panic(fmt.Sprintf("cannot call super method %q: class %q has no parent", methodName, cls.name))
		return EncodeInt(0) // unreachable
	}
	m := cls.parent.getMethod(methodName)
	if m == nil {
		v.Panic(fmt.Sprintf("super method %q not found on parent of class %q", methodName, cls.name))
		return EncodeInt(0) // unreachable
	}
	return m.impl(v, obj, args)
}

// IsMap checks if a handle points to a map.
func (v *VM) IsMap(h Handle) bool { return v.isMap(h) }

// MapKeyAt returns the key at the given zero-based position in the map's
// insertion order. The second return is false when the map is empty at that
// position or the handle does not point to a map.
func (v *VM) MapKeyAt(m Handle, index int) (Value, bool) { return v.mapKeyAt(m, index) }

// StringLength returns the byte length of a string value, or false when the
// value is not a string.
func (v *VM) StringLength(s Value) (int, bool) { return v.stringLength(s) }

// StringByteAt returns a one-byte string at the given byte index, or false
// when the value is not a string or the index is out of range.
func (v *VM) StringByteAt(s Value, index int) (Value, bool) { return v.stringByteAt(s, index) }

// GetObjectTypeName returns the class name of an object handle, or "<non-object>" if not an object.
func (v *VM) GetObjectTypeName(obj Handle) string {
	idx := v.ResolveHandle(obj)
	if idx < 0 || idx >= v.MemTop() {
		return "<non-object>"
	}
	cid := classID(v.getObjectClassID(obj))
	cls := v.classRegistry.getByID(cid)
	if cls == nil {
		return "<unknown>"
	}
	return cls.name
}

// IsInstanceOf checks if an object is an instance of the named class or interface.
func (v *VM) IsInstanceOf(obj Handle, typeName string) bool {
	idx := v.ResolveHandle(obj)
	if idx < 0 || idx >= v.MemTop() {
		return false
	}
	cid := classID(v.getObjectClassID(obj))
	cls := v.classRegistry.getByID(cid)
	if cls == nil {
		return false
	}
	// Check class hierarchy.
	for c := cls; c != nil; c = c.parent {
		if c.name == typeName {
			return true
		}
	}
	// Check interface implementations.
	return v.ifaceRegistry.isInstanceOfInterface(cls.name, typeName, v.classRegistry)
}

// Panic raises a runtime error.
func (v *VM) Panic(msg string) { panic(msg) }

// --- InterfaceRegistry exported methods ---

// RegisterInterface registers an interface definition.
func (ir *InterfaceRegistry) RegisterInterface(iface *InterfaceDef) { ir.register(iface) }

// GetInterfaceByName looks up an interface by name.
func (ir *InterfaceRegistry) GetInterfaceByName(name string) *InterfaceDef { return ir.getByName(name) }

// RegisterImplementation records that a class implements an interface.
func (ir *InterfaceRegistry) RegisterImplementation(className, ifaceName string) {
	ir.registerImplementation(className, ifaceName)
}

// IsInstanceOfInterface checks if a class implements an interface (walking inheritance).
func (ir *InterfaceRegistry) IsInstanceOfInterface(className, ifaceName string, classReg *ClassRegistry) bool {
	return ir.isInstanceOfInterface(className, ifaceName, classReg)
}

// NewInterfaceDef creates a new interface definition with method signatures.
func NewInterfaceDef(name string, methods []InterfaceMethodSig) *InterfaceDef {
	return &interfaceDef{name: name, methods: methods}
}

// NewInterfaceMethodSig creates a method signature for interface definitions.
func NewInterfaceMethodSig(name string, paramCount int) InterfaceMethodSig {
	return interfaceMethodSig{name: name, paramCount: paramCount}
}

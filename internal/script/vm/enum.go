package vm

// enumID is a 1-based runtime identifier for a registered enum type. It is
// baked into enum values (value.go) and TypeIDs (types.go) so `is`/`as`
// checks and struct field typing can identify the declaring enum.
type enumID uint32

// EnumMemberDef describes a single enum member for registration.
type EnumMemberDef struct {
	Name  string
	Value int32
}

// enumDef holds an enum type's closed member set.
type enumDef struct {
	id      enumID
	name    string
	members []EnumMemberDef
}

func (e *enumDef) ID() uint32 { return uint32(e.id) }
func (e *enumDef) Name() string {
	if e == nil {
		return ""
	}
	return e.name
}
func (e *enumDef) Members() []EnumMemberDef {
	if e == nil {
		return nil
	}
	return append([]EnumMemberDef(nil), e.members...)
}

// MemberValue returns the value of a named member and whether it exists.
func (e *enumDef) MemberValue(name string) (int32, bool) {
	if e == nil {
		return 0, false
	}
	for i := range e.members {
		if e.members[i].Name == name {
			return e.members[i].Value, true
		}
	}
	return 0, false
}

// HasValue reports whether v is one of the enum's member values (closed set).
func (e *enumDef) HasValue(v int32) bool {
	if e == nil {
		return false
	}
	for i := range e.members {
		if e.members[i].Value == v {
			return true
		}
	}
	return false
}

// enumRegistry manages all registered enum types.
type enumRegistry struct {
	enums    map[enumID]*enumDef
	nameToID map[string]enumID
	nextID   enumID
}

func newEnumRegistry() *enumRegistry {
	return &enumRegistry{
		enums:    make(map[enumID]*enumDef),
		nameToID: make(map[string]enumID),
		nextID:   1,
	}
}

// register adds an enum definition; registering the same name twice is
// idempotent and returns the existing ID.
func (er *enumRegistry) register(name string, members []EnumMemberDef) enumID {
	if id, exists := er.nameToID[name]; exists {
		return id
	}
	id := er.nextID
	er.nextID++
	er.enums[id] = &enumDef{id: id, name: name, members: append([]EnumMemberDef(nil), members...)}
	er.nameToID[name] = id
	return id
}

func (er *enumRegistry) get(id enumID) *enumDef {
	return er.enums[id]
}

func (er *enumRegistry) getByName(name string) *enumDef {
	if id, exists := er.nameToID[name]; exists {
		return er.enums[id]
	}
	return nil
}

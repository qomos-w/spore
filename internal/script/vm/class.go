package vm

import (
	"fmt"
	"sort"
)

// classID identifies a registered class.
type classID uint32

// fieldDef describes a class field.
type fieldDef struct {
	name       string
	typeID     typeID
	offset     int // offset in object memory (1-based, 0 is header)
	ownerClass *classDef
}

// methodImpl is the signature for a class method implementation.
type methodImpl func(v *vm, receiver handle, args []value) value

// methodDef describes a class method.
type methodDef struct {
	name       string
	impl       methodImpl
	isOpen     bool
	isOverride bool
	order      int
}

// classDef holds a class's metadata, fields, methods, and vtable.
type classDef struct {
	id              classID
	name            string
	parent          *classDef
	fields          []fieldDef
	methods         map[string]*methodDef
	vtable          []methodImpl
	allFields       []fieldDef
	fieldCount      int
	nextMethodOrder int
}

func newClass(id classID, name string, parent *classDef) *classDef {
	return &classDef{
		id:      id,
		name:    name,
		parent:  parent,
		fields:  make([]fieldDef, 0),
		methods: make(map[string]*methodDef),
		vtable:  make([]methodImpl, 0),
	}
}

func (c *classDef) addField(name string, tid typeID) {
	c.fields = append(c.fields, fieldDef{name: name, typeID: tid})
}

func (c *classDef) addMethod(name string, impl methodImpl) {
	c.methods[name] = &methodDef{name: name, impl: impl, isOpen: false, order: c.nextMethodOrder}
	c.nextMethodOrder++
}

func (c *classDef) addOpenMethod(name string, impl methodImpl) {
	c.methods[name] = &methodDef{name: name, impl: impl, isOpen: true, order: c.nextMethodOrder}
	c.nextMethodOrder++
}

func (c *classDef) overrideMethod(name string, impl methodImpl) {
	if c.parent != nil {
		parentMethod := c.parent.getMethod(name)
		if parentMethod == nil {
			panic(fmt.Sprintf("cannot override method %q: method not found in parent class %q", name, c.parent.name))
		}
		if !parentMethod.isOpen {
			panic(fmt.Sprintf("cannot override method %q: parent method in class %q is not open", name, c.parent.name))
		}
	}
	c.methods[name] = &methodDef{name: name, impl: impl, isOverride: true, order: c.nextMethodOrder}
	c.nextMethodOrder++
}

func (c *classDef) setParent(parent *classDef) {
	c.parent = parent
}

func (c *classDef) computeFieldOffsets() {
	c.allFields = make([]fieldDef, 0)
	offset := 1 // 0 is header

	if c.parent != nil {
		c.allFields = append(c.allFields, c.parent.allFields...)
		offset += len(c.parent.allFields)
	}

	for i := range c.fields {
		c.fields[i].offset = offset
		c.fields[i].ownerClass = c
		c.allFields = append(c.allFields, c.fields[i])
		offset++
	}

	c.fieldCount = len(c.allFields)
}

func (c *classDef) buildVTable() {
	// Start with parent vtable (if any).
	if c.parent != nil {
		c.vtable = make([]methodImpl, len(c.parent.vtable))
		copy(c.vtable, c.parent.vtable)
	} else {
		c.vtable = make([]methodImpl, 0)
	}

	// Override methods replace matching parent vtable slots.
	// Open and regular methods get appended as new slots.
	for _, method := range c.sortedMethods() {
		if method.isOverride {
			// Find the parent vtable slot for this method and replace it.
			if c.parent != nil {
				slotIdx := c.parent.findVTableSlot(method.name)
				if slotIdx >= 0 && slotIdx < len(c.vtable) {
					c.vtable[slotIdx] = method.impl
				}
			}
		} else {
			c.vtable = append(c.vtable, method.impl)
		}
	}
}

func (c *classDef) sortedMethods() []*methodDef {
	methods := make([]*methodDef, 0, len(c.methods))
	for _, method := range c.methods {
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].order < methods[j].order
	})
	return methods
}

// findVTableSlot returns the vtable index for a named method, or -1.
func (c *classDef) findVTableSlot(methodName string) int {
	idx := 0
	// Walk up parent chain to count slots from ancestors.
	if c.parent != nil {
		idx = c.parent.findVTableSlot(methodName)
		if idx >= 0 {
			return idx
		}
		idx = len(c.parent.vtable)
	}
	// Check own methods (non-override).
	for _, m := range c.sortedMethods() {
		if m.name == methodName && !m.isOverride {
			return idx
		}
		if !m.isOverride {
			idx++
		}
	}
	return -1
}

func (c *classDef) getField(name string) *fieldDef {
	for i := range c.allFields {
		if c.allFields[i].name == name {
			return &c.allFields[i]
		}
	}
	return nil
}

func (c *classDef) getMethod(name string) *methodDef {
	if m, ok := c.methods[name]; ok {
		return m
	}
	if c.parent != nil {
		return c.parent.getMethod(name)
	}
	return nil
}

// classRegistry manages all registered classes.
type classRegistry struct {
	classes     map[classID]*classDef
	classByName map[string]*classDef
	nextID      classID
}

func newClassRegistry() *classRegistry {
	return &classRegistry{
		classes:     make(map[classID]*classDef),
		classByName: make(map[string]*classDef),
		nextID:      1,
	}
}

func (cr *classRegistry) register(class *classDef) classID {
	if class.id == 0 {
		class.id = cr.nextID
		cr.nextID++
	}
	class.computeFieldOffsets()
	class.buildVTable()
	cr.classes[class.id] = class
	cr.classByName[class.name] = class
	return class.id
}

func (cr *classRegistry) getByName(name string) *classDef {
	return cr.classByName[name]
}

func (cr *classRegistry) getByID(id classID) *classDef {
	return cr.classes[id]
}

func (cr *classRegistry) callMethod(v *vm, receiver handle, methodName string, args []value) value {
	cid := v.getObjectClassID(receiver)
	class := cr.getByID(classID(cid))
	if class == nil {
		panic(fmt.Sprintf("cannot call method %q: object class not found", methodName))
	}
	method := class.getMethod(methodName)
	if method == nil {
		panic(fmt.Sprintf("method %q not found on class %q", methodName, class.name))
	}
	return method.impl(v, receiver, args)
}

func (cr *classRegistry) callSuperMethod(v *vm, receiver handle, methodName string, args []value) value {
	cid := v.getObjectClassID(receiver)
	class := cr.getByID(classID(cid))
	if class == nil {
		panic(fmt.Sprintf("cannot call super method %q: object class not found", methodName))
	}
	if class.parent == nil {
		panic(fmt.Sprintf("cannot call super method %q: class %q has no parent", methodName, class.name))
	}
	method := class.parent.getMethod(methodName)
	if method == nil {
		panic(fmt.Sprintf("super method %q not found on parent of class %q", methodName, class.name))
	}
	return method.impl(v, receiver, args)
}

// --- Interface registry ---

// interfaceMethodSig describes a single method signature in an interface.
type interfaceMethodSig struct {
	name       string
	paramCount int
}

// interfaceDef holds an interface's metadata: name and required method signatures.
type interfaceDef struct {
	name    string
	methods []interfaceMethodSig
}

// interfaceRegistry manages all registered interfaces.
type interfaceRegistry struct {
	ifaces     map[string]*interfaceDef
	implMap    map[string][]string // interface name → implementing class names
	classImpls map[string][]string // class name → interface names it implements
}

func newInterfaceRegistry() *interfaceRegistry {
	return &interfaceRegistry{
		ifaces:     make(map[string]*interfaceDef),
		implMap:    make(map[string][]string),
		classImpls: make(map[string][]string),
	}
}

func (ir *interfaceRegistry) register(iface *interfaceDef) {
	ir.ifaces[iface.name] = iface
}

func (ir *interfaceRegistry) getByName(name string) *interfaceDef {
	return ir.ifaces[name]
}

// registerImplementation records that className implements ifaceName.
// The caller (compiler) is responsible for verifying the contract.
func (ir *interfaceRegistry) registerImplementation(className, ifaceName string) {
	ir.implMap[ifaceName] = append(ir.implMap[ifaceName], className)
	ir.classImpls[className] = append(ir.classImpls[className], ifaceName)
}

// isInstanceOfInterface checks whether the class named className implements
// the interface named ifaceName (directly or via inheritance).
func (ir *interfaceRegistry) isInstanceOfInterface(className, ifaceName string, classReg *classRegistry) bool {
	// Walk the class hierarchy; check each class's implemented interfaces.
	cls := classReg.getByName(className)
	for cls != nil {
		for _, implName := range ir.classImpls[cls.name] {
			if implName == ifaceName {
				return true
			}
		}
		cls = cls.parent
	}
	return false
}

// classesImplementing returns all class names that directly implement the named interface.
func (ir *interfaceRegistry) classesImplementing(ifaceName string) []string {
	return ir.implMap[ifaceName]
}

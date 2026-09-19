package vm

import (
	"testing"
)

func TestInterfaceRegistryRegisterAndGet(t *testing.T) {
	ir := newInterfaceRegistry()
	iface := &interfaceDef{
		name: "Printable",
		methods: []interfaceMethodSig{
			{name: "print", paramCount: 0},
		},
	}
	ir.register(iface)

	got := ir.getByName("Printable")
	if got == nil || got.name != "Printable" {
		t.Fatal("getByName failed to find registered interface")
	}
	if ir.getByName("Missing") != nil {
		t.Error("getByName should return nil for unregistered interface")
	}
}

func TestInterfaceRegistryRegisterImplementation(t *testing.T) {
	ir := newInterfaceRegistry()
	ir.register(&interfaceDef{name: "Serializable", methods: nil})
	ir.register(&interfaceDef{name: "Comparable", methods: nil})
	ir.registerImplementation("Widget", "Serializable")
	ir.registerImplementation("Widget", "Comparable")
	ir.registerImplementation("Gadget", "Serializable")

	impls := ir.classesImplementing("Serializable")
	if len(impls) != 2 {
		t.Fatalf("Serializable implementors = %d, want 2", len(impls))
	}

	impls = ir.classesImplementing("Comparable")
	if len(impls) != 1 || impls[0] != "Widget" {
		t.Errorf("Comparable implementors = %v, want [Widget]", impls)
	}
}

func TestInterfaceIsInstanceOfInterfaceDirect(t *testing.T) {
	ir := newInterfaceRegistry()
	cr := newClassRegistry()

	ir.register(&interfaceDef{name: "Runnable", methods: nil})
	cls := newClass(1, "Worker", nil)
	cr.register(cls)
	ir.registerImplementation("Worker", "Runnable")

	if !ir.isInstanceOfInterface("Worker", "Runnable", cr) {
		t.Error("Worker should be instance of Runnable")
	}
	if ir.isInstanceOfInterface("Worker", "Missing", cr) {
		t.Error("Worker should not be instance of Missing")
	}
}

func TestInterfaceIsInstanceOfInterfaceInherited(t *testing.T) {
	ir := newInterfaceRegistry()
	cr := newClassRegistry()

	ir.register(&interfaceDef{name: "Runnable", methods: nil})

	parent := newClass(1, "Base", nil)
	parent.computeFieldOffsets()
	parent.buildVTable()
	cr.register(parent)
	ir.registerImplementation("Base", "Runnable")

	child := newClass(2, "Derived", parent)
	child.computeFieldOffsets()
	child.buildVTable()
	cr.register(child)
	// Derived does NOT directly implement Runnable, but Base does.

	if !ir.isInstanceOfInterface("Derived", "Runnable", cr) {
		t.Error("Derived should inherit Runnable from Base")
	}
}

func TestInterfaceIsInstanceOfInterfaceNotInherited(t *testing.T) {
	ir := newInterfaceRegistry()
	cr := newClassRegistry()

	ir.register(&interfaceDef{name: "Runnable", methods: nil})

	parent := newClass(1, "Base", nil)
	parent.computeFieldOffsets()
	parent.buildVTable()
	cr.register(parent)
	// Only child implements Runnable, not parent.

	child := newClass(2, "Derived", parent)
	child.computeFieldOffsets()
	child.buildVTable()
	cr.register(child)
	ir.registerImplementation("Derived", "Runnable")

	if ir.isInstanceOfInterface("Base", "Runnable", cr) {
		t.Error("Base should NOT be instance of Runnable (only child implements it)")
	}
}

func TestInterfaceDispatchSurvivesGCPressure(t *testing.T) {
	v := newVM(16384, 256)

	iface := &interfaceDef{
		name:    "Greeter",
		methods: []interfaceMethodSig{{name: "greet", paramCount: 0}},
	}
	v.ifaceRegistry.register(iface)

	cls := newClass(1, "Hello", nil)
	cls.addField("name", typeString)
	cls.addMethod("greet", func(v *vm, receiver handle, args []value) value {
		return v.getField(receiver, "name")
	})
	cls.computeFieldOffsets()
	cls.buildVTable()
	v.classRegistry.register(cls)
	v.ifaceRegistry.registerImplementation("Hello", "Greeter")

	obj := v.createObject(1)
	v.setField(obj, "name", v.encodeString("world"))
	release := v.addTemporaryRoot(encodeHandle(obj))
	defer release()

	for round := 0; round < 4; round++ {
		for j := 0; j < 128; j++ {
			_ = v.newArray(typeAny, 8)
		}
		v.gc.collect()
	}

	if !v.ifaceRegistry.isInstanceOfInterface("Hello", "Greeter", v.classRegistry) {
		t.Fatal("Hello should still implement Greeter after GC pressure")
	}
	result := v.classRegistry.callMethod(v, obj, "greet", nil)
	if v.decodeString(result) != "world" {
		t.Errorf("interface dispatch after GC = %q, want world", v.decodeString(result))
	}
}

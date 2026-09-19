package vm

import (
	"testing"
)

// --- Class field offset computation ---

func TestClassFieldOffsetsNoParent(t *testing.T) {
	c := newClass(1, "Simple", nil)
	c.addField("x", typeInt)
	c.addField("y", typeFloat)
	c.addField("z", typeString)

	c.computeFieldOffsets()

	if len(c.allFields) != 3 {
		t.Fatalf("allFields count = %d, want 3", len(c.allFields))
	}
	// Offsets are 1-based (0 is header).
	for i, f := range c.allFields {
		if f.offset != 1+i {
			t.Errorf("field %q offset = %d, want %d", f.name, f.offset, 1+i)
		}
	}
	if c.fieldCount != 3 {
		t.Errorf("fieldCount = %d, want 3", c.fieldCount)
	}
}

func TestClassFieldOffsetsWithParent(t *testing.T) {
	parent := newClass(1, "Base", nil)
	parent.addField("a", typeInt)
	parent.addField("b", typeFloat)
	parent.computeFieldOffsets()

	child := newClass(2, "Derived", parent)
	child.addField("c", typeString)
	child.addField("d", typeInt)
	child.computeFieldOffsets()

	if len(child.allFields) != 4 {
		t.Fatalf("allFields count = %d, want 4", len(child.allFields))
	}
	// Parent fields: a=1, b=2
	// Child fields: c=3, d=4
	wantOffsets := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	for _, f := range child.allFields {
		if f.offset != wantOffsets[f.name] {
			t.Errorf("field %q offset = %d, want %d", f.name, f.offset, wantOffsets[f.name])
		}
	}
	if child.fieldCount != 4 {
		t.Errorf("fieldCount = %d, want 4", child.fieldCount)
	}
}

func TestClassFieldOffsetsThreeLevels(t *testing.T) {
	grandparent := newClass(1, "A", nil)
	grandparent.addField("x", typeInt)
	grandparent.computeFieldOffsets()

	parent := newClass(2, "B", grandparent)
	parent.addField("y", typeFloat)
	parent.computeFieldOffsets()

	child := newClass(3, "C", parent)
	child.addField("z", typeString)
	child.computeFieldOffsets()

	if len(child.allFields) != 3 {
		t.Fatalf("allFields count = %d, want 3", len(child.allFields))
	}
	wantOffsets := map[string]int{"x": 1, "y": 2, "z": 3}
	for _, f := range child.allFields {
		if f.offset != wantOffsets[f.name] {
			t.Errorf("field %q offset = %d, want %d", f.name, f.offset, wantOffsets[f.name])
		}
	}
}

func TestClassFieldMapSizeForArrayHeldObject(t *testing.T) {
	v := NewVM(4096, 256)

	shopClass := NewClass(1, "Shop", nil)
	shopClass.AddField("prices", TypeInvalid)
	shopClass.ComputeFieldOffsets()
	shopClass.BuildVTable()
	v.ClassReg().RegisterClass(shopClass)

	shops := v.NewArray(TypeInvalid, 1)
	shop := v.CreateObject(shopClass.ID())
	prices := v.NewMap(TypeInvalid, TypeInvalid, 16)
	v.MapSet(prices, v.EncodeString("potion"), EncodeInt(3))
	v.MapSet(prices, v.EncodeString("ether"), EncodeInt(5))
	v.SetField(shop, "prices", EncodeHandle(prices))
	v.SetArrayElement(shops, 0, EncodeHandle(shop))

	storedShop := DecodeHandle(v.GetArrayElement(shops, 0))
	storedPrices := DecodeHandle(v.GetField(storedShop, "prices"))
	if got := v.MapSize(storedPrices); got != 2 {
		t.Fatalf("expected array-held object's map field size 2, got %d", got)
	}
}

func TestClassFieldMapHandleForArrayHeldObjectIsMap(t *testing.T) {
	v := NewVM(4096, 256)

	shopClass := NewClass(1, "Shop", nil)
	shopClass.AddField("prices", TypeInvalid)
	shopClass.ComputeFieldOffsets()
	shopClass.BuildVTable()
	v.ClassReg().RegisterClass(shopClass)

	shops := v.NewArray(TypeInvalid, 1)
	shop := v.CreateObject(shopClass.ID())
	prices := v.NewMap(TypeInvalid, TypeInvalid, 16)
	v.MapSet(prices, v.EncodeString("potion"), EncodeInt(3))
	v.SetField(shop, "prices", EncodeHandle(prices))
	v.SetArrayElement(shops, 0, EncodeHandle(shop))

	storedShop := DecodeHandle(v.GetArrayElement(shops, 0))
	storedPrices := DecodeHandle(v.GetField(storedShop, "prices"))
	if !v.IsMap(storedPrices) {
		t.Fatal("expected array-held object's prices field handle to be recognized as map")
	}
}

// --- VTable building ---

func TestVTableNoParent(t *testing.T) {
	c := newClass(1, "C", nil)
	c.addMethod("m1", func(v *vm, receiver handle, args []value) value { return encodeInt(1) })
	c.addMethod("m2", func(v *vm, receiver handle, args []value) value { return encodeInt(2) })
	c.buildVTable()

	if len(c.vtable) != 2 {
		t.Fatalf("vtable size = %d, want 2", len(c.vtable))
	}
	// Methods should be dispatchable.
	v := newVM(4096, 256)
	v.classRegistry.register(c)
	obj := v.createObject(1)
	if DecodeInt(c.vtable[0](v, obj, nil)) != 1 {
		t.Error("vtable[0] should dispatch m1")
	}
	if DecodeInt(c.vtable[1](v, obj, nil)) != 2 {
		t.Error("vtable[1] should dispatch m2")
	}
}

func TestVTableInheritedFromParent(t *testing.T) {
	parent := newClass(1, "P", nil)
	parent.addMethod("base", func(v *vm, receiver handle, args []value) value { return encodeInt(10) })
	parent.buildVTable()

	child := newClass(2, "C", parent)
	child.addMethod("derived", func(v *vm, receiver handle, args []value) value { return encodeInt(20) })
	child.buildVTable()

	// Child vtable = [base, derived]
	if len(child.vtable) != 2 {
		t.Fatalf("vtable size = %d, want 2", len(child.vtable))
	}
	v := newVM(4096, 256)
	v.classRegistry.register(parent)
	v.classRegistry.register(child)

	parentObj := v.createObject(1)
	childObj := v.createObject(2)

	if DecodeInt(child.vtable[0](v, childObj, nil)) != 10 {
		t.Error("vtable[0] should dispatch inherited base")
	}
	if DecodeInt(child.vtable[1](v, childObj, nil)) != 20 {
		t.Error("vtable[1] should dispatch derived")
	}
	// Parent object still uses parent vtable.
	if DecodeInt(parent.vtable[0](v, parentObj, nil)) != 10 {
		t.Error("parent vtable[0] should dispatch base")
	}
}

func TestVTableOpenMethodOverride(t *testing.T) {
	parent := newClass(1, "P", nil)
	parent.addOpenMethod("greet", func(v *vm, receiver handle, args []value) value {
		return v.encodeString("hello from parent")
	})
	parent.buildVTable()

	child := newClass(2, "C", parent)
	child.overrideMethod("greet", func(v *vm, receiver handle, args []value) value {
		return v.encodeString("hello from child")
	})
	child.buildVTable()

	// Child vtable should have overridden slot 0.
	if len(child.vtable) != 1 {
		t.Fatalf("vtable size = %d, want 1", len(child.vtable))
	}

	v := newVM(4096, 256)
	v.classRegistry.register(parent)
	v.classRegistry.register(child)

	parentObj := v.createObject(1)
	childObj := v.createObject(2)

	if v.decodeString(child.vtable[0](v, childObj, nil)) != "hello from child" {
		t.Error("child vtable[0] should dispatch overridden greet")
	}
	if v.decodeString(parent.vtable[0](v, parentObj, nil)) != "hello from parent" {
		t.Error("parent vtable[0] should still dispatch original greet")
	}
}

func TestVTableOverridePlusNewMethods(t *testing.T) {
	parent := newClass(1, "P", nil)
	parent.addOpenMethod("greet", func(v *vm, receiver handle, args []value) value { return encodeInt(1) })
	parent.addMethod("work", func(v *vm, receiver handle, args []value) value { return encodeInt(2) })
	parent.buildVTable()

	child := newClass(2, "C", parent)
	child.overrideMethod("greet", func(v *vm, receiver handle, args []value) value { return encodeInt(99) })
	child.addMethod("play", func(v *vm, receiver handle, args []value) value { return encodeInt(3) })
	child.buildVTable()

	// Vtable: [greet(overridden), work, play]
	if len(child.vtable) != 3 {
		t.Fatalf("vtable size = %d, want 3", len(child.vtable))
	}
	v := newVM(4096, 256)
	v.classRegistry.register(parent)
	v.classRegistry.register(child)

	obj := v.createObject(2)
	if DecodeInt(child.vtable[0](v, obj, nil)) != 99 {
		t.Error("vtable[0] should be overridden greet")
	}
	if DecodeInt(child.vtable[1](v, obj, nil)) != 2 {
		t.Error("vtable[1] should be inherited work")
	}
	if DecodeInt(child.vtable[2](v, obj, nil)) != 3 {
		t.Error("vtable[2] should be new play")
	}
}

// --- getMethod with inheritance ---

func TestClassGetMethodInheritedFromParent(t *testing.T) {
	parent := newClass(1, "P", nil)
	parent.addMethod("foo", func(v *vm, receiver handle, args []value) value { return encodeInt(42) })

	child := newClass(2, "C", parent)
	child.addMethod("bar", func(v *vm, receiver handle, args []value) value { return encodeInt(7) })

	// Child should find foo via parent.
	m := child.getMethod("foo")
	if m == nil {
		t.Fatal("child should inherit method foo from parent")
	}
	// Child should find its own bar.
	m = child.getMethod("bar")
	if m == nil {
		t.Fatal("child should have method bar")
	}
	// Parent should not see child methods.
	m = parent.getMethod("bar")
	if m != nil {
		t.Error("parent should not see child method bar")
	}
}

// --- classRegistry ---

func TestClassRegistryAutoAssignsID(t *testing.T) {
	cr := newClassRegistry()
	c := newClass(0, "Auto", nil)
	c.addField("v", typeInt)
	id := cr.register(c)
	if id == 0 {
		t.Error("expected auto-assigned class ID > 0")
	}
	if c.id != id {
		t.Errorf("class.id = %d, want %d", c.id, id)
	}
}

func TestClassRegistryGetByName(t *testing.T) {
	cr := newClassRegistry()
	c := newClass(0, "MyClass", nil)
	cr.register(c)
	found := cr.getByName("MyClass")
	if found == nil || found.name != "MyClass" {
		t.Error("getByName failed to find registered class")
	}
	if cr.getByName("Missing") != nil {
		t.Error("getByName should return nil for unregistered name")
	}
}

func TestClassRegistryGetByID(t *testing.T) {
	cr := newClassRegistry()
	c := newClass(5, "ByID", nil)
	cr.register(c)
	found := cr.getByID(5)
	if found == nil || found.name != "ByID" {
		t.Error("getByID failed to find registered class")
	}
	if cr.getByID(999) != nil {
		t.Error("getByID should return nil for unregistered ID")
	}
}

// --- callMethod & callSuperMethod ---

func TestCallMethodDispatch(t *testing.T) {
	v := newVM(4096, 256)

	parent := newClass(1, "Base", nil)
	parent.addField("val", typeInt)
	parent.addOpenMethod("getVal", func(v *vm, receiver handle, args []value) value {
		return v.getField(receiver, "val")
	})
	parent.buildVTable()
	v.classRegistry.register(parent)

	child := newClass(2, "Child", parent)
	child.overrideMethod("getVal", func(v *vm, receiver handle, args []value) value {
		baseVal := v.getField(receiver, "val")
		return encodeInt(baseVal.decodeInt() * 10)
	})
	child.buildVTable()
	v.classRegistry.register(child)

	parentObj := v.createObject(1)
	v.setField(parentObj, "val", encodeInt(5))

	childObj := v.createObject(2)
	v.setField(childObj, "val", encodeInt(5))

	result := v.classRegistry.callMethod(v, parentObj, "getVal", nil)
	if result.decodeInt() != 5 {
		t.Errorf("parent method result = %d, want 5", result.decodeInt())
	}

	result = v.classRegistry.callMethod(v, childObj, "getVal", nil)
	if result.decodeInt() != 50 {
		t.Errorf("child overridden method result = %d, want 50", result.decodeInt())
	}
}

func TestCallSuperMethod(t *testing.T) {
	v := newVM(4096, 256)

	parent := newClass(1, "Base", nil)
	parent.addField("val", typeInt)
	parent.addOpenMethod("getVal", func(v *vm, receiver handle, args []value) value {
		return v.getField(receiver, "val")
	})
	parent.buildVTable()
	v.classRegistry.register(parent)

	child := newClass(2, "Child", parent)
	child.addMethod("callSuper", func(v *vm, receiver handle, args []value) value {
		return v.classRegistry.callSuperMethod(v, receiver, "getVal", args)
	})
	child.buildVTable()
	v.classRegistry.register(child)

	obj := v.createObject(2)
	v.setField(obj, "val", encodeInt(42))

	result := v.classRegistry.callSuperMethod(v, obj, "getVal", nil)
	if result.decodeInt() != 42 {
		t.Errorf("super method result = %d, want 42", result.decodeInt())
	}
}

// --- Inherited fields in objects ---

func TestObjectWithInheritedFields(t *testing.T) {
	v := newVM(4096, 256)

	parent := newClass(1, "Animal", nil)
	parent.addField("name", typeString)
	parent.addField("age", typeInt)
	v.classRegistry.register(parent)

	child := newClass(2, "Dog", parent)
	child.addField("breed", typeString)
	v.classRegistry.register(child)

	obj := v.createObject(2)
	v.setField(obj, "name", v.encodeString("Rex"))
	v.setField(obj, "age", encodeInt(5))
	v.setField(obj, "breed", v.encodeString("Shepherd"))

	if v.decodeString(v.getField(obj, "name")) != "Rex" {
		t.Error("inherited field name not accessible")
	}
	if v.getField(obj, "age").decodeInt() != 5 {
		t.Error("inherited field age not accessible")
	}
	if v.decodeString(v.getField(obj, "breed")) != "Shepherd" {
		t.Error("own field breed not accessible")
	}
}

// --- IsInstanceOf ---

func TestIsInstanceOfClassHierarchy(t *testing.T) {
	v := NewVM(4096, 256)

	parent := NewClass(1, "Animal", nil)
	parent.AddField("name", typeString)
	parent.BuildVTable()
	v.ClassReg().RegisterClass(parent)

	child := NewClass(2, "Dog", parent)
	child.AddField("breed", typeString)
	child.BuildVTable()
	v.ClassReg().RegisterClass(child)

	obj := v.CreateObject(2)

	if !v.IsInstanceOf(obj, "Dog") {
		t.Error("Dog object should be instance of Dog")
	}
	if !v.IsInstanceOf(obj, "Animal") {
		t.Error("Dog object should be instance of Animal (parent)")
	}
	if v.IsInstanceOf(obj, "Cat") {
		t.Error("Dog object should not be instance of Cat")
	}
}

func TestIsInstanceOfWithInterface(t *testing.T) {
	v := NewVM(4096, 256)

	iface := NewInterfaceDef("Serializable", []InterfaceMethodSig{
		NewInterfaceMethodSig("serialize", 0),
	})
	v.IfaceReg().RegisterInterface(iface)

	cls := NewClass(1, "Widget", nil)
	cls.AddField("data", typeString)
	cls.AddMethod("serialize", func(v *VM, receiver Handle, args []Value) Value {
		return v.EncodeString("widget")
	})
	cls.BuildVTable()
	v.ClassReg().RegisterClass(cls)
	v.IfaceReg().RegisterImplementation("Widget", "Serializable")

	obj := v.CreateObject(1)
	if !v.IsInstanceOf(obj, "Serializable") {
		t.Error("Widget should be instance of Serializable")
	}
	if !v.IsInstanceOf(obj, "Widget") {
		t.Error("Widget should be instance of Widget")
	}
}

func TestClassMethodDispatchSurvivesGCPressure(t *testing.T) {
	v := newVM(16384, 256)

	cls := newClass(1, "Counter", nil)
	cls.addField("n", typeInt)
	cls.addMethod("read", func(v *vm, receiver handle, args []value) value {
		return v.getField(receiver, "n")
	})
	cls.computeFieldOffsets()
	cls.buildVTable()
	v.classRegistry.register(cls)

	obj := v.createObject(1)
	v.setField(obj, "n", encodeInt(123))
	release := v.addTemporaryRoot(encodeHandle(obj))
	defer release()

	for round := 0; round < 4; round++ {
		for j := 0; j < 128; j++ {
			_ = v.newArray(typeAny, 8)
		}
		v.gc.collect()
	}

	result := v.classRegistry.callMethod(v, obj, "read", nil)
	if result.decodeInt() != 123 {
		t.Errorf("method dispatch after GC = %d, want 123", result.decodeInt())
	}
}

func TestClassOverriddenMethodDispatchSurvivesGCPressure(t *testing.T) {
	v := newVM(16384, 256)

	parent := newClass(1, "Base", nil)
	parent.addField("n", typeInt)
	parent.addOpenMethod("scaled", func(v *vm, receiver handle, args []value) value {
		return encodeInt(v.getField(receiver, "n").decodeInt() * 2)
	})
	parent.computeFieldOffsets()
	parent.buildVTable()
	v.classRegistry.register(parent)

	child := newClass(2, "Triple", parent)
	child.overrideMethod("scaled", func(v *vm, receiver handle, args []value) value {
		return encodeInt(v.getField(receiver, "n").decodeInt() * 3)
	})
	child.computeFieldOffsets()
	child.buildVTable()
	v.classRegistry.register(child)

	parentObj := v.createObject(1)
	v.setField(parentObj, "n", encodeInt(5))
	childObj := v.createObject(2)
	v.setField(childObj, "n", encodeInt(7))

	rel1 := v.addTemporaryRoot(encodeHandle(parentObj))
	defer rel1()
	rel2 := v.addTemporaryRoot(encodeHandle(childObj))
	defer rel2()

	for round := 0; round < 4; round++ {
		for j := 0; j < 128; j++ {
			_ = v.newMap(typeString, typeInt, 4)
		}
		v.gc.collect()
	}

	pResult := v.classRegistry.callMethod(v, parentObj, "scaled", nil)
	if pResult.decodeInt() != 10 {
		t.Errorf("parent dispatch after GC = %d, want 10", pResult.decodeInt())
	}
	cResult := v.classRegistry.callMethod(v, childObj, "scaled", nil)
	if cResult.decodeInt() != 21 {
		t.Errorf("child overridden dispatch after GC = %d, want 21", cResult.decodeInt())
	}
}

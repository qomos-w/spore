package vm

import (
	"testing"
)

func TestVMNewAndMemory(t *testing.T) {
	v := newVM(1024, 256)
	if v == nil {
		t.Fatal("newVM returned nil")
	}
	if v.memTop != 1 {
		t.Errorf("memTop = %d, want 1", v.memTop)
	}
}

func TestVMObjectAllocation(t *testing.T) {
	v := newVM(4096, 256)

	// Register a class.
	class := newClass(1, "Test", nil)
	class.addField("x", typeInt)
	class.addField("y", typeInt)
	v.classRegistry.register(class)

	// Create an object.
	h := v.createObject(1)
	if h == invalidHandle {
		t.Fatal("createObject returned invalid handle")
	}

	// Set fields.
	v.setField(h, "x", encodeInt(42))
	v.setField(h, "y", encodeInt(99))

	// Get fields.
	x := v.getField(h, "x")
	y := v.getField(h, "y")

	if x.decodeInt() != 42 {
		t.Errorf("x = %d, want 42", x.decodeInt())
	}
	if y.decodeInt() != 99 {
		t.Errorf("y = %d, want 99", y.decodeInt())
	}
}

func TestVMStringOperations(t *testing.T) {
	v := newVM(4096, 256)

	// Small string.
	s1 := v.encodeString("hello")
	if !s1.isSmallString() {
		t.Error("small string not detected")
	}
	if v.decodeString(s1) != "hello" {
		t.Errorf("decode small string = %q, want %q", v.decodeString(s1), "hello")
	}

	// Medium string (8-256 bytes).
	s2 := v.encodeString("hello world, this is a medium string test")
	if v.decodeString(s2) != "hello world, this is a medium string test" {
		t.Errorf("decode medium string = %q, want %q", v.decodeString(s2), "hello world, this is a medium string test")
	}

	// String concatenation.
	result := v.concatStrings(v.encodeString("foo"), v.encodeString("bar"))
	if v.decodeString(result) != "foobar" {
		t.Errorf("concat = %q, want %q", v.decodeString(result), "foobar")
	}
}

func TestVMNumericArea(t *testing.T) {
	v := newVM(4096, 256)

	// Long.
	lv := v.encodeLong(12345678901234)
	if !lv.isLong() {
		t.Error("long not detected")
	}
	if v.decodeLong(lv) != 12345678901234 {
		t.Errorf("decodeLong = %d, want 12345678901234", v.decodeLong(lv))
	}

	// Double.
	dv := v.encodeDouble(3.14159265358979)
	if !dv.isDouble() {
		t.Error("double not detected")
	}
	if v.decodeDouble(dv) != 3.14159265358979 {
		t.Errorf("decodeDouble = %g, want 3.14159265358979", v.decodeDouble(dv))
	}
}

func TestVMStructOperations(t *testing.T) {
	v := newVM(4096, 256)

	// Register a struct.
	fields := []fieldDef{
		{name: "name", typeID: typeString},
		{name: "age", typeID: typeInt},
	}
	sid, err := v.structRegistry.register("Person", fields)
	if err != nil {
		t.Fatal(err)
	}

	// Create a struct instance.
	h := v.newStruct(sid, []value{v.encodeString("Alice"), encodeInt(30)})

	// Access by name.
	name := v.getStructFieldByName(h, "name")
	age := v.getStructFieldByName(h, "age")

	if v.decodeString(name) != "Alice" {
		t.Errorf("name = %q, want %q", v.decodeString(name), "Alice")
	}
	if age.decodeInt() != 30 {
		t.Errorf("age = %d, want 30", age.decodeInt())
	}

	// Modify by name.
	v.setStructFieldByName(h, "age", encodeInt(31))
	if v.getStructFieldByName(h, "age").decodeInt() != 31 {
		t.Error("struct field update failed")
	}
}

func TestVMClassMethodDispatchDoesNotRegisterFunction(t *testing.T) {
	v := NewVM(4096, 256)

	class := NewClass(1, "Counter", nil)
	class.AddMethod("value", func(v *VM, receiver Handle, args []Value) Value {
		return EncodeInt(7)
	})
	class.BuildVTable()
	v.ClassReg().RegisterClass(class)

	if v.FuncReg().HasFunction("value") {
		t.Fatal("class method must not be registered as top-level function")
	}
	if v.FuncReg().HasFunction("Counter.value") {
		t.Fatal("class method must not leak into function registry under qualified name")
	}

	h := v.CreateObject(class.ID())
	result := v.CallMethod(h, "value", nil)
	if DecodeInt(result) != 7 {
		t.Fatalf("expected method dispatch result 7, got %d", DecodeInt(result))
	}
}

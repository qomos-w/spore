package vm

import (
	"testing"
)

func TestFunctionRegistryRegisterAndGet(t *testing.T) {
	fr := newFunctionRegistry()
	fn := &functionDef{
		name:       "add",
		parameters: []paramDef{{name: "a", typeID: typeInt}, {name: "b", typeID: typeInt}},
		returnType: typeInt,
		body:       &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(args[0].decodeInt() + args[1].decodeInt()) }},
	}
	fr.register(fn)

	got := fr.get("add")
	if got == nil || got.name != "add" {
		t.Fatal("get failed to find registered function")
	}
	if fr.get("missing") != nil {
		t.Error("get should return nil for unregistered function")
	}
}

func TestFunctionRegistryHas(t *testing.T) {
	fr := newFunctionRegistry()
	if fr.has("foo") {
		t.Error("has should return false for unregistered function")
	}
	fr.register(&functionDef{name: "foo", body: &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(0) }}})
	if !fr.has("foo") {
		t.Error("has should return true for registered function")
	}
}

func TestFunctionRegistryCall(t *testing.T) {
	v := newVM(4096, 256)
	fr := v.funcReg

	fr.register(&functionDef{
		name:       "double",
		parameters: []paramDef{{name: "x", typeID: typeInt}},
		returnType: typeInt,
		body:       &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(args[0].decodeInt() * 2) }},
	})

	result := fr.call(v, "double", []value{encodeInt(21)})
	if result.decodeInt() != 42 {
		t.Errorf("double(21) = %d, want 42", result.decodeInt())
	}
}

func TestFunctionRegistryCallMissingReturnsZero(t *testing.T) {
	v := newVM(4096, 256)
	result := v.funcReg.call(v, "nonexistent", nil)
	if result.decodeInt() != 0 {
		t.Errorf("calling missing function should return 0, got %d", result.decodeInt())
	}
}

func TestFunctionRegistryCallWrongArityReturnsZero(t *testing.T) {
	v := newVM(4096, 256)
	v.funcReg.register(&functionDef{
		name:       "needs2",
		parameters: []paramDef{{name: "a", typeID: typeInt}, {name: "b", typeID: typeInt}},
		returnType: typeInt,
		body:       &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(args[0].decodeInt() + args[1].decodeInt()) }},
	})

	// Too few arguments.
	result := v.funcReg.call(v, "needs2", []value{encodeInt(1)})
	if result.decodeInt() != 0 {
		t.Errorf("wrong arity should return 0, got %d", result.decodeInt())
	}

	// Too many arguments.
	result = v.funcReg.call(v, "needs2", []value{encodeInt(1), encodeInt(2), encodeInt(3)})
	if result.decodeInt() != 0 {
		t.Errorf("wrong arity should return 0, got %d", result.decodeInt())
	}
}

func TestFunctionRegistryRegisterNative(t *testing.T) {
	fr := newFunctionRegistry()
	fr.registerNative("square", []paramDef{{name: "x", typeID: typeInt}}, typeInt,
		func(v *vm, args []value) value { return encodeInt(args[0].decodeInt() * args[0].decodeInt()) })

	if !fr.has("square") {
		t.Fatal("registerNative should register the function")
	}

	v := newVM(4096, 256)
	result := fr.call(v, "square", []value{encodeInt(7)})
	if result.decodeInt() != 49 {
		t.Errorf("square(7) = %d, want 49", result.decodeInt())
	}
}

func TestFunctionRegistryOverwrite(t *testing.T) {
	fr := newFunctionRegistry()
	fr.register(&functionDef{
		name:       "fn",
		returnType: typeInt,
		body:       &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(1) }},
	})
	fr.register(&functionDef{
		name:       "fn",
		returnType: typeInt,
		body:       &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(2) }},
	})

	v := newVM(4096, 256)
	result := fr.call(v, "fn", nil)
	if result.decodeInt() != 2 {
		t.Errorf("overwritten fn() = %d, want 2", result.decodeInt())
	}
}

func TestBytecodeFunctionBody(t *testing.T) {
	body := &bytecodeFunctionBody{
		executeFn: func(v *vm, args []value) value {
			return encodeInt(999)
		},
	}
	v := newVM(4096, 256)
	result := body.execute(v, nil)
	if result.decodeInt() != 999 {
		t.Errorf("bytecode body result = %d, want 999", result.decodeInt())
	}
}

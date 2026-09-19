package script

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

func TestRuntimeBindInterfaceObjectSealedAfterLoad(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, &testHostGreeter{}); err == nil {
		t.Fatal("expected BindInterfaceObject to fail after LoadSource")
	}
}

func TestRuntimeBindInterfaceObjectRejectsNilTarget(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "greet"}},
	}, nil); err == nil {
		t.Fatal("expected BindInterfaceObject to reject nil target")
	}
}

func TestRuntimeBindInterfaceObjectRejectsEmptyNamespace(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "greet"}},
	}, &testHostGreeter{}); err == nil {
		t.Fatal("expected BindInterfaceObject to reject empty namespace")
	}
}

type testMultiMethod struct {
	Value string
}

func (m *testMultiMethod) Label() string     { return m.Value }
func (m *testMultiMethod) Append(s string) string { return m.Value + s }

func TestRuntimeBindInterfaceMultiMethodProxy(t *testing.T) {
	m := &testMultiMethod{Value: "multi-"}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("tool", "Multi", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{
			{
				Name:    "label",
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			{
				Name:       "append",
				Parameters: []schema.ParameterDesc{{Name: "s", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
				Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		},
	}, m); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Multi from "tool"
export fun get_label(): string { return Multi.label() }
export fun get_appended(): string { return Multi.append("-x") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r1, err := rt.Call("get_label")
	if err != nil {
		t.Fatalf("get_label: %v", err)
	}
	var s1 string
	if err := r1.DecodeInto(&s1); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s1 != "multi-" {
		t.Fatalf("get_label got %q, want %q", s1, "multi-")
	}
	r2, err := rt.Call("get_appended")
	if err != nil {
		t.Fatalf("get_appended: %v", err)
	}
	var s2 string
	if err := r2.DecodeInto(&s2); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s2 != "multi--x" {
		t.Fatalf("get_appended got %q, want %q", s2, "multi--x")
	}
}

type testCalc struct{}

func (c *testCalc) Add(a, b int) int { return a + b }

func TestRuntimeBindInterfaceMultiArgMethod(t *testing.T) {
	c := &testCalc{}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("math", "Calc", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name: "add",
			Parameters: []schema.ParameterDesc{
				{Name: "a", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
				{Name: "b", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		}},
	}, c); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Calc from "math"
export fun sum(): int { return Calc.add(3, 4) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("sum")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var v int
	if err := result.DecodeInto(&v); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestRuntimeBindInterfaceMultipleProxies(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	a := &testHostGreeter{Prefix: "A-"}
	b := &testHostGreeter{Prefix: "B-"}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, a); err != nil {
		t.Fatalf("BindInterfaceObject A: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Other", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, b); err != nil {
		t.Fatalf("BindInterfaceObject B: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
import Other from "host"
export fun a(): string { return Greeter.greet("x") }
export fun b(): string { return Other.greet("x") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	ra, err := rt.Call("a")
	if err != nil {
		t.Fatalf("Call a: %v", err)
	}
	var sa string
	if err := ra.DecodeInto(&sa); err != nil {
		t.Fatalf("DecodeInto a: %v", err)
	}
	if sa != "A-x" {
		t.Fatalf("a got %q, want %q", sa, "A-x")
	}
	rb, err := rt.Call("b")
	if err != nil {
		t.Fatalf("Call b: %v", err)
	}
	var sb string
	if err := rb.DecodeInto(&sb); err != nil {
		t.Fatalf("DecodeInto b: %v", err)
	}
	if sb != "B-x" {
		t.Fatalf("b got %q, want %q", sb, "B-x")
	}
	ha, ok := rt.HostInterfaceHandleForValue(a)
	if !ok {
		t.Fatal("HostInterfaceHandleForValue(a) should succeed")
	}
	hb, ok := rt.HostInterfaceHandleForValue(b)
	if !ok {
		t.Fatal("HostInterfaceHandleForValue(b) should succeed")
	}
	if ha == hb {
		t.Fatal("two different proxy objects should have different handles")
	}
}

func TestRuntimeBindInterfaceClonePreservesProxy(t *testing.T) {
	greeter := &testHostGreeter{Prefix: "clone-"}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := cloned.LoadSource("clone", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("cloned LoadSource: %v", err)
	}
	result, err := cloned.Call("speak", "world")
	if err != nil {
		t.Fatalf("cloned Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "clone-world" {
		t.Fatalf("got %q, want %q", s, "clone-world")
	}
}

func TestRuntimeBindInterfaceResetPreservesProxy(t *testing.T) {
	greeter := &testHostGreeter{Prefix: "reset-"}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("v1", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("LoadSource v1: %v", err)
	}
	r1, err := rt.Call("speak", "first")
	if err != nil {
		t.Fatalf("Call v1: %v", err)
	}
	var s1 string
	if err := r1.DecodeInto(&s1); err != nil {
		t.Fatalf("DecodeInto v1: %v", err)
	}
	if s1 != "reset-first" {
		t.Fatalf("v1 got %q, want %q", s1, "reset-first")
	}
	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := rt.LoadSource("v2", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("LoadSource v2: %v", err)
	}
	r2, err := rt.Call("speak", "second")
	if err != nil {
		t.Fatalf("Call v2: %v", err)
	}
	var s2 string
	if err := r2.DecodeInto(&s2); err != nil {
		t.Fatalf("DecodeInto v2: %v", err)
	}
	if s2 != "reset-second" {
		t.Fatalf("v2 got %q, want %q", s2, "reset-second")
	}
}

func TestRuntimeBoundInterfacesIncludesObjectBindings(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceDesc("ns", "DescOnly", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "m"}},
	}); err != nil {
		t.Fatalf("BindInterfaceDesc: %v", err)
	}
	if err := rt.BindInterfaceObject("ns", "WithObject", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "m"}},
	}, &testHostGreeter{Prefix: ""}); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	ifaces := rt.BoundInterfaces()
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 bound interfaces, got %d", len(ifaces))
	}
	names := map[string]bool{}
	for _, i := range ifaces {
		names[i.Name] = true
	}
	if !names["DescOnly"] || !names["WithObject"] {
		t.Fatalf("expected DescOnly and WithObject, got %v", names)
	}
}

func TestRuntimeBindInterfaceIsCheck(t *testing.T) {
	greeter := &testHostGreeter{Prefix: "is-"}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun check_is(): bool { return Greeter is Greeter }
	export fun check_greet(): string { return Greeter.greet("x") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r, err := rt.Call("check_is")
	if err != nil {
		t.Fatalf("check_is: %v", err)
	}
	var b bool
	if err := r.DecodeInto(&b); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if !b {
		t.Fatal("expected Greeter is Greeter to be true")
	}
}

func TestRuntimeBindInterfaceIsCheckNegative(t *testing.T) {
	greeter := &testHostGreeter{Prefix: ""}
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "greet"}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun check(): bool { return Greeter is NonExistent }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r, err := rt.Call("check")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var b bool
	if err := r.DecodeInto(&b); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if b {
		t.Fatal("expected Greeter is NonExistent to be false")
	}
}

func TestRuntimeHostInterfaceHandleForUnknownValue(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	handle, ok := rt.HostInterfaceHandleForValue("not_bound")
	if ok || handle != vm.InvalidHandle {
		t.Fatalf("expected (InvalidHandle, false) for unknown value, got (%v, %v)", handle, ok)
	}
	target, ok := rt.HostInterfaceObjectForHandle(vm.Handle(999))
	if ok || target != nil {
		t.Fatalf("expected (nil, false) for unknown handle, got (%v, %v)", target, ok)
	}
}

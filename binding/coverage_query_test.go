package binding_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

func buildQueryCapability(t *testing.T) binding.RegisteredCapability {
	t.Helper()
	b := binding.NewCapability("query", "module")
	if err := b.AddFreeFunction("echo", func(s string) string { return s }); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	if err := b.AddObject("Widget", schema.ObjectDesc{
		Kind:   schema.TypeKindStruct,
		Fields: []schema.FieldDesc{{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}); err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	if err := b.AddInterface("Describer", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{Name: "describe", Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}}}},
	}); err != nil {
		t.Fatalf("AddInterface: %v", err)
	}
	if err := b.AddTypeAlias("ID", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("AddTypeAlias: %v", err)
	}
	if err := b.AddValue("version", "1.2.3"); err != nil {
		t.Fatalf("AddValue: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return cap
}

func newBindingWithQueryCapability(t *testing.T) *binding.ScriptBinding {
	t.Helper()
	sb := binding.NewScriptBinding()
	if err := sb.RegisterCapability(buildQueryCapability(t)); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	return sb
}

func TestScriptBindingDescribeCapability(t *testing.T) {
	sb := newBindingWithQueryCapability(t)
	desc, ok := sb.DescribeCapability("query")
	if !ok || desc.Name != "query" || desc.Kind != "module" {
		t.Fatalf("unexpected describe result: ok=%v desc=%+v", ok, desc)
	}
	if len(desc.Objects) != 1 || len(desc.Interfaces) != 1 || len(desc.TypeAliases) != 1 || len(desc.Values) != 1 {
		t.Fatalf("expected object/interface/alias/value exported, got %+v", desc)
	}
	if _, ok := sb.DescribeCapability("missing"); ok {
		t.Fatal("expected miss for unregistered capability")
	}
	all := sb.DescribeCapabilities()
	if len(all) != 1 || all[0].Name != "query" {
		t.Fatalf("expected one capability, got %+v", all)
	}
}

func TestScriptBindingNilRegistryFallbacks(t *testing.T) {
	sb := &binding.ScriptBinding{}
	if _, ok := sb.DescribeCapability("query"); ok {
		t.Fatal("expected miss with nil registry")
	}
	if sb.DescribeCapabilities() != nil {
		t.Fatal("expected nil with nil registry")
	}
	if _, ok := sb.FindCapabilityObject("query", "Widget"); ok {
		t.Fatal("expected miss with nil registry")
	}
	if _, ok := sb.FindCapabilityInterface("query", "Describer"); ok {
		t.Fatal("expected miss with nil registry")
	}
	if _, ok := sb.FindCapabilityTypeAlias("query", "ID"); ok {
		t.Fatal("expected miss with nil registry")
	}
	if _, _, ok := sb.FindCapabilityValue("query", "version"); ok {
		t.Fatal("expected miss with nil registry")
	}
	if _, err := sb.InvokeCapability(context.Background(), "query", "echo", nil); err == nil {
		t.Fatal("expected error with nil registry")
	}
	if err := sb.RegisterCapability(buildQueryCapability(t)); err != nil {
		t.Fatalf("RegisterCapability should lazily create registry: %v", err)
	}
	if _, ok := sb.DescribeCapability("query"); !ok {
		t.Fatal("expected capability after lazy registration")
	}
}

func TestScriptBindingFindCapabilityMembers(t *testing.T) {
	sb := newBindingWithQueryCapability(t)

	obj, ok := sb.FindCapabilityObject("query", "Widget")
	if !ok || obj.Name != "Widget" || len(obj.Fields) != 1 || obj.Fields[0].Name != "id" {
		t.Fatalf("unexpected object descriptor: ok=%v %+v", ok, obj)
	}
	if _, ok := sb.FindCapabilityObject("query", "Missing"); ok {
		t.Fatal("expected object miss")
	}
	if _, ok := sb.FindCapabilityObject("missing", "Widget"); ok {
		t.Fatal("expected object miss for unknown capability")
	}

	iface, ok := sb.FindCapabilityInterface("query", "Describer")
	if !ok || len(iface.Methods) != 1 || iface.Methods[0].Name != "describe" {
		t.Fatalf("unexpected interface descriptor: ok=%v %+v", ok, iface)
	}
	if _, ok := sb.FindCapabilityInterface("query", "Missing"); ok {
		t.Fatal("expected interface miss")
	}
	if _, ok := sb.FindCapabilityInterface("missing", "Describer"); ok {
		t.Fatal("expected interface miss for unknown capability")
	}

	alias, ok := sb.FindCapabilityTypeAlias("query", "ID")
	if !ok || alias.Name != "string" {
		t.Fatalf("unexpected alias descriptor: ok=%v %+v", ok, alias)
	}
	if _, ok := sb.FindCapabilityTypeAlias("query", "Missing"); ok {
		t.Fatal("expected alias miss")
	}
	if _, ok := sb.FindCapabilityTypeAlias("missing", "ID"); ok {
		t.Fatal("expected alias miss for unknown capability")
	}

	valueDesc, value, ok := sb.FindCapabilityValue("query", "version")
	if !ok || value != "1.2.3" || valueDesc.Name != "version" || valueDesc.Type.Name != "string" {
		t.Fatalf("unexpected value lookup: ok=%v desc=%+v value=%v", ok, valueDesc, value)
	}
	if _, _, ok := sb.FindCapabilityValue("query", "missing"); ok {
		t.Fatal("expected value miss")
	}
	if _, _, ok := sb.FindCapabilityValue("missing", "version"); ok {
		t.Fatal("expected value miss for unknown capability")
	}
}

func TestScriptBindingInvokeCapabilityErrors(t *testing.T) {
	sb := newBindingWithQueryCapability(t)
	if _, err := sb.InvokeCapability(context.Background(), "missing", "echo", nil); err == nil {
		t.Fatal("expected error for unknown capability")
	}
	if _, err := sb.InvokeCapability(context.Background(), "query", "missing", nil); err == nil {
		t.Fatal("expected error for unknown callable")
	}
	out, err := sb.InvokeCapability(context.Background(), "query", "echo", "hi")
	if err != nil || out != "hi" {
		t.Fatalf("expected echo hi, got %v err=%v", out, err)
	}
}

func TestRegistryRegistrationValidation(t *testing.T) {
	valid := buildQueryCapability(t)
	echoDesc := valid.Desc.Callables[0]

	cases := []struct {
		name string
		cap  binding.RegisteredCapability
		want string
	}{
		{"empty capability name", binding.RegisteredCapability{}, "capability name cannot be empty"},
		{"no members", binding.RegisteredCapability{Desc: binding.CapabilityDesc{Name: "x"}}, "must define at least one"},
		{"descriptor count mismatch", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Callables = append([]schema.CallableDesc(nil), valid.Desc.Callables...)
			c.Desc.Callables[0].Name = "renamed"
			return c
		}(), "descriptor/callable count mismatch"},
		{"empty callable desc name", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Callables = append([]schema.CallableDesc(nil), valid.Desc.Callables...)
			c.Desc.Callables[0].Name = ""
			return c
		}(), "callable with empty name"},
		{"duplicate declared callable", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Callables = append(c.Desc.Callables, c.Desc.Callables[0])
			return c
		}(), "already declared"},
		{"empty runtime callable name", func() binding.RegisteredCapability {
			c := valid
			c.Callables[""] = c.Callables["echo"]
			return c
		}(), "callable with empty runtime name"},
		{"runtime callable missing descriptor", func() binding.RegisteredCapability {
			c := valid
			c.Callables["extra"] = c.Callables["echo"]
			return c
		}(), "missing descriptor"},
		{"value with empty name", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Values = append(c.Desc.Values, binding.CapabilityValueDesc{Name: ""})
			return c
		}(), "value with empty name"},
		{"duplicate value declaration", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Values = append(c.Desc.Values, valid.Desc.Values[0])
			return c
		}(), "value .* already declared"},
		{"value missing runtime value", func() binding.RegisteredCapability {
			c := valid
			c.Desc.Values = append(c.Desc.Values, binding.CapabilityValueDesc{Name: "extra"})
			return c
		}(), "missing runtime value"},
		{"value missing descriptor", func() binding.RegisteredCapability {
			c := valid
			c.Values["extra"] = 42
			return c
		}(), "missing descriptor"},
	}
	for _, tc := range cases {
		reg := binding.NewMemoryCapabilityRegistry()
		err := reg.Register(tc.cap)
		if err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
	_ = echoDesc
}

func TestCapabilityBuilderAddInterfaceCloneAddValue(t *testing.T) {
	b := binding.NewCapability("probe", "module")
	if err := b.AddInterface("", schema.InterfaceDesc{}); err == nil {
		t.Fatal("expected empty interface name error")
	}
	if err := b.AddInterface("Describer", schema.InterfaceDesc{}); err != nil {
		t.Fatalf("AddInterface: %v", err)
	}
	if err := b.AddInterface("Describer", schema.InterfaceDesc{}); err == nil {
		t.Fatal("expected duplicate interface error")
	}
	if err := b.AddValue("", 1); err == nil {
		t.Fatal("expected empty value name error")
	}
	if err := b.AddValue("nilValue", nil); err == nil {
		t.Fatal("expected nil value error")
	}
	if err := b.AddValue("answer", 42); err != nil {
		t.Fatalf("AddValue: %v", err)
	}
	if err := b.AddValue("answer", 43); err == nil {
		t.Fatal("expected duplicate value error")
	}

	clone := b.Clone()
	if err := clone.AddInterface("Describer", schema.InterfaceDesc{}); err == nil {
		t.Fatal("expected clone duplicate interface error (clone shares interface list)")
	}
	if err := b.AddInterface("Describer", schema.InterfaceDesc{}); err == nil {
		t.Fatal("expected original still intact after clone")
	}

	var nilBuilder *binding.CapabilityBuilder
	if nilBuilder.Clone() != nil {
		t.Fatal("expected nil clone of nil builder")
	}
}

func TestCheckExecutionBudgets(t *testing.T) {
	ctx := context.Background()
	if err := binding.CheckExecution(ctx, binding.ExecutionBudget{}, nil); err != nil {
		t.Fatalf("no-budget check failed: %v", err)
	}
	if err := binding.CheckExecution(ctx, binding.ExecutionBudget{MaxInstructions: 10}, &binding.ExecutionState{Instructions: 5}); err != nil {
		t.Fatalf("under budget should pass: %v", err)
	}
	if err := binding.CheckExecution(ctx, binding.ExecutionBudget{MaxInstructions: 10}, &binding.ExecutionState{Instructions: 10}); err == nil {
		t.Fatal("expected instruction budget exceeded")
	}
	if err := binding.CheckExecution(ctx, binding.ExecutionBudget{MaxHostCalls: 3}, &binding.ExecutionState{HostCalls: 4}); err == nil {
		t.Fatal("expected host call budget exceeded")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := binding.CheckExecution(cancelled, binding.ExecutionBudget{}, nil); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestValidateInvocationStageMatrix(t *testing.T) {
	unary, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}
	stream, err := schema.NewStreamingCallableDesc("stream", nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, false)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}

	if err := binding.ValidateInvocationStage(unary, binding.InvocationStageUnary); err != nil {
		t.Fatalf("unary/unary should pass: %v", err)
	}
	if err := binding.ValidateInvocationStage(unary, binding.InvocationStageNext); err == nil {
		t.Fatal("expected unary to reject next stage")
	}
	if err := binding.ValidateInvocationStage(stream, binding.InvocationStageNext); err != nil {
		t.Fatalf("streaming/next should pass: %v", err)
	}
	if err := binding.ValidateInvocationStage(stream, binding.InvocationStageFinal); err != nil {
		t.Fatalf("streaming/final should pass: %v", err)
	}
	if err := binding.ValidateInvocationStage(stream, binding.InvocationStageUnary); err == nil {
		t.Fatal("expected streaming to reject unary stage")
	}

	noNext := stream
	noNext.Streaming.Next = nil
	if err := binding.ValidateInvocationStage(noNext, binding.InvocationStageNext); err == nil {
		t.Fatal("expected missing next schema error")
	}
	noFinal := stream
	noFinal.Streaming.Final = nil
	if err := binding.ValidateInvocationStage(noFinal, binding.InvocationStageFinal); err == nil {
		t.Fatal("expected missing final schema error")
	}

	badMode := stream
	badMode.Mode = schema.CallableMode("weird")
	if err := binding.ValidateInvocationStage(badMode, binding.InvocationStageNext); err == nil {
		t.Fatal("expected unsupported mode error")
	}
}

func TestInvokeGoFunctionForHostProxy(t *testing.T) {
	fn := reflect.ValueOf(func(a int, b string) (string, error) {
		if b == "boom" {
			return "", context.Canceled
		}
		return b, nil
	})
	out, err := binding.InvokeGoFunctionForHostProxy(fn, []any{7, "ok"})
	if err != nil || len(out) != 1 || out[0] != "ok" {
		t.Fatalf("expected [ok], got %v err=%v", out, err)
	}
	if _, err := binding.InvokeGoFunctionForHostProxy(fn, []any{7, "boom"}); err == nil {
		t.Fatal("expected function error propagation")
	}
	if _, err := binding.InvokeGoFunctionForHostProxy(fn, []any{"not-an-int", "ok"}); err == nil {
		t.Fatal("expected unsupported arg error")
	}
	voidFn := reflect.ValueOf(func() {})
	out, err = binding.InvokeGoFunctionForHostProxy(voidFn, nil)
	if err != nil || out != nil {
		t.Fatalf("expected nil results for void function, got %v err=%v", out, err)
	}
	panicFn := reflect.ValueOf(func() (int, error) { panic("kaboom") })
	if _, err := binding.InvokeGoFunctionForHostProxy(panicFn, nil); err == nil {
		t.Fatal("expected panic to surface as error")
	}
}

func TestExecutableRegistryForEachAdapter(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("greet", greet); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	seen := map[string]bool{}
	sb.Executors.ForEachAdapter(func(name string, _ binding.ExecutableAdapter) bool {
		seen[name] = true
		return true
	})
	if !seen["greet"] {
		t.Fatalf("expected greet adapter visited, got %v", seen)
	}
}

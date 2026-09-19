package binding_test

import (
	"context"
	"errors"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

type capabilityInput struct {
	Text string
}

type capabilityOutput struct {
	Text string
}

func capabilityEcho(in capabilityInput) (capabilityOutput, error) {
	return capabilityOutput{Text: in.Text}, nil
}

func capabilityEchoContext(ctx context.Context, in capabilityInput) (capabilityOutput, error) {
	prefix, _ := ctx.Value("prefix").(string)
	return capabilityOutput{Text: prefix + in.Text}, nil
}

func capabilityNoArgs() (capabilityOutput, error) {
	return capabilityOutput{}, nil
}

func capabilityBadInput(text string) (capabilityOutput, error) {
	return capabilityOutput{Text: text}, nil
}

func capabilityPointerInput(in *capabilityInput) (capabilityOutput, error) {
	return capabilityOutput{}, nil
}

func capabilityPointerOutput(in capabilityInput) (*capabilityOutput, error) {
	return &capabilityOutput{Text: in.Text}, nil
}

func capabilityNoError(in capabilityInput) capabilityOutput {
	return capabilityOutput{Text: in.Text}
}

func capabilityOnlyError(in capabilityInput) error {
	return nil
}

func capabilityMultipleBusinessReturns(in capabilityInput) (capabilityOutput, capabilityOutput, error) {
	return capabilityOutput{}, capabilityOutput{}, nil
}

func capabilityVariadic(in capabilityInput, rest ...string) (capabilityOutput, error) {
	return capabilityOutput{}, nil
}

func capabilityAnonymousInput(in struct{ Text string }) (capabilityOutput, error) {
	return capabilityOutput{Text: in.Text}, nil
}

func TestCapabilityBuilderBuildAndInvoke(t *testing.T) {
	builder := binding.NewCapability("tool", "service").WithMetadata("owner", "test")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	if err := builder.AddFunction("execute_ctx", capabilityEchoContext); err != nil {
		t.Fatalf("AddFunction execute_ctx: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if cap.Desc.Name != "tool" {
		t.Fatalf("expected capability name tool, got %s", cap.Desc.Name)
	}
	if cap.Desc.Metadata["owner"] != "test" {
		t.Fatalf("expected metadata owner=test, got %#v", cap.Desc.Metadata)
	}
	if len(cap.Desc.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(cap.Desc.Callables))
	}
	callable, ok := cap.Callables["execute"]
	if !ok {
		t.Fatal("expected execute callable")
	}
	result, err := callable.Invoke(context.Background(), capabilityInput{Text: "hello"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out, ok := result.(capabilityOutput)
	if !ok {
		t.Fatalf("expected capabilityOutput, got %T", result)
	}
	if out.Text != "hello" {
		t.Fatalf("expected hello, got %q", out.Text)
	}
	ctxCallable := cap.Callables["execute_ctx"]
	ctxResult, err := ctxCallable.Invoke(context.WithValue(context.Background(), "prefix", "ctx:"), map[string]any{"Text": "hello"})
	if err != nil {
		t.Fatalf("Invoke execute_ctx: %v", err)
	}
	ctxOut := ctxResult.(capabilityOutput)
	if ctxOut.Text != "ctx:hello" {
		t.Fatalf("expected ctx:hello, got %q", ctxOut.Text)
	}
}

func TestCapabilityBuilderAddFunctionRejectsInvalidShapes(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	cases := []struct {
		name string
		fn   any
	}{
		{name: "no_args", fn: capabilityNoArgs},
		{name: "bad_input", fn: capabilityBadInput},
		{name: "pointer_input", fn: capabilityPointerInput},
		{name: "pointer_output", fn: capabilityPointerOutput},
		{name: "no_error", fn: capabilityNoError},
		{name: "only_error", fn: capabilityOnlyError},
		{name: "multi_return", fn: capabilityMultipleBusinessReturns},
		{name: "variadic", fn: capabilityVariadic},
		{name: "anonymous", fn: capabilityAnonymousInput},
	}
	for _, tc := range cases {
		if err := builder.AddFunction(tc.name, tc.fn); err == nil {
			t.Fatalf("expected AddFunction to reject %s", tc.name)
		}
	}
}

func TestCapabilityBuilderRejectsDuplicateCallable(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("first AddFunction: %v", err)
	}
	if err := builder.AddFunction("execute", capabilityEcho); err == nil {
		t.Fatal("expected duplicate callable error")
	}
}

func TestCapabilityBuilderRejectsEmptyCapability(t *testing.T) {
	if _, err := binding.NewCapability("", "service").Build(); err == nil {
		t.Fatal("expected empty capability name error")
	}
	if _, err := binding.NewCapability("tool", "service").Build(); err == nil {
		t.Fatal("expected empty callable set error")
	}
}

func TestCapabilityCallableInvokeReturnsHostError(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	errBoom := errors.New("boom")
	if err := builder.AddFunction("execute", func(in capabilityInput) (capabilityOutput, error) {
		return capabilityOutput{}, errBoom
	}); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = cap.Callables["execute"].Invoke(context.Background(), capabilityInput{Text: "x"})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected host error boom, got %v", err)
	}
}

func TestCapabilityBuilderAddFunctionRejectsNil(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", nil); err == nil {
		t.Fatal("expected nil function rejection")
	}
}

func TestCapabilityCallableInvokeHandlesNilContext(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEchoContext); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := cap.Callables["execute"].Invoke(nil, capabilityInput{Text: "hello"})
	if err != nil {
		t.Fatalf("Invoke with nil context: %v", err)
	}
	out := result.(capabilityOutput)
	if out.Text != "hello" {
		t.Fatalf("expected hello, got %q", out.Text)
	}
}

type taggedInput struct {
	DisplayName string `json:"display_name"`
	InternalID  int    `json:"-"`
	Omitted     string `json:",omitempty"`
	NoTag       string
}

type taggedOutput struct {
	Message string `json:"msg"`
	Hidden  string `json:"-"`
}

func capabilityEchoTagged(in taggedInput) (taggedOutput, error) {
	return taggedOutput{Message: in.DisplayName + "/" + in.NoTag}, nil
}

func TestCapabilityBuilderJsonTagRoundTrip(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEchoTagged); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := cap.Callables["execute"].Invoke(context.Background(), map[string]any{
		"display_name": "hello",
		"NoTag":        "world",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out := result.(taggedOutput)
	if out.Message != "hello/world" {
		t.Fatalf("expected hello/world, got %q", out.Message)
	}
}

func TestCapabilityBuilderJsonTagRejectsUnknownKey(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEchoTagged); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = cap.Callables["execute"].Invoke(context.Background(), map[string]any{
		"DisplayName": "hello", // wrong key — should be display_name
	})
	if err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestCapabilityBuilder_AddObjectAndTypeAlias(t *testing.T) {
	b := binding.NewCapability("geom", "types")
	if err := b.AddObject("Point", schema.ObjectDesc{Kind: schema.TypeKindStruct, Fields: []schema.FieldDesc{
		{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
	}}); err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	if err := b.AddTypeAlias("UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("AddTypeAlias: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cap.Desc.Objects) != 1 || cap.Desc.Objects[0].Name != "Point" {
		t.Fatalf("expected 1 object Point, got %+v", cap.Desc.Objects)
	}
	if len(cap.Desc.TypeAliases) != 1 {
		t.Fatalf("expected 1 type alias, got %+v", cap.Desc.TypeAliases)
	}
	if td, ok := cap.Desc.TypeAliases["UserId"]; !ok || td.Name != "string" {
		t.Fatalf("expected UserId type alias, got %+v", cap.Desc.TypeAliases)
	}
	if len(cap.Callables) != 0 {
		t.Fatalf("expected no callables, got %d", len(cap.Callables))
	}
}

func TestCapabilityBuilder_TypeOnlyCapabilityCanRegister(t *testing.T) {
	b := binding.NewCapability("types", "types")
	if err := b.AddTypeAlias("Coord", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}); err != nil {
		t.Fatalf("AddTypeAlias: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cap.Desc.TypeAliases) != 1 {
		t.Fatalf("expected 1 type alias, got %+v", cap.Desc.TypeAliases)
	}
}

func TestCapabilityBuilder_AddValueRejectsNil(t *testing.T) {
	b := binding.NewCapability("config", "module")
	if err := b.AddValue("answer", nil); err == nil {
		t.Fatal("expected nil value error")
	}
}

func TestCapabilityBuilder_AddObjectRejectsDuplicate(t *testing.T) {
	b := binding.NewCapability("geom", "types")
	if err := b.AddObject("Point", schema.ObjectDesc{Kind: schema.TypeKindStruct}); err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	if err := b.AddObject("Point", schema.ObjectDesc{Kind: schema.TypeKindStruct}); err == nil {
		t.Fatal("expected duplicate object error")
	}
}

func TestCapabilityBuilder_AddTypeAliasRejectsDuplicate(t *testing.T) {
	b := binding.NewCapability("types", "types")
	if err := b.AddTypeAlias("UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("AddTypeAlias: %v", err)
	}
	if err := b.AddTypeAlias("UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}); err == nil {
		t.Fatal("expected duplicate type alias error")
	}
}

func TestCapabilityBuilder_AddFreeFunction_SingleParam(t *testing.T) {
	b := binding.NewCapability("math", "module")
	if err := b.AddFreeFunction("abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	callable, ok := cap.Callables["abs"]
	if !ok {
		t.Fatal("expected abs callable")
	}
	desc := callable.Desc()
	if len(desc.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Type.Name != "double" {
		t.Fatalf("expected double parameter, got %s", desc.Parameters[0].Type.Name)
	}
	result, err := callable.Invoke(context.Background(), -3.14)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result != 3.14 {
		t.Fatalf("expected 3.14, got %v", result)
	}
}

func TestCapabilityBuilder_AddFreeFunction_MultiParam(t *testing.T) {
	b := binding.NewCapability("math", "module")
	if err := b.AddFreeFunction("max", func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	callable := cap.Callables["max"]
	desc := callable.Desc()
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(desc.Parameters))
	}
	result, err := callable.Invoke(context.Background(), []any{3, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result != 5 {
		t.Fatalf("expected 5, got %v", result)
	}
}

func TestCapabilityBuilder_AddFreeFunction_WithError(t *testing.T) {
	b := binding.NewCapability("math", "module")
	if err := b.AddFreeFunction("div", func(a, b float64) (float64, error) {
		if b == 0 {
			return 0, errors.New("division by zero")
		}
		return a / b, nil
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	callable := cap.Callables["div"]
	_, err = callable.Invoke(context.Background(), []any{1.0, 0.0})
	if err == nil {
		t.Fatal("expected division by zero error")
	}
	result, err := callable.Invoke(context.Background(), []any{10.0, 2.0})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result != 5.0 {
		t.Fatalf("expected 5.0, got %v", result)
	}
}

func TestCapabilityBuilder_AddFreeFunction_RejectsVariadic(t *testing.T) {
	b := binding.NewCapability("math", "module")
	if err := b.AddFreeFunction("sum", func(nums ...int) int {
		s := 0
		for _, n := range nums {
			s += n
		}
		return s
	}); err == nil {
		t.Fatal("expected variadic rejection")
	}
}

func TestCapabilityBuilder_AddFreeFunction_RejectsDuplicate(t *testing.T) {
	b := binding.NewCapability("math", "module")
	if err := b.AddFreeFunction("abs", func(x float64) float64 { return x }); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	if err := b.AddFreeFunction("abs", func(x float64) float64 { return x }); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestCapabilityBuilder_AddFreeFunction_ZeroParams(t *testing.T) {
	b := binding.NewCapability("env", "module")
	if err := b.AddFreeFunction("now", func() int { return 42 }); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := cap.Callables["now"].Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestCapabilityBuilder_AddFreeFunction_SingleParamSlice(t *testing.T) {
	b := binding.NewCapability("util", "module")
	if err := b.AddFreeFunction("len", func(items []string) int { return len(items) }); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := cap.Callables["len"].Invoke(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result != 3 {
		t.Fatalf("expected 3, got %v", result)
	}
}

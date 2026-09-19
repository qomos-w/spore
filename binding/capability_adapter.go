package binding

import (
	"context"
	"fmt"
	"reflect"

	"github.com/qomos-w/spore/schema"
)

type capabilityFunctionCallable struct {
	desc       schema.CallableDesc
	fn         reflect.Value
	hasContext bool
	inputType  reflect.Type
	outputType reflect.Type
}

func (c *capabilityFunctionCallable) Desc() schema.CallableDesc {
	return schema.CloneCallableDesc(c.desc)
}

func (c *capabilityFunctionCallable) Invoke(ctx context.Context, input any) (any, error) {
	bound, err := bindCapabilityInput(c.inputType, input)
	if err != nil {
		return nil, err
	}
	args := make([]reflect.Value, 0, 2)
	if c.hasContext {
		if ctx == nil {
			ctx = context.Background()
		}
		args = append(args, reflect.ValueOf(ctx))
	}
	args = append(args, bound)
	results := c.fn.Call(args)
	if errValue := results[1]; !errValue.IsNil() {
		return nil, errValue.Interface().(error)
	}
	return results[0].Interface(), nil
}

func wrapCapabilityFunction(name string, fn any) (CapabilityCallable, error) {
	if name == "" {
		return nil, fmt.Errorf("callable name cannot be empty")
	}
	value := reflect.ValueOf(fn)
	if !value.IsValid() {
		return nil, fmt.Errorf("callable %q requires function", name)
	}
	typ := value.Type()
	if typ.Kind() != reflect.Func {
		return nil, fmt.Errorf("callable %q requires function, got %s", name, typ.Kind())
	}
	if typ.IsVariadic() {
		return nil, fmt.Errorf("callable %q does not support variadic functions", name)
	}

	shape, err := describeCapabilityFunctionShape(name, typ)
	if err != nil {
		return nil, err
	}

	inDesc, err := schema.DescribeReflectType(shape.inputType)
	if err != nil {
		return nil, err
	}
	outDesc, err := schema.DescribeReflectType(shape.outputType)
	if err != nil {
		return nil, err
	}
	desc := schema.CallableDesc{
		Name: name,
		Parameters: []schema.ParameterDesc{{
			Name: "input",
			Type: inDesc,
		}},
		Returns:  []schema.TypeDesc{outDesc},
		HasError: true,
		Mode:     schema.CallableModeUnary,
	}
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return nil, err
	}

	return &capabilityFunctionCallable{
		desc:       desc,
		fn:         value,
		hasContext: shape.hasContext,
		inputType:  shape.inputType,
		outputType: shape.outputType,
	}, nil
}

type capabilityFunctionShape struct {
	hasContext bool
	inputType  reflect.Type
	outputType reflect.Type
}

func describeCapabilityFunctionShape(name string, typ reflect.Type) (capabilityFunctionShape, error) {
	if typ.NumOut() != 2 {
		return capabilityFunctionShape{}, fmt.Errorf("callable %q must return (Out, error)", name)
	}
	if typ.Out(1) != errorType {
		return capabilityFunctionShape{}, fmt.Errorf("callable %q second return must be error", name)
	}
	outputType := typ.Out(0)
	if err := validateCapabilityStructType(name, "output", outputType); err != nil {
		return capabilityFunctionShape{}, err
	}

	shape := capabilityFunctionShape{outputType: outputType}
	switch typ.NumIn() {
	case 1:
		shape.inputType = typ.In(0)
	case 2:
		if typ.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() {
			return capabilityFunctionShape{}, fmt.Errorf("callable %q first parameter must be context.Context", name)
		}
		shape.hasContext = true
		shape.inputType = typ.In(1)
	default:
		return capabilityFunctionShape{}, fmt.Errorf("callable %q must accept exactly one input struct and optional context.Context", name)
	}
	if err := validateCapabilityStructType(name, "input", shape.inputType); err != nil {
		return capabilityFunctionShape{}, err
	}
	return shape, nil
}

func validateCapabilityStructType(callableName, position string, typ reflect.Type) error {
	if typ.Kind() == reflect.Pointer {
		return fmt.Errorf("callable %q %s must not be pointer type", callableName, position)
	}
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("callable %q %s must be struct, got %s", callableName, position, typ.Kind())
	}
	if typ.Name() == "" {
		return fmt.Errorf("callable %q %s must be named struct", callableName, position)
	}
	return nil
}

// freeFunctionCallable implements CapabilityCallable for free-form Go functions
// with any number of parameters (e.g. math.Abs, math.Max, strings.Contains).
type freeFunctionCallable struct {
	desc       schema.CallableDesc
	fn         reflect.Value
	paramCount int
}

func (c *freeFunctionCallable) Desc() schema.CallableDesc {
	return schema.CloneCallableDesc(c.desc)
}

func (c *freeFunctionCallable) Invoke(ctx context.Context, input any) (any, error) {
	var args []any
	switch c.paramCount {
	case 0:
		args = []any{}
	case 1:
		args = []any{input}
	default:
		args = input.([]any)
	}
	results, err := invokeGoFunction(c.fn, args)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func wrapFreeFunction(name string, fn any) (CapabilityCallable, error) {
	if name == "" {
		return nil, fmt.Errorf("callable name cannot be empty")
	}
	value := reflect.ValueOf(fn)
	if !value.IsValid() {
		return nil, fmt.Errorf("callable %q requires function", name)
	}
	typ := value.Type()
	if typ.Kind() != reflect.Func {
		return nil, fmt.Errorf("callable %q requires function, got %s", name, typ.Kind())
	}
	if typ.IsVariadic() {
		return nil, fmt.Errorf("callable %q does not support variadic functions", name)
	}

	desc, err := schema.DescribeGoFunction(name, fn)
	if err != nil {
		return nil, err
	}
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return nil, err
	}

	return &freeFunctionCallable{
		desc:       desc,
		fn:         value,
		paramCount: typ.NumIn(),
	}, nil
}

func bindCapabilityInput(target reflect.Type, input any) (reflect.Value, error) {
	value := reflect.ValueOf(input)
	if !value.IsValid() {
		return reflect.Value{}, fmt.Errorf("invalid capability input")
	}
	bound, err := bindGoFunctionArg(target, value)
	if err != nil {
		return reflect.Value{}, err
	}
	if !bound.IsValid() {
		return reflect.Value{}, fmt.Errorf("invalid capability input")
	}
	if !bound.Type().AssignableTo(target) {
		return reflect.Value{}, fmt.Errorf("capability input type %s is not assignable to %s", bound.Type(), target)
	}
	return bound, nil
}

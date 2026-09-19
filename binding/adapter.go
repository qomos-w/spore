package binding

import (
	"fmt"
	"math"
	"reflect"
	"runtime/debug"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/invoke"
	"github.com/qomos-w/spore/schema"
)

// callableAdapter is the single ExecutableAdapter implementation in the
// binding plane.
//
// It unifies the three layers that previously bridged executables into the
// registry: the reflected-Go-function / unary / streaming adapter, the
// capability-callable wrapper, and the capability-to-executable bridge. A
// capability callable is now just another shape handled by the same adapter
// (capability != nil), so there is exactly one Invoke/validation path and one
// place where invocation outcomes are built.
type callableAdapter struct {
	desc       schema.CallableDesc
	unary      func(args []any) (any, error)
	next       func(args []any) (any, error)
	final      func(args []any) (any, error)
	capability CapabilityCallable // non-nil for capability-backed adapters
}

// NewUnaryInvocationAdapter creates an adapter for a unary callable.
func NewUnaryInvocationAdapter(desc schema.CallableDesc, fn func(args []any) (any, error)) (ExecutableAdapter, error) {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return nil, err
	}
	if normalizedCallableMode(desc) != schema.CallableModeUnary {
		return nil, fmt.Errorf("callable %q is not unary", desc.Name)
	}
	if fn == nil {
		return nil, fmt.Errorf("unary callable %q requires handler", desc.Name)
	}
	return &callableAdapter{desc: schema.CloneCallableDesc(desc), unary: fn}, nil
}

// NewGoFunctionAdapter creates an adapter from a Go function value.
// The function is described via reflection and bound for invocation.
func NewGoFunctionAdapter(name string, fn any) (ExecutableAdapter, error) {
	desc, err := schema.DescribeGoFunction(name, fn)
	if err != nil {
		return nil, err
	}
	value := reflect.ValueOf(fn)
	return NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		results, err := invokeGoFunction(value, args)
		if err != nil {
			return nil, err
		}
		switch len(results) {
		case 0:
			return nil, nil
		case 1:
			return results[0], nil
		default:
			return nil, fmt.Errorf("unsupported Go function result count: %d", len(results))
		}
	})
}

// NewStreamingInvocationAdapter creates an adapter for a streaming callable.
func NewStreamingInvocationAdapter(desc schema.CallableDesc, next func(args []any) (any, error), final func(args []any) (any, error)) (ExecutableAdapter, error) {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return nil, err
	}
	if normalizedCallableMode(desc) != schema.CallableModeStreaming {
		return nil, fmt.Errorf("callable %q is not streaming", desc.Name)
	}
	if desc.Streaming.Next != nil && next == nil {
		return nil, fmt.Errorf("streaming callable %q requires next handler", desc.Name)
	}
	if desc.Streaming.Final != nil && final == nil {
		return nil, fmt.Errorf("streaming callable %q requires final handler", desc.Name)
	}
	return &callableAdapter{desc: schema.CloneCallableDesc(desc), next: next, final: final}, nil
}

// NewCapabilityExecutableAdapter exposes a capability callable as a flattened
// binding callable. The returned adapter is the same callableAdapter type as
// every other adapter; the capability callable is simply its executable body.
func NewCapabilityExecutableAdapter(capabilityName string, callable CapabilityCallable) (ExecutableAdapter, error) {
	if capabilityName == "" {
		return nil, fmt.Errorf("capability name cannot be empty")
	}
	if callable == nil {
		return nil, fmt.Errorf("capability %q requires callable", capabilityName)
	}
	local := callable.Desc()
	if local.Name == "" {
		return nil, fmt.Errorf("capability %q callable name cannot be empty", capabilityName)
	}
	if normalizedCallableMode(local) != schema.CallableModeUnary {
		return nil, fmt.Errorf("capability %q callable %q must be unary", capabilityName, local.Name)
	}
	flattened := capabilityCallableDesc(capabilityName, local)
	return &callableAdapter{
		desc:       flattened,
		capability: callable,
	}, nil
}

func (a *callableAdapter) Callable() schema.CallableDesc {
	return schema.CloneCallableDesc(a.desc)
}

func (a *callableAdapter) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if req.Callable != a.desc.Name {
		return InvocationOutcome{}, fmt.Errorf("adapter for %q cannot handle %q", a.desc.Name, req.Callable)
	}
	if err := ValidateInvocationStage(a.desc, req.Stage); err != nil {
		return InvocationOutcome{}, err
	}
	if err := ValidateInvocationArgs(a.desc, req.Args); err != nil {
		return InvocationOutcome{}, err
	}
	if a.capability != nil {
		return a.invokeCapability(req)
	}
	return a.invokeHandler(req)
}

func (a *callableAdapter) invokeHandler(req InvocationRequest) (InvocationOutcome, error) {
	var (
		payload any
		callErr error
	)
	switch req.Stage {
	case InvocationStageUnary:
		if a.unary == nil {
			return InvocationOutcome{}, fmt.Errorf("callable %q has no unary handler", a.desc.Name)
		}
		payload, callErr = a.unary(req.Args)
	case InvocationStageNext:
		if a.next == nil {
			return InvocationOutcome{}, fmt.Errorf("callable %q has no next handler", a.desc.Name)
		}
		payload, callErr = a.next(req.Args)
	case InvocationStageFinal:
		if a.final == nil {
			return InvocationOutcome{}, fmt.Errorf("callable %q has no final handler", a.desc.Name)
		}
		payload, callErr = a.final(req.Args)
	default:
		return InvocationOutcome{}, fmt.Errorf("unsupported invocation stage %q", req.Stage)
	}

	if callErr != nil {
		result, err := NewInvocationErrorDescWithCode(a.desc, req.Stage, callErr.Error(), "")
		if err != nil {
			return InvocationOutcome{}, err
		}
		return NewInvocationOutcome(result, nil)
	}
	result, err := DescribeInvocationResult(a.desc, req.Stage)
	if err != nil {
		return InvocationOutcome{}, err
	}
	return NewInvocationOutcome(result, payload)
}

func (a *callableAdapter) invokeCapability(req InvocationRequest) (InvocationOutcome, error) {
	var input any
	if len(req.Args) == 1 {
		input = req.Args[0]
	} else {
		input = req.Args
	}
	payload, err := a.capability.Invoke(req.Context, input)
	if err != nil {
		diag := diagnostics.FromError(err, diagnostics.Descriptor{
			Category: diagnostics.CategoryHost,
			Code:     "native_call_failed",
			Path:     "binding/capability/invoke",
			Message:  err.Error(),
		})
		result, descErr := NewInvocationErrorDescWithDiagnostic(a.desc, req.Stage, diag)
		if descErr != nil {
			return InvocationOutcome{}, descErr
		}
		return NewInvocationOutcome(result, nil)
	}
	result, err := DescribeInvocationResult(a.desc, req.Stage)
	if err != nil {
		return InvocationOutcome{}, err
	}
	return NewInvocationOutcome(result, payload)
}

// capabilityCallableDesc returns the flattened descriptor a capability callable
// is exposed under in the callable/executable planes.
func capabilityCallableDesc(capabilityName string, local schema.CallableDesc) schema.CallableDesc {
	flattened := schema.CloneCallableDesc(local)
	flattened.Name = capabilityCallableName(capabilityName, local.Name)
	return flattened
}

func capabilityCallableName(capabilityName, callableName string) string {
	return capabilityName + "." + callableName
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func InvokeGoFunctionForHostProxy(fn reflect.Value, args []any) ([]any, error) {
	return invokeGoFunction(fn, args)
}

func invokeGoFunction(fn reflect.Value, args []any) (out []any, err error) {
	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		value, bindErr := bindGoFunctionArg(fn.Type().In(i), reflect.ValueOf(arg))
		if bindErr != nil {
			return nil, fmt.Errorf("unsupported Go function arg %d: %w", i, bindErr)
		}
		if !value.IsValid() {
			return nil, fmt.Errorf("unsupported Go function arg %d: invalid bound value", i)
		}
		in[i] = value
	}
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("host function panic: %v\n%s", r, debug.Stack())
		}
	}()
	results := fn.Call(in)
	if len(results) == 0 {
		return nil, nil
	}

	values := make([]any, 0, len(results))
	last := len(results) - 1
	for i, result := range results {
		if result.Type().Implements(errorType) {
			if i != last {
				return nil, fmt.Errorf("unsupported Go function signature: error must be last return")
			}
			if !result.IsNil() {
				return nil, result.Interface().(error)
			}
			continue
		}
		values = append(values, result.Interface())
	}
	return values, nil
}

func bindGoFunctionArg(target reflect.Type, value reflect.Value) (reflect.Value, error) {
	value = normalizeMapValue(value)
	if isStructTarget(target) && isStringMap(value.Type()) {
		return bindStructArg(target, value)
	}
	if target.Kind() == reflect.Pointer && value.Type().AssignableTo(target.Elem()) {
		ptr := reflect.New(target.Elem())
		ptr.Elem().Set(value)
		return ptr, nil
	}
	if target.Kind() == reflect.Slice && target.Elem().Kind() == reflect.Uint8 && value.Kind() == reflect.String {
		return reflect.ValueOf([]byte(value.String())), nil
	}
	if value.Type().AssignableTo(target) {
		return value, nil
	}
	if value.Type().ConvertibleTo(target) {
		if isNarrowingConvert(value.Kind(), target.Kind()) && !valueFitsInTarget(value, target) {
			return reflect.Value{}, fmt.Errorf("value %v overflows target type %s", value.Interface(), target)
		}
		return value.Convert(target), nil
	}
	// Slice conversion: a Spore array reaches the binding layer as []any
	// after vmValueToAny projects it. If the target is a typed slice
	// (e.g. []string for strings.Join), each element needs to be converted
	// individually since []any is not directly assignable to []string.
	if target.Kind() == reflect.Slice && value.Kind() == reflect.Slice {
		elemType := target.Elem()
		out := reflect.MakeSlice(target, value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			elem := value.Index(i)
			// Unwrap interface{} wrapping that []any introduces.
			if elem.Kind() == reflect.Interface {
				elem = elem.Elem()
			}
			if !elem.IsValid() {
				continue
			}
			converted, err := bindGoFunctionArg(elemType, elem)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("slice element %d: %w", i, err)
			}
			if converted.IsValid() {
				out.Index(i).Set(converted)
			}
		}
		return out, nil
	}
	return value, nil
}

func bindStructArg(target reflect.Type, value reflect.Value) (reflect.Value, error) {
	structType := target
	wantsPointer := false
	if structType.Kind() == reflect.Pointer {
		wantsPointer = true
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("target %s is not a struct", target.String())
	}

	bound := reflect.New(structType).Elem()
	known := make(map[string]reflect.StructField, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		name := JSONTagName(field)
		if name == "-" {
			continue
		}
		known[name] = field
	}

	iter := value.MapRange()
	for iter.Next() {
		key := iter.Key().String()
		field, ok := known[key]
		if !ok {
			return reflect.Value{}, fmt.Errorf("struct field %s is not defined on %s", key, structType.String())
		}
		fieldValue := normalizeMapValue(iter.Value())
		boundField, err := bindGoFunctionArg(field.Type, fieldValue)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("field %s: %w", key, err)
		}
		fieldDesc, err := schema.DescribeReflectType(field.Type)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("field %s: %w", key, err)
		}
		if err := invoke.ValidatePayloadValueAgainstType(fieldDesc, boundField); err != nil {
			return reflect.Value{}, fmt.Errorf("field %s: %w", key, err)
		}
		if targetField := bound.FieldByIndex(field.Index); !boundField.Type().AssignableTo(targetField.Type()) {
			return reflect.Value{}, fmt.Errorf("field %s: value type %s is not assignable to %s", key, boundField.Type().String(), targetField.Type().String())
		} else {
			targetField.Set(boundField)
		}
	}

	if wantsPointer {
		ptr := reflect.New(structType)
		ptr.Elem().Set(bound)
		return ptr, nil
	}
	return bound, nil
}

func normalizeMapValue(value reflect.Value) reflect.Value {
	if value.Kind() == reflect.Interface && !value.IsNil() {
		return value.Elem()
	}
	return value
}

func isStructTarget(target reflect.Type) bool {
	if target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	return target.Kind() == reflect.Struct
}

func isStringMap(target reflect.Type) bool {
	return target.Kind() == reflect.Map && target.Key().Kind() == reflect.String
}

// --- Narrowing conversion guards ---

// isNarrowingConvert reports whether converting from src to dst may lose data.
func isNarrowingConvert(src, dst reflect.Kind) bool {
	switch {
	case isIntKind(src) && isIntKind(dst):
		return intBitWidth(src) > intBitWidth(dst)
	case src == reflect.Float64 && dst == reflect.Float32:
		return true
	case isFloatKind(src) && isIntKind(dst):
		return true
	}
	return false
}

func isIntKind(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Uint64
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

func intBitWidth(k reflect.Kind) int {
	switch k {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	case reflect.Int, reflect.Uint:
		return int(reflect.TypeOf(int(0)).Size()) * 8
	case reflect.Int64, reflect.Uint64:
		return 64
	}
	return 0
}

// valueFitsInTarget checks that v's value can be represented exactly in target.
func valueFitsInTarget(v reflect.Value, target reflect.Type) bool {
	switch target.Kind() {
	case reflect.Int8:
		return fitsSignedRange(v, -1<<7, 1<<7-1)
	case reflect.Int16:
		return fitsSignedRange(v, -1<<15, 1<<15-1)
	case reflect.Int32:
		return fitsSignedRange(v, -1<<31, 1<<31-1)
	case reflect.Int64, reflect.Int:
		return true
	case reflect.Uint8:
		return fitsUnsignedRange(v, 1<<8-1)
	case reflect.Uint16:
		return fitsUnsignedRange(v, 1<<16-1)
	case reflect.Uint32:
		return fitsUnsignedRange(v, 1<<32-1)
	case reflect.Uint64, reflect.Uint:
		return true
	case reflect.Float32:
		f := toFloat64(v)
		if math.IsInf(float64(float32(f)), 0) {
			return false
		}
		// Integer values that exceed float32 mantissa (24 bits) lose exactness.
		if isIntKind(v.Kind()) {
			return float64(float32(f)) == f
		}
		return true
	case reflect.Float64:
		return true
	}
	return true
}

func fitsSignedRange(v reflect.Value, min, max int64) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		return i >= min && i <= max
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if min >= 0 {
			return int64(u) >= min && int64(u) <= max
		}
		return false // unsigned cannot fit in signed range with negative min
	}
	return false
}

func fitsUnsignedRange(v reflect.Value, max uint64) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		if i < 0 {
			return false
		}
		return uint64(i) <= max
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() <= max
	}
	return false
}

func toFloat64(v reflect.Value) float64 {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return 0
}

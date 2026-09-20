package bytecode

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/invoke"
)

// value_codec.go is the single host<->VM value codec. Every boundary path
// funnels through the helpers here:
//
//   - source-callable arguments (toVMArgs, anyToVMValueAtPath)
//   - host struct fields (goStructToVMStruct)
//   - heuristic map->struct native results (mapToVMStruct)
//   - native call results (InvokeVMNative)
//   - imported native values (ResolveImportedNativeValue)
//   - host-interface returns (HostAnyToVMValue / HostVMValueToAny)
//   - host-side result decoding (DecodeHostValue)
//
// The Go-type <-> VM mapping therefore lives in exactly one file instead of
// being spread across the evaluator, the interpreter and the runtime.
//
// Extension seam: a host Go type that is not one of the built-in primitives
// opts into the boundary by implementing HostValueEncoder (host -> VM) and/or
// HostValueDecoder (projected host value -> typed target). The codec probes
// those interfaces before its reflection fallbacks, so teaching the boundary a
// new type costs a single change — the type's own method set — rather than
// edits to six separate type switches.
//
// Hot path: the built-in primitive cases of anyToVMValueAtPath and
// vmValueToAnyWithHost are unchanged exact type switches that allocate nothing.
// The interface probes are reached only by values that already miss every
// primitive case, so built-ins keep their previous per-value cost and the seam
// adds no allocation.

// HostValueEncoder lets a host Go type control its own host->VM encoding. It is
// consulted by the codec on every host->VM path once the built-in primitive
// cases do not match, so implementing it on a type is the only change required
// to make that type cross the boundary everywhere.
type HostValueEncoder interface {
	EncodeToVM(vm_ *vm.VM, path string) (vm.Value, error)
}

// HostValueDecoder lets a host Go type control its own decoding from a projected
// host value. It is consulted by DecodeHostValue before its built-in
// target-kind switch, so an extension type can be used directly as a DecodeInto
// destination.
type HostValueDecoder interface {
	DecodeFromHost(value any) error
}

// HostInterfaceResolver lets script.Runtime surface host-backed proxy objects through the VM bridge.
type HostInterfaceResolver interface {
	HostInterfaceHandleForValue(value any) (vm.Handle, bool)
	HostInterfaceObjectForHandle(handle vm.Handle) (any, bool)
}

// HostAnyToVMValue converts a host value into a VM value using the evaluator bridge.
func HostAnyToVMValue(host HostInterfaceResolver, vm_ *vm.VM, v any, path string) (vm.Value, error) {
	return anyToVMValueAtPath(host, vm_, v, path)
}

// HostVMValueToAny converts a VM value back into a host value using the evaluator bridge.
func HostVMValueToAny(host HostInterfaceResolver, vm_ *vm.VM, v vm.Value) any {
	return vmValueToAnyWithHost(host, vm_, v)
}

// vmArgPaths pre-builds the per-index argument diagnostic paths; argument
// lists are short, so the common indices avoid fmt.Sprintf entirely.
var vmArgPaths = [...]string{
	"vm/evaluator/argument/0", "vm/evaluator/argument/1",
	"vm/evaluator/argument/2", "vm/evaluator/argument/3",
	"vm/evaluator/argument/4", "vm/evaluator/argument/5",
	"vm/evaluator/argument/6", "vm/evaluator/argument/7",
}

func vmArgPath(i int) string {
	if i < len(vmArgPaths) {
		return vmArgPaths[i]
	}
	return "vm/evaluator/argument/" + strconv.Itoa(i)
}

func toVMArgs(host HostInterfaceResolver, vm_ *vm.VM, args []any) ([]vm.Value, error) {
	vmArgs := make([]vm.Value, len(args))
	for i, arg := range args {
		value, err := anyToVMValueAtPath(host, vm_, arg, vmArgPath(i))
		if err != nil {
			return nil, err
		}
		vmArgs[i] = value
	}
	return vmArgs, nil
}

func mustToVMArgs(host HostInterfaceResolver, vm_ *vm.VM, args []any) []vm.Value {
	vmArgs, err := toVMArgs(host, vm_, args)
	if err != nil {
		return nil
	}
	return vmArgs
}

func anyToVMValue(vm_ *vm.VM, v any) (vm.Value, error) {
	return anyToVMValueAtPath(nil, vm_, v, "vm/evaluator/argument")
}

func anyToVMValueAtPath(host HostInterfaceResolver, vm_ *vm.VM, v any, path string) (vm.Value, error) {
	if host != nil {
		if handle, ok := host.HostInterfaceHandleForValue(v); ok {
			return vm.EncodeHandle(handle), nil
		}
	}
	switch val := v.(type) {
	case int:
		if val < -2147483648 || val > 2147483647 {
			return vm.EncodeInt(0), &RuntimeError{
				Code:     "value_out_of_range",
				Category: diagnostics.CategoryRuntime,
				Path:     path,
				Message:  fmt.Sprintf("Go int value %d exceeds script int32 range", val),
			}
		}
		return vm.EncodeInt(int32(val)), nil
	case int32:
		return vm.EncodeInt(val), nil
	case int64:
		return vm.EncodeLong(val, vm_), nil
	case uint64:
		return vm.EncodeULong(val, vm_), nil
	case float32:
		return vm.EncodeFloat(val), nil
	case float64:
		return vm.EncodeDouble(val, vm_), nil
	case bool:
		return vm.EncodeBool(val), nil
	case string:
		return vm_.EncodeString(val), nil
	case time.Time:
		return vm_.EncodeString(val.Format(time.RFC3339Nano)), nil
	case []byte:
		return vm_.EncodeBytes(val), nil
	case []any:
		return sliceToVMArray(host, vm_, reflect.ValueOf(val), path)
	case []map[string]any:
		return sliceToVMArray(host, vm_, reflect.ValueOf(val), path)
	case map[string]any:
		return stringMapToVMMap(host, vm_, reflect.ValueOf(val), path)
	case nil:
		return vm.EncodeHandle(vm.InvalidHandle), nil
	default:
		// Extension seam: a host type that missed every built-in case controls
		// its own encoding. Probed before the reflection fallbacks so a single
		// interface implementation covers arguments, struct fields, native
		// results and native values at once.
		if enc, ok := v.(HostValueEncoder); ok {
			return enc.EncodeToVM(vm_, path)
		}
		rv := reflect.ValueOf(v)
		if rv.IsValid() {
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				return sliceToVMArray(host, vm_, rv, path)
			case reflect.Map:
				if rv.Type().Key().Kind() == reflect.String {
					return stringMapToVMMap(host, vm_, rv, path)
				}
			case reflect.Struct:
				return goStructToVMStruct(host, vm_, rv, path)
			}
		}
		return unsupportedVMArgument(v, path)
	}
}

func sliceToVMArray(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	arr := vm_.NewArray(vm.TypeInvalid, 0)
	releaseArrRoot := vm_.AddTemporaryRoot(vm.EncodeHandle(arr))
	defer releaseArrRoot()
	for i := 0; i < rv.Len(); i++ {
		elemVal, err := anyToVMValueAtPath(host, vm_, rv.Index(i).Interface(), path+"/"+strconv.Itoa(i))
		if err != nil {
			return vm.EncodeInt(0), err
		}
		releaseElemRoot := vm_.AddTemporaryRoot(elemVal)
		vm_.ArrayPush(arr, elemVal)
		releaseElemRoot()
	}
	return vm.EncodeHandle(arr), nil
}

func stringMapToVMMap(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	m := vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, rv.Len())
	// Root the outer map: converting nested values allocates, and an
	// unrooted map gets swept once the heap crosses its GC threshold —
	// the reused memory then corrupts the next MapSet ("map is full").
	releaseRoot := vm_.AddTemporaryRoot(vm.EncodeHandle(m))
	defer releaseRoot()
	iter := rv.MapRange()
	for iter.Next() {
		keyString := iter.Key().String()
		elemVal, err := anyToVMValueAtPath(host, vm_, iter.Value().Interface(), path+"/"+keyString)
		if err != nil {
			return vm.EncodeInt(0), err
		}
		encodedKey := vm_.EncodeString(keyString)
		// EncodeString may allocate a heap string; root the pending pair so a
		// GC triggered by that allocation cannot sweep the unstored elemVal.
		releaseElemRoot := vm_.AddTemporaryRoot(elemVal, encodedKey)
		vm_.MapSet(m, encodedKey, elemVal)
		releaseElemRoot()
	}
	return vm.EncodeHandle(m), nil
}

func goStructToVMStruct(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return unsupportedVMArgument(rv.Interface(), path)
	}
	structName := rv.Type().Name()
	sd := vm_.StructReg().GetStruct(structName)
	if sd == nil {
		return unsupportedVMArgument(rv.Interface(), path+"/struct-not-registered:"+structName)
	}
	fields := sd.Fields()
	fieldValues := make([]vm.Value, len(fields))
	// Root each converted field as it is produced: later field conversions
	// allocate, and unrooted handles (nested maps/arrays/structs) would be
	// swept by a mid-conversion GC pass before NewStructInstance stores them.
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for i, f := range fields {
		fieldRv := rv.FieldByName(f.Name())
		if !fieldRv.IsValid() {
			fieldRv = findFieldByJSONTag(rv, f.Name())
		}
		if !fieldRv.IsValid() {
			return unsupportedVMArgument(rv.Interface(), fmt.Sprintf("%s/field-%s-not-found", path, f.Name()))
		}
		val, err := anyToVMValueAtPath(host, vm_, fieldRv.Interface(), path+"/"+f.Name())
		if err != nil {
			return vm.EncodeInt(0), err
		}
		fieldValues[i] = val
		releases = append(releases, vm_.AddTemporaryRoot(val))
	}
	handle := vm_.NewStructInstance(structName, fieldValues)
	if handle == vm.InvalidHandle {
		return unsupportedVMArgument(rv.Interface(), path+"/new-struct-failed")
	}
	return vm.EncodeHandle(handle), nil
}

func findFieldByJSONTag(rv reflect.Value, name string) reflect.Value {
	typ := rv.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if tag == name {
			return rv.Field(i)
		}
	}
	return reflect.Value{}
}

func projectNativeResult(v any) any {
	if v == nil {
		return nil
	}
	// A host type that owns its boundary encoding must reach the codec intact;
	// projecting it to a map/slice first would bypass the extension seam.
	if _, ok := v.(HostValueEncoder); ok {
		return v
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			bytes := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(bytes), rv)
			return bytes
		}
		items := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			items[i] = projectNativeResult(rv.Index(i).Interface())
		}
		return items
	case reflect.Struct:
		if rv.Type() == reflect.TypeOf(time.Time{}) {
			return rv.Interface().(time.Time).Format(time.RFC3339Nano)
		}
		result := make(map[string]any, rv.NumField())
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			name := invoke.JSONTagName(field)
			if name == "-" {
				continue
			}
			result[name] = projectNativeResult(rv.Field(i).Interface())
		}
		return result
	case reflect.Array:
		items := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			items[i] = projectNativeResult(rv.Index(i).Interface())
		}
		return items
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			result := make(map[string]any, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				result[iter.Key().String()] = projectNativeResult(iter.Value().Interface())
			}
			return result
		}
	}
	return v
}

func unsupportedVMArgument(v any, path string) (vm.Value, error) {
	return vm.EncodeInt(0), &RuntimeError{
		Code:     "unsupported_vm_argument_type",
		Category: diagnostics.CategoryRuntime,
		Path:     path,
		Message:  fmt.Sprintf("unsupported VM argument type %T", v),
	}
}

func vmValueToAny(v_ *vm.VM, v vm.Value) any {
	return vmValueToAnyWithHost(nil, v_, v)
}

func vmValueToAnyWithHost(host HostInterfaceResolver, v_ *vm.VM, v vm.Value) any {
	if vm.IsDouble(v) {
		return v_.DecodeDouble(v)
	}
	if vm.IsNull(v) {
		return nil
	}
	if vm.IsBool(v) {
		return vm.DecodeBool(v)
	}
	if vm.IsLong(v) {
		return v_.DecodeLong(v)
	}
	if vm.IsULong(v) {
		return v_.DecodeULong(v)
	}
	if vm.IsInt(v) {
		return int(vm.DecodeInt(v))
	}
	if vm.IsFloat(v) {
		return float64(vm.DecodeFloat(v))
	}
	if vm.IsBytes(v) {
		return v_.DecodeBytes(v)
	}
	if vm.IsString(v) {
		return v_.DecodeString(v)
	}
	if vm.IsHandle(v) {
		h := vm.DecodeHandle(v)
		if host != nil {
			if target, ok := host.HostInterfaceObjectForHandle(h); ok {
				return target
			}
		}
		idx := v_.ResolveHandle(h)
		if idx >= 0 && idx < v_.MemTop() {
			header := v_.MemoryAt(idx)
			if v_.IsLongHeapHeader(header) {
				return v_.DecodeLong(v)
			}
			if v_.IsULongHeapHeader(header) {
				return v_.DecodeULong(v)
			}
			if v_.IsLargeStringHeader(header) {
				return v_.DecodeString(v)
			}
			if v_.IsLargeBytesHeader(header) {
				return v_.DecodeBytes(v)
			}
			if v_.IsMap(h) {
				result := make(map[string]any)
				v_.MapIterate(h, func(key, val vm.Value) bool {
					result[v_.DecodeString(key)] = vmValueToAnyWithHost(host, v_, val)
					return true
				})
				return result
			}
			if v_.IsStruct(h) {
				sd := v_.StructReg().GetStructByID(uint32(v_.MemoryAt(idx)>>32) & 0x3FFFFFFF)
				if sd != nil {
					result := make(map[string]any, sd.FieldCount())
					fields := sd.Fields()
					for i := 0; i < sd.FieldCount(); i++ {
						result[fields[i].Name()] = vmValueToAnyWithHost(host, v_, v_.GetStructFieldByIndex(h, i))
					}
					return result
				}
			}
			length := v_.ArrayLength(h)
			items := make([]any, length)
			for i := 0; i < length; i++ {
				items[i] = vmValueToAnyWithHost(host, v_, v_.GetArrayElement(h, i))
			}
			return items
		}
	}
	return nil
}

// mapToVMStruct attempts to convert a map[string]any into a VM struct
// by matching its keys against registered struct field names.
func (e *VMEvaluator) mapToVMStruct(m map[string]any) (vm.Value, bool) {
	if e.vm_ == nil || len(m) == 0 || len(e.registeredNativeStructs) == 0 {
		return vm.EncodeInt(0), false
	}
	for name := range e.registeredNativeStructs {
		sd := e.vm_.StructReg().GetStruct(name)
		if sd == nil {
			continue
		}
		fields := sd.Fields()
		if len(fields) != len(m) {
			continue
		}
		match := true
		for _, f := range fields {
			if _, ok := m[f.Name()]; !ok {
				match = false
				break
			}
		}
		if match {
			fieldValues := make([]vm.Value, len(fields))
			for i, f := range fields {
				val, err := anyToVMValueAtPath(nil, e.vm_, m[f.Name()], "vm/struct/"+name+"/"+f.Name())
				if err != nil {
					return vm.EncodeInt(0), false
				}
				fieldValues[i] = val
			}
			handle := e.vm_.NewStructInstance(name, fieldValues)
			if handle != vm.InvalidHandle {
				return vm.EncodeHandle(handle), true
			}
		}
	}
	return vm.EncodeInt(0), false
}

// --- Host scalar projection ---

// toInt32/toFloat32/toInt64/toUInt64/toFloat64 project a host `any` scalar
// (compiler constants and decoded host values) onto the concrete numeric type
// the VM immediate expects. They live in the codec so the Go-scalar mapping is
// owned in one place alongside the VM encoding.

func toInt32(x any) int32 {
	switch v := x.(type) {
	case int:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	default:
		return 0
	}
}

func toFloat32(x any) float32 {
	switch v := x.(type) {
	case float32:
		return v
	case float64:
		return float32(v)
	case int:
		return float32(v)
	case int32:
		return float32(v)
	default:
		return 0
	}
}

func toInt64(x any) int64 {
	switch v := x.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	default:
		return 0
	}
}

func toUInt64(x any) uint64 {
	switch v := x.(type) {
	case uint64:
		return v
	case int64:
		return uint64(v)
	case int:
		return uint64(v)
	case int32:
		return uint64(v)
	default:
		return 0
	}
}

func toFloat64(x any) float64 {
	switch v := x.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

// --- Host result decoding ---

// DecodeHostValue copies a projected host value (as returned by
// vmValueToAnyWithHost) into a typed Go destination. It is the host-side half
// of the codec: VM -> host projection produces a canonical Go value, and this
// entry point narrows it back onto the caller's concrete target.
//
// A destination that implements HostValueDecoder takes over its own decoding,
// mirroring the HostValueEncoder seam on the outbound path.
func DecodeHostValue(dst any, value any) error {
	if dec, ok := dst.(HostValueDecoder); ok {
		return dec.DecodeFromHost(value)
	}
	switch p := dst.(type) {
	case *any:
		*p = value
		return nil
	case *string:
		s, ok := value.(string)
		if !ok {
			return decodeTypeMismatch("string", value)
		}
		*p = s
		return nil
	case *bool:
		b, ok := value.(bool)
		if !ok {
			return decodeTypeMismatch("bool", value)
		}
		*p = b
		return nil
	case *int:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int(n)
		return nil
	case *int8:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int8(n)
		return nil
	case *int16:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int16(n)
		return nil
	case *int32:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int32(n)
		return nil
	case *int64:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *uint:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint(n)
		return nil
	case *uint8:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint8(n)
		return nil
	case *uint16:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint16(n)
		return nil
	case *uint32:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint32(n)
		return nil
	case *uint64:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *float32:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = float32(f)
		return nil
	case *float64:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = f
		return nil
	case *[]any:
		s, ok := value.([]any)
		if !ok {
			return decodeTypeMismatch("[]any", value)
		}
		*p = s
		return nil
	case *map[string]any:
		m, ok := value.(map[string]any)
		if !ok {
			return decodeTypeMismatch("map[string]any", value)
		}
		*p = m
		return nil
	}
	return fmt.Errorf("DecodeInto does not support target type %T", dst)
}

func decodeInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	}
	return 0, decodeTypeMismatch("integer", value)
}

func decodeUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case int:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	}
	return 0, decodeTypeMismatch("unsigned integer", value)
}

func decodeFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	}
	return 0, decodeTypeMismatch("number", value)
}

func decodeTypeMismatch(target string, value any) error {
	if value == nil {
		return fmt.Errorf("DecodeInto: cannot decode nil into %s target", target)
	}
	return fmt.Errorf("DecodeInto: cannot decode %T into %s target", value, target)
}

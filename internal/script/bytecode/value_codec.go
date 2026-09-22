package bytecode

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
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

// --- Lazy diagnostic paths ---
//
// Container conversion recurses per element/field/key. Building the joined
// "path/key" string up front costs one allocation per element even on the
// success path, which dominates host->VM boxing for perception-style
// snapshots. Recursion instead pushes the pending segment onto a pooled
// conversion context; the joined string is materialized only at error sites.
type pathSegKind uint8

const (
	pathSegBase pathSegKind = iota
	pathSegKey
	pathSegIndex
)

type pathSeg struct {
	kind  pathSegKind
	key   string // pathSegBase: base path; pathSegKey: map key / field name
	index int
}

func segKey(key string) pathSeg { return pathSeg{kind: pathSegKey, key: key} }
func segIndex(i int) pathSeg    { return pathSeg{kind: pathSegIndex, index: i} }
func segBase(p string) pathSeg  { return pathSeg{kind: pathSegBase, key: p} }

// convCtx carries the state of one host->VM conversion: the host resolver,
// the target VM and the stack of pending diagnostic path segments. Contexts
// are pooled, so steady-state conversion (any depth) allocates nothing on
// the Go side.
type convCtx struct {
	host HostInterfaceResolver
	vm_  *vm.VM
	segs []pathSeg // segs[0] is the base path
}

var convCtxPool = sync.Pool{New: func() any { return new(convCtx) }}

func acquireConvCtx(host HostInterfaceResolver, vm_ *vm.VM, path string) *convCtx {
	c := convCtxPool.Get().(*convCtx)
	c.host = host
	c.vm_ = vm_
	c.segs = append(c.segs[:0], segBase(path))
	return c
}

func releaseConvCtx(c *convCtx) {
	c.host = nil
	c.vm_ = nil
	convCtxPool.Put(c)
}

func (c *convCtx) push(seg pathSeg) { c.segs = append(c.segs, seg) }
func (c *convCtx) pop()             { c.segs = c.segs[:len(c.segs)-1] }

// path joins the pending segment stack into the same "a/b/c" form the codec
// historically built eagerly. Error paths only.
func (c *convCtx) path() string {
	var b strings.Builder
	for _, seg := range c.segs {
		switch seg.kind {
		case pathSegBase:
			b.WriteString(seg.key)
		case pathSegKey:
			b.WriteByte('/')
			b.WriteString(seg.key)
		case pathSegIndex:
			b.WriteByte('/')
			b.WriteString(strconv.Itoa(seg.index))
		}
	}
	return b.String()
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
	c := acquireConvCtx(host, vm_, path)
	val, err := c.convert(v)
	releaseConvCtx(c)
	return val, err
}

func (c *convCtx) convert(v any) (vm.Value, error) {
	host, vm_ := c.host, c.vm_
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
				Path:     c.path(),
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
		return c.anySlice(val)
	case []map[string]any:
		return c.anySlice(val)
	case map[string]any:
		return c.anyMap(val)
	case nil:
		return vm.EncodeHandle(vm.InvalidHandle), nil
	default:
		// Extension seam: a host type that missed every built-in case controls
		// its own encoding. Probed before the reflection fallbacks so a single
		// interface implementation covers arguments, struct fields, native
		// results and native values at once.
		if enc, ok := v.(HostValueEncoder); ok {
			return enc.EncodeToVM(vm_, c.path())
		}
		rv := reflect.ValueOf(v)
		if rv.IsValid() {
			return c.convertReflect(rv)
		}
		return unsupportedVMArgument(v, c.path())
	}
}

var (
	byteSliceType = reflect.TypeOf([]byte(nil))
	timeType      = reflect.TypeOf(time.Time{})
)

// reflectValueToVM converts a reflect.Value without boxing it through
// interface{} first: every .Interface() call on a non-pointer-shaped value
// allocates (reflect packEface -> unsafe_New), so typed containers
// (map[string]float64, []int, struct fields, ...) dispatch on Kind and read
// through the typed accessors instead. Behavior mirrors convertAnyToVM's
// type switch value-for-value, including which kinds are unsupported.
func (c *convCtx) convertReflect(rv reflect.Value) (vm.Value, error) {
	vm_ := c.vm_
	// Composite kinds may implement the extension seam; probe them the same
	// way the any-based path does (only reached when primitive cases miss).
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct, reflect.Pointer:
		if enc, ok := rv.Interface().(HostValueEncoder); ok {
			return enc.EncodeToVM(vm_, c.path())
		}
	}
	switch rv.Kind() {
	case reflect.Bool:
		return vm.EncodeBool(rv.Bool()), nil
	case reflect.Int:
		val := rv.Int()
		if val < -2147483648 || val > 2147483647 {
			return vm.EncodeInt(0), &RuntimeError{
				Code:     "value_out_of_range",
				Category: diagnostics.CategoryRuntime,
				Path:     c.path(),
				Message:  fmt.Sprintf("Go int value %d exceeds script int32 range", val),
			}
		}
		return vm.EncodeInt(int32(val)), nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return vm.EncodeInt(int32(rv.Int())), nil
	case reflect.Int64:
		return vm.EncodeLong(rv.Int(), vm_), nil
	case reflect.Uint64:
		return vm.EncodeULong(rv.Uint(), vm_), nil
	case reflect.Float32:
		return vm.EncodeFloat(float32(rv.Float())), nil
	case reflect.Float64:
		return vm.EncodeDouble(rv.Float(), vm_), nil
	case reflect.String:
		return vm_.EncodeString(rv.String()), nil
	case reflect.Interface:
		if rv.IsNil() {
			return vm.EncodeHandle(vm.InvalidHandle), nil
		}
		return c.convertReflect(rv.Elem())
	case reflect.Slice:
		if rv.Type() == byteSliceType {
			return vm_.EncodeBytes(rv.Bytes()), nil
		}
		return c.reflectSlice(rv)
	case reflect.Array:
		return c.reflectSlice(rv)
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			return c.stringMap(rv)
		}
	case reflect.Struct:
		if rv.Type() == timeType {
			return vm_.EncodeString(rv.Interface().(time.Time).Format(time.RFC3339Nano)), nil
		}
		return c.goStruct(rv)
	}
	return unsupportedVMArgument(rv.Interface(), c.path())
}

// anySlice converts the untyped host slice shapes ([]any and
// []map[string]any) with direct iteration — no reflect scaffolding, no
// per-element boxing. The VM array is preallocated to the exact length.
func (c *convCtx) anySlice[T any](s []T) (vm.Value, error) {
	vm_ := c.vm_
	arr := vm_.NewArray(vm.TypeInvalid, len(s))
	scope := vm_.BeginRootScope()
	defer scope.End()
	scope.Add(vm.EncodeHandle(arr))
	for i, elem := range s {
		c.push(segIndex(i))
		elemVal, err := c.convert(elem)
		c.pop()
		if err != nil {
			return vm.EncodeInt(0), err
		}
		scope.Add(elemVal)
		vm_.SetArrayElement(arr, i, elemVal)
		scope.Trim(1)
	}
	return vm.EncodeHandle(arr), nil
}

func (c *convCtx) reflectSlice(rv reflect.Value) (vm.Value, error) {
	vm_ := c.vm_
	arr := vm_.NewArray(vm.TypeInvalid, rv.Len())
	scope := vm_.BeginRootScope()
	defer scope.End()
	scope.Add(vm.EncodeHandle(arr))
	for i := 0; i < rv.Len(); i++ {
		c.push(segIndex(i))
		elemVal, err := c.convertReflect(rv.Index(i))
		c.pop()
		if err != nil {
			return vm.EncodeInt(0), err
		}
		scope.Add(elemVal)
		vm_.SetArrayElement(arr, i, elemVal)
		scope.Trim(1)
	}
	return vm.EncodeHandle(arr), nil
}

// anyMap converts the dominant host map shape (map[string]any, the
// JSON-style snapshot type) with direct Go map iteration: no reflect.Value
// scaffolding, no boxed key/value copies, no per-entry path strings.
func (c *convCtx) anyMap(m map[string]any) (vm.Value, error) {
	vm_ := c.vm_
	vmMap := vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, len(m))
	// Root the outer map: converting nested values allocates, and an
	// unrooted map gets swept once the heap crosses its GC threshold —
	// the reused memory then corrupts the next MapSet ("map is full").
	scope := vm_.BeginRootScope()
	defer scope.End()
	scope.Add(vm.EncodeHandle(vmMap))
	for keyString, val := range m {
		c.push(segKey(keyString))
		elemVal, err := c.convert(val)
		c.pop()
		if err != nil {
			return vm.EncodeInt(0), err
		}
		// Root the pending value before EncodeString: interning may allocate
		// VM memory and trigger a GC that would otherwise sweep the unstored
		// handle. Once MapSet stores the pair they are reachable through the
		// rooted map, so the batched roots can be trimmed again.
		scope.Add(elemVal)
		encodedKey := vm_.EncodeString(keyString)
		scope.Add(encodedKey)
		vm_.MapSet(vmMap, encodedKey, elemVal)
		scope.Trim(2)
	}
	return vm.EncodeHandle(vmMap), nil
}

func (c *convCtx) stringMap(rv reflect.Value) (vm.Value, error) {
	vm_ := c.vm_
	m := vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, rv.Len())
	scope := vm_.BeginRootScope()
	defer scope.End()
	scope.Add(vm.EncodeHandle(m))
	iter := rv.MapRange()
	for iter.Next() {
		keyString := iter.Key().String()
		c.push(segKey(keyString))
		elemVal, err := c.convertReflect(iter.Value())
		c.pop()
		if err != nil {
			return vm.EncodeInt(0), err
		}
		scope.Add(elemVal)
		encodedKey := vm_.EncodeString(keyString)
		scope.Add(encodedKey)
		vm_.MapSet(m, encodedKey, elemVal)
		scope.Trim(2)
	}
	return vm.EncodeHandle(m), nil
}

func (c *convCtx) goStruct(rv reflect.Value) (vm.Value, error) {
	vm_ := c.vm_
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return unsupportedVMArgument(rv.Interface(), c.path())
	}
	structName := rv.Type().Name()
	sd := vm_.StructReg().GetStruct(structName)
	if sd == nil {
		return unsupportedVMArgument(rv.Interface(), c.path()+"/struct-not-registered:"+structName)
	}
	fields := sd.Fields()
	fieldValues := make([]vm.Value, len(fields))
	// Root each converted field in one batched scope: later field
	// conversions allocate, and unrooted handles (nested maps/arrays/structs)
	// would be swept by a mid-conversion GC pass before NewStructInstance
	// stores them.
	scope := vm_.BeginRootScope()
	defer scope.End()
	for i, f := range fields {
		fieldRv := rv.FieldByName(f.Name())
		if !fieldRv.IsValid() {
			fieldRv = findFieldByJSONTag(rv, f.Name())
		}
		if !fieldRv.IsValid() {
			return unsupportedVMArgument(rv.Interface(), fmt.Sprintf("%s/field-%s-not-found", c.path(), f.Name()))
		}
		c.push(segKey(f.Name()))
		val, err := c.convertReflect(fieldRv)
		c.pop()
		if err != nil {
			return vm.EncodeInt(0), err
		}
		fieldValues[i] = val
		scope.Add(val)
	}
	handle := vm_.NewStructInstance(structName, fieldValues)
	if handle == vm.InvalidHandle {
		return unsupportedVMArgument(rv.Interface(), c.path()+"/new-struct-failed")
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

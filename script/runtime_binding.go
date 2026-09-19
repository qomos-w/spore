package script

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// This file holds the Runtime binding surface: capability registration,
// descriptor validation, and namespace commit.

// BoundFunction describes a host-bound callable exposed through Runtime.
// Info captures the same host-facing shape as Exports/LookupCallable.
type BoundFunction struct {
	Namespace string
	Name      string
	Callable  string
	Info      CallableInfo
}

// BoundValue describes a host-bound read-only value exposed through Runtime.
type BoundValue struct {
	Namespace string
	Name      string
	Desc      binding.CapabilityValueDesc
}

// BoundStruct describes a host-bound struct/object type exposed through Runtime.
// It is registered via BindStructDesc or BindStruct and becomes importable by
// script code as a compile-time type (e.g. `import Point from "host"`).
type BoundStruct struct {
	Namespace  string
	Name       string
	ObjectDesc schema.ObjectDesc
}

// BoundInterface describes a host-bound interface type exposed through Runtime.
// It is registered via BindInterfaceDesc or BindInterfaceObject and becomes
// importable by script code as a compile-time interface type.
type BoundInterface struct {
	Namespace     string
	Name          string
	InterfaceDesc schema.InterfaceDesc
}

// BoundTypeAlias describes a host-bound type alias exposed through Runtime.
// It is registered via BindTypeAlias and becomes importable by script code as a
// compile-time type (e.g. `import UserId from "host"`).
type BoundTypeAlias struct {
	Namespace string
	Name      string
	TypeDesc  schema.TypeDesc
}

// BindFunc exposes a host function under a namespace and name. Multiple
// BindFunc / BindValue / BindObject calls accumulate into the same namespace
// until the bindings are sealed by the first LoadModule/LoadSource call.
// After that point, further Bind* calls on any namespace return an error.
func (rt *Runtime) BindFunc(namespace, name string, fn any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddFreeFunction(name, fn); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindValue exposes a read-only host value under a namespace and name.
// Like BindFunc, multiple BindValue / BindFunc / BindObject calls accumulate
// into the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindValue(namespace, name string, v any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	return rt.bindPendingValue(namespace, name, v)
}

func (rt *Runtime) bindPendingValue(namespace, name string, v any) error {
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddValue(name, v); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindStructDesc registers a schema-described struct type under a namespace and
// name, making it importable by script code as a compile-time struct type.
// This is the canonical descriptor-based registration API for host-defined
// struct shapes.
//
// After BindStructDesc, script code can write:
//
//	import Point from "geom"
//	fun dist(p: Point): int { return p.x + p.y }
//
// Field naming convention:
//
// Script code references fields by the exact Name string in
// schema.FieldDesc. BindStruct (the reflection convenience wrapper) derives
// field names from Go json tags and falls back to the Go field name, which is
// typically lowerCamelCase or PascalCase depending on tags. To keep
// descriptor-first registrations consistent with reflection-derived ones,
// prefer the same naming convention you would expect from BindStruct on the
// matching Go type. Mixing PascalCase here with lowerCamelCase elsewhere will
// produce inconsistent script-visible names without any error from
// BindStructDesc itself.
//
// BindStructDesc only defines compile-time type visibility. It does not bind a
// runtime Go object instance; hosts that need runtime object binding should use
// the binding package directly.
func (rt *Runtime) BindStructDesc(namespace, name string, desc schema.ObjectDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("struct name cannot be empty")
	}
	if err := validateStructDescriptor(name, desc); err != nil {
		return err
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddObject(name, desc); err != nil {
		return err
	}
	rt.registerBoundStructInVM(name, desc)
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindStruct registers a Go struct type under a namespace and name, making it
// importable by script code as a compile-time struct type. This is a
// reflection-based convenience wrapper over BindStructDesc. The value argument
// should be a zero value of the struct type (e.g. `MyStruct{}`); its type is
// reflected to produce the schema.ObjectDesc that the linker uses for type
// checking and script-visible field names.
//
// After BindStruct, script code can write:
//
//	import Point from "geom"
//	fun dist(p: Point): int { return p.x + p.y }
//
// BindStruct composes freely with BindFunc/BindValue/BindObject/BindTypeAlias
// on the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindStruct(namespace, name string, value any) error {
	if value == nil {
		return fmt.Errorf("struct value cannot be nil")
	}
	obj, err := schema.DescribeGoStruct(value)
	if err != nil {
		return fmt.Errorf("cannot describe struct %q: %w", name, err)
	}
	return rt.BindStructDesc(namespace, name, obj)
}

// BindInterfaceDesc registers a schema-described interface type under a namespace and
// name, making it importable by script code as a compile-time interface type.
// The descriptor is method-only: fields, inheritance metadata, and runtime object
// state are not part of this surface.
func (rt *Runtime) BindInterfaceDesc(namespace, name string, desc schema.InterfaceDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	if err := validateInterfaceDescriptor(name, desc); err != nil {
		return err
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddInterface(name, desc); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindInterfaceObject registers a host-backed interface object under a namespace and
// name. The bound symbol is importable as both a compile-time interface type and
// a runtime value whose surface exposes only the descriptor's methods.
func (rt *Runtime) BindInterfaceObject(namespace, name string, desc schema.InterfaceDesc, target any) error {
	if target == nil {
		return fmt.Errorf("interface target cannot be nil")
	}
	if err := rt.BindInterfaceDesc(namespace, name, desc); err != nil {
		return err
	}
	key := hostInterfaceBindingKey{Namespace: namespace, Name: name}
	if existingID, ok := rt.hostInterfaceBindings[key]; ok {
		delete(rt.hostInterfaceObjects, existingID)
	}
	objID := atomic.AddUint64(&rt.nextHostInterfaceID, 1)
	rt.hostInterfaceBindings[key] = objID
	rt.hostInterfaceObjects[objID] = hostInterfaceObject{
		ID:             objID,
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		Target:         target,
		ProxyClassName: hostInterfaceProxyClassName(namespace, name),
		Handle:         vm.InvalidHandle,
		Pending:        true,
	}
	return rt.bindPendingValue(namespace, name, target)
}

// BindStructType is a generic convenience over BindStruct that does not require
// a zero value instance. The type parameter T is the Go struct type to register.
//
// Example:
//
//	rt.BindStructType[Point]("geom", "Point")
//
// BindStructType delegates to BindStruct internally and shares all of its
// validation and sealing semantics.
func BindStructType[T any](rt *Runtime, namespace, name string) error {
	var zero T
	return rt.BindStruct(namespace, name, zero)
}

// BindTypeAlias registers a compile-time type alias under a namespace and name,
// making it importable by script code. This is useful for exposing canonical
// type names (e.g. `UserId`, `Timestamp`) that script code can use in type
// annotations without re-declaring the underlying shape.
//
// After BindTypeAlias, script code can write:
//
//	import UserId from "types"
//	fun greet(id: UserId): string { return "hello " + id }
//
// BindTypeAlias composes freely with BindFunc/BindValue/BindObject/BindStruct
// on the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindTypeAlias(namespace, name string, td schema.TypeDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("type alias name cannot be empty")
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddTypeAlias(name, td); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindTypeAliasName is a convenience shorthand for BindTypeAlias that accepts a
// primitive type name string instead of a full schema.TypeDesc. Supported names
// are the canonical Spore scalar types: "string", "int", "long", "float",
// "double", "bool", and "any". For complex or custom types, use BindTypeAlias
// with an explicit schema.TypeDesc.
func (rt *Runtime) BindTypeAliasName(namespace, name string, typeName string) error {
	td := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: typeName}
	return rt.BindTypeAlias(namespace, name, td)
}

// BindObject exposes a group of host-visible surfaces under a single
// namespace in one call. Map entries whose values are Go functions become
// callables; other entries become read-only values. BindObject does NOT
// register struct/object types — use BindStructDesc for the canonical
// descriptor-based API, or BindStruct as the reflection convenience form.
//
// BindObject is a convenience shape for simple hosts; the underlying
// binding/capability model remains the canonical authority. Hosts that need
// stricter typing or richer schema should reach for the binding package
// directly rather than treating BindObject as a second module system.
func (rt *Runtime) BindObject(namespace string, values map[string]any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if len(values) == 0 {
		return nil
	}
	builder := rt.builderForNamespace(namespace)
	for name, v := range values {
		if name == "" {
			return fmt.Errorf("BindObject %q: entry name cannot be empty", namespace)
		}
		if v != nil && reflect.TypeOf(v).Kind() == reflect.Func {
			if err := builder.AddFreeFunction(name, v); err != nil {
				return err
			}
			continue
		}
		if err := builder.AddValue(name, v); err != nil {
			return err
		}
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// Bind is a unified convenience dispatcher that automatically routes each map
// entry to the appropriate binding primitive based on its Go type:
//
//   - function values → BindFunc (callable)
//   - struct values   → BindStruct (reflection convenience over BindStructDesc)
//   - everything else → BindValue (read-only value)
//
// Bind is the LLM-friendly convenience entry point when callers want one map to
// contain both runtime values and compile-time type definitions. It is not a
// new semantic category; it is derived from the canonical binding primitives.
//
// Example:
//
//	rt.Bind("env", map[string]any{
//	    "echo":   func(s string) string { return s },
//	    "name":   "spore",
//	    "Config": Config{},  // struct zero value → type definition
//	})
//
// Bind composes freely with itself and with explicit BindFunc/BindValue/BindStruct
// calls on the same namespace. Entries are processed in map iteration order.
func (rt *Runtime) Bind(namespace string, values map[string]any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if len(values) == 0 {
		return nil
	}
	for name, v := range values {
		if name == "" {
			return fmt.Errorf("Bind %q: entry name cannot be empty", namespace)
		}
		if v == nil {
			if err := rt.BindValue(namespace, name, nil); err != nil {
				return err
			}
			continue
		}
		rv := reflect.ValueOf(v)
		typ := rv.Type()
		switch typ.Kind() {
		case reflect.Func:
			if err := rt.BindFunc(namespace, name, v); err != nil {
				return err
			}
		case reflect.Struct:
			if typ == reflect.TypeOf(time.Time{}) {
				// time.Time is a value, not a type definition.
				if err := rt.BindValue(namespace, name, v); err != nil {
					return err
				}
			} else {
				if err := rt.BindStruct(namespace, name, v); err != nil {
					return err
				}
			}
		default:
			if err := rt.BindValue(namespace, name, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStructDescriptor(name string, desc schema.ObjectDesc) error {
	if desc.Kind != schema.TypeKindStruct {
		return fmt.Errorf("struct descriptor kind must be struct")
	}
	if desc.Name != "" && desc.Name != name {
		return fmt.Errorf("struct descriptor name %q does not match binding name %q", desc.Name, name)
	}
	if desc.Parent != "" || desc.IsOpen || len(desc.Implements) > 0 || len(desc.Methods) > 0 {
		return fmt.Errorf("struct descriptor must not declare parent/open/interfaces/methods")
	}
	seen := make(map[string]struct{}, len(desc.Fields))
	for _, field := range desc.Fields {
		if field.Name == "" {
			return fmt.Errorf("struct descriptor field name cannot be empty")
		}
		if _, ok := seen[field.Name]; ok {
			return fmt.Errorf("duplicate field %q in struct descriptor", field.Name)
		}
		seen[field.Name] = struct{}{}
		if err := validateTypeDesc(field.Type); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}
	return nil
}

func validateInterfaceDescriptor(name string, desc schema.InterfaceDesc) error {
	if desc.Name != "" && desc.Name != name {
		return fmt.Errorf("interface descriptor name %q does not match binding name %q", desc.Name, name)
	}
	seen := make(map[string]struct{}, len(desc.Methods))
	for _, method := range desc.Methods {
		if method.Name == "" {
			return fmt.Errorf("interface descriptor method name cannot be empty")
		}
		if _, ok := seen[method.Name]; ok {
			return fmt.Errorf("duplicate method %q in interface descriptor", method.Name)
		}
		seen[method.Name] = struct{}{}
		for _, param := range method.Parameters {
			if param.Name == "" {
				return fmt.Errorf("method %q parameter name cannot be empty", method.Name)
			}
			if err := validateTypeDesc(param.Type); err != nil {
				return fmt.Errorf("method %q parameter %q: %w", method.Name, param.Name, err)
			}
		}
		for i, ret := range method.Returns {
			if err := validateTypeDesc(ret); err != nil {
				return fmt.Errorf("method %q return %d: %w", method.Name, i, err)
			}
		}
	}
	return nil
}

func validateTypeDesc(desc schema.TypeDesc) error {
	if desc.Kind == schema.TypeKindInvalid {
		return fmt.Errorf("type descriptor kind cannot be invalid")
	}
	switch desc.Kind {
	case schema.TypeKindArray:
		if desc.Element == nil {
			return fmt.Errorf("array type descriptor must declare element")
		}
		return validateTypeDesc(*desc.Element)
	case schema.TypeKindMap:
		if desc.Key == nil {
			return fmt.Errorf("map type descriptor must declare key")
		}
		if desc.Value == nil {
			return fmt.Errorf("map type descriptor must declare value")
		}
		if err := validateTypeDesc(*desc.Key); err != nil {
			return err
		}
		return validateTypeDesc(*desc.Value)
	case schema.TypeKindStruct, schema.TypeKindClass:
		if desc.Name == "" && desc.ClassName == "" {
			return fmt.Errorf("object type descriptor must declare name or className")
		}
	}
	return nil
}

// registerBoundStructInVM mirrors a host-bound ObjectDesc into the underlying
// VM struct registry so that goStructToVMStruct can encode Go values of the
// matching type when they are passed as Call arguments. Without this bridge,
// only structs declared inside script source are reachable from the VM, and
// host-bound structs would fail with unsupported_vm_argument_type.
//
// Field types are recorded as TypeInvalid (matching compiler.registerStructs);
// the VM accesses fields by name and slot, not by type. Field offset i+1
// matches the layout used by NewStructInstance and getStructFieldByName.
func (rt *Runtime) registerBoundStructInVM(name string, desc schema.ObjectDesc) {
	if rt == nil || rt.evaluator == nil {
		return
	}
	v := rt.evaluator.VM()
	if v == nil {
		return
	}
	fields := make([]vm.FieldDef, len(desc.Fields))
	for i, f := range desc.Fields {
		fields[i] = vm.NewFieldDef(f.Name, vm.TypeInvalid, i+1)
	}
	v.StructReg().RegisterStruct(name, fields)
}

// builderForNamespace returns the pending builder for namespace, creating one
// on first access. Callers must check bindingsCommitted before invoking.
func (rt *Runtime) builderForNamespace(namespace string) *binding.CapabilityBuilder {
	if b, ok := rt.pendingNamespaces[namespace]; ok {
		return b
	}
	b := binding.NewCapability(namespace, "host")
	rt.pendingNamespaces[namespace] = b
	return b
}

// refreshNamespaceSnapshot rebuilds the BoundFunctions / BoundValues snapshot
// for namespace from the current builder state. Call after each successful
// Add* on the pending builder so introspection accessors remain in sync.
//
// Order matters: Build is called first so a builder error short-circuits
// without touching the existing snapshot. Old entries for this namespace are
// then evicted, and the fresh descriptors take their place.
func (rt *Runtime) refreshNamespaceSnapshot(namespace string, builder *binding.CapabilityBuilder) error {
	cap, err := builder.Build()
	if err != nil {
		return err
	}
	prefix := namespace + "/"
	for k := range rt.boundFuncs {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundFuncs, k)
		}
	}
	for k := range rt.boundValues {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundValues, k)
		}
	}
	for _, desc := range cap.Desc.Callables {
		info := callableInfoFromDesc(desc)
		rt.boundFuncs[namespace+"/"+desc.Name] = BoundFunction{
			Namespace: namespace,
			Name:      desc.Name,
			Callable:  namespace + "." + desc.Name,
			Info:      info,
		}
	}
	for _, desc := range cap.Desc.Values {
		rt.boundValues[namespace+"/"+desc.Name] = BoundValue{
			Namespace: namespace,
			Name:      desc.Name,
			Desc:      desc,
		}
	}
	for k := range rt.boundObjects {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundObjects, k)
		}
	}
	for _, obj := range cap.Desc.Objects {
		rt.boundObjects[namespace+"/"+obj.Name] = BoundStruct{
			Namespace:  namespace,
			Name:       obj.Name,
			ObjectDesc: schema.CloneObjectDesc(obj),
		}
	}
	for k := range rt.boundInterfaces {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundInterfaces, k)
		}
	}
	for _, iface := range cap.Desc.Interfaces {
		rt.boundInterfaces[namespace+"/"+iface.Name] = BoundInterface{
			Namespace:     namespace,
			Name:          iface.Name,
			InterfaceDesc: schema.CloneInterfaceDesc(iface),
		}
	}
	for k := range rt.boundTypeAliases {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundTypeAliases, k)
		}
	}
	for name, td := range cap.Desc.TypeAliases {
		rt.boundTypeAliases[namespace+"/"+name] = BoundTypeAlias{
			Namespace: namespace,
			Name:      name,
			TypeDesc:  schema.CloneTypeDesc(td),
		}
	}
	return nil
}

// commitPendingBindings registers all pending namespace builders into the
// underlying ScriptBinding and exposes their callables. Namespaces are
// registered in lexical order so the same binding setup commits in the same
// order across runs (important for reproducible diagnostic output on
// commit-time failures). After commit the binding surface is sealed:
// subsequent Bind* calls return an error.
func (rt *Runtime) commitPendingBindings() error {
	if rt.bindingsCommitted {
		return nil
	}
	rt.bindingsCommitted = true
	namespaces := make([]string, 0, len(rt.pendingNamespaces))
	for ns := range rt.pendingNamespaces {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		builder := rt.pendingNamespaces[namespace]
		cap, err := builder.Build()
		if err != nil {
			return err
		}
		if err := rt.binding.RegisterCapability(cap); err != nil {
			return err
		}
		if len(cap.Desc.Callables) > 0 {
			if err := rt.binding.ExposeCapabilityCallables(namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

// finalizePendingHostInterfaces registers proxy classes and allocates object
// handles for all pending host interface bindings on the current execution VM.
// Must be called after CompileLoweredProgram so that rt.evaluator.VM() is the
// execution VM, not a stale pre-compile instance.
func (rt *Runtime) finalizePendingHostInterfaces() error {
	for _, obj := range rt.hostInterfaceObjects {
		if !obj.Pending {
			continue
		}
		if _, err := rt.registerHostInterfaceObject(obj.Namespace, obj.Name, obj.InterfaceDesc, obj.Target); err != nil {
			return err
		}
	}
	return nil
}

func (rt *Runtime) boundInterfacesInNamespace(namespace string) []BoundInterface {
	if rt == nil || len(rt.boundInterfaces) == 0 {
		return nil
	}
	result := make([]BoundInterface, 0)
	for _, info := range rt.boundInterfaces {
		if info.Namespace == namespace {
			result = append(result, info)
		}
	}
	return result
}

// BoundFunctions returns host functions bound through the Runtime facade.
func (rt *Runtime) BoundFunctions() []BoundFunction {
	if rt == nil || len(rt.boundFuncs) == 0 {
		return nil
	}
	result := make([]BoundFunction, 0, len(rt.boundFuncs))
	for _, info := range rt.boundFuncs {
		result = append(result, info)
	}
	return result
}

// BoundValues returns host values bound through the Runtime facade.
func (rt *Runtime) BoundValues() []BoundValue {
	if rt == nil || len(rt.boundValues) == 0 {
		return nil
	}
	result := make([]BoundValue, 0, len(rt.boundValues))
	for _, info := range rt.boundValues {
		result = append(result, info)
	}
	return result
}

// BoundObjects returns host struct/object types bound through the Runtime
// facade via BindStructDesc or BindStruct. These are compile-time types visible
// to script code through import, not runtime values.
func (rt *Runtime) BoundObjects() []BoundStruct {
	if rt == nil || len(rt.boundObjects) == 0 {
		return nil
	}
	result := make([]BoundStruct, 0, len(rt.boundObjects))
	for _, info := range rt.boundObjects {
		result = append(result, info)
	}
	return result
}

// BoundInterfaces returns host interface types bound through the Runtime facade
// via BindInterfaceDesc. These are compile-time interface types visible to
// script code through import, not runtime values.
func (rt *Runtime) BoundInterfaces() []BoundInterface {
	if rt == nil || len(rt.boundInterfaces) == 0 {
		return nil
	}
	result := make([]BoundInterface, 0, len(rt.boundInterfaces))
	for _, info := range rt.boundInterfaces {
		result = append(result, info)
	}
	return result
}

// BoundTypeAliases returns host type aliases bound through the Runtime facade
// via BindTypeAlias. These are compile-time types visible to script code
// through import, not runtime values.
func (rt *Runtime) BoundTypeAliases() []BoundTypeAlias {
	if rt == nil || len(rt.boundTypeAliases) == 0 {
		return nil
	}
	result := make([]BoundTypeAlias, 0, len(rt.boundTypeAliases))
	for _, info := range rt.boundTypeAliases {
		result = append(result, info)
	}
	return result
}

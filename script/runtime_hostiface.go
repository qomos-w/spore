package script

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// This file holds the Runtime host-interface surface: interface classes,
// instance objects, handle plumbing, and method dispatch.

type hostInterfaceBindingKey struct {
	Namespace string
	Name      string
}

type hostInterfaceClass struct {
	Namespace      string
	Name           string
	InterfaceDesc  schema.InterfaceDesc
	ProxyClassName string
	ClassID        uint32
}

type hostInterfaceObject struct {
	ID             uint64
	Namespace      string
	Name           string
	InterfaceDesc  schema.InterfaceDesc
	Target         any
	ProxyClassName string
	Handle         vm.Handle
	Pending        bool
}

func (rt *Runtime) ensureHostInterfaceClass(namespace, name string, desc schema.InterfaceDesc) error {
	if rt == nil || rt.evaluator == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	key := namespace + "/" + name
	if _, ok := rt.hostInterfaceClasses[key]; ok {
		return nil
	}
	v := rt.evaluator.VM()
	if v == nil {
		return fmt.Errorf("runtime VM is not initialized")
	}
	if v.IfaceReg().GetInterfaceByName(name) == nil {
		methods := make([]vm.InterfaceMethodSig, len(desc.Methods))
		for i, method := range desc.Methods {
			methods[i] = vm.NewInterfaceMethodSig(method.Name, len(method.Parameters))
		}
		v.IfaceReg().RegisterInterface(vm.NewInterfaceDef(name, methods))
	}
	proxyClassName := hostInterfaceProxyClassName(namespace, name)
	class := v.ClassReg().GetClassByName(proxyClassName)
	if class == nil {
		class = vm.NewClass(0, proxyClassName, nil)
		for _, method := range desc.Methods {
			methodName := method.Name
			class.AddMethod(methodName, func(v *vm.VM, receiver vm.Handle, args []vm.Value) vm.Value {
				return rt.invokeHostInterfaceMethod(receiver, methodName, args)
			})
		}
		classID := v.ClassReg().RegisterClass(class)
		class = v.ClassReg().GetClassByName(proxyClassName)
		if class == nil {
			return fmt.Errorf("proxy class %q registration failed", proxyClassName)
		}
		v.IfaceReg().RegisterImplementation(proxyClassName, name)
		rt.hostInterfaceClasses[key] = hostInterfaceClass{
			Namespace:      namespace,
			Name:           name,
			InterfaceDesc:  schema.CloneInterfaceDesc(desc),
			ProxyClassName: proxyClassName,
			ClassID:        classID,
		}
		return nil
	}
	v.IfaceReg().RegisterImplementation(proxyClassName, name)
	rt.hostInterfaceClasses[key] = hostInterfaceClass{
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		ProxyClassName: proxyClassName,
		ClassID:        class.ID(),
	}
	return nil
}

func (rt *Runtime) registerHostInterfaceObject(namespace, name string, desc schema.InterfaceDesc, target any) (hostInterfaceObject, error) {
	if err := rt.ensureHostInterfaceClass(namespace, name, desc); err != nil {
		return hostInterfaceObject{}, err
	}
	classInfo := rt.hostInterfaceClasses[namespace+"/"+name]
	v := rt.evaluator.VM()
	if v == nil {
		return hostInterfaceObject{}, fmt.Errorf("runtime VM is not initialized")
	}
	key := hostInterfaceBindingKey{Namespace: namespace, Name: name}
	if id, ok := rt.hostInterfaceBindings[key]; ok {
		if existing, exists := rt.hostInterfaceObjects[id]; exists {
			if !existing.Pending && existing.Handle != vm.InvalidHandle && reflect.DeepEqual(existing.Target, target) {
				return existing, nil
			}
			// Target differs — create a fresh object instead of overwriting.
			// The old object remains valid via its existing handle.
		}
	}
	if existing, ok := rt.lookupHostInterfaceObjectByTarget(namespace, name, target); ok && !existing.Pending {
		return existing, nil
	}
	handle := v.CreateObject(classInfo.ClassID)
	if handle == vm.InvalidHandle {
		return hostInterfaceObject{}, fmt.Errorf("proxy object allocation failed for %s.%s", namespace, name)
	}
	id := atomic.AddUint64(&rt.nextHostInterfaceID, 1)
	obj := hostInterfaceObject{
		ID:             id,
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		Target:         target,
		ProxyClassName: classInfo.ProxyClassName,
		Handle:         handle,
		Pending:        false,
	}
	rt.hostInterfaceObjects[id] = obj
	rt.hostInterfaceHandles[handle] = id
	rt.hostInterfaceBindings[hostInterfaceBindingKey{Namespace: namespace, Name: name}] = id
	return obj, nil
}

// RegisterHostInterfaceInstance registers an additional runtime instance of a
// previously-bound host interface. The interface class must already exist
// (created by BindInterfaceObject); this call only allocates a new proxy
// object handle for the given target.
func (rt *Runtime) RegisterHostInterfaceInstance(namespace, name string, target any) error {
	if target == nil {
		return fmt.Errorf("interface target cannot be nil")
	}
	classInfo, ok := rt.hostInterfaceClasses[namespace+"/"+name]
	if !ok {
		return fmt.Errorf("interface %s.%s has not been registered", namespace, name)
	}
	_, err := rt.registerHostInterfaceObject(namespace, name, classInfo.InterfaceDesc, target)
	return err
}

func (rt *Runtime) lookupHostInterfaceObjectByTarget(namespace, name string, target any) (hostInterfaceObject, bool) {
	for _, obj := range rt.hostInterfaceObjects {
		if obj.Namespace == namespace && obj.Name == name && reflect.DeepEqual(obj.Target, target) {
			return obj, true
		}
	}
	return hostInterfaceObject{}, false
}

func (rt *Runtime) hostInterfaceObjectForHandle(handle vm.Handle) (hostInterfaceObject, bool) {
	if rt == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := rt.hostInterfaceHandles[handle]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := rt.hostInterfaceObjects[id]
	return obj, ok
}

func (rt *Runtime) lookupBoundHostInterfaceObject(namespace, name string) (hostInterfaceObject, bool) {
	if rt == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := rt.hostInterfaceBindings[hostInterfaceBindingKey{Namespace: namespace, Name: name}]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := rt.hostInterfaceObjects[id]
	return obj, ok
}

// markHostInterfaceRoots is the GC root provider that keeps host interface
// proxy objects live. Proxy objects are allocated in the VM heap via
// CreateObject but are only referenced from the Runtime's handle table, which
// the VM GC cannot see — so without this provider sustained allocation
// reclaims a proxy, the handle is reused, and method dispatch panics with
// "object class not found".
func (rt *Runtime) markHostInterfaceRoots(visit func(vm.Value)) {
	if rt == nil {
		return
	}
	for h := range rt.hostInterfaceHandles {
		visit(vm.EncodeHandle(h))
	}
}

// attachHostIfaceRoots subscribes the host-interface root provider to the
// evaluator's VM lifecycle: it registers on the current VM immediately and
// re-registers on every VM swap (each CompileLoweredProgram builds a fresh
// VM, discarding providers attached to the old one). Wiring this at every
// evaluator construction path (NewRuntimeWith, Clone, Reset) makes
// "host-bound objects stay rooted for the RT lifetime" a construction-time
// guarantee instead of call-site discipline at bind/finalize time.
func (rt *Runtime) attachHostIfaceRoots(eval *bytecode.VMEvaluator) {
	eval.OnVMReplaced(func(v *vm.VM) {
		rt.hostIfaceRootProviderID = v.AddRootProvider(rt.markHostInterfaceRoots)
		rt.hostIfaceRootProviderVM = v
	})
}

func (rt *Runtime) lookupHostInterfaceObjectForValue(value any) (hostInterfaceObject, bool) {
	for _, obj := range rt.hostInterfaceObjects {
		if obj.Pending || obj.Handle == vm.InvalidHandle {
			continue
		}
		if reflect.DeepEqual(obj.Target, value) {
			return obj, true
		}
	}
	return hostInterfaceObject{}, false
}

func (rt *Runtime) hostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	obj, ok := rt.lookupHostInterfaceObjectForValue(value)
	if !ok || obj.Pending || obj.Handle == vm.InvalidHandle {
		return vm.InvalidHandle, false
	}
	return obj.Handle, true
}

func (rt *Runtime) HostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	return rt.hostInterfaceHandleForValue(value)
}

func (rt *Runtime) HostInterfaceObjectForHandle(handle vm.Handle) (any, bool) {
	obj, ok := rt.hostInterfaceObjectForHandle(handle)
	if !ok {
		return nil, false
	}
	return obj.Target, true
}

func hostInterfaceProxyClassName(namespace, name string) string {
	namespace = strings.ReplaceAll(namespace, "/", "__")
	namespace = strings.ReplaceAll(namespace, "\\", "__")
	return "__host_iface__" + namespace + "__" + name
}

func (rt *Runtime) invokeHostInterfaceMethod(receiver vm.Handle, methodName string, args []vm.Value) vm.Value {
	obj, ok := rt.hostInterfaceObjectForHandle(receiver)
	if !ok {
		panic(fmt.Sprintf("host interface receiver not found for handle %v", receiver))
	}
	method, ok := lookupHostMethod(obj.Target, methodName)
	if !ok {
		panic(fmt.Sprintf("host interface method %q not found on %T", methodName, obj.Target))
	}
	goArgs := make([]any, len(args))
	for i, arg := range args {
		goArgs[i] = bytecode.HostVMValueToAny(rt, rt.evaluator.VM(), arg)
	}
	results, err := binding.InvokeGoFunctionForHostProxy(method, goArgs)
	if err != nil {
		panic(err)
	}
	if len(results) == 0 {
		return vm.EncodeHandle(vm.InvalidHandle)
	}
	out, err := bytecode.HostAnyToVMValue(rt, rt.evaluator.VM(), results[0], "vm/evaluator/host-interface-method/"+methodName)
	if err != nil {
		panic(err)
	}
	return out
}

func exportedMethodName(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

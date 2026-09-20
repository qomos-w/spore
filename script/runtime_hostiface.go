package script

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// This file holds the Runtime host-interface surface: interface classes,
// instance objects, handle plumbing, and method dispatch. All proxy state
// lives in the single hostInterfaceLedger (hostiface_ledger.go); the code here
// is a Runtime-level view over it, so Reset/Clone/Close never enumerate or
// rewrite the durable object set.

type hostInterfaceBindingKey struct {
	Namespace string
	Name      string
}

// hostInterfaceObject is the durable record of one host interface proxy target.
// It carries only VM-independent facts — identity (ID), symbol (Namespace /
// Name), shape (InterfaceDesc) and payload (Target). Live state (the proxy
// handle, and whether the proxy is currently registered) is deliberately absent:
// it belongs to the VM-scoped projection in hostInterfaceLedger, so discarding
// the execution VM cannot leave a stale field behind on the durable record.
type hostInterfaceObject struct {
	ID             uint64
	Namespace      string
	Name           string
	InterfaceDesc  schema.InterfaceDesc
	Target         any
	ProxyClassName string
}

// ensureHostInterfaceClass makes the interface definition and proxy class for
// (namespace, name) exist on the current execution VM, and returns the proxy
// class ID and class name. It is idempotent and reads the VM's own class/iface
// registries as the source of truth — there is no separate class index to keep
// in sync or to invalidate, because the registries die with the VM.
func (rt *Runtime) ensureHostInterfaceClass(namespace, name string, desc schema.InterfaceDesc) (uint32, string, error) {
	if rt == nil || rt.evaluator == nil {
		return 0, "", fmt.Errorf("runtime is not initialized")
	}
	v := rt.evaluator.VM()
	if v == nil {
		return 0, "", fmt.Errorf("runtime VM is not initialized")
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
		v.ClassReg().RegisterClass(class)
		class = v.ClassReg().GetClassByName(proxyClassName)
		if class == nil {
			return 0, "", fmt.Errorf("proxy class %q registration failed", proxyClassName)
		}
	}
	v.IfaceReg().RegisterImplementation(proxyClassName, name)
	return class.ID(), proxyClassName, nil
}

// registerHostInterfaceObject guarantees a live proxy on the current execution
// VM for (namespace, name) backed by target, and returns its durable record.
//
// The ledger invariant is "one record per bound (symbol, target)": a matching
// live proxy is reused as-is, a matching pending record is adopted (given a
// handle) rather than duplicated, and only a genuinely new target mints a fresh
// record. That keeps finalize idempotent and leaves no orphaned records behind
// when a proxy is re-registered after a VM swap.
func (rt *Runtime) registerHostInterfaceObject(namespace, name string, desc schema.InterfaceDesc, target any) (hostInterfaceObject, error) {
	classID, proxyClassName, err := rt.ensureHostInterfaceClass(namespace, name, desc)
	if err != nil {
		return hostInterfaceObject{}, err
	}
	v := rt.evaluator.VM()
	if v == nil {
		return hostInterfaceObject{}, fmt.Errorf("runtime VM is not initialized")
	}
	l := &rt.hostIface
	key := hostInterfaceBindingKey{Namespace: namespace, Name: name}

	if existing, ok := l.lookupByTarget(namespace, name, target, true); ok {
		return existing, nil
	}
	obj, ok := l.lookupByTarget(namespace, name, target, false)
	if !ok {
		id := l.allocID()
		obj = hostInterfaceObject{
			ID:             id,
			Namespace:      namespace,
			Name:           name,
			InterfaceDesc:  schema.CloneInterfaceDesc(desc),
			Target:         target,
			ProxyClassName: proxyClassName,
		}
		l.objects[id] = obj
	}
	handle := v.CreateObject(classID)
	if handle == vm.InvalidHandle {
		return hostInterfaceObject{}, fmt.Errorf("proxy object allocation failed for %s.%s", namespace, name)
	}
	l.handles[handle] = obj.ID
	l.bindings[key] = obj.ID
	return obj, nil
}

// RegisterHostInterfaceInstance registers an additional runtime instance of a
// previously-bound host interface. The symbol must already be bound to an
// interface object (created by BindInterfaceObject); this call only allocates a
// new proxy object handle for the given target. The precondition is the durable
// symbol binding, not a VM-scoped class cache, so an instance may also be
// registered after a Reset — the proxy class is re-created on the fresh VM.
func (rt *Runtime) RegisterHostInterfaceInstance(namespace, name string, target any) error {
	if target == nil {
		return fmt.Errorf("interface target cannot be nil")
	}
	bound, ok := rt.hostIface.lookupBound(namespace, name)
	if !ok {
		return fmt.Errorf("interface %s.%s has not been registered", namespace, name)
	}
	_, err := rt.registerHostInterfaceObject(namespace, name, bound.InterfaceDesc, target)
	return err
}

func (rt *Runtime) hostInterfaceObjectForHandle(handle vm.Handle) (hostInterfaceObject, bool) {
	if rt == nil {
		return hostInterfaceObject{}, false
	}
	return rt.hostIface.objectForHandle(handle)
}

// attachHostIfaceRoots subscribes the host-interface root provider to the
// evaluator's VM lifecycle. The subscription fires immediately for the current
// VM and again on every VM swap (each CompileLoweredProgram builds a fresh VM,
// discarding providers attached to the old one), so "host-bound objects stay
// rooted for the RT lifetime" is a construction-time guarantee rather than
// call-site discipline at bind/finalize time. The same callback is the single
// point at which the ledger's VM-scoped projection is invalidated.
func (rt *Runtime) attachHostIfaceRoots(eval *bytecode.VMEvaluator) {
	eval.OnVMReplaced(rt.hostIface.bindVM)
}

func (rt *Runtime) hostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	return rt.hostIface.handleForValue(value)
}

// HostInterfaceHandleForValue returns the live proxy handle bound to value, if
// any. It reports false while the matching proxy is pending (bound but not yet
// registered against the execution VM).
func (rt *Runtime) HostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	return rt.hostInterfaceHandleForValue(value)
}

// HostInterfaceObjectForHandle returns the host target behind a live proxy
// handle.
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

// invokeHostInterfaceMethod dispatches a script-side method call on a bound
// host interface proxy to its Go target.
//
// Failure contract: this code runs inside a vm class-method body, a seam whose
// signature carries a value only, so failures are raised as panics and are
// unwrapped back into structured errors by the bytecode boundary recovery
// (internal/script/bytecode/panic_recovery.go). The two kinds are deliberately
// separated:
//
//   - the receiver and method lookups are invariants (a proxy handle no ledger
//     record backs, or a method the proxy class never registered). They panic
//     with a plain message and surface as the vm_internal_panic code;
//   - a host target that returned an error, or a result the codec cannot turn
//     into a VM value, is recoverable, so it panics with a *bytecode.RuntimeError
//     that keeps its real diagnostic code (native_call_failed, or whatever the
//     host error already carried / unsupported_vm_argument_type).
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
		panic(hostInterfaceCallFailed(methodName, obj.Target, err))
	}
	if len(results) == 0 {
		return vm.EncodeHandle(vm.InvalidHandle)
	}
	out, err := bytecode.HostAnyToVMValue(rt, rt.evaluator.VM(), results[0], "vm/evaluator/host-interface-method/"+methodName)
	if err != nil {
		// HostAnyToVMValue already reports *bytecode.RuntimeError
		// (unsupported_vm_argument_type); raise it as the structured panic the
		// boundary unwraps, so the code survives.
		panic(err)
	}
	return out
}

// hostInterfaceCallFailed wraps a failing bound Go method in the same stable
// code the native-callable path uses (native_call_failed), so a host can branch
// on one code for "a bound Go function raised an error" whether the target was
// reached as a native callable or through an interface proxy. A host error that
// already carries a structured diagnostic code keeps it — the fallback only
// supplies the code for plain errors.
func hostInterfaceCallFailed(methodName string, target any, err error) *bytecode.RuntimeError {
	diag := diagnostics.FromError(err, diagnostics.Descriptor{
		Category: diagnostics.CategoryHost,
		Code:     "native_call_failed",
		Path:     "vm/call/host-interface",
		Message:  fmt.Sprintf("host interface method %s on %T failed: %v", methodName, target, err),
	})
	return &bytecode.RuntimeError{
		Code:     diag.Code,
		Category: diag.Category,
		Path:     diag.Path,
		Message:  diag.Message,
		Stack:    diag.Stack,
		Cause:    diag.Cause,
	}
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

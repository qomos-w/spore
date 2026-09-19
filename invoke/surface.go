package invoke

import "github.com/qomos-w/spore/schema"

// CallableSource is the read/write view of a callable registry that the
// script engine needs: snapshot registered descriptors and register new
// ones produced by compilation.
type CallableSource interface {
	Lookup(name string) (schema.CallableDesc, bool)
	List() []schema.CallableDesc
	Register(desc schema.CallableDesc) error
}

// ExecutorSource is the view of an executable registry that the script
// engine needs: locate adapters, enumerate them, and register adapters
// for newly compiled callables.
type ExecutorSource interface {
	Lookup(name string) (ExecutableAdapter, bool)
	ForEachAdapter(fn func(name string, adapter ExecutableAdapter) bool)
	RegisterAdapter(adapter ExecutableAdapter) error
}

// ScriptSurface is the complete host-binding surface consumed by the
// script engine (bytecode VM evaluator and frontend). It decouples
// internal/script from the binding package: the engine talks to this
// interface, and binding.ScriptBinding satisfies it.
//
// CallableSource/ExecutorSource return nil when the backing registries
// are absent; callers treat nil as "no native surface".
type ScriptSurface interface {
	CallableSource() CallableSource
	ExecutorSource() ExecutorSource
	Invoke(req InvocationRequest) (InvocationOutcome, error)
	DescribeCapabilities() []CapabilityDesc
	FindCapabilityObject(capabilityName, objectName string) (schema.ObjectDesc, bool)
	FindCapabilityInterface(capabilityName, interfaceName string) (schema.InterfaceDesc, bool)
	FindCapabilityTypeAlias(capabilityName, aliasName string) (schema.TypeDesc, bool)
	FindCapabilityValue(capabilityName, valueName string) (CapabilityValueDesc, any, bool)
}

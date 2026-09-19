package binding_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/invoke"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// Compile-time golden pin for the #6 registry/adapter convergence keep-list.
//
// Every declaration in this file pins a public type shape or signature that
// the convergence must not change (see the registry-converge-consumer-confirm
// card, sections A and B). If any of them drifts, this file — and therefore the
// whole binding test package — stops compiling. That is the point: a failure
// here means the red line was crossed, and the fix is to restore the API, not
// to edit this file.
//
// The pins are intentionally written as ordinary package-level declarations so
// they are checked by the compiler without any runtime test run.
// ============================================================================

// --- A. Frozen type shapes ------------------------------------------------

// RegisteredCapability keeps its Desc + Callables fields (Policy/Values too).
var _ = binding.RegisteredCapability{
	Desc:      binding.CapabilityDesc{},
	Policy:    binding.CapabilityPolicy{},
	Callables: map[string]binding.CapabilityCallable{},
	Values:    map[string]any{},
}

// keepListCallable pins the CapabilityCallable method set: Desc + Invoke(ctx, input).
type keepListCallable struct{}

func (keepListCallable) Desc() schema.CallableDesc              { return schema.CallableDesc{} }
func (keepListCallable) Invoke(context.Context, any) (any, error) { return nil, nil }

var _ binding.CapabilityCallable = keepListCallable{}

// ViewProjection keeps its Fields map[string]any field.
var _ = binding.ViewProjection{Fields: map[string]any{}}

// CapabilityDesc / PipelineDesc / PipelineStepDesc / CapabilityValueDesc shapes.
var _ = binding.CapabilityDesc{Name: "", Kind: ""}
var _ = binding.PipelineDesc{Name: ""}
var _ = binding.PipelineStepDesc{Name: ""}
var _ = binding.CapabilityValueDesc{Name: ""}

// Alias identity: binding.X and invoke.X are the same type (v0.3.0 precedent).
var (
	_ invoke.CapabilityDesc      = binding.CapabilityDesc{}
	_ invoke.CapabilityValueDesc = binding.CapabilityValueDesc{}
	_ invoke.PipelineDesc        = binding.PipelineDesc{}
	_ invoke.PipelineStepDesc    = binding.PipelineStepDesc{}

	_ binding.InvocationStage          = invoke.InvocationStageUnary
	_ binding.InvocationResultKind     = invoke.InvocationResultValue
	_ binding.ExecutionState           = invoke.ExecutionState{}
	_ binding.ExecutionBudget          = invoke.ExecutionBudget{}
	_ binding.InvocationErrorDesc      = invoke.InvocationErrorDesc{}
	_ binding.InvocationResultDesc     = invoke.InvocationResultDesc{}
	_ binding.StreamingEventResultDesc = invoke.StreamingEventResultDesc{}
	_ binding.InvocationRequest        = invoke.InvocationRequest{}
	_ binding.InvocationOutcome        = invoke.InvocationOutcome{}
	_ binding.ExecutableAdapter        = invoke.ExecutableAdapter(nil)
	_ binding.ValueCarrier             = invoke.ValueCarrier{}
	_ binding.ValueCarrierKind         = invoke.ValueCarrierNone
	_ binding.PipelineRef              = invoke.PipelineRef{}
	_ binding.ContractError            = invoke.ContractError{}
)

// Invocation-stage / result / carrier constants keep their types.
var (
	_ binding.InvocationStage      = binding.InvocationStageUnary
	_ binding.InvocationStage      = binding.InvocationStageNext
	_ binding.InvocationStage      = binding.InvocationStageFinal
	_ binding.InvocationResultKind = binding.InvocationResultValue
	_ binding.InvocationResultKind = binding.InvocationResultError
	_ binding.ValueCarrierKind     = binding.ValueCarrierNone
	_ binding.ValueCarrierKind     = binding.ValueCarrierScalar
	_ binding.ValueCarrierKind     = binding.ValueCarrierObject
	_ binding.ValueCarrierKind     = binding.ValueCarrierList
	_ string                       = binding.CodeInvalidArgumentCount
	_ string                       = binding.CodeNilArgument
	_ string                       = binding.CodeInvalidArgumentType
	_ string                       = binding.CodeInvalidInvocationStage
	_ string                       = binding.CodeMissingStreamNextSchema
	_ string                       = binding.CodeMissingStreamFinalSchema
	_ string                       = binding.CodeUnsupportedCallableMode
)

// --- B. Frozen registration API -------------------------------------------

var (
	_ func() *binding.ScriptBinding                                      = binding.NewScriptBinding
	_ func(string, string) *binding.CapabilityBuilder                    = binding.NewCapability
	_ func(reflect.Value, []any) ([]any, error)                          = binding.InvokeGoFunctionForHostProxy

	_ func(*binding.ScriptBinding, binding.RegisteredCapability) error = (*binding.ScriptBinding).RegisterCapability
	_ func(*binding.ScriptBinding, string) error                       = (*binding.ScriptBinding).ExposeCapabilityCallables
	_ func(*binding.ScriptBinding, string) (binding.CapabilityDesc, bool) = (*binding.ScriptBinding).DescribeCapability

	_ func(*binding.CapabilityBuilder, string, any) error                 = (*binding.CapabilityBuilder).AddFunction
	_ func(*binding.CapabilityBuilder, string, any) error                 = (*binding.CapabilityBuilder).AddFreeFunction
	_ func(*binding.CapabilityBuilder, string, schema.InterfaceDesc) error = (*binding.CapabilityBuilder).AddInterface
	_ func(*binding.CapabilityBuilder, string, schema.ObjectDesc) error   = (*binding.CapabilityBuilder).AddObject
	_ func(*binding.CapabilityBuilder, string, schema.TypeDesc) error     = (*binding.CapabilityBuilder).AddTypeAlias
	_ func(*binding.CapabilityBuilder, string, any) error                 = (*binding.CapabilityBuilder).AddValue
	_ func(*binding.CapabilityBuilder, string, binding.PipelineDesc) error = (*binding.CapabilityBuilder).AddPipeline
	_ func(*binding.CapabilityBuilder, string, string) *binding.CapabilityBuilder = (*binding.CapabilityBuilder).WithMetadata
	_ func(*binding.CapabilityBuilder) (binding.RegisteredCapability, error) = (*binding.CapabilityBuilder).Build
	_ func(*binding.CapabilityBuilder) *binding.CapabilityBuilder         = (*binding.CapabilityBuilder).Clone
)

// ScriptBinding satisfies the engine-facing invoke.ScriptSurface contract.
var _ invoke.ScriptSurface = (*binding.ScriptBinding)(nil)

// keepListGoldenTest keeps `go test` from reporting the package as having no
// tests relevant to this file; the pins above are the real check.
func TestKeepListGoldenPins(t *testing.T) {
	// Compilation of this file is the assertion. Nothing to run.
	_ = t
}

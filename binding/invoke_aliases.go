package binding

import (
	"context"
	"reflect"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/invoke"
	"github.com/qomos-w/spore/schema"
)

// The invocation contract (stages, requests, outcomes, budgets, capability
// and pipeline descriptors, contract errors, and the validation helpers
// around them) now lives in the leaf package `invoke`. The script engine
// (internal/script) depends only on that contract, not on this package.
//
// Everything below re-exports the contract under the historical binding
// names as type aliases and thin delegations, so existing dependents are
// unaffected: alias-identity means binding.InvocationStage and
// invoke.InvocationStage are the same type.

type (
	InvocationStage          = invoke.InvocationStage
	InvocationResultKind     = invoke.InvocationResultKind
	ExecutionState           = invoke.ExecutionState
	ExecutionBudget          = invoke.ExecutionBudget
	InvocationErrorDesc      = invoke.InvocationErrorDesc
	InvocationResultDesc     = invoke.InvocationResultDesc
	StreamingEventResultDesc = invoke.StreamingEventResultDesc
	InvocationRequest        = invoke.InvocationRequest
	InvocationOutcome        = invoke.InvocationOutcome
	ExecutableAdapter        = invoke.ExecutableAdapter
	ValueCarrier             = invoke.ValueCarrier
	ValueCarrierKind         = invoke.ValueCarrierKind
	CapabilityDesc           = invoke.CapabilityDesc
	CapabilityValueDesc      = invoke.CapabilityValueDesc
	PipelineDesc             = invoke.PipelineDesc
	PipelineStepDesc         = invoke.PipelineStepDesc
	PipelineRef              = invoke.PipelineRef
	ContractError            = invoke.ContractError
)

const (
	InvocationStageUnary         = invoke.InvocationStageUnary
	InvocationStageNext          = invoke.InvocationStageNext
	InvocationStageFinal         = invoke.InvocationStageFinal
	InvocationResultValue        = invoke.InvocationResultValue
	InvocationResultError        = invoke.InvocationResultError
	ValueCarrierNone             = invoke.ValueCarrierNone
	ValueCarrierScalar           = invoke.ValueCarrierScalar
	ValueCarrierObject           = invoke.ValueCarrierObject
	ValueCarrierList             = invoke.ValueCarrierList
	CodeInvalidArgumentCount     = invoke.CodeInvalidArgumentCount
	CodeNilArgument              = invoke.CodeNilArgument
	CodeInvalidArgumentType      = invoke.CodeInvalidArgumentType
	CodeInvalidInvocationStage   = invoke.CodeInvalidInvocationStage
	CodeMissingStreamNextSchema  = invoke.CodeMissingStreamNextSchema
	CodeMissingStreamFinalSchema = invoke.CodeMissingStreamFinalSchema
	CodeUnsupportedCallableMode  = invoke.CodeUnsupportedCallableMode
)

func CheckExecution(ctx context.Context, budget ExecutionBudget, state *ExecutionState) error {
	return invoke.CheckExecution(ctx, budget, state)
}

func DescribeInvocationResult(desc schema.CallableDesc, stage InvocationStage) (InvocationResultDesc, error) {
	return invoke.DescribeInvocationResult(desc, stage)
}

func DescribeStreamingEventResult(desc schema.CallableDesc, event schema.StreamingEventKind) (StreamingEventResultDesc, error) {
	return invoke.DescribeStreamingEventResult(desc, event)
}

func NewInvocationErrorDesc(desc schema.CallableDesc, stage InvocationStage, message string) (InvocationResultDesc, error) {
	return invoke.NewInvocationErrorDesc(desc, stage, message)
}

func NewInvocationErrorDescWithCode(desc schema.CallableDesc, stage InvocationStage, message string, diagnosticCode string) (InvocationResultDesc, error) {
	return invoke.NewInvocationErrorDescWithCode(desc, stage, message, diagnosticCode)
}

func NewInvocationErrorDescWithDiagnostic(desc schema.CallableDesc, stage InvocationStage, diag diagnostics.Descriptor) (InvocationResultDesc, error) {
	return invoke.NewInvocationErrorDescWithDiagnostic(desc, stage, diag)
}

func ValidateInvocationStage(desc schema.CallableDesc, stage InvocationStage) error {
	return invoke.ValidateInvocationStage(desc, stage)
}

func NewValueCarrier(value any) *ValueCarrier {
	return invoke.NewValueCarrier(value)
}

func NewInvocationOutcome(result InvocationResultDesc, payload any) (InvocationOutcome, error) {
	return invoke.NewInvocationOutcome(result, payload)
}

func ValidateInvocationArgs(desc schema.CallableDesc, args []any) error {
	return invoke.ValidateInvocationArgs(desc, args)
}

func JSONTagName(field reflect.StructField) string {
	return invoke.JSONTagName(field)
}

// Compile-time guarantees that binding.ScriptBinding satisfies the engine's
// script surface contract.
var _ invoke.ScriptSurface = (*ScriptBinding)(nil)

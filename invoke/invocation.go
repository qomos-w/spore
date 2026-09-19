package invoke

import (
	"context"
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// InvocationStage classifies the stage of a callable invocation.
// Contract: public semantic contract — invocation stage classification.
type InvocationStage string

// InvocationResultKind classifies whether an invocation produced a value or error.
// Contract: public semantic contract — invocation result classification.
type InvocationResultKind string

type ExecutionState struct {
	Instructions uint64
	HostCalls    uint32
}

func CheckExecution(ctx context.Context, budget ExecutionBudget, state *ExecutionState) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if state != nil {
		if budget.MaxInstructions > 0 && state.Instructions >= budget.MaxInstructions {
			return fmt.Errorf("instruction budget exceeded")
		}
		if budget.MaxHostCalls > 0 && state.HostCalls > budget.MaxHostCalls {
			return fmt.Errorf("host call budget exceeded")
		}
	}
	return nil
}

const (
	InvocationStageUnary InvocationStage = "unary"
	InvocationStageNext  InvocationStage = "next"
	InvocationStageFinal InvocationStage = "final"

	InvocationResultValue InvocationResultKind = "value"
	InvocationResultError InvocationResultKind = "error"
)

// InvocationErrorDesc carries structured context for an invocation failure.
// Contract: public semantic contract — structured invocation error with callable/stage context.
type InvocationErrorDesc struct {
	Callable       string
	Stage          InvocationStage
	Message        string
	DiagnosticCode string // Optional: structured diagnostic code (e.g., script evaluation error category)
	Category       string
	Span           diagnostics.Span
	Path           string
	Identity       string
	Stack          []diagnostics.Frame
	Cause          *diagnostics.Descriptor
}

// InvocationResultDesc describes the outcome shape of an invocation.
// Contract: public semantic contract — structured invocation result with schema context.
type InvocationResultDesc struct {
	Callable string
	Mode     schema.CallableMode
	Stage    InvocationStage
	Kind     InvocationResultKind
	Value    *schema.TypeDesc
	Error    *InvocationErrorDesc
}

// StreamingEventResultDesc describes the schema of a message-style streaming event
// layered over an existing streaming callable invocation stage.
// Contract: public semantic contract — event-to-stage schema projection for streaming callables.
type StreamingEventResultDesc struct {
	Callable string
	Mode     schema.CallableMode
	Stage    InvocationStage
	Event    schema.StreamingEventKind
	Value    *schema.TypeDesc
}

// DescribeInvocationResult produces an InvocationResultDesc for a successful
// invocation of the given callable at the given stage.
// Contract: public semantic contract — successful invocation result construction.
func DescribeInvocationResult(desc schema.CallableDesc, stage InvocationStage) (InvocationResultDesc, error) {
	if err := ValidateInvocationStage(desc, stage); err != nil {
		return InvocationResultDesc{}, err
	}
	mode := desc.Mode
	if mode == "" {
		mode = schema.CallableModeUnary
	}

	value, err := invocationValueForStage(desc, stage)
	if err != nil {
		return InvocationResultDesc{}, err
	}

	return InvocationResultDesc{
		Callable: desc.Name,
		Mode:     mode,
		Stage:    stage,
		Kind:     InvocationResultValue,
		Value:    schema.CloneTypeDescPtr(value),
		Error:    nil,
	}, nil
}

// DescribeStreamingEventResult produces a schema descriptor for a message-style
// streaming event layered over the existing next/final invocation protocol.
// Contract: public semantic contract — message event result schema construction.
func DescribeStreamingEventResult(desc schema.CallableDesc, event schema.StreamingEventKind) (StreamingEventResultDesc, error) {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return StreamingEventResultDesc{}, err
	}
	mode := desc.Mode
	if mode == "" {
		mode = schema.CallableModeUnary
	}
	if mode != schema.CallableModeStreaming {
		return StreamingEventResultDesc{}, fmt.Errorf("callable %q is not streaming", desc.Name)
	}
	if desc.Streaming == nil || desc.Streaming.Message == nil {
		return StreamingEventResultDesc{}, fmt.Errorf("streaming callable %q does not define message protocol", desc.Name)
	}

	var (
		stage InvocationStage
		value *schema.TypeDesc
	)
	switch event {
	case schema.StreamingEventStart:
		stage = InvocationStageNext
		value = desc.Streaming.Message.Start
	case schema.StreamingEventDelta:
		stage = InvocationStageNext
		value = desc.Streaming.Message.Delta
	case schema.StreamingEventEnd:
		stage = InvocationStageFinal
		value = desc.Streaming.Message.End
	default:
		return StreamingEventResultDesc{}, fmt.Errorf("unsupported streaming event kind %q", event)
	}
	if value == nil {
		return StreamingEventResultDesc{}, fmt.Errorf("streaming callable %q does not define %s event schema", desc.Name, event)
	}
	if err := ValidateInvocationStage(desc, stage); err != nil {
		return StreamingEventResultDesc{}, err
	}

	return StreamingEventResultDesc{
		Callable: desc.Name,
		Mode:     mode,
		Stage:    stage,
		Event:    event,
		Value:    schema.CloneTypeDescPtr(value),
	}, nil
}

// NewInvocationErrorDesc produces an InvocationResultDesc for a failed
// invocation of the given callable at the given stage.
// Contract: public semantic contract — error invocation result construction.
func NewInvocationErrorDesc(desc schema.CallableDesc, stage InvocationStage, message string) (InvocationResultDesc, error) {
	return NewInvocationErrorDescWithCode(desc, stage, message, "")
}

// NewInvocationErrorDescWithCode produces an InvocationResultDesc for a failed
// invocation with an optional structured diagnostic code. Script execution
// adapters use this to propagate evaluation-specific diagnostic categories
// (e.g., type mismatch, unsupported term) through the binding seam.
// Contract: public semantic contract — error invocation result with diagnostic code.
func NewInvocationErrorDescWithCode(desc schema.CallableDesc, stage InvocationStage, message string, diagnosticCode string) (InvocationResultDesc, error) {
	return NewInvocationErrorDescWithDiagnostic(desc, stage, diagnostics.Descriptor{Code: diagnosticCode, Message: message})
}

func NewInvocationErrorDescWithDiagnostic(desc schema.CallableDesc, stage InvocationStage, diag diagnostics.Descriptor) (InvocationResultDesc, error) {
	if err := ValidateInvocationStage(desc, stage); err != nil {
		return InvocationResultDesc{}, err
	}
	mode := desc.Mode
	if mode == "" {
		mode = schema.CallableModeUnary
	}
	diag = diagnostics.Normalize(diag)
	return InvocationResultDesc{
		Callable: desc.Name,
		Mode:     mode,
		Stage:    stage,
		Kind:     InvocationResultError,
		Value:    nil,
		Error: &InvocationErrorDesc{
			Callable:       desc.Name,
			Stage:          stage,
			Message:        diag.Message,
			DiagnosticCode: diag.Code,
			Category:       string(diag.Category),
			Span:           diag.Span,
			Path:           diag.Path,
			Identity:       diag.Identity,
			Stack:          append([]diagnostics.Frame(nil), diag.Stack...),
			Cause:          diagnostics.ClonePtr(diag.Cause),
		},
	}, nil
}

// ValidateInvocationStage checks that the invocation stage is valid for the
// given callable descriptor. Exported for use by ExecutableAdapter implementations
// that need to enforce the same contract as the built-in adapters.
// Contract: public semantic contract — invocation stage validation.
func ValidateInvocationStage(desc schema.CallableDesc, stage InvocationStage) error {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return err
	}
	mode := desc.Mode
	if mode == "" {
		mode = schema.CallableModeUnary
	}

	switch mode {
	case schema.CallableModeUnary:
		if stage != InvocationStageUnary {
			return newContractErrorWithTypes(CodeInvalidInvocationStage, desc.Name, stage, "binding/invocation/stage",
				string(InvocationStageUnary), string(stage),
				"unary callable %q does not support %q stage", desc.Name, stage)
		}
		return nil
	case schema.CallableModeStreaming:
		if stage != InvocationStageNext && stage != InvocationStageFinal {
			return newContractErrorWithTypes(CodeInvalidInvocationStage, desc.Name, stage, "binding/invocation/stage",
				"next|final", string(stage),
				"streaming callable %q does not support %q stage", desc.Name, stage)
		}
		if stage == InvocationStageNext && desc.Streaming.Next == nil {
			return newContractErrorWithTypes(CodeMissingStreamNextSchema, desc.Name, stage, "binding/stream/schema/next",
				"next schema", "nil",
				"streaming callable %q does not define next schema", desc.Name)
		}
		if stage == InvocationStageFinal && desc.Streaming.Final == nil {
			return newContractErrorWithTypes(CodeMissingStreamFinalSchema, desc.Name, stage, "binding/stream/schema/final",
				"final schema", "nil",
				"streaming callable %q does not define final schema", desc.Name)
		}
		return nil
	default:
		return newContractErrorWithTypes(CodeUnsupportedCallableMode, desc.Name, stage, "binding/callable/mode",
			"unary|streaming", string(desc.Mode),
			"unsupported callable mode %q", desc.Mode)
	}
}

func invocationValueForStage(desc schema.CallableDesc, stage InvocationStage) (*schema.TypeDesc, error) {
	if err := ValidateInvocationStage(desc, stage); err != nil {
		return nil, err
	}
	mode := desc.Mode
	if mode == "" {
		mode = schema.CallableModeUnary
	}

	switch mode {
	case schema.CallableModeUnary:
		if len(desc.Returns) == 0 {
			return nil, nil
		}
		value := schema.CloneTypeDesc(desc.Returns[0])
		return &value, nil
	case schema.CallableModeStreaming:
		if stage == InvocationStageNext {
			return schema.CloneTypeDescPtr(desc.Streaming.Next), nil
		}
		return schema.CloneTypeDescPtr(desc.Streaming.Final), nil
	default:
		return nil, fmt.Errorf("unsupported callable mode %q", desc.Mode)
	}
}

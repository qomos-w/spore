package invoke

import (
	"context"
	"reflect"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// InvocationRequest is a single request to invoke a callable.
// Contract: public semantic contract — invocation request shape.
type InvocationRequest struct {
	Callable string
	Stage    InvocationStage
	Args     []any
	Context  context.Context
	Budget   ExecutionBudget
}

// ExecutableAdapter is the seam between a schema-described callable and
// its executable implementation. Implementations must return a Callable()
// descriptor that matches the registered callable.
// SPI: pluggable callable execution backend — alternative implementations
// can be provided to Registry.RegisterAdapter.
type ExecutableAdapter interface {
	Callable() schema.CallableDesc
	Invoke(req InvocationRequest) (InvocationOutcome, error)
}

// JSONTagName extracts the JSON field name from a struct field tag.
// Returns "-" if the field should be ignored, or the field name if no tag.
// This is the canonical field-name projection contract shared between input
// binding and output projection layers.
func JSONTagName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	if tag == "-" {
		return "-"
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		tag = tag[:idx]
	}
	if tag == "" {
		return field.Name
	}
	return tag
}

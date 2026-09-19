package diagnostics

import (
	"encoding/json"
	"fmt"
	"strings"
)

type MultiError struct {
	errors   []error
	fallback Descriptor
}

func NewMultiError(errors []error) error {
	return NewMultiErrorWithFallback(errors, Descriptor{Category: CategoryLoad, Path: "frontend/diagnostics"})
}

func NewMultiErrorWithFallback(errors []error, fallback Descriptor) error {
	if len(errors) == 0 {
		return nil
	}
	if len(errors) == 1 {
		return errors[0]
	}
	copied := append([]error(nil), errors...)
	return &MultiError{errors: copied, fallback: Normalize(fallback)}
}

func (e *MultiError) Error() string {
	if e == nil || len(e.errors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e.errors))
	for _, err := range e.errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return fmt.Sprintf("%d diagnostics: %s", len(parts), strings.Join(parts, "; "))
}

func (e *MultiError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return append([]error(nil), e.errors...)
}

func (e *MultiError) DiagnosticCode() string { return "multiple_diagnostics" }
func (e *MultiError) DiagnosticCategory() Category {
	if e == nil {
		return CategoryLoad
	}
	if e.fallback.Category != "" {
		return e.fallback.Category
	}
	return CategoryLoad
}
func (e *MultiError) DiagnosticPath() string {
	if e == nil {
		return "frontend/diagnostics"
	}
	if e.fallback.Path != "" {
		return e.fallback.Path
	}
	return "frontend/diagnostics"
}
func (e *MultiError) DiagnosticCause() *Descriptor {
	if e == nil || len(e.errors) == 0 {
		return nil
	}
	var head *Descriptor
	for i := len(e.errors) - 1; i >= 0; i-- {
		if e.errors[i] == nil {
			continue
		}
		diag := FromError(e.errors[i], e.fallback)
		diag.Cause = head
		cloned := Clone(diag)
		head = &cloned
	}
	return head
}

func (e *MultiError) Diagnostics() []Descriptor {
	if e == nil || len(e.errors) == 0 {
		return nil
	}
	result := make([]Descriptor, 0, len(e.errors))
	for _, err := range e.errors {
		if err == nil {
			continue
		}
		result = append(result, FromError(err, e.fallback))
	}
	return result
}

func (e *MultiError) Envelope() []map[string]any {
	if e == nil || len(e.errors) == 0 {
		return nil
	}
	bundle := make([]map[string]any, 0, len(e.errors))
	for _, diag := range e.Diagnostics() {
		bundle = append(bundle, diag.Envelope())
	}
	return bundle
}

func (e *MultiError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Envelope())
}

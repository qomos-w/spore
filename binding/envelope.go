package binding

import "github.com/qomos-w/spore/diagnostics"

func (e InvocationErrorDesc) Envelope() map[string]any {
	return diagnostics.Descriptor{
		Category: diagnostics.Category(e.Category),
		Code:     e.DiagnosticCode,
		Message:  e.Message,
		Callable: e.Callable,
		Stage:    string(e.Stage),
		Span:     e.Span,
		Path:     e.Path,
		Identity: e.Identity,
		Stack:    append([]diagnostics.Frame(nil), e.Stack...),
		Cause:    diagnostics.ClonePtr(e.Cause),
	}.Envelope()
}

func (r InvocationResultDesc) Envelope() map[string]any {
	m := map[string]any{
		"callable": r.Callable,
		"mode":     string(r.Mode),
		"stage":    string(r.Stage),
		"kind":     string(r.Kind),
	}
	if r.Value != nil {
		m["value"] = r.Value.Name
	}
	if r.Error != nil {
		m["error"] = r.Error.Envelope()
	}
	return m
}

package diagnostics

type coder interface {
	DiagnosticCode() string
}

type categorizer interface {
	DiagnosticCategory() Category
}

type spanner interface {
	DiagnosticSpan() Span
}

type pather interface {
	DiagnosticPath() string
}

type stacker interface {
	DiagnosticStack() []Frame
}

type causer interface {
	DiagnosticCause() *Descriptor
}

type expecteder interface {
	DiagnosticExpected() string
}

type actualer interface {
	DiagnosticActual() string
}

type hinter interface {
	DiagnosticHint() string
}

// FromError converts an arbitrary Go error into a normalized diagnostic descriptor.
// The fallback descriptor supplies canonical context when the error itself does not.
func FromError(err error, fallback Descriptor) Descriptor {
	diag := Normalize(fallback)
	if err == nil {
		return diag
	}
	if diag.Message == "" {
		diag.Message = err.Error()
	}
	if c, ok := err.(coder); ok {
		diag.Code = c.DiagnosticCode()
	}
	if c, ok := err.(categorizer); ok {
		diag.Category = c.DiagnosticCategory()
	}
	if s, ok := err.(spanner); ok {
		diag.Span = s.DiagnosticSpan()
	}
	if p, ok := err.(pather); ok {
		diag.Path = p.DiagnosticPath()
	}
	if s, ok := err.(stacker); ok {
		if stack := s.DiagnosticStack(); len(stack) > 0 {
			diag.Stack = append([]Frame(nil), stack...)
		}
	}
	if c, ok := err.(causer); ok {
		diag.Cause = ClonePtr(c.DiagnosticCause())
	}
	if e, ok := err.(expecteder); ok {
		diag.Expected = e.DiagnosticExpected()
	}
	if a, ok := err.(actualer); ok {
		diag.Actual = a.DiagnosticActual()
	}
	if diag.Hint == "" {
		diag.Hint = HintFor(err)
	}
	return Normalize(diag)
}

package diagnostics

// Clone returns a deep copy of the diagnostic descriptor.
func Clone(diag Descriptor) Descriptor {
	cloned := diag
	if len(diag.Stack) > 0 {
		cloned.Stack = append([]Frame(nil), diag.Stack...)
	}
	if diag.Cause != nil {
		cause := Clone(*diag.Cause)
		cloned.Cause = &cause
	}
	return cloned
}

// ClonePtr returns a deep copy of the diagnostic descriptor pointer.
func ClonePtr(diag *Descriptor) *Descriptor {
	if diag == nil {
		return nil
	}
	cloned := Clone(*diag)
	return &cloned
}

// Normalize applies stable defaulting for category and keeps copies isolated.
func Normalize(diag Descriptor) Descriptor {
	cloned := Clone(diag)
	if cloned.Category == "" {
		cloned.Category = CategoryRuntime
	}
	return cloned
}

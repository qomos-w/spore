package diagnostics

// Envelope returns a transport/tool-friendly map representation with stable keys.
func (d Descriptor) Envelope() map[string]any {
	normalized := Normalize(d)
	return descriptorToMap(normalized)
}

func descriptorToMap(d Descriptor) map[string]any {
	m := map[string]any{}
	if d.Category != "" {
		m["category"] = string(d.Category)
	}
	if d.Code != "" {
		m["code"] = d.Code
	}
	if d.Message != "" {
		m["message"] = d.Message
	}
	if d.Callable != "" {
		m["callable"] = d.Callable
	}
	if d.Stage != "" {
		m["stage"] = d.Stage
	}
	if span := spanToMap(d.Span); span != nil {
		m["span"] = span
	}
	if d.Path != "" {
		m["path"] = d.Path
	}
	if d.Identity != "" {
		m["identity"] = d.Identity
	}
	if d.Expected != "" {
		m["expected"] = d.Expected
	}
	if d.Actual != "" {
		m["actual"] = d.Actual
	}
	if d.Hint != "" {
		m["hint"] = d.Hint
	}
	if len(d.Stack) > 0 {
		stack := make([]map[string]any, 0, len(d.Stack))
		for _, frame := range d.Stack {
			stack = append(stack, frameToMap(frame))
		}
		m["stack"] = stack
	}
	if d.Cause != nil {
		m["cause"] = descriptorToMap(*d.Cause)
	}
	return m
}

func frameToMap(frame Frame) map[string]any {
	m := map[string]any{}
	if frame.Callable != "" {
		m["callable"] = frame.Callable
	}
	if frame.Stage != "" {
		m["stage"] = frame.Stage
	}
	if span := spanToMap(frame.Span); span != nil {
		m["span"] = span
	}
	return m
}

func spanToMap(span Span) map[string]any {
	m := map[string]any{}
	if start := positionToMap(span.Start); start != nil {
		m["start"] = start
	}
	if end := positionToMap(span.End); end != nil {
		m["end"] = end
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func positionToMap(pos Position) map[string]any {
	if pos.Line == 0 && pos.Column == 0 {
		return nil
	}
	m := map[string]any{}
	if pos.Line != 0 {
		m["line"] = pos.Line
	}
	if pos.Column != 0 {
		m["column"] = pos.Column
	}
	return m
}

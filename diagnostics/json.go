package diagnostics

import "encoding/json"

type jsonPosition struct {
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
}

type jsonSpan struct {
	Start *jsonPosition `json:"start,omitempty"`
	End   *jsonPosition `json:"end,omitempty"`
}

type jsonFrame struct {
	Callable string    `json:"callable,omitempty"`
	Stage    string    `json:"stage,omitempty"`
	Span     *jsonSpan `json:"span,omitempty"`
}

type jsonDescriptor struct {
	Category string           `json:"category,omitempty"`
	Code     string           `json:"code,omitempty"`
	Message  string           `json:"message,omitempty"`
	Callable string           `json:"callable,omitempty"`
	Stage    string           `json:"stage,omitempty"`
	Span     *jsonSpan        `json:"span,omitempty"`
	Path     string           `json:"path,omitempty"`
	Identity string           `json:"identity,omitempty"`
	Expected string           `json:"expected,omitempty"`
	Actual   string           `json:"actual,omitempty"`
	Hint     string           `json:"hint,omitempty"`
	Stack    []jsonFrame      `json:"stack,omitempty"`
	Cause    *jsonDescriptor  `json:"cause,omitempty"`
}

func (d Descriptor) MarshalJSON() ([]byte, error) {
	normalized := Normalize(d)
	enc := toJSONDescriptor(normalized)
	return json.Marshal(enc)
}

func toJSONDescriptor(d Descriptor) jsonDescriptor {
	enc := jsonDescriptor{
		Category: string(d.Category),
		Code:     d.Code,
		Message:  d.Message,
		Callable: d.Callable,
		Stage:    d.Stage,
		Path:     d.Path,
		Identity: d.Identity,
		Expected: d.Expected,
		Actual:   d.Actual,
		Hint:     d.Hint,
	}
	if span := toJSONSpan(d.Span); span != nil {
		enc.Span = span
	}
	if len(d.Stack) > 0 {
		enc.Stack = make([]jsonFrame, 0, len(d.Stack))
		for _, frame := range d.Stack {
			enc.Stack = append(enc.Stack, toJSONFrame(frame))
		}
	}
	if d.Cause != nil {
		cause := toJSONDescriptor(*d.Cause)
		enc.Cause = &cause
	}
	return enc
}

func toJSONFrame(frame Frame) jsonFrame {
	enc := jsonFrame{Callable: frame.Callable, Stage: frame.Stage}
	if span := toJSONSpan(frame.Span); span != nil {
		enc.Span = span
	}
	return enc
}

func toJSONSpan(span Span) *jsonSpan {
	start := toJSONPosition(span.Start)
	end := toJSONPosition(span.End)
	if start == nil && end == nil {
		return nil
	}
	return &jsonSpan{Start: start, End: end}
}

func toJSONPosition(pos Position) *jsonPosition {
	if pos.Line == 0 && pos.Column == 0 {
		return nil
	}
	return &jsonPosition{Line: pos.Line, Column: pos.Column}
}

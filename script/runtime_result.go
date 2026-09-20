package script

import (
	"fmt"

	"github.com/qomos-w/spore/internal/script/bytecode"
)

// This file holds Result, the public value surface returned from Runtime calls,
// and its host-facing accessors.

// Result is the public value surface returned from Runtime calls.
//
// Default recommendation: first call Unwrap when you only care whether the
// invocation succeeded, then DecodeInto the value. Use the tri-state helpers
// (Ok/Void/Error) only when you explicitly need to distinguish void success
// from runtime failure.
//
// Observable states:
//
//   - Success with a value: Error is nil, Value is non-nil. The script
//     returned a regular result.
//   - Success without a value: Error is nil, Value is nil. The script
//     returned void (or returned an explicit nil/null — these are
//     indistinguishable at this layer).
//   - Failure: Error is non-nil. The script raised a runtime error; Value
//     should be ignored.
//
// Returned err from Call/CallNext/CallFinal is a different channel: it signals
// host-side or invocation-pipeline failure before a successful script result
// was produced.
type Result struct {
	Value any
	Error *RuntimeError
	err   error // host-side error captured for Unwrap
}

// Ok reports whether the call succeeded (Error is nil). It is true for both
// the "value returned" and "void return" cases. Hosts that need to
// distinguish those two cases additionally check Void or Value != nil.
func (r Result) Ok() bool { return r.Error == nil }

// Void reports whether the call succeeded but produced no value. It is true
// only when Error is nil and Value is nil. Returning false does NOT prove a
// runtime error — also check Ok.
func (r Result) Void() bool { return r.Error == nil && r.Value == nil }

// DecodeInto copies the result value into dst when the types are compatible.
// Supported destinations: *any, *string, *bool, *int (and its sized variants),
// *uint (and its sized variants), *float32, *float64, *[]any, *map[string]any.
// Numeric values are converted with Go's standard cast rules; mismatches and
// unsupported targets return a non-nil error. If the result already carries a
// runtime error, that error is surfaced and dst is left untouched.
func (r Result) DecodeInto(dst any) error {
	if r.Error != nil {
		return r.Error
	}
	if dst == nil {
		return fmt.Errorf("decode target cannot be nil")
	}
	return bytecode.DecodeHostValue(dst, r.Value)
}

// Unwrap returns a single error that merges both host-side failure and script
// runtime failure (Result.Error). This is the default convenience path for
// callers that only care whether the invocation succeeded, not which layer
// failed.
//
// Usage:
//
//	result, err := rt.Call("greet")
//	if err != nil {
//	    return err
//	}
//	if err := result.Unwrap(); err != nil {
//	    return err
//	}
//	var s string
//	result.DecodeInto(&s)
//
// Unwrap returns nil when the call succeeded (Ok is true). It never loses
// structured diagnostic information: if the error came from script code, it is
// still a *RuntimeError with Code/Category/Stack fields.
func (r Result) Unwrap() error {
	if r.Error != nil {
		return r.Error
	}
	if r.err != nil {
		return r.err
	}
	return nil
}

// AsString returns the result value as a string. It is a convenience over
// DecodeInto for callers that only need a string and don't want to declare a
// variable and pass its address.
func (r Result) AsString() (string, error) {
	var s string
	if err := r.DecodeInto(&s); err != nil {
		return "", err
	}
	return s, nil
}

// AsInt returns the result value as an int. Numeric values are converted with
// Go's standard int64→int cast rules.
func (r Result) AsInt() (int, error) {
	var n int
	if err := r.DecodeInto(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// AsSlice returns the result value as a []any slice.
func (r Result) AsSlice() ([]any, error) {
	var s []any
	if err := r.DecodeInto(&s); err != nil {
		return nil, err
	}
	return s, nil
}

// AsMap returns the result value as a map[string]any.
func (r Result) AsMap() (map[string]any, error) {
	var m map[string]any
	if err := r.DecodeInto(&m); err != nil {
		return nil, err
	}
	return m, nil
}

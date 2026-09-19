package transport

import (
	"errors"
	"fmt"
	"testing"

	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

func TestJSONPointerToDottedPath(t *testing.T) {
	cases := []struct {
		ptr  jsontext.Pointer
		want string
	}{
		{ptr: "", want: ""},                     // top-level failure
		{ptr: "/Level", want: ".Level"},         // simple field
		{ptr: "/Sub/Level", want: ".Sub.Level"}, // nested field
		{ptr: "/Items/0", want: ".Items[0]"},    // array index
		{ptr: "/Items/12/Tags/3", want: ".Items[12].Tags[3]"},
		{ptr: "/a~1b", want: ".a/b"}, // RFC 6901 ~1 → /
		{ptr: "/a~0b", want: ".a~b"}, // RFC 6901 ~0 → ~
		{ptr: "/~01", want: ".~1"},   // escaped ~ followed by literal 1
		{ptr: "/007", want: ".007"},  // non-canonical index is a field name
		{ptr: "/+5", want: ".+5"},    // signed token is a field name
		{ptr: "/-0", want: ".-0"},    // non-canonical zero is a field name
		{ptr: "/0", want: "[0]"},     // canonical index at depth 1
		{ptr: "/Data", want: ".Data"},
	}
	for _, tc := range cases {
		if got := jsonPointerToDottedPath(tc.ptr); got != tc.want {
			t.Errorf("jsonPointerToDottedPath(%q) = %q, want %q", tc.ptr, got, tc.want)
		}
	}
}

func TestJSONErrorPath(t *testing.T) {
	// No pointer context: plain wrapped error.
	if got := jsonErrorPath(fmt.Errorf("plain: %w", errors.New("boom"))); got != "" {
		t.Errorf("plain error: got path %q, want empty", got)
	}

	// Semantic error with nested pointer, wrapped through fmt.Errorf.
	var sub struct{ Level int32 }
	semErr := jsonv2.Unmarshal([]byte(`{"Level":"x"}`), &sub)
	if semErr == nil {
		t.Fatal("expected unmarshal error")
	}
	if got := jsonErrorPath(fmt.Errorf("wrapped: %w", semErr)); got != ".Level" {
		t.Errorf("wrapped semantic error: got path %q, want .Level", got)
	}

	// Syntactic error (duplicate name) with pointer, wrapped.
	var m map[string]any
	synErr := jsonv2.Unmarshal([]byte(`{"a":1,"a":2}`), &m)
	if synErr == nil {
		t.Fatal("expected duplicate-name error")
	}
	if got := jsonErrorPath(fmt.Errorf("wrapped: %w", synErr)); got != ".a" {
		t.Errorf("wrapped syntactic error: got path %q, want .a", got)
	}

	// Top-level scalar failure: pointer is empty, path stays "".
	var n int32
	topErr := jsonv2.Unmarshal([]byte(`"x"`), &n)
	if topErr == nil {
		t.Fatal("expected scalar mismatch error")
	}
	if got := jsonErrorPath(topErr); got != "" {
		t.Errorf("top-level failure: got path %q, want empty", got)
	}
}

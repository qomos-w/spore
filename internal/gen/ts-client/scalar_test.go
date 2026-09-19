package tsclient

import "testing"

// TestRenderScalar_SharedTable locks the TypeScript scalar mapping for the
// client surface. It mirrors internal/gen/ts's TestRenderScalar: both
// generators resolve through internal/gen/common's single scalar table, and
// this test keeps the client side from drifting away from it.
func TestRenderScalar_SharedTable(t *testing.T) {
	cases := map[string]string{
		"bool":    "boolean",
		"int":     "number",
		"int8":    "number",
		"int16":   "number",
		"int64":   "number",
		"uint32":  "number",
		"float32": "number",
		"float64": "number",
		"string":  "string",
		"bytes":   "Uint8Array",
		"any":     "unknown",
		"null":    "null",
		"":        "unknown",
		"weird":   "weird",
	}
	for in, want := range cases {
		if got := renderScalar(in); got != want {
			t.Errorf("renderScalar(%q) = %q, want %q", in, got, want)
		}
	}
}

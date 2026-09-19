package common

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestVisibility_StringRoundTrip(t *testing.T) {
	for _, v := range []Visibility{
		VisibilityInternal, VisibilityPublic, VisibilityAdmin, VisibilityDiagnostic,
	} {
		got, ok := ParseVisibility(v.String())
		if !ok || got != v {
			t.Errorf("roundtrip failed for %v: got %v ok=%v", v, got, ok)
		}
	}
	if _, ok := ParseVisibility("nonsense"); ok {
		t.Errorf("expected ParseVisibility(\"nonsense\") to return ok=false")
	}
}

func TestShouldEmit_DefaultsToPublic(t *testing.T) {
	if !ShouldEmit(nil, VisibilityPublic) {
		t.Errorf("empty filter must include VisibilityPublic")
	}
	for _, v := range []Visibility{VisibilityInternal, VisibilityAdmin, VisibilityDiagnostic} {
		if ShouldEmit(nil, v) {
			t.Errorf("empty filter must exclude %v", v)
		}
	}

	var opts Options
	if !opts.ShouldEmit(VisibilityPublic) || opts.ShouldEmit(VisibilityAdmin) {
		t.Errorf("Options.ShouldEmit must mirror ShouldEmit with the empty filter")
	}
}

func TestShouldEmit_FiltersByList(t *testing.T) {
	opts := Options{Visibilities: []Visibility{VisibilityPublic, VisibilityAdmin}}
	want := map[Visibility]bool{
		VisibilityInternal:   false,
		VisibilityPublic:     true,
		VisibilityAdmin:      true,
		VisibilityDiagnostic: false,
	}
	for v, expected := range want {
		if got := opts.ShouldEmit(v); got != expected {
			t.Errorf("ShouldEmit(%v) = %v, want %v", v, got, expected)
		}
	}
}

func TestValidateCallables_Accepts(t *testing.T) {
	chunk := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	cases := map[string][]NamedCallableDesc{
		"nil": nil,
		"unary": {{
			Namespace: "auth", Name: "login", Mode: schema.CallableModeUnary,
		}},
		"streaming with chunk": {{
			Namespace: "auth", Name: "tail", Mode: schema.CallableModeStreaming,
			ChunkSchemaID: 2, Chunk: &chunk,
		}},
		"same name different namespace": {
			{Namespace: "auth", Name: "login", Mode: schema.CallableModeUnary},
			{Namespace: "billing", Name: "login", Mode: schema.CallableModeUnary},
		},
	}
	for name, callables := range cases {
		if err := ValidateCallables(callables, ValidateOptions{}); err != nil {
			t.Errorf("case %q: unexpected rejection: %v", name, err)
		}
	}
}

func TestValidateCallables_Rejects(t *testing.T) {
	chunk := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	cases := map[string][]NamedCallableDesc{
		"empty namespace": {{
			Namespace: "", Name: "x", Mode: schema.CallableModeUnary,
		}},
		"empty name": {{
			Namespace: "x", Name: "", Mode: schema.CallableModeUnary,
		}},
		"unary with chunk schema id": {{
			Namespace: "x", Name: "x", Mode: schema.CallableModeUnary, ChunkSchemaID: 2,
		}},
		"unary with chunk desc": {{
			Namespace: "x", Name: "x", Mode: schema.CallableModeUnary, Chunk: &chunk,
		}},
		"streaming without chunk id": {{
			Namespace: "x", Name: "x", Mode: schema.CallableModeStreaming, Chunk: &chunk,
		}},
		"streaming without chunk desc": {{
			Namespace: "x", Name: "x", Mode: schema.CallableModeStreaming, ChunkSchemaID: 2,
		}},
		"unknown mode": {{
			Namespace: "x", Name: "x", Mode: schema.CallableMode("psychic"),
		}},
		"duplicate name": {
			{Namespace: "x", Name: "dup", Mode: schema.CallableModeUnary},
			{Namespace: "x", Name: "dup", Mode: schema.CallableModeUnary},
		},
	}
	for name, callables := range cases {
		if err := ValidateCallables(callables, ValidateOptions{}); err == nil {
			t.Errorf("case %q: expected rejection, got nil", name)
		}
	}
}

// TestValidateCallables_RejectStreaming locks the hook that lets go-server keep
// its MVP restriction without duplicating the shared validator.
func TestValidateCallables_RejectStreaming(t *testing.T) {
	var seenNS, seenName string
	opts := ValidateOptions{RejectStreaming: func(ns, name string) error {
		seenNS, seenName = ns, name
		return errStreamingUnsupported
	}}

	streaming := []NamedCallableDesc{{
		Namespace: "exp09", Name: "tail",
		Mode: schema.CallableModeStreaming, ChunkSchemaID: 2,
	}}
	err := ValidateCallables(streaming, opts)
	if err != errStreamingUnsupported {
		t.Fatalf("expected hook error, got %v", err)
	}
	if seenNS != "exp09" || seenName != "tail" {
		t.Errorf("hook saw (%q, %q), want (exp09, tail)", seenNS, seenName)
	}

	// The hook must only intercept streaming callables.
	unary := []NamedCallableDesc{{Namespace: "exp09", Name: "ping", Mode: schema.CallableModeUnary}}
	if err := ValidateCallables(unary, opts); err != nil {
		t.Errorf("unary callable must pass the streaming hook: %v", err)
	}
}

func TestWriteHeader(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"empty writes nothing", "", ""},
		{"adds separator newline", "// x", "// x\n\n"},
		{"does not double the trailing newline", "// x\n", "// x\n\n"},
		{"multi-line", "// a\n// b", "// a\n// b\n\n"},
	}
	for _, c := range cases {
		var b strings.Builder
		WriteHeader(&b, c.header)
		if got := b.String(); got != c.want {
			t.Errorf("case %q: got %q, want %q", c.name, got, c.want)
		}
	}
}

// errStreamingUnsupported is a sentinel identity for the RejectStreaming hook.
var errStreamingUnsupported = errSentinel("streaming unsupported")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

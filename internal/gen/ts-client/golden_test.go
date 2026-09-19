package tsclient

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// updateGolden controls whether the golden_test rewrites testdata/golden
// from the current Generate output. Run
// `go test ./internal/gen/ts-client -update` after intentional rendering
// changes; commit the resulting files.
var updateGolden = flag.Bool("update", false, "rewrite testdata/golden")

// TestGenerate_Golden exercises Generate end-to-end with a representative
// auth namespace (mixed unary + streaming, plus an admin-only callable
// that should be filtered out under the public profile) and a billing
// namespace whose only callable returns void.
func TestGenerate_Golden(t *testing.T) {
	callables := []ts.NamedCallableDesc{
		// Streaming callable returning a struct final.
		{
			Namespace:     "auth",
			Name:          "tail_logins",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeStreaming,
			ReqSchemaID:   1,
			ChunkSchemaID: 2,
			FinalSchemaID: 3,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "TailLoginsReq", ClassName: "TailLoginsReq"},
			Chunk:         &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginEvent", ClassName: "LoginEvent"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "TailLoginsFinal", ClassName: "TailLoginsFinal"},
		},
		// Unary callable.
		{
			Namespace:     "auth",
			Name:          "lookup_user",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   4,
			FinalSchemaID: 5,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LookupUserReq", ClassName: "LookupUserReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LookupUserResp", ClassName: "LookupUserResp"},
		},
		// Admin-only callable — should NOT appear in public-filtered output.
		{
			Namespace:     "auth",
			Name:          "admin_drop_session",
			Visibility:    ts.VisibilityAdmin,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   90,
			FinalSchemaID: 91,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "AdminDropSessionReq", ClassName: "AdminDropSessionReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindVoid},
		},
		// Streaming callable whose final is void — exercises the void-final
		// path (no ./types.js import needed for the final type).
		{
			Namespace:     "billing",
			Name:          "tail_invoices",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeStreaming,
			ReqSchemaID:   100,
			ChunkSchemaID: 101,
			FinalSchemaID: 102,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "TailInvoicesReq", ClassName: "TailInvoicesReq"},
			Chunk:         &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Invoice", ClassName: "Invoice"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindVoid},
		},
		// Unary callable whose req and final are scalar types — exercises
		// the no-import path: nothing pulled from ./types.js because both
		// signatures are inline scalars.
		{
			Namespace:     "billing",
			Name:          "next_invoice_id",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   200,
			FinalSchemaID: 201,
			Req:           schema.TypeDesc{Kind: schema.TypeKindVoid},
			Final:         schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		},
	}

	files, err := Generate(callables, ts.Options{
		Visibilities: []ts.Visibility{ts.VisibilityPublic},
		Header:       "// AUTO-GENERATED — DO NOT EDIT",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	goldenDir := filepath.Join("testdata", "golden")
	if *updateGolden {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatal(err)
		}
		for path, content := range files {
			full := filepath.Join(goldenDir, path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("client golden written to %s", goldenDir)
		return
	}

	for path, content := range files {
		want, err := os.ReadFile(filepath.Join(goldenDir, path))
		if err != nil {
			t.Errorf("missing golden %s: %v", path, err)
			continue
		}
		if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(content, "\r\n", "\n") {
			t.Errorf("golden mismatch for %s:\n--- want\n%s\n--- got\n%s", path, want, content)
		}
	}

	// Admin-only callable must not leak.
	for path, content := range files {
		if got := string(content); contains(got, "admin_drop_session") {
			t.Errorf("admin-only callable leaked into %s:\n%s", path, got)
		}
	}
}

// TestGenerate_RejectsInvalidCallables locks the validateCallables contract.
func TestGenerate_RejectsInvalidCallables(t *testing.T) {
	cases := map[string][]ts.NamedCallableDesc{
		"empty namespace": {{
			Namespace: "", Name: "x", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
		}},
		"empty name": {{
			Namespace: "x", Name: "", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
		}},
		"streaming missing chunk schema id": {{
			Namespace: "x", Name: "stream_call", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeStreaming, ReqSchemaID: 1, FinalSchemaID: 2,
			Chunk: &schema.TypeDesc{Kind: schema.TypeKindVoid},
		}},
		"streaming missing chunk type desc": {{
			Namespace: "x", Name: "stream_call", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeStreaming, ReqSchemaID: 1, ChunkSchemaID: 2, FinalSchemaID: 3,
		}},
		"unary with chunk schema id": {{
			Namespace: "x", Name: "unary_call", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, ChunkSchemaID: 2, FinalSchemaID: 3,
		}},
		"unknown mode": {{
			Namespace: "x", Name: "weird", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableMode("psychic"), ReqSchemaID: 1, FinalSchemaID: 2,
		}},
		"duplicate name": {
			{Namespace: "x", Name: "dup", Visibility: ts.VisibilityPublic,
				Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2},
			{Namespace: "x", Name: "dup", Visibility: ts.VisibilityPublic,
				Mode: schema.CallableModeUnary, ReqSchemaID: 3, FinalSchemaID: 4},
		},
	}
	for name, callables := range cases {
		if _, err := Generate(callables, ts.Options{}); err == nil {
			t.Errorf("case %q: expected rejection, got nil", name)
		}
	}
}

// TestGenerate_EmptyCallablesProducesNoFiles guards against generating empty
// stubs: a manifest with no callables (or with all callables filtered out)
// must produce zero output files.
func TestGenerate_EmptyCallablesProducesNoFiles(t *testing.T) {
	files, err := Generate(nil, ts.Options{})
	if err != nil {
		t.Fatalf("Generate(nil): %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected zero files, got %d", len(files))
	}

	// Filter-only-admin manifest under public filter — also empty.
	adminOnly := []ts.NamedCallableDesc{{
		Namespace: "x", Name: "admin", Visibility: ts.VisibilityAdmin,
		Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
	}}
	files, err = Generate(adminOnly, ts.Options{
		Visibilities: []ts.Visibility{ts.VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate(adminOnly): %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected zero files (everything filtered), got %d", len(files))
	}
}

// TestClassNameFor verifies the namespace → class name mapping.
func TestClassNameFor(t *testing.T) {
	cases := map[string]string{
		"":        "Client",
		"auth":    "AuthClient",
		"signals": "SignalsClient",
		"a":       "AClient",
	}
	for ns, want := range cases {
		if got := classNameFor(ns); got != want {
			t.Errorf("classNameFor(%q) = %q, want %q", ns, got, want)
		}
	}
}

// contains is a thin wrapper to avoid pulling in strings everywhere.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

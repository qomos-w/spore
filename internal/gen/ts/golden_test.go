package ts

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

// updateGolden controls whether the golden_test rewrites testdata/golden
// from the current Generate output. Run `go test ./internal/gen/ts -update`
// after intentional rendering changes; commit the resulting files.
var updateGolden = flag.Bool("update", false, "rewrite testdata/golden")

// TestGenerate_Golden exercises Generate end-to-end with a small but
// representative input (struct with primitive, array, and map fields plus
// a struct reference) and diffs the output against committed golden files.
func TestGenerate_Golden(t *testing.T) {
	input := []NamedObjectDesc{
		{
			Namespace:  "demo",
			SchemaID:   1,
			Name:       "User",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "User",
				Fields: []schema.FieldDesc{
					{Name: "ID", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "Age", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
				},
			},
		},
		{
			Namespace:  "demo",
			SchemaID:   2,
			Name:       "Room",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "Room",
				Fields: []schema.FieldDesc{
					{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "Members", Type: schema.TypeDesc{
						Kind: schema.TypeKindArray,
						Element: &schema.TypeDesc{
							Kind:      schema.TypeKindStruct,
							Name:      "User",
							ClassName: "User",
						},
					}},
					{Name: "Roles", Type: schema.TypeDesc{
						Kind:  schema.TypeKindMap,
						Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
						Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
					}},
				},
			},
		},
		// An admin-only entry — should NOT appear in public-filtered output.
		{
			Namespace:  "demo",
			SchemaID:   99,
			Name:       "AdminMetrics",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "AdminMetrics",
				Fields: []schema.FieldDesc{
					{Name: "Secret", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
	}

	files, err := Generate(input, nil, Options{
		Visibilities: []Visibility{VisibilityPublic},
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
		t.Logf("golden files written to %s", goldenDir)
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

	// Also assert AdminMetrics is absent from any generated content.
	for path, content := range files {
		if filepath.Base(path) != "types.ts" {
			continue
		}
		if got := string(content); contains(got, "AdminMetrics") {
			t.Errorf("admin-only AdminMetrics leaked into %s under public filter:\n%s", path, got)
		}
	}
}

// TestGenerate_RejectsDuplicateSchemaID locks the validateInput contract.
func TestGenerate_RejectsDuplicateSchemaID(t *testing.T) {
	input := []NamedObjectDesc{
		{
			Namespace: "x", SchemaID: 1, Name: "A", Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{Kind: schema.TypeKindStruct, Name: "A"},
		},
		{
			Namespace: "x", SchemaID: 1, Name: "B", Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{Kind: schema.TypeKindStruct, Name: "B"},
		},
	}
	if _, err := Generate(input, nil, Options{}); err == nil {
		t.Errorf("expected duplicate SchemaID rejection")
	}
}

// TestGenerate_RejectsEmptyNamespace documents the validation surface.
func TestGenerate_RejectsEmptyNamespace(t *testing.T) {
	input := []NamedObjectDesc{
		{Namespace: "", SchemaID: 1, Name: "A", Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{Kind: schema.TypeKindStruct, Name: "A"}},
	}
	if _, err := Generate(input, nil, Options{}); err == nil {
		t.Errorf("expected empty Namespace rejection")
	}
}

// TestGenerateCallables_Golden exercises the callable manifest path: a
// namespace that mixes schemas and callables (auth) and a namespace that
// gains types.ts/registry.ts only because a public callable references an
// otherwise filtered schema. The admin callable should still be filtered out.
func TestGenerateCallables_Golden(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "auth",
			SchemaID:   1,
			Name:       "TailLoginsReq",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "TailLoginsReq",
				Fields: []schema.FieldDesc{{Name: "Limit", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   2,
			Name:       "LoginEvent",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "LoginEvent",
				Fields: []schema.FieldDesc{
					{Name: "User", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "At", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   3,
			Name:       "TailLoginsFinal",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "TailLoginsFinal",
				Fields: []schema.FieldDesc{{Name: "Total", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   4,
			Name:       "LookupUserReq",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "LookupUserReq",
				Fields: []schema.FieldDesc{{Name: "ID", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   5,
			Name:       "LookupUserResp",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "LookupUserResp",
				Fields: []schema.FieldDesc{{Name: "User", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "User", ClassName: "User"}}},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   90,
			Name:       "AdminDropSessionReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "AdminDropSessionReq",
				Fields: []schema.FieldDesc{{Name: "SessionID", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "signals",
			SchemaID:   100,
			Name:       "PingReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "PingReq",
				Fields: []schema.FieldDesc{{Name: "Limit", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			},
		},
	}
	callables := []NamedCallableDesc{
		{
			Namespace:     "auth",
			Name:          "tail_logins",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeStreaming,
			ReqSchemaID:   1,
			ChunkSchemaID: 2,
			FinalSchemaID: 3,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "TailLoginsReq", ClassName: "TailLoginsReq"},
			Chunk:         &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginEvent", ClassName: "LoginEvent"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "TailLoginsFinal", ClassName: "TailLoginsFinal"},
		},
		{
			Namespace:     "auth",
			Name:          "lookup_user",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   4,
			FinalSchemaID: 5,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LookupUserReq", ClassName: "LookupUserReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LookupUserResp", ClassName: "LookupUserResp"},
		},
		{
			Namespace:     "auth",
			Name:          "admin_drop_session",
			Visibility:    VisibilityAdmin,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   90,
			FinalSchemaID: 91,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "AdminDropSessionReq", ClassName: "AdminDropSessionReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindVoid},
		},
		{
			Namespace:     "signals",
			Name:          "tail_pings",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeStreaming,
			ReqSchemaID:   100,
			ChunkSchemaID: 101,
			FinalSchemaID: 102,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "PingReq", ClassName: "PingReq"},
			Chunk:         &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Ping", ClassName: "Ping"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindVoid},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Visibilities: []Visibility{VisibilityPublic},
		Header:       "// AUTO-GENERATED — DO NOT EDIT",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	goldenDir := filepath.Join("testdata", "golden_callables")
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
		t.Logf("callables golden written to %s", goldenDir)
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

	if _, ok := files["signals/types.ts"]; !ok {
		t.Errorf("signals/types.ts missing (public callable should retain referenced request schema)")
	}
	if _, ok := files["signals/registry.ts"]; !ok {
		t.Errorf("signals/registry.ts missing (public callable should retain referenced request schema)")
	}
	if _, ok := files["signals/callables.ts"]; !ok {
		t.Errorf("signals/callables.ts missing (namespace has callables)")
	}
	if _, ok := files["signals/index.ts"]; !ok {
		t.Errorf("signals/index.ts missing (namespace appears in input)")
	}
	if got := files["signals/types.ts"]; !contains(got, "export interface PingReq") {
		t.Errorf("signals/types.ts missing PingReq:\n%s", got)
	}
	for path, content := range files {
		if filepath.Base(path) == "callables.ts" && contains(content, "admin_drop_session") {
			t.Errorf("admin-only callable leaked into %s:\n%s", path, content)
		}
		if filepath.Base(path) == "types.ts" && contains(content, "AdminDropSessionReq") {
			t.Errorf("admin-only schema leaked into %s without a surviving callable reference:\n%s", path, content)
		}
	}
}

// TestGenerate_RejectsCallableModeMismatch locks the validateCallables
// contract on streaming mode requiring chunk schema info.
func TestGenerate_RejectsCallableModeMismatch(t *testing.T) {
	streamingNoChunk := []NamedCallableDesc{{
		Namespace: "x", Name: "stream_call", Visibility: VisibilityPublic,
		Mode:          schema.CallableModeStreaming,
		ReqSchemaID:   1,
		FinalSchemaID: 2,
	}}
	if _, err := Generate(nil, streamingNoChunk, Options{}); err == nil {
		t.Errorf("expected streaming-without-chunk rejection")
	}

	unaryWithChunk := []NamedCallableDesc{{
		Namespace: "x", Name: "unary_call", Visibility: VisibilityPublic,
		Mode:          schema.CallableModeUnary,
		ReqSchemaID:   1,
		ChunkSchemaID: 2,
		FinalSchemaID: 3,
	}}
	if _, err := Generate(nil, unaryWithChunk, Options{}); err == nil {
		t.Errorf("expected unary-with-chunk rejection")
	}
}

// TestGenerate_RejectsDuplicateCallableName locks the (namespace, name)
// uniqueness contract on callables.
func TestGenerate_RejectsDuplicateCallableName(t *testing.T) {
	callables := []NamedCallableDesc{
		{Namespace: "x", Name: "dup", Visibility: VisibilityPublic, Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2},
		{Namespace: "x", Name: "dup", Visibility: VisibilityPublic, Mode: schema.CallableModeUnary, ReqSchemaID: 3, FinalSchemaID: 4},
	}
	if _, err := Generate(nil, callables, Options{}); err == nil {
		t.Errorf("expected duplicate callable name rejection")
	}
}

// TestParseVisibility_Roundtrip checks every defined value round-trips.
func TestParseVisibility_Roundtrip(t *testing.T) {
	for _, v := range []Visibility{VisibilityInternal, VisibilityPublic, VisibilityAdmin, VisibilityDiagnostic} {
		got, ok := ParseVisibility(v.String())
		if !ok || got != v {
			t.Errorf("roundtrip failed for %v: got %v ok=%v", v, got, ok)
		}
	}
	if _, ok := ParseVisibility("nonsense"); ok {
		t.Errorf("expected ParseVisibility(\"nonsense\") to return ok=false")
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

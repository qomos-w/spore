package goserver

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
// `go test ./internal/gen/go-server -update` after intentional rendering
// changes; commit the resulting files.
var updateGolden = flag.Bool("update", false, "rewrite testdata/golden")

// TestGenerate_Golden exercises Generate end-to-end with a representative
// auth namespace (mixed unary callables, plus an admin-only callable
// that should be filtered out under the public profile) and a billing
// namespace whose callable lives in its own file.
//
// The fixture intentionally exercises:
//   - Multi-callable namespace → multi-case dispatch switch in the same file
//   - Visibility filtering → admin_drop_session must NOT appear in output
//   - Multi-namespace → independent <ns>_dispatcher_gen.go files
//   - Multi-segment callable names ("lookup_user") → Go method TitleCase
//   - Mixed scalar field types (string + int + bool) → exercises the
//     scalar-go-type mapping table
func TestGenerate_Golden(t *testing.T) {
	schemas := []ts.NamedObjectDesc{
		{
			Namespace:  "auth",
			SchemaID:   1,
			Name:       "LoginReq",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LoginReq",
				Fields: []schema.FieldDesc{
					{Name: "user", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   2,
			Name:       "LoginResp",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LoginResp",
				Fields: []schema.FieldDesc{
					{Name: "ok", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}},
					{Name: "token", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   4,
			Name:       "LookupUserReq",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LookupUserReq",
				Fields: []schema.FieldDesc{
					{Name: "userId", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   5,
			Name:       "LookupUserResp",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LookupUserResp",
				Fields: []schema.FieldDesc{
					{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "age", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
				},
			},
		},
		// Admin-only schema — present in manifest but not referenced by
		// any public callable; ensures the public-only output stays
		// agnostic of admin shapes.
		{
			Namespace:  "auth",
			SchemaID:   90,
			Name:       "AdminDropSessionReq",
			Visibility: ts.VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "AdminDropSessionReq",
				Fields: []schema.FieldDesc{
					{Name: "sessionId", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   91,
			Name:       "AdminDropSessionResp",
			Visibility: ts.VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "AdminDropSessionResp",
				Fields: []schema.FieldDesc{
					{Name: "ok", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}},
				},
			},
		},
		{
			Namespace:  "billing",
			SchemaID:   100,
			Name:       "ChargeReq",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "ChargeReq",
				Fields: []schema.FieldDesc{
					{Name: "amount", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int64"}},
				},
			},
		},
		{
			Namespace:  "billing",
			SchemaID:   101,
			Name:       "ChargeResp",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "ChargeResp",
				Fields: []schema.FieldDesc{
					{Name: "invoiceId", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
			},
		},
	}
	callables := []ts.NamedCallableDesc{
		{
			Namespace:     "auth",
			Name:          "login",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   1,
			FinalSchemaID: 2,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginReq", ClassName: "LoginReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginResp", ClassName: "LoginResp"},
		},
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
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "AdminDropSessionResp", ClassName: "AdminDropSessionResp"},
		},
		{
			Namespace:     "billing",
			Name:          "charge",
			Visibility:    ts.VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   100,
			FinalSchemaID: 101,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "ChargeReq", ClassName: "ChargeReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "ChargeResp", ClassName: "ChargeResp"},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Package:      "testpkg",
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
		t.Logf("go-server golden written to %s", goldenDir)
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

	// Admin-only callable must not leak into any output file.
	for path, content := range files {
		if strings.Contains(content, "admin_drop_session") {
			t.Errorf("admin-only callable leaked into %s:\n%s", path, content)
		}
		if strings.Contains(content, "AdminDropSession") {
			t.Errorf("admin-only struct name leaked into %s:\n%s", path, content)
		}
	}
}

// TestGenerate_RejectsInvalidCallables locks the validateCallables contract.
func TestGenerate_RejectsInvalidCallables(t *testing.T) {
	cases := map[string][]ts.NamedCallableDesc{
		"empty namespace": {{
			Namespace: "", Name: "x", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
			Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
			Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Y", ClassName: "Y"},
		}},
		"empty name": {{
			Namespace: "x", Name: "", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
			Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
			Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Y", ClassName: "Y"},
		}},
		"streaming rejected": {{
			Namespace: "x", Name: "tail", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeStreaming, ReqSchemaID: 1, ChunkSchemaID: 2, FinalSchemaID: 3,
			Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
			Chunk: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "C", ClassName: "C"},
			Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "F", ClassName: "F"},
		}},
		"unary with chunk schema id": {{
			Namespace: "x", Name: "y", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableModeUnary, ReqSchemaID: 1, ChunkSchemaID: 2, FinalSchemaID: 3,
			Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
			Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Y", ClassName: "Y"},
		}},
		"unknown mode": {{
			Namespace: "x", Name: "weird", Visibility: ts.VisibilityPublic,
			Mode: schema.CallableMode("psychic"), ReqSchemaID: 1, FinalSchemaID: 2,
			Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
			Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Y", ClassName: "Y"},
		}},
		"duplicate name": {
			{Namespace: "x", Name: "dup", Visibility: ts.VisibilityPublic,
				Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
				Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "X", ClassName: "X"},
				Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Y", ClassName: "Y"}},
			{Namespace: "x", Name: "dup", Visibility: ts.VisibilityPublic,
				Mode: schema.CallableModeUnary, ReqSchemaID: 3, FinalSchemaID: 4,
				Req:   schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "A", ClassName: "A"},
				Final: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "B", ClassName: "B"}},
		},
	}
	for name, callables := range cases {
		if _, err := Generate(nil, callables, Options{Package: "testpkg"}); err == nil {
			t.Errorf("case %q: expected rejection, got nil", name)
		}
	}
}

// TestGenerate_RequiresPackage guards against accidentally emitting Go
// files with an empty `package ` declaration, which would compile but
// be confusing during inspection.
func TestGenerate_RequiresPackage(t *testing.T) {
	if _, err := Generate(nil, nil, Options{}); err == nil {
		t.Errorf("expected rejection for empty Package, got nil")
	}
	if _, err := Generate(nil, nil, Options{Package: "   "}); err == nil {
		t.Errorf("expected rejection for whitespace Package, got nil")
	}
}

// TestGenerate_RejectsMissingSchema verifies the codegen fails loudly
// when a callable references a struct that doesn't exist in
// manifest.schemas[]. Without this check the codegen would emit a Go
// file that referenced an undeclared type and only fail at user
// compile time — much harder to debug.
func TestGenerate_RejectsMissingSchema(t *testing.T) {
	callables := []ts.NamedCallableDesc{{
		Namespace:     "auth",
		Name:          "login",
		Visibility:    ts.VisibilityPublic,
		Mode:          schema.CallableModeUnary,
		ReqSchemaID:   1,
		FinalSchemaID: 2,
		Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginReq", ClassName: "LoginReq"},
		Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginResp", ClassName: "LoginResp"},
	}}
	if _, err := Generate(nil, callables, Options{Package: "testpkg"}); err == nil {
		t.Errorf("expected rejection for missing schemas, got nil")
	}
}

// TestGenerate_RejectsNonStructFields verifies the codegen rejects
// fields whose types are not yet supported by the MVP marshal logic
// (nested struct, array, map). exp09 stays inside the supported subset
// today; this test guards against silent degradation if a future
// caller wires in a richer manifest.
func TestGenerate_RejectsNonStructFields(t *testing.T) {
	schemas := []ts.NamedObjectDesc{
		{
			Namespace:  "auth",
			SchemaID:   1,
			Name:       "LoginReq",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LoginReq",
				Fields: []schema.FieldDesc{
					{Name: "tags", Type: schema.TypeDesc{
						Kind:    schema.TypeKindArray,
						Name:    "array",
						Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
					}},
				},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   2,
			Name:       "LoginResp",
			Visibility: ts.VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind:   schema.TypeKindStruct,
				Name:   "LoginResp",
				Fields: []schema.FieldDesc{},
			},
		},
	}
	callables := []ts.NamedCallableDesc{{
		Namespace:     "auth",
		Name:          "login",
		Visibility:    ts.VisibilityPublic,
		Mode:          schema.CallableModeUnary,
		ReqSchemaID:   1,
		FinalSchemaID: 2,
		Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginReq", ClassName: "LoginReq"},
		Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "LoginResp", ClassName: "LoginResp"},
	}}
	if _, err := Generate(schemas, callables, Options{Package: "testpkg"}); err == nil {
		t.Errorf("expected rejection for non-scalar field, got nil")
	}
}

// TestHandlerNameFor verifies the namespace → handler interface name mapping.
func TestHandlerNameFor(t *testing.T) {
	cases := map[string]string{
		"auth":    "AuthHandler",
		"billing": "BillingHandler",
		"a":       "AHandler",
	}
	for ns, want := range cases {
		if got := handlerNameFor(ns); got != want {
			t.Errorf("handlerNameFor(%q) = %q, want %q", ns, got, want)
		}
	}
}

// TestDispatchNameFor verifies the namespace → top-level dispatch fn mapping.
func TestDispatchNameFor(t *testing.T) {
	cases := map[string]string{
		"auth":    "DispatchAuth",
		"billing": "DispatchBilling",
	}
	for ns, want := range cases {
		if got := dispatchNameFor(ns); got != want {
			t.Errorf("dispatchNameFor(%q) = %q, want %q", ns, got, want)
		}
	}
}

// TestGoMethodName verifies the wire-name → Go-method mapping.
func TestGoMethodName(t *testing.T) {
	cases := map[string]string{
		"login":              "Login",
		"lookup_user":        "LookupUser",
		"admin_drop_session": "AdminDropSession",
	}
	for in, want := range cases {
		if got := goMethodName(in); got != want {
			t.Errorf("goMethodName(%q) = %q, want %q", in, got, want)
		}
	}
}

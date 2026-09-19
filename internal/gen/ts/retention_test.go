package ts

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

// TestRetention_SameNameDifferentNamespace pins the regression where
// retention was tracked by name and silently leaked schemas with the same
// name across namespaces. Only the namespace whose (namespace, schemaID)
// is referenced by a public callable should keep its filtered schema.
func TestRetention_SameNameDifferentNamespace(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "alpha",
			SchemaID:   1,
			Name:       "SharedReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "SharedReq",
				Fields: []schema.FieldDesc{{Name: "Token", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "beta",
			SchemaID:   1,
			Name:       "SharedReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "SharedReq",
				Fields: []schema.FieldDesc{{Name: "Limit", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			},
		},
		{
			Namespace:  "alpha",
			SchemaID:   2,
			Name:       "SharedResp",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "SharedResp",
				Fields: []schema.FieldDesc{{Name: "OK", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}}},
			},
		},
	}
	callables := []NamedCallableDesc{
		{
			Namespace:     "alpha",
			Name:          "shared_call",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   1,
			FinalSchemaID: 2,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "SharedReq", ClassName: "SharedReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "SharedResp", ClassName: "SharedResp"},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Visibilities: []Visibility{VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if got, ok := files["alpha/types.ts"]; !ok || !strings.Contains(got, "SharedReq") {
		t.Errorf("alpha namespace should retain its referenced SharedReq:\n%s", got)
	}
	if _, ok := files["beta/types.ts"]; ok {
		t.Errorf("beta namespace should not gain types.ts via name aliasing across namespaces; files: %v", keysOf(files))
	}
	if _, ok := files["beta/registry.ts"]; ok {
		t.Errorf("beta namespace should not gain registry.ts via name aliasing across namespaces; files: %v", keysOf(files))
	}
}

// TestRetention_NestedStructIsClosed locks in that retention follows
// nested struct references inside a retained schema, even when those
// nested types would otherwise be filtered out.
func TestRetention_NestedStructIsClosed(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "world",
			SchemaID:   10,
			Name:       "Outer",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "Outer",
				Fields: []schema.FieldDesc{
					{Name: "Inner", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Inner", ClassName: "Inner"}},
				},
			},
		},
		{
			Namespace:  "world",
			SchemaID:   11,
			Name:       "Inner",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "Inner",
				Fields: []schema.FieldDesc{{Name: "ID", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "world",
			SchemaID:   12,
			Name:       "Resp",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "Resp",
				Fields: []schema.FieldDesc{{Name: "OK", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}}},
			},
		},
	}
	callables := []NamedCallableDesc{
		{
			Namespace:     "world",
			Name:          "fetch_outer",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   10,
			FinalSchemaID: 12,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Outer", ClassName: "Outer"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Resp", ClassName: "Resp"},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Visibilities: []Visibility{VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	got, ok := files["world/types.ts"]
	if !ok {
		t.Fatalf("world/types.ts missing entirely; files: %v", keysOf(files))
	}
	for _, needle := range []string{"export interface Outer", "export interface Inner", "export interface Resp"} {
		if !strings.Contains(got, needle) {
			t.Errorf("expected %q in world/types.ts:\n%s", needle, got)
		}
	}
}

// TestRetention_FollowsCallableSchemaID covers the case where the
// TypeDesc.ClassName carried by the callable matches the entry's
// NamedObjectDesc.Name even though Object.Name diverges. Retention must
// still pick up the right (namespace, schemaID) pair.
func TestRetention_FollowsCallableSchemaID(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "x",
			SchemaID:   7,
			Name:       "ActualReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "internalActualReq",
				Fields: []schema.FieldDesc{{Name: "K", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "x",
			SchemaID:   8,
			Name:       "ActualResp",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "internalActualResp",
				Fields: []schema.FieldDesc{{Name: "OK", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}}},
			},
		},
	}
	callables := []NamedCallableDesc{
		{
			Namespace:     "x",
			Name:          "do_thing",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   7,
			FinalSchemaID: 8,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "ActualReq", ClassName: "ActualReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "ActualResp", ClassName: "ActualResp"},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Visibilities: []Visibility{VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	got, ok := files["x/types.ts"]
	if !ok {
		t.Fatalf("x/types.ts missing; files: %v", keysOf(files))
	}
	for _, needle := range []string{"export interface internalActualReq", "export interface internalActualResp"} {
		if !strings.Contains(got, needle) {
			t.Errorf("expected %q in x/types.ts:\n%s", needle, got)
		}
	}
}

// TestRetention_CrossNamespaceCallableReferencesSharedSchema pins the case
// where a callable lives in one namespace but the schema it references has
// been moved to a shared namespace (e.g. "system"). The referenced schema
// must be retained in the shared namespace's types.ts, not in the callable's
// namespace.
func TestRetention_CrossNamespaceCallableReferencesSharedSchema(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "system",
			SchemaID:   1,
			Name:       "SharedReq",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "SharedReq",
				Fields: []schema.FieldDesc{{Name: "Token", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "system",
			SchemaID:   2,
			Name:       "SharedResp",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "SharedResp",
				Fields: []schema.FieldDesc{{Name: "OK", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}}},
			},
		},
	}
	callables := []NamedCallableDesc{
		{
			Namespace:     "workspace",
			Name:          "shared_call",
			Visibility:    VisibilityPublic,
			Mode:          schema.CallableModeUnary,
			ReqSchemaID:   1,
			FinalSchemaID: 2,
			Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "SharedReq", ClassName: "SharedReq"},
			Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "SharedResp", ClassName: "SharedResp"},
		},
	}

	files, err := Generate(schemas, callables, Options{
		Visibilities: []Visibility{VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	got, ok := files["system/types.ts"]
	if !ok {
		t.Fatalf("system/types.ts missing; files: %v", keysOf(files))
	}
	for _, needle := range []string{"export interface SharedReq", "export interface SharedResp"} {
		if !strings.Contains(got, needle) {
			t.Errorf("expected %q in system/types.ts\n%s", needle, got)
		}
	}
	if _, ok := files["workspace/types.ts"]; ok {
		t.Errorf("workspace namespace should not gain its own types.ts; files: %v", keysOf(files))
	}
	if _, ok := files["workspace/registry.ts"]; ok {
		t.Errorf("workspace namespace should not gain its own registry.ts; files: %v", keysOf(files))
	}
}

// TestGenerate_CrossNamespaceFieldImport verifies that when a public schema
// in one namespace holds a field referencing a struct owned by another
// namespace, the generated types.ts qualifies the reference with an import
// alias (e.g. systemTypes.Blob) and emits the matching import. The referenced
// schema must also be retained in its owning namespace's types.ts.
func TestGenerate_CrossNamespaceFieldImport(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "auth",
			SchemaID:   1,
			Name:       "UploadReq",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "UploadReq",
				Fields: []schema.FieldDesc{
					{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "Payload", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Blob", ClassName: "Blob"}},
					{Name: "Items", Type: schema.TypeDesc{
						Kind:   schema.TypeKindArray,
						Element: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Blob", ClassName: "Blob"},
					}},
				},
			},
		},
		{
			Namespace:  "system",
			SchemaID:   50,
			Name:       "Blob",
			Visibility: VisibilityAdmin,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct, Name: "Blob",
				Fields: []schema.FieldDesc{{Name: "Data", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bytes"}}},
			},
		},
	}

	files, err := Generate(schemas, nil, Options{
		Visibilities: []Visibility{VisibilityPublic},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	authTypes, ok := files["auth/types.ts"]
	if !ok {
		t.Fatalf("auth/types.ts missing; files: %v", keysOf(files))
	}
	for _, needle := range []string{
		`Payload: systemTypes.Blob;`,
		`Items: systemTypes.Blob[];`,
		`import * as systemTypes from "../system/types.js";`,
	} {
		if !strings.Contains(authTypes, needle) {
			t.Errorf("auth/types.ts missing %q:\n%s", needle, authTypes)
		}
	}

	sysTypes, ok := files["system/types.ts"]
	if !ok {
		t.Fatalf("system/types.ts missing (referenced Blob must be retained); files: %v", keysOf(files))
	}
	if !strings.Contains(sysTypes, "export interface Blob") {
		t.Errorf("system/types.ts missing Blob:\n%s", sysTypes)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

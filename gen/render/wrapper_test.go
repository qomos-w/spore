// Package render_test exercises the public code-generation façade from the
// outside, the way an external embedder (e.g. gospore) consumes it: one entry
// point per generator, all four reachable from this single package.
package render_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/qomos-w/spore/gen/render"
	"github.com/qomos-w/spore/schema"
)

// demoStruct builds a public single-field struct schema entry in namespace
// "demo".
func demoStruct(id uint64, name string) render.NamedObjectDesc {
	return render.NamedObjectDesc{
		Namespace:  "demo",
		SchemaID:   id,
		Name:       name,
		Visibility: render.VisibilityPublic,
		Object: schema.ObjectDesc{
			Kind:     schema.TypeKindStruct,
			Name:     name,
			SchemaID: id,
			Fields: []schema.FieldDesc{
				{Name: "msg", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		},
	}
}

func demoCallable(name string) render.NamedCallableDesc {
	return render.NamedCallableDesc{
		Namespace:     "demo",
		Name:          name,
		Visibility:    render.VisibilityPublic,
		Mode:          schema.CallableModeUnary,
		ReqSchemaID:   1,
		FinalSchemaID: 2,
		Req:           schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "EchoReq", ClassName: "EchoReq"},
		Final:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "EchoResp", ClassName: "EchoResp"},
	}
}

func fileKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestGenerate_TypeScriptTypes(t *testing.T) {
	files, err := render.Generate(
		[]render.NamedObjectDesc{demoStruct(1, "EchoReq")},
		nil,
		render.Options{Visibilities: []render.Visibility{render.VisibilityPublic}},
	)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src, ok := files["demo/types.ts"]
	if !ok {
		t.Fatalf("expected demo/types.ts, got %v", fileKeys(files))
	}
	if !strings.Contains(src, "interface EchoReq") {
		t.Errorf("types.ts missing interface EchoReq:\n%s", src)
	}
}

func TestGenerateTSClient_ClientClasses(t *testing.T) {
	files, err := render.GenerateTSClient(
		[]render.NamedCallableDesc{demoCallable("echo")},
		render.Options{Visibilities: []render.Visibility{render.VisibilityPublic}},
	)
	if err != nil {
		t.Fatalf("GenerateTSClient: %v", err)
	}
	src, ok := files["demo/client.ts"]
	if !ok {
		t.Fatalf("expected demo/client.ts, got %v", fileKeys(files))
	}
	if !strings.Contains(src, "Client") {
		t.Errorf("client.ts missing a client class:\n%s", src)
	}
}

func TestGenerateGoServer_Dispatchers(t *testing.T) {
	files, err := render.GenerateGoServer(
		[]render.NamedObjectDesc{demoStruct(1, "EchoReq"), demoStruct(2, "EchoResp")},
		[]render.NamedCallableDesc{demoCallable("echo")},
		render.GoServerOptions{
			Package:      "demo",
			Visibilities: []render.Visibility{render.VisibilityPublic},
		},
	)
	if err != nil {
		t.Fatalf("GenerateGoServer: %v", err)
	}
	src, ok := files["demo_dispatcher_gen.go"]
	if !ok {
		t.Fatalf("expected demo_dispatcher_gen.go, got %v", fileKeys(files))
	}
	for _, want := range []string{"DemoHandler", "DispatchDemo"} {
		if !strings.Contains(src, want) {
			t.Errorf("dispatcher missing %q:\n%s", want, src)
		}
	}
}

func TestRenderGoTypes_And_Registry(t *testing.T) {
	objs := []schema.ObjectDesc{demoStruct(1, "EchoReq").Object}
	types, err := render.RenderGoTypes(objs, render.GoTypesOptions{Package: "demo"})
	if err != nil {
		t.Fatalf("RenderGoTypes: %v", err)
	}
	for _, want := range []string{"package demo", "type EchoReq struct"} {
		if !strings.Contains(string(types), want) {
			t.Errorf("go-types output missing %q:\n%s", want, types)
		}
	}

	reg, err := render.RenderGoTypesRegistry(
		[]render.RegistryEntry{{ID: 1, Name: "EchoReq", SourceFile: "demo.spore"}},
		render.GoTypesOptions{Package: "demo"},
	)
	if err != nil {
		t.Fatalf("RenderGoTypesRegistry: %v", err)
	}
	for _, want := range []string{"var SchemaIDs", "1: \"EchoReq\""} {
		if !strings.Contains(string(reg), want) {
			t.Errorf("registry output missing %q:\n%s", want, reg)
		}
	}
}

func TestAssignSequentialSchemaIDs_PublicHelper(t *testing.T) {
	objs := []schema.ObjectDesc{demoStruct(0, "EchoReq").Object}
	render.AssignSequentialSchemaIDs(objs, "demo._300.spore")
	if objs[0].SchemaID != 300 {
		t.Errorf("expected schema ID 300 from filename base, got %d", objs[0].SchemaID)
	}
}

func TestVisibilityContract(t *testing.T) {
	for _, want := range []render.Visibility{
		render.VisibilityInternal,
		render.VisibilityPublic,
		render.VisibilityAdmin,
		render.VisibilityDiagnostic,
	} {
		got, ok := render.ParseVisibility(want.String())
		if !ok || got != want {
			t.Errorf("ParseVisibility(%q) = %v,%v; want %v,true", want.String(), got, ok, want)
		}
	}
	if _, ok := render.ParseVisibility("nonsense"); ok {
		t.Error("unknown visibility should report ok=false")
	}
}

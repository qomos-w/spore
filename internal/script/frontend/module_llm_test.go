package frontend

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
)

func TestFrontend_ModuleLLMDiscoveryAPIs(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export struct Point { x: int, y: int }
export type IntPair = array<int>
export var PI: double = 3.14`,
		"util": `export add from "math"
export fun twice(v: int): int { return add(v, v) }`,
	})
	if err := f.LoadSource(`import twice from "util"
import Point from "math"
fun use(p: Point): int { return twice(p.x) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	summary, ok := f.ModuleSummary("util")
	if !ok {
		t.Fatal("expected util summary")
	}
	if summary.ExportCounts.Callables != 2 {
		t.Fatalf("expected util callable exports to include re-export, got %+v", summary.ExportCounts)
	}
	if !containsStr(summary.Dependencies, "math") {
		t.Fatalf("expected util dependency on math, got %v", summary.Dependencies)
	}
	rootResolutions := f.RootImportResolutions()
	if len(rootResolutions) != 2 {
		t.Fatalf("expected two root import resolutions, got %+v", rootResolutions)
	}
	resolved, ok := f.ExplainRootImport("twice")
	if !ok || resolved.RequestedName != "twice" || resolved.Kind != "callable" || resolved.Explanation == "" {
		t.Fatalf("unexpected root callable resolution: %+v %v", resolved, ok)
	}
	typeResolved, ok := f.ExplainRootImport("Point")
	if !ok || typeResolved.Kind != "struct" || typeResolved.Explanation == "" {
		t.Fatalf("unexpected root type resolution: %+v %v", typeResolved, ok)
	}
	trace, ok := f.ExplainRootImportTrace("twice")
	if !ok || trace.TargetModulePath != "util" || len(trace.Hops) != 1 {
		t.Fatalf("unexpected root trace: %+v %v", trace, ok)
	}
	if trace.Hops[0].ModulePath != "<root>" || trace.Hops[0].TargetModulePath != "util" {
		t.Fatalf("unexpected trace hops: %+v", trace.Hops)
	}
	moduleText, ok := f.ExplainModuleText("util")
	if !ok || len(moduleText) == 0 {
		t.Fatalf("unexpected module text: %q %v", moduleText, ok)
	}
	importers := f.FindSymbolImporters("util", "twice")
	if len(importers) != 1 || importers[0].ImporterPath != "<root>" {
		t.Fatalf("expected util.twice importer from root, got %+v", importers)
	}
	decls := f.Declarations()
	if len(decls.ImportedSymbols) != 1 || decls.ImportedSymbols[0].LocalName != "twice" || decls.ImportedSymbols[0].Explanation == "" {
		t.Fatalf("unexpected root imported symbols: %+v", decls.ImportedSymbols)
	}
	if len(decls.ImportedTypes) != 1 || decls.ImportedTypes[0].LocalName != "Point" || decls.ImportedTypes[0].Explanation == "" {
		t.Fatalf("unexpected root imported types: %+v", decls.ImportedTypes)
	}

	summaries := f.AllModuleSummaries()
	if len(summaries) != 2 || summaries[0].Path != "math" || summaries[1].Path != "util" {
		t.Fatalf("expected sorted summaries for math/util, got %+v", summaries)
	}
}

func TestFrontend_LastModuleDiagnostics(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{"math": `export fun add(a: int, b: int): int { return a + b }`})

	err = f.LoadSource(`import sub from "math"
fun use(): int { return sub(1, 2) }`)
	if err == nil {
		t.Fatal("expected missing export error")
	}
	diags := f.LastModuleDiagnostics()
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %+v", diags)
	}
	if diags[0].Code != "missing_module_export" || diags[0].Category != string(diagnostics.CategoryLoad) || diags[0].Hint == "" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
	if diags[0].ImportName != "sub" || diags[0].LocalName != "sub" || diags[0].SourceModulePath != "math" || diags[0].Phase != "resolve_export" {
		t.Fatalf("expected detailed import context, got %+v", diags[0])
	}
	if len(diags[0].AvailableExports) != 1 || diags[0].AvailableExports[0] != "add" {
		t.Fatalf("expected stable available exports, got %+v", diags[0].AvailableExports)
	}

	err = f.LoadSource(`import add from "missing"
fun use(): int { return add(1, 2) }`)
	if err == nil {
		t.Fatal("expected missing module error")
	}
	diags = f.LastModuleDiagnostics()
	if len(diags) != 1 || diags[0].Code != "module_not_found" || diags[0].Phase != "resolve_module" || diags[0].SourceModulePath != "missing" {
		t.Fatalf("unexpected module-not-found diagnostic: %+v", diags)
	}

	if err := f.LoadSource(`import add from "math"
fun use(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource success: %v", err)
	}
	if got := f.LastModuleDiagnostics(); got != nil {
		t.Fatalf("expected diagnostics to reset after success, got %+v", got)
	}
}

func TestFrontend_AllDiagnosticCodesRegistered(t *testing.T) {
	type entry struct {
		code     string
		category diagnostics.Category
	}
	entries := []entry{
		// schema codes (diagnostic_error.go)
		{"export_boundary_violation", diagnostics.CategorySchema},
		{"callable_lowering_failed", diagnostics.CategorySchema},
		{"stream_fun_requires_yield", diagnostics.CategorySchema},
		{"yield_requires_stream_fun", diagnostics.CategorySchema},
		{"stream_expr_body_unsupported", diagnostics.CategorySchema},
		{"stream_final_type_required", diagnostics.CategorySchema},
		{"yield_value_required", diagnostics.CategorySchema},
		{"yield_type_mismatch", diagnostics.CategorySchema},
		{"yield_type_inference_failed", diagnostics.CategorySchema},
		{"array_literal_type_mismatch", diagnostics.CategorySchema},
		{"map_literal_type_mismatch", diagnostics.CategorySchema},
		// load codes (diagnostic_error.go + frontend.go)
		{"module_not_found", diagnostics.CategoryLoad},
		{"missing_module_export", diagnostics.CategoryLoad},
		{"cyclic_import", diagnostics.CategoryLoad},
		{"missing_module_resolver", diagnostics.CategoryLoad},
		{"invalid_module_path", diagnostics.CategoryLoad},
		{"duplicate_import_binding", diagnostics.CategoryLoad},
		{"parse_error", diagnostics.CategoryLoad},
		// host code (frontend.go)
		{"no_evaluator", diagnostics.CategoryHost},
	}
	for _, e := range entries {
		info, ok := diagnostics.LookupCode(e.code)
		if !ok {
			t.Fatalf("expected frontend diagnostic code %q to be registered", e.code)
		}
		if info.Category != e.category {
			t.Fatalf("code %q registered under category %q, want %q", e.code, info.Category, e.category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", e.code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", e.code)
		}
	}
}

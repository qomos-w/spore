package frontend

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

type compileOnlyFrontendBackend struct{}

func (compileOnlyFrontendBackend) CompileProgram(prog *Program) error { return nil }

type recordingRuntimeBackend struct {
	result any
	err    error
	calls  []string
	args   [][]any
}

func (b *recordingRuntimeBackend) CompileProgram(prog *Program) error { return nil }

func (b *recordingRuntimeBackend) Evaluate(callable string, stage binding.InvocationStage, args []any) (any, error) {
	b.calls = append(b.calls, callable)
	copied := append([]any(nil), args...)
	b.args = append(b.args, copied)
	if b.err != nil {
		return nil, b.err
	}
	return b.result, nil
}

func (b *recordingRuntimeBackend) Reset(callable string, args []any) {}

func TestFrontend_InvokeStage_StreamNextDispatchesToRuntime(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	backend := &recordingRuntimeBackend{result: 1}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("InvokeStage next: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 1 {
		t.Fatalf("expected payload 1, got %+v", outcome.Payload)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "emit" {
		t.Fatalf("expected backend call emit, got %+v", backend.calls)
	}
	if len(backend.args) != 1 || len(backend.args[0]) != 0 {
		t.Fatalf("expected no args, got %+v", backend.args)
	}
}

func TestFrontend_InvokeStage_StreamFinalDispatchesToRuntime(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	backend := &recordingRuntimeBackend{result: 2}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("InvokeStage final: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 2 {
		t.Fatalf("expected payload 2, got %+v", outcome.Payload)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "emit" {
		t.Fatalf("expected backend call emit, got %+v", backend.calls)
	}
}

func TestFrontend_InvokeStage_UnaryCallableRejectsNextStage(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	backend := &recordingRuntimeBackend{result: 1}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("fun one(): int = 1"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = f.InvokeStage("one", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected unary callable to reject next stage")
	}

	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
		t.Fatalf("expected invalid_invocation_stage code, got %v", err)
	}
}

func TestFrontend_InvokeStage_StreamCallableRejectsUnaryStage(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	backend := &recordingRuntimeBackend{result: 1}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = f.InvokeStage("emit", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected stream callable to reject unary stage")
	}

	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
		t.Fatalf("expected invalid_invocation_stage code, got %v", err)
	}
}

func TestFrontend_LoadSource_RegistersUnaryExpressionBodyCallable(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun greet(name: string): string = name"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	desc, ok := f.Callable("greet")
	if !ok {
		t.Fatal("expected callable greet to be registered")
	}
	if desc.Name != "greet" {
		t.Fatalf("expected callable greet, got %q", desc.Name)
	}
	if len(desc.Parameters) != 1 || desc.Parameters[0].Type.Name != "string" {
		t.Fatalf("expected one string param, got %+v", desc.Parameters)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Name != "string" {
		t.Fatalf("expected string return, got %+v", desc.Returns)
	}
}

func TestFrontend_Invoke_PassesArgsToScriptCallable(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	backend := &recordingRuntimeBackend{result: "ok"}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("fun greet(name: string): string = name"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("greet", []any{"alice"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "ok" {
		t.Fatalf("expected payload ok, got %+v", outcome.Payload)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "greet" {
		t.Fatalf("expected runtime backend to be called for greet, got %+v", backend.calls)
	}
	if len(backend.args) != 1 || len(backend.args[0]) != 1 || backend.args[0][0] != "alice" {
		t.Fatalf("expected runtime backend args [alice], got %+v", backend.args)
	}
}

func TestFrontend_Invoke_RejectsWrongArgCount(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun add(a: int, b: int): int = a + b"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = f.Invoke("add", []any{1})
	if err == nil {
		t.Fatal("expected wrong arg count error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestFrontend_Invoke_RejectsUnknownCallable(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun known(): int = 1"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = f.Invoke("missing", nil)
	if err == nil {
		t.Fatal("expected error for unknown callable")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestFrontend_SnapshotsCallableDescriptors(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("boost", func(base int, bonus int) int { return base + bonus }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	desc, ok := f.Callable("boost")
	if !ok {
		t.Fatal("expected boost callable descriptor")
	}
	if desc.Name != "boost" {
		t.Fatalf("expected callable name boost, got %s", desc.Name)
	}
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(desc.Parameters))
	}
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return, got %d", len(desc.Returns))
	}

	callables := f.Callables()
	if _, ok := callables["boost"]; !ok {
		t.Fatal("expected boost in callable snapshot")
	}
	callables["boost"] = callables["boost"]
	if desc, ok := f.Callable("boost"); !ok || desc.Name != "boost" {
		t.Fatalf("frontend callable snapshot should remain addressable after returned map mutation, got %v %v", desc.Name, ok)
	}
}

func TestParseModuleBuildsCanonicalDeclarations(t *testing.T) {
	prog, err := parseModule(strings.Join([]string{
		"struct Player { hp: int, name: string }",
		"",
		"fun boost(base: int, bonus: int): int {}",
		"fun echo(message: string): string {}",
	}, "\n"))
	if err != nil {
		t.Fatalf("parseModule: %v", err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Objects) != 1 {
		t.Fatalf("expected 1 object declaration, got %d", len(meta.Objects))
	}
	if meta.Objects[0].Name != "Player" {
		t.Fatalf("expected object Player, got %s", meta.Objects[0].Name)
	}
	if len(meta.Objects[0].Fields) != 2 {
		t.Fatalf("expected 2 object fields, got %d", len(meta.Objects[0].Fields))
	}
	if meta.Objects[0].Fields[0].Type.Kind != schema.TypeKindScalar {
		t.Fatalf("expected first object field scalar type, got %+v", meta.Objects[0].Fields[0].Type)
	}
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callable declarations, got %d", len(meta.Callables))
	}
	if meta.Callables[0].Name != "boost" {
		t.Fatalf("expected first callable boost, got %s", meta.Callables[0].Name)
	}
	if meta.Callables[1].Parameters[0].Type.Name != "string" {
		t.Fatalf("expected second callable parameter type string, got %s", meta.Callables[1].Parameters[0].Type.Name)
	}
}

func TestParseModuleSupportsMultilineCanonicalDeclarations(t *testing.T) {
	prog, err := parseModule(strings.Join([]string{
		"struct Player {",
		"  hp: int,",
		"  name: string",
		"}",
		"fun boost(",
		"  base: int,",
		"  bonus: int",
		"): int {}",
	}, "\n"))
	if err != nil {
		t.Fatalf("parseModule multiline: %v", err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Objects) != 1 || len(meta.Objects[0].Fields) != 2 {
		t.Fatalf("expected multiline object with 2 fields, got %+v", meta.Objects)
	}
	if len(meta.Callables) != 1 || len(meta.Callables[0].Parameters) != 2 {
		t.Fatalf("expected multiline callable with 2 params, got %+v", meta.Callables)
	}
}

func TestDeclarationCollector_PreservesFrontendCallableSource(t *testing.T) {
	compiled, err := DeclarationCollector{}.CollectSource("fun greet(name: string): string = name")
	if err != nil {
		t.Fatalf("CollectSource: %v", err)
	}
	callables := compiled.CallableDeclarations()
	if len(callables) != 1 {
		t.Fatalf("expected 1 callable declaration, got %d", len(callables))
	}
	if callables[0].Source == nil {
		t.Fatal("expected callable declaration to preserve frontend-owned source payload")
	}
	if callables[0].Source.Name == nil || callables[0].Source.Name.Value != "greet" {
		t.Fatalf("expected source payload for greet, got %+v", callables[0].Source)
	}
	if !callables[0].Exported {
		if callables[0].Desc.Name != "greet" {
			t.Fatalf("expected callable descriptor greet, got %q", callables[0].Desc.Name)
		}
	}
}

func TestFrontend_LoadSourceParsesCompoundTypes(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"struct Player { tags: array<string>, attrs: map<string, int>, owner: Account }",
		"fun collect(ids: array<int>, attrs: map<string, int>): array<string> {}",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource compound types: %v", err)
	}

	player, ok := f.Object("Player")
	if !ok {
		t.Fatal("expected Player object descriptor")
	}
	if player.Fields[0].Type.Kind != schema.TypeKindArray || player.Fields[0].Type.Element == nil || player.Fields[0].Type.Element.Name != "string" {
		t.Fatalf("expected tags to be array<string>, got %+v", player.Fields[0].Type)
	}
	if player.Fields[1].Type.Kind != schema.TypeKindMap || player.Fields[1].Type.Key == nil || player.Fields[1].Type.Key.Name != "string" || player.Fields[1].Type.Value == nil || player.Fields[1].Type.Value.Name != "int" {
		t.Fatalf("expected attrs to be map<string, int>, got %+v", player.Fields[1].Type)
	}
	if player.Fields[2].Type.Kind != schema.TypeKindClass || player.Fields[2].Type.ClassName != "Account" {
		t.Fatalf("expected owner to be class Account, got %+v", player.Fields[2].Type)
	}

	collect, ok := f.Callable("collect")
	if !ok {
		t.Fatal("expected collect callable descriptor")
	}
	if collect.Parameters[0].Type.Kind != schema.TypeKindArray || collect.Returns[0].Kind != schema.TypeKindArray {
		t.Fatalf("expected array types on collect signature, got %+v", collect)
	}
}

func TestFrontend_LoadSourceWithCompileOnlyBackendRegistersNoEvaluatorAdapter(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetVMCompileHook(compileOnlyFrontendBackend{})

	if err := f.LoadSource("fun collect(ids: array<int>): array<string> {}"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("collect", []any{[]int{1, 2}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error-shaped invocation result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "no_evaluator" {
		t.Fatalf("expected no_evaluator diagnostic, got %+v", outcome.Result.Error)
	}
}

func TestFrontend_LoadSourceRegistersExecutableAdapter(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun collect(ids: array<int>): array<string> {}"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	adapter, ok := sb.Executors.Lookup("collect")
	if !ok {
		t.Fatal("expected executable adapter for source-defined callable")
	}
	if adapter.Callable().Name != "collect" {
		t.Fatalf("expected adapter callable collect, got %s", adapter.Callable().Name)
	}

	outcome, err := f.Invoke("collect", []any{[]int{1, 2}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error-shaped invocation result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "collect" {
		t.Fatalf("expected callable collect in result, got %s", outcome.Result.Callable)
	}
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "no evaluator") {
		t.Fatalf("expected no-evaluator execution error, got %+v", outcome.Result.Error)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for not-implemented execution, got %+v", outcome.Payload)
	}
}

func TestFrontend_NoEvaluatorInvocationErrorHasUnifiedEnvelope(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun collect(ids: array<int>): array<string> {}"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("collect", []any{[]int{1, 2}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected InvocationErrorDesc")
	}
	if outcome.Result.Error.DiagnosticCode == "" {
		t.Fatal("expected DiagnosticCode")
	}
	if outcome.Result.Error.Category == "" {
		t.Fatal("expected Category")
	}
	if outcome.Result.Error.Callable != "collect" {
		t.Fatalf("expected callable collect, got %q", outcome.Result.Error.Callable)
	}
	if outcome.Result.Error.Message == "" {
		t.Fatal("expected non-empty error message")
	}
	if len(outcome.Result.Error.Stack) == 0 {
		t.Fatal("expected non-empty diagnostic stack")
	}
	if outcome.Result.Error.Stack[0].Callable == "" {
		t.Fatalf("expected stack callable, got %+v", outcome.Result.Error.Stack)
	}
}

func TestFrontend_ParseErrorCarriesStructuredDiagnostic(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = f.LoadSource("fun bad(: int) {}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
	categorizer, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || categorizer.DiagnosticCategory() == "" {
		t.Fatalf("expected non-empty DiagnosticCategory, got %v", err)
	}
	spanned, ok := err.(interface{ DiagnosticSpan() diagnostics.Span })
	if !ok {
		t.Fatalf("expected diagnostic span, got %T", err)
	}
	span := spanned.DiagnosticSpan()
	if span.Start.Line <= 0 || span.Start.Column <= 0 {
		t.Fatalf("expected positive span location, got %+v", span)
	}
}

func TestFrontend_ParseErrorCarriesDiagnosticCodeAndLocation(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = f.LoadSource("fun bad(: int) {}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "parse_error" {
		t.Fatalf("expected parse_error code, got %v", err)
	}
	spanned, ok := err.(interface{ DiagnosticSpan() diagnostics.Span })
	if !ok {
		t.Fatalf("expected diagnostic span, got %T", err)
	}
	if spanned.DiagnosticSpan().Start.Line == 0 {
		t.Fatalf("expected non-zero span line, got %+v", spanned.DiagnosticSpan())
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/parse/token" {
		t.Fatalf("expected frontend/parse/token path, got %v", err)
	}
}

func TestFrontend_MultiDiagnosticErrorChainsCauses(t *testing.T) {
	err := diagnostics.NewMultiError([]error{
		newParseError(1, 1, "first"),
		newSchemaValidationError("export_boundary_violation", "second", diagnostics.Span{}, "export.parameter"),
	})
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "multiple_diagnostics" {
		t.Fatalf("expected multiple_diagnostics code, got %v", err)
	}
	cause, ok := err.(interface {
		DiagnosticCause() *diagnostics.Descriptor
	})
	if !ok || cause.DiagnosticCause() == nil {
		t.Fatalf("expected diagnostic cause chain, got %v", err)
	}
	if cause.DiagnosticCause().Code != "parse_error" {
		t.Fatalf("expected parse_error at head, got %+v", cause.DiagnosticCause())
	}
	if cause.DiagnosticCause().Cause == nil || cause.DiagnosticCause().Cause.Code != "export_boundary_violation" {
		t.Fatalf("expected export_boundary_violation chained cause, got %+v", cause.DiagnosticCause())
	}
	bundler, ok := err.(interface{ Envelope() []map[string]any })
	if !ok {
		t.Fatalf("expected bundle envelope support, got %T", err)
	}
	bundle := bundler.Envelope()
	if len(bundle) != 2 {
		t.Fatalf("expected 2 diagnostic envelopes, got %+v", bundle)
	}
	if bundle[0]["code"] != "parse_error" || bundle[1]["code"] != "export_boundary_violation" {
		t.Fatalf("unexpected bundle ordering/content: %+v", bundle)
	}
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("MarshalJSON: %v", marshalErr)
	}
	text := string(payload)
	if !strings.Contains(text, "\"code\":\"parse_error\"") || !strings.Contains(text, "\"code\":\"export_boundary_violation\"") {
		t.Fatalf("expected diagnostic codes in JSON bundle, got %s", text)
	}
}

func TestFrontend_LoadSourceUsesInternalEvaluator(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	backend := &recordingRuntimeBackend{result: []string{"ok"}}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("fun collect(ids: array<int>): array<string> {}"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("collect", []any{[]int{1, 2}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	values, ok := outcome.Payload.Value.([]string)
	if !ok || len(values) != 1 || values[0] != "ok" {
		t.Fatalf("expected evaluator payload [ok], got %+v", outcome.Payload)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "collect" {
		t.Fatalf("expected runtime backend call for collect, got %+v", backend.calls)
	}
	if len(backend.args) != 1 || len(backend.args[0]) != 1 {
		t.Fatalf("expected runtime backend args to be recorded, got %+v", backend.args)
	}
}

func TestFrontend_LoadSourceReportsExecutionTypeMismatch(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun join(left: string, right: string): string = left + right"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	_, err = f.Invoke("join", []any{"hello", 1})
	if err == nil {
		t.Fatal("expected binding argument type error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_argument_type" {
		t.Fatalf("expected invalid_argument_type code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "binding/args/type" {
		t.Fatalf("expected binding/args/type path, got %v", err)
	}
	if !strings.Contains(err.Error(), "arg \"right\" invalid") {
		t.Fatalf("expected argument localization in error, got %v", err)
	}
}

func TestFrontend_LoadSourceExportBoundaryCarriesSchemaDiagnostic(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = f.LoadSource("class Animal {}\nexport fun make(a: Animal): void {}")
	if err == nil {
		t.Fatal("expected export boundary error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "export_boundary_violation" {
		t.Fatalf("expected export_boundary_violation code, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "export.parameter" {
		t.Fatalf("expected export.parameter path, got %v", err)
	}
}

func TestFrontend_LoadSourceExportFunDoesNotAutoCreateBindingAuthority(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("export fun greet(): string {}\nfun internal(): string {}\n"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "greet" {
		t.Fatalf("expected exportable callable greet only, got %+v", exports)
	}
	if _, ok := f.LookupExportedCallable("internal"); ok {
		t.Fatal("non-export fun must not become exported callable")
	}
}

func TestFrontend_LoadSourcePreservesDeclarationFirstExportBoundary(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"class Service {",
		"  fun run(): void {}",
		"}",
		"export fun create(): void {}",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "create" {
		t.Fatalf("expected only top-level exported callable create, got %+v", exports)
	}
	if _, ok := f.Callable("run"); ok {
		t.Fatal("class method must not leak into top-level callable surface")
	}
	if _, ok := f.LookupExportedCallable("run"); ok {
		t.Fatal("class method must not leak into exported callable surface")
	}
}

type recordingVMLoweringBackend struct {
	compiled bool
	seenDesc bool
}

func (b *recordingVMLoweringBackend) CompileLoweredProgram(compiled CompiledDeclarations, prog *Program) error {
	if prog == nil || len(prog.Stmts) == 0 {
		return fmt.Errorf("expected parsed program with statements")
	}
	if len(compiled.CallableDeclarations()) == 0 {
		return fmt.Errorf("expected compiled declarations to be available at lowering seam")
	}
	b.compiled = true
	b.seenDesc = true
	return nil
}

func (b *recordingVMLoweringBackend) CompileProgram(prog *Program) error {
	if prog == nil || len(prog.Stmts) == 0 {
		return fmt.Errorf("expected parsed program with statements")
	}
	b.compiled = true
	return nil
}

func TestFrontend_LoadSourceVMHookUsesFrontendOwnedSeam(t *testing.T) {
	backend := &recordingVMLoweringBackend{}
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetVMCompileHook(backend)

	if err := f.LoadSource("fun greet(): void { return }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if !backend.compiled {
		t.Fatal("expected LoadSource to route parsed program through frontend-owned VM seam")
	}
	if !backend.seenDesc {
		t.Fatal("expected lowering seam to receive compiled declarations alongside parsed program")
	}
}

func TestCollectDeclarationsBuildsCompiledDeclarations(t *testing.T) {
	source := strings.Join([]string{
		"fun hidden(): int = 0",
		"export fun first(): int = 1",
		"struct Counter { value: int }",
		"export fun second(name: string): string = name",
	}, "\n")
	prog, err := parseModule(source)
	if err != nil {
		t.Fatalf("parseModule: %v", err)
	}
	meta := declarationsFromProgram(prog)
	compiled := compiledDeclarationsFromMetadata(meta)
	if len(compiled.CallableDeclarations()) != 3 {
		t.Fatalf("expected 3 callable declarations, got %+v", compiled.CallableDeclarations())
	}
	if len(compiled.ObjectDeclarations()) != 1 {
		t.Fatalf("expected 1 object declaration, got %+v", compiled.ObjectDeclarations())
	}
	if len(compiled.ExportedMetadata()) != 2 {
		t.Fatalf("expected 2 exported metadata entries, got %+v", compiled.ExportedMetadata())
	}
	compiledViaCollector, err := collectDeclarations(source)
	if err != nil {
		t.Fatalf("collectDeclarations: %v", err)
	}
	collector := DeclarationCollector{}
	compiledViaObject, err := collector.CollectSource(source)
	if err != nil {
		t.Fatalf("CollectSource: %v", err)
	}
	if len(compiledViaCollector.ExportedMetadata()) != len(compiled.ExportedMetadata()) {
		t.Fatalf("expected collector pipeline parity, got %+v vs %+v", compiledViaCollector.ExportedMetadata(), compiled.ExportedMetadata())
	}
	if len(compiledViaObject.ExportedMetadata()) != len(compiled.ExportedMetadata()) {
		t.Fatalf("expected collector object parity, got %+v vs %+v", compiledViaObject.ExportedMetadata(), compiled.ExportedMetadata())
	}
}

func TestFrontend_LoadSourceRegistersFromCompiledDeclarations(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"export fun first(): int = 1",
		"struct Item { value: int }",
		"fun second(a: int): int = a",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	compiled, err := compileDeclarations(source)
	if err != nil {
		t.Fatalf("compileDeclarations: %v", err)
	}
	if len(compiled.Callables()) != len(f.Callables()) {
		t.Fatalf("expected compiled and frontend callables to match, got %d vs %d", len(compiled.Callables()), len(f.Callables()))
	}
	if len(compiled.Objects()) != len(f.Objects()) {
		t.Fatalf("expected compiled and frontend objects to match, got %d vs %d", len(compiled.Objects()), len(f.Objects()))
	}
	if len(compiled.ExportedCallables()) != len(f.ExportedCallables()) {
		t.Fatalf("expected compiled and frontend exports to match, got %d vs %d", len(compiled.ExportedCallables()), len(f.ExportedCallables()))
	}
	if _, ok := sb.Callables.Lookup("first"); !ok {
		t.Fatal("expected first callable registered in binding")
	}
}

func TestFrontend_CompiledDeclarationsIncludeImportsAndExportedVariables(t *testing.T) {
	compiled, err := compileDeclarations("import add as plus from \"math\"\nexport var answer: int = 42\nfun use(): int = answer")
	if err != nil {
		t.Fatalf("compileDeclarations: %v", err)
	}
	imports := compiled.Imports()
	if len(imports) != 1 {
		t.Fatalf("expected 1 import, got %+v", imports)
	}
	if imports[0].Name != "add" || imports[0].Alias != "plus" || imports[0].Path != "math" {
		t.Fatalf("unexpected import metadata: %+v", imports[0])
	}
	vars := compiled.ExportedVariables()
	if len(vars) != 1 || vars[0].Name != "answer" {
		t.Fatalf("unexpected exported variables: %+v", vars)
	}
	if lookup, ok := compiled.LookupExportedVariable("answer"); !ok || lookup.Type != "int" {
		t.Fatalf("expected exported variable lookup, got %+v %v", lookup, ok)
	}
}

func TestFrontend_ExportedCallablesEmptyWithoutExports(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := f.LoadSource("fun local(a: int): int = a"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	exports := f.ExportedCallables()
	if len(exports) != 0 {
		t.Fatalf("expected no exports, got %d", len(exports))
	}
	if _, ok := f.LookupExportedCallable("local"); ok {
		t.Fatal("expected local callable to not be discoverable as exported")
	}
}

func TestFrontend_ExportedCallablesSignatureOrderAndCopies(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"fun hidden(): int = 0",
		"export fun first(): int = 1",
		"struct Counter { value: int }",
		"export fun second(name: string): string = name",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	compiled, err := compileDeclarations(source)
	if err != nil {
		t.Fatalf("compileDeclarations: %v", err)
	}
	if len(compiled.CallableDeclarations()) != 3 {
		t.Fatalf("expected compiled callable declaration payloads, got %+v", compiled.CallableDeclarations())
	}
	if len(compiled.ObjectDeclarations()) != 1 {
		t.Fatalf("expected compiled object declaration payloads, got %+v", compiled.ObjectDeclarations())
	}
	if len(compiled.ExportedCallables()) != 2 {
		t.Fatalf("expected 2 compiled exports, got %+v", compiled)
	}

	exports := f.ExportedCallables()
	if len(exports) != 2 {
		t.Fatalf("expected 2 exports, got %d", len(exports))
	}
	if exports[0].Name != "first" || exports[1].Name != "second" {
		t.Fatalf("unexpected export order: %+v", exports)
	}
	if len(exports[1].Parameters) != 1 || exports[1].Parameters[0].Name != "name" || exports[1].Parameters[0].Type.Name != "string" {
		t.Fatalf("unexpected second export parameters: %+v", exports[1].Parameters)
	}

	lookup, ok := f.LookupExportedCallable("second")
	if !ok || lookup.Name != "second" {
		t.Fatalf("expected second export lookup, got %+v %v", lookup, ok)
	}
	if _, ok := f.LookupExportedCallable("hidden"); ok {
		t.Fatal("expected hidden callable to be excluded")
	}

	meta := f.Declarations()
	if len(meta.ExportedCallables) != 2 {
		t.Fatalf("expected 2 exported declarations, got %+v", meta)
	}
	if meta.ExportedCallables[0].Name != "first" || meta.ExportedCallables[1].Name != "second" {
		t.Fatalf("unexpected declaration export order: %+v", meta.ExportedCallables)
	}

	if compiled.ExportedCallables()[0].Name != "first" || compiled.ExportedCallables()[1].Name != "second" {
		t.Fatalf("unexpected compiled export order: %+v", compiled.ExportedCallables())
	}
	lookup, ok = compiled.LookupExportedCallable("second")
	if !ok || lookup.Name != "second" {
		t.Fatalf("expected compiled lookup for second, got %+v %v", lookup, ok)
	}

	exportedMeta := compiled.ExportedMetadata()
	if len(exportedMeta) != 2 {
		t.Fatalf("expected 2 exported metadata entries, got %+v", exportedMeta)
	}
	if exportedMeta[0].Name != "first" || exportedMeta[1].Name != "second" {
		t.Fatalf("unexpected exported metadata order: %+v", exportedMeta)
	}
	if exportedMeta[1].ReturnType != "string" {
		t.Fatalf("expected second return type string, got %+v", exportedMeta[1])
	}
	if len(exportedMeta[1].Parameters) != 1 || exportedMeta[1].Parameters[0].Name != "name" || exportedMeta[1].Parameters[0].Type != "string" {
		t.Fatalf("unexpected exported metadata params: %+v", exportedMeta[1].Parameters)
	}
	metaLookup, ok := compiled.LookupExportedMetadata("second")
	if !ok || metaLookup.Name != "second" {
		t.Fatalf("expected exported metadata lookup for second, got %+v %v", metaLookup, ok)
	}

	exports[0].Name = "mutated"
	exports[1].Parameters[0].Name = "changed"
	again := f.ExportedCallables()
	if again[0].Name != "first" {
		t.Fatalf("expected export copy isolation, got %+v", again)
	}
	if again[1].Parameters[0].Name != "name" {
		t.Fatalf("expected parameter copy isolation, got %+v", again[1].Parameters)
	}
}

func TestFrontend_LoadSourceRegistersCallableDescriptors(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"fun boost(base: int, bonus: int): int {}",
		"fun echo(message: string): string {}",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	boost, ok := f.Callable("boost")
	if !ok {
		t.Fatal("expected boost callable after source load")
	}
	if boost.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary callable mode, got %s", boost.Mode)
	}
	if len(boost.Parameters) != 2 {
		t.Fatalf("expected 2 boost parameters, got %d", len(boost.Parameters))
	}
	if boost.Parameters[0].Name != "base" || boost.Parameters[0].Type.Name != "int" {
		t.Fatalf("unexpected first boost parameter: %+v", boost.Parameters[0])
	}
	if len(boost.Returns) != 1 || boost.Returns[0].Name != "int" {
		t.Fatalf("unexpected boost return types: %+v", boost.Returns)
	}

	echo, ok := sb.Callables.Lookup("echo")
	if !ok {
		t.Fatal("expected echo callable descriptor in script binding")
	}
	if len(echo.Parameters) != 1 || echo.Parameters[0].Type.Name != "string" {
		t.Fatalf("unexpected echo parameters: %+v", echo.Parameters)
	}
}

func TestFrontend_LoadSourceSupportsMultilineDeclarations(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"struct Player {",
		"  hp: int,",
		"  name: string",
		"}",
		"fun boost(",
		"  base: int,",
		"  bonus: int",
		"): int {}",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource multiline: %v", err)
	}
	player, ok := f.Object("Player")
	if !ok || len(player.Fields) != 2 {
		t.Fatalf("expected multiline Player object with 2 fields, got %+v %v", player, ok)
	}
	boost, ok := f.Callable("boost")
	if !ok || len(boost.Parameters) != 2 {
		t.Fatalf("expected multiline boost callable with 2 params, got %+v %v", boost, ok)
	}
}

func TestFrontend_LoadSourceExtractsObjectDescriptors(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := strings.Join([]string{
		"struct Player { hp: int, name: string }",
		"struct Health { value: int }",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	objects := f.Objects()
	if len(objects) != 2 {
		t.Fatalf("expected 2 object descriptors, got %d", len(objects))
	}
	player, ok := f.Object("Player")
	if !ok {
		t.Fatal("expected Player object descriptor")
	}
	if len(player.Fields) != 2 {
		t.Fatalf("expected 2 Player fields, got %d", len(player.Fields))
	}
	if player.Fields[0].Name != "hp" || player.Fields[0].Type.Name != "int" {
		t.Fatalf("unexpected first Player field: %+v", player.Fields[0])
	}
}

func TestFrontend_LoadSourceRejectsUnknownDeclaration(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.LoadSource("widget Player { hp int }")
	if err == nil {
		t.Fatal("expected unknown declaration error")
	}
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), "expected declaration") {
		t.Fatalf("expected line-aware unknown declaration error, got %v", err)
	}
}

func TestFrontend_LoadSourceRejectsDuplicateParameterNames(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.LoadSource("fun boost(base: int, base: int): int {}")
	if err == nil {
		t.Fatal("expected duplicate parameter name error")
	}
	if !strings.Contains(err.Error(), "duplicate parameter") {
		t.Fatalf("expected duplicate parameter diagnostic, got %v", err)
	}
}

func TestFrontend_LoadSourceRejectsDuplicateFieldNames(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.LoadSource("struct Player { hp: int, hp: int }")
	if err == nil {
		t.Fatal("expected duplicate field name error")
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("expected duplicate field diagnostic, got %v", err)
	}
}

func TestFrontend_LoadSourceRejectsInvalidCompoundType(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.LoadSource("fun bad(items: map<string>): int {}")
	if err == nil {
		t.Fatal("expected invalid compound type error")
	}
	if !strings.Contains(err.Error(), "expected ,") && !strings.Contains(err.Error(), "invalid map type") && !strings.Contains(err.Error(), "expected type") {
		t.Fatalf("expected invalid map type diagnostic, got %v", err)
	}
}

func TestFrontend_LoadSourceRejectsInvalidSource(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = f.LoadSource("fun broken(base: int):")
	if err == nil {
		t.Fatal("expected invalid source error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line context in parse error, got %v", err)
	}
}

func TestFrontend_LoadSourceRejectsDuplicateCallable(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := "fun boost(base: int): int {}"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("initial LoadSource: %v", err)
	}
	if err := f.LoadSource(source); err == nil {
		t.Fatal("expected duplicate callable registration error")
	}
}

func TestFrontend_LoadSourceRejectsDuplicateObject(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	source := "struct Player { hp: int }"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("initial LoadSource: %v", err)
	}
	if err := f.LoadSource(source); err == nil {
		t.Fatal("expected duplicate object registration error")
	}
}

func TestFrontend_InvokeUsesScriptBindingOutcome(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("boost", func(base int, bonus int) int { return base + bonus }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	outcome, err := f.Invoke("boost", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "boost" {
		t.Fatalf("expected callable boost, got %s", outcome.Result.Callable)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestFrontend_InvokeMissingCallableUsesBindingError(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = f.Invoke("missing", nil)
	if err == nil {
		t.Fatal("expected missing callable error")
	}
	if err.Error() != "callable \"missing\" is not registered" {
		t.Fatalf("expected binding missing callable error, got %v", err)
	}
}

func TestFrontend_CompiledDeclarationsIncludeExportedObjectsAndTypes(t *testing.T) {
	compiled, err := compileDeclarations(`export struct Point { x: int, y: int }
export type UserId = string
export var origin: Point = Point{x: 0, y: 0}
fun use(p: Point): UserId { return "test" }
`)
	if err != nil {
		t.Fatalf("compileDeclarations: %v", err)
	}
	objects := compiled.ExportedObjects()
	if len(objects) != 1 || objects[0].Name != "Point" {
		t.Fatalf("expected 1 exported object Point, got %+v", objects)
	}
	if len(objects[0].Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(objects[0].Fields))
	}
	types := compiled.ExportedTypes()
	if len(types) != 1 || types[0].Name != "UserId" {
		t.Fatalf("expected 1 exported type UserId, got %+v", types)
	}
	if types[0].Type != "string" {
		t.Fatalf("expected UserId type string, got %s", types[0].Type)
	}
	if types[0].TypeDesc.Kind != schema.TypeKindScalar || types[0].TypeDesc.Name != "string" {
		t.Fatalf("expected UserId TypeDesc scalar string, got %+v", types[0].TypeDesc)
	}
}

func TestFrontend_ModuleLinkImportsExportedStruct(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }
export fun makePoint(x: int, y: int): Point { return Point{x: x, y: y} }`,
	})

	if err := f.LoadSource(`import Point from "geom"
import makePoint from "geom"
fun use(): int {
	var p: Point = makePoint(1, 2)
	return p.x
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	compiled := f.CompiledDeclarations()
	// Imported struct should appear in importedTypes, not importedSymbols.
	symbols := compiled.ImportedSymbols()
	types := compiled.ImportedTypes()

	var hasPointSymbol bool
	for _, sym := range symbols {
		if sym.LocalName == "Point" {
			hasPointSymbol = true
		}
	}
	if hasPointSymbol {
		t.Fatal("imported struct Point should not appear as runtime imported symbol")
	}

	var pointType *ImportedTypeMetadata
	for i := range types {
		if types[i].LocalName == "Point" {
			pointType = &types[i]
			break
		}
	}
	if pointType == nil {
		t.Fatalf("expected imported type Point, got %+v", types)
	}
	if pointType.Kind != "struct" {
		t.Fatalf("expected Kind struct, got %s", pointType.Kind)
	}
	if pointType.ObjectDesc == nil || pointType.ObjectDesc.Name != "Point" {
		t.Fatalf("expected ObjectDesc for Point, got %+v", pointType.ObjectDesc)
	}
}

func TestFrontend_ModuleLinkImportsExportedTypeAlias(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"types": `export type UserId = string
export fun getName(): UserId { return "alice" }`,
	})

	if err := f.LoadSource(`import UserId from "types"
import getName from "types"
fun use(): UserId {
	return getName()
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	compiled := f.CompiledDeclarations()
	types := compiled.ImportedTypes()

	var userIdType *ImportedTypeMetadata
	for i := range types {
		if types[i].LocalName == "UserId" {
			userIdType = &types[i]
			break
		}
	}
	if userIdType == nil {
		t.Fatalf("expected imported type UserId, got %+v", types)
	}
	if userIdType.Kind != "type" {
		t.Fatalf("expected Kind type, got %s", userIdType.Kind)
	}
	if userIdType.TypeDesc.Kind != schema.TypeKindScalar || userIdType.TypeDesc.Name != "string" {
		t.Fatalf("expected TypeDesc scalar string, got %+v", userIdType.TypeDesc)
	}
}

func TestFrontend_ModuleLinkMissingExportedStructOrType(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"empty": `export fun foo(): int { return 1 }`,
	})

	err = f.LoadSource(`import MissingStruct from "empty"
fun use(): int { return 1 }`)
	if err == nil {
		t.Fatal("expected error for missing exported struct")
	}
	if !strings.Contains(err.Error(), "does not export") {
		t.Fatalf("expected missing export error, got %v", err)
	}
}

func TestFrontend_ModuleLinkStructTypeUsedInLocalCallableSignature(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }`,
	})

	if err := f.LoadSource(`import Point from "geom"
fun dist(p: Point): int { return p.x + p.y }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callable, ok := f.Callable("dist")
	if !ok {
		t.Fatal("expected callable dist to be registered")
	}
	if len(callable.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(callable.Parameters))
	}
	param := callable.Parameters[0]
	if param.Type.Kind != schema.TypeKindStruct || param.Type.Name != "Point" {
		t.Fatalf("expected parameter type Point (struct), got %+v", param.Type)
	}
}

func TestFrontend_ModuleLinkImportTypeCollidesWithLocalDeclaration(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"types": `export type UserId = string`,
	})

	err = f.LoadSource(`import UserId from "types"
struct UserId { name: string }
fun use(): int { return 1 }`)
	if err == nil {
		t.Fatal("expected error when import collides with local struct declaration")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

func TestFrontend_ModuleLinkImportTypeAliasArrayOfStruct(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"types": `export struct Point { x: int, y: int }
export type Points = array<Point>`,
	})

	if err := f.LoadSource(`import Points from "types"
import Point from "types"
fun firstX(points: Points): int {
	return points[0].x
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callable, ok := f.Callable("firstX")
	if !ok {
		t.Fatal("expected callable firstX")
	}
	if len(callable.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(callable.Parameters))
	}
	param := callable.Parameters[0]
	if param.Type.Kind != schema.TypeKindArray {
		t.Fatalf("expected array parameter, got %+v", param.Type)
	}
	if param.Type.Element == nil {
		t.Fatal("expected array element type")
	}
	if param.Type.Element.Kind != schema.TypeKindStruct || param.Type.Element.Name != "Point" {
		t.Fatalf("expected array<Point>, got %+v", param.Type.Element)
	}
}

func TestFrontend_ModuleLinkImportStructThenUseInLocalStructField(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }`,
	})

	if err := f.LoadSource(`import Point from "geom"
struct Line { start: Point, end: Point }
fun length(): int { return 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	obj, ok := f.Object("Line")
	if !ok {
		t.Fatal("expected object Line")
	}
	if len(obj.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(obj.Fields))
	}
	for _, field := range obj.Fields {
		if field.Type.Kind != schema.TypeKindStruct || field.Type.Name != "Point" {
			t.Fatalf("expected Point field, got %+v", field.Type)
		}
	}
}

func TestFrontend_NativeModuleImportStructType(t *testing.T) {
	sb := binding.NewScriptBinding()
	desc := binding.CapabilityDesc{
		Name: "geom",
		Objects: []schema.ObjectDesc{
			{Kind: schema.TypeKindStruct, Name: "Point", Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
				{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			}},
		},
	}
	cap := binding.RegisteredCapability{Desc: desc}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}

	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.LoadSource(`import Point from "geom"
fun dist(p: Point): int { return p.x + p.y }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callable, ok := f.Callable("dist")
	if !ok {
		t.Fatal("expected callable dist")
	}
	if len(callable.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(callable.Parameters))
	}
	param := callable.Parameters[0]
	if param.Type.Kind != schema.TypeKindStruct || param.Type.Name != "Point" {
		t.Fatalf("expected parameter type Point (struct), got %+v", param.Type)
	}

	// Verify imported type metadata, not symbol metadata.
	compiled := f.CompiledDeclarations()
	for _, sym := range compiled.ImportedSymbols() {
		if sym.LocalName == "Point" {
			t.Fatal("native imported struct Point should not appear as runtime imported symbol")
		}
	}
	types := compiled.ImportedTypes()
	var pointType *ImportedTypeMetadata
	for i := range types {
		if types[i].LocalName == "Point" {
			pointType = &types[i]
			break
		}
	}
	if pointType == nil {
		t.Fatalf("expected imported type Point, got %+v", types)
	}
	if pointType.Kind != "struct" {
		t.Fatalf("expected Kind struct, got %s", pointType.Kind)
	}
}

func TestFrontend_NativeModuleImportTypeAlias(t *testing.T) {
	sb := binding.NewScriptBinding()
	desc := binding.CapabilityDesc{
		Name: "types",
		TypeAliases: map[string]schema.TypeDesc{
			"UserId": {Kind: schema.TypeKindScalar, Name: "string"},
		},
	}
	cap := binding.RegisteredCapability{Desc: desc}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}

	f, err := New(sb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.LoadSource(`import UserId from "types"
fun greet(name: UserId): string { return "hello " + name }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callable, ok := f.Callable("greet")
	if !ok {
		t.Fatal("expected callable greet")
	}
	if len(callable.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(callable.Parameters))
	}
	param := callable.Parameters[0]
	if param.Type.Kind != schema.TypeKindScalar || param.Type.Name != "string" {
		t.Fatalf("expected parameter type string, got %+v", param.Type)
	}
}

func TestFrontend_ModuleCacheReusesParsedModules(t *testing.T) {
	callCount := 0
	resolver := MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
	}
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Wrap resolver to count invocations.
	countingResolver := &countingModuleResolver{inner: resolver, count: &callCount}
	f.SetModuleResolver(countingResolver)

	// First LoadSource triggers module parsing.
	if err := f.LoadSource(`import add from "math"
fun calc1(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource 1: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 resolver call after first load, got %d", callCount)
	}

	// Second LoadSource with the same module should reuse cache.
	if err := f.LoadSource(`import add from "math"
fun calc2(): int { return add(3, 4) }`); err != nil {
		t.Fatalf("LoadSource 2: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 resolver call after second load (cache hit), got %d", callCount)
	}

	// Clear cache and load again should trigger re-parse.
	f.ClearModuleCache()
	if err := f.LoadSource(`import add from "math"
fun calc3(): int { return add(5, 6) }`); err != nil {
		t.Fatalf("LoadSource 3: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 resolver calls after cache clear, got %d", callCount)
	}
}

type countingModuleResolver struct {
	inner MapModuleResolver
	count *int
}

func (r *countingModuleResolver) ResolveModule(path string) (string, error) {
	*r.count++
	return r.inner.ResolveModule(path)
}

func TestFrontend_CyclicTypeImport_StructNamesResolve(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"a": `export struct Node { value: int }
export struct Edge { source: Node, target: Node }`,
		"b": `import Node from "a"
export struct Graph { nodes: array<Node> }`,
	})
	if err := f.LoadSource(`import Node from "a"
import Graph from "b"
fun use(n: Node): int { return 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	// Verify that Node (imported struct) is usable in local callable signature.
	callable, ok := f.Callable("use")
	if !ok {
		t.Fatal("expected callable use")
	}
	if len(callable.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(callable.Parameters))
	}
	if callable.Parameters[0].Type.Kind != schema.TypeKindStruct || callable.Parameters[0].Type.Name != "Node" {
		t.Fatalf("expected Node parameter, got %+v", callable.Parameters[0].Type)
	}
}

func TestFrontend_CyclicTypeImport_BetweenTwoModules(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"a": `import B from "b"
export struct A { next: B }`,
		"b": `import A from "a"
export struct B { prev: A }`,
	})
	if err := f.LoadSource(`import A from "a"
import B from "b"
fun use(a: A, b: B): int { return 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	callable, ok := f.Callable("use")
	if !ok {
		t.Fatal("expected callable use")
	}
	if len(callable.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(callable.Parameters))
	}
	if callable.Parameters[0].Type.Kind != schema.TypeKindStruct || callable.Parameters[0].Type.Name != "A" {
		t.Fatalf("expected A parameter, got %+v", callable.Parameters[0].Type)
	}
	if callable.Parameters[1].Type.Kind != schema.TypeKindStruct || callable.Parameters[1].Type.Name != "B" {
		t.Fatalf("expected B parameter, got %+v", callable.Parameters[1].Type)
	}
}

func TestFrontend_CyclicRuntimeImport_StillRejected(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Re-exports trigger transitive module resolution, which surfaces the cycle.
	f.SetModuleResolver(MapModuleResolver{
		"a": `export fb from "b"
export fun fa(): int { return 1 }`,
		"b": `export fa from "a"
export fun fb(): int { return 2 }`,
	})
	err = f.LoadSource(`import fa from "a"
fun use(): int { return fa() }`)
	if err == nil {
		t.Fatal("expected cyclic import error for runtime symbols")
	}
	// Runtime cyclic imports should be rejected; accept either "cyclic" or "missing" diagnostic.
	if !strings.Contains(err.Error(), "cyclic") && !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected cyclic or missing error, got %v", err)
	}
}

func TestFrontend_WildcardReExport_Metadata(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun mul(a: int, b: int): int { return a * b }
export struct Point { x: int, y: int }
export type IntPair = array<int>`,
		"api": `export * from "math"`,
	})
	if err := f.LoadSource(`import add from "api"
fun use(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	// Local callable should be registered.
	if _, ok := f.Callable("use"); !ok {
		t.Fatal("expected local callable use")
	}
	// Verify imported symbol metadata for the callable import.
	decl := f.Declarations()
	var foundAdd bool
	for _, sym := range decl.ImportedSymbols {
		if sym.Name == "add" && sym.Path == "api" {
			foundAdd = true
			break
		}
	}
	if !foundAdd {
		t.Logf("ImportedSymbols: %+v", decl.ImportedSymbols)
		t.Fatal("expected add in imported symbols from api")
	}
}

func TestFrontend_ModulePathValidation(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		invalid bool
	}{
		{"simple name", "math", false},
		{"with slash", "std/math", false},
		{"with dot", "./local", false},
		{"with hyphen", "my-module", false},
		{"with underscore", "my_module", false},
		{"with at-sign", "@scope/lib", false},
		{"nested path", "a/b/c", false},
		{"empty", "", true},
		{"absolute slash", "/math", true},
		{"absolute backslash", `\math`, true},
		{"traversal", "../escape", true},
		{"traversal mid", "foo/../bar", true},
		{"consecutive slashes", "std//math", true},
		{"trailing slash", "math/", true},
		{"trailing backslash", `math\`, true},
		{"invalid char space", "my module", true},
		{"invalid char colon", "my:module", true},
		{"invalid char percent", "my%module", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := New(binding.NewScriptBinding())
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			f.SetModuleResolver(MapModuleResolver{
				"math": `export fun add(a: int, b: int): int { return a + b }`,
			})
			src := fmt.Sprintf(`import add from "%s"%sfun use(): int { return add(1, 2) }`, tc.path, "\n")
			err = f.LoadSource(src)
			if tc.invalid {
				if err == nil {
					t.Fatalf("expected error for path %q", tc.path)
				}
				msg := err.Error()
				if !strings.Contains(msg, "module path") && !strings.Contains(msg, "expected string literal") {
					t.Fatalf("expected module path error for path %q, got %v", tc.path, err)
				}
			} else {
				// For valid paths that aren't in resolver, expect "not found" rather than module path error
				if err != nil && (strings.Contains(err.Error(), "module path") || strings.Contains(err.Error(), "cannot be empty")) {
					t.Fatalf("unexpected validation error for path %q: %v", tc.path, err)
				}
			}
		})
	}
}

func TestFrontend_ModuleGraph(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun mul(a: int, b: int): int { return a * b }`,
		"strings": `export fun concat(a: string, b: string): string { return a + b }`,
		"api": `export * from "math"
export * from "strings"`,
	})
	if err := f.LoadSource(`import add from "api"
import concat from "strings"
fun use(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	g := f.ModuleGraph()
	if g == nil {
		t.Fatal("expected module graph")
	}

	// Check modules present.
	mods := g.Modules()
	if len(mods) != 4 { // root (""), api, math, strings
		t.Fatalf("expected 4 modules, got %d: %v", len(mods), mods)
	}

	// Check dependencies.
	rootDeps := g.Dependencies("")
	if len(rootDeps) != 2 || !containsStr(rootDeps, "api") || !containsStr(rootDeps, "strings") {
		t.Fatalf("expected root deps [api strings], got %v", rootDeps)
	}

	apiDeps := g.Dependencies("api")
	if len(apiDeps) != 2 || !containsStr(apiDeps, "math") || !containsStr(apiDeps, "strings") {
		t.Fatalf("expected api deps [math strings], got %v", apiDeps)
	}

	mathDeps := g.Dependencies("math")
	if len(mathDeps) != 0 {
		t.Fatalf("expected math deps empty, got %v", mathDeps)
	}

	// Check reverse dependencies.
	mathRev := g.ReverseDependencies("math")
	if len(mathRev) != 1 || mathRev[0] != "api" {
		t.Fatalf("expected math reverse deps [api], got %v", mathRev)
	}

	// Check no cycle.
	if g.HasCycle() {
		t.Fatalf("expected no cycle, got: %v", g.CyclePath())
	}

	// Check topological order.
	order, err := g.TopologicalOrder()
	if err != nil {
		t.Fatalf("topological order error: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("expected order length 4, got %d: %v", len(order), order)
	}
}

func TestFrontend_ModuleGraph_CycleDetection(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"a": `export fb from "b"
export fun fa(): int { return 1 }`,
		"b": `export fa from "a"
export fun fb(): int { return 2 }`,
	})
	// Loading should fail due to cyclic runtime import.
	_ = f.LoadSource(`import fa from "a"
fun use(): int { return fa() }`)
	// Graph should still be available and show the cycle.
	g := f.ModuleGraph()
	if g == nil {
		t.Fatal("expected module graph even after error")
	}
	if !g.HasCycle() {
		t.Fatal("expected cycle in graph")
	}
	cycle := g.CyclePath()
	if len(cycle) == 0 {
		t.Fatal("expected non-empty cycle path")
	}
	// Cycle should involve "a" and "b".
	var hasA, hasB bool
	for _, p := range cycle {
		if p == "a" {
			hasA = true
		}
		if p == "b" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("expected cycle to involve a and b, got %v", cycle)
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func TestFrontend_ModuleExports_Introspection(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun mul(a: int, b: int): int { return a * b }
export struct Point { x: int, y: int }
export type IntPair = array<int>
export var PI: double = 3.14`,
	})
	if err := f.LoadSource(`import add from "math"
fun use(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Query single module exports.
	exports, ok := f.ModuleExports("math")
	if !ok {
		t.Fatal("expected exports for math module")
	}
	if exports.Path != "math" {
		t.Fatalf("expected path math, got %q", exports.Path)
	}
	if len(exports.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(exports.Callables))
	}
	if len(exports.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(exports.Objects))
	}
	if len(exports.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(exports.Types))
	}
	if len(exports.Variables) != 1 {
		t.Fatalf("expected 1 variable, got %d", len(exports.Variables))
	}

	// Verify callable names.
	foundAdd, foundMul := false, false
	for _, c := range exports.Callables {
		if c.Name == "add" {
			foundAdd = true
		}
		if c.Name == "mul" {
			foundMul = true
		}
	}
	if !foundAdd || !foundMul {
		t.Fatalf("expected add and mul callables, got %+v", exports.Callables)
	}

	// Query unknown module.
	_, ok = f.ModuleExports("unknown")
	if ok {
		t.Fatal("expected no exports for unknown module")
	}

	// Query all modules.
	all := f.AllModuleExports()
	if len(all) != 1 {
		t.Fatalf("expected 1 module, got %d", len(all))
	}
	if all[0].Path != "math" {
		t.Fatalf("expected math, got %q", all[0].Path)
	}
}

func TestFrontend_ModuleExports_SporeSyntax(t *testing.T) {
	f, err := New(binding.NewScriptBinding())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun mul(a: int, b: int): int { return a * b }
export struct Point { x: int, y: int }
export type IntPair = array<int>
export var PI: double = 3.14`,
	})
	if err := f.LoadSource(`import add from "math"
fun use(): int { return add(1, 2) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	exports, ok := f.ModuleExports("math")
	if !ok {
		t.Fatal("expected exports for math")
	}

	syns := exports.SporeSyntax()
	if !strings.Contains(syns, "fun add(a: int, b: int): int") {
		t.Fatalf("expected add syntax, got:\n%s", syns)
	}
	if !strings.Contains(syns, "fun mul(a: int, b: int): int") {
		t.Fatalf("expected mul syntax, got:\n%s", syns)
	}
	if !strings.Contains(syns, "struct Point {x: int, y: int}") {
		t.Fatalf("expected Point syntax, got:\n%s", syns)
	}
	if !strings.Contains(syns, "type IntPair = array<int>") {
		t.Fatalf("expected IntPair syntax, got:\n%s", syns)
	}
	if !strings.Contains(syns, "var PI: double") {
		t.Fatalf("expected PI syntax, got:\n%s", syns)
	}
}

func TestSporeSyntax_Types(t *testing.T) {
	cases := []struct {
		name     string
		td       schema.TypeDesc
		expected string
	}{
		{"scalar int", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, "int"},
		{"scalar string", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, "string"},
		{"array", schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}, "array<int>"},
		{"map", schema.TypeDesc{Kind: schema.TypeKindMap, Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}, "map<string, int>"},
		{"struct", schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Point"}, "Point"},
		{"class", schema.TypeDesc{Kind: schema.TypeKindClass, ClassName: "User"}, "User"},
		{"void", schema.TypeDesc{Kind: schema.TypeKindVoid}, "void"},
		{"nested array", schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}}, "array<array<int>>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SporeSyntax(tc.td)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestSporeSyntax_Callable(t *testing.T) {
	c := schema.CallableDesc{
		Name: "add",
		Parameters: []schema.ParameterDesc{
			{Name: "a", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "b", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		Mode:    schema.CallableModeUnary,
	}
	got := CallableSporeSyntax(c)
	expected := "fun add(a: int, b: int): int"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}

	stream := schema.CallableDesc{
		Name: "chat",
		Parameters: []schema.ParameterDesc{
			{Name: "prompt", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		Mode:    schema.CallableModeStreaming,
	}
	gotStream := CallableSporeSyntax(stream)
	expectedStream := "stream fun chat(prompt: string): string"
	if gotStream != expectedStream {
		t.Fatalf("expected %q, got %q", expectedStream, gotStream)
	}
}

func TestParseValue_Scalars(t *testing.T) {
	cases := []struct {
		source   string
		expected any
	}{
		{"42", int64(42)},
		{"-5", int64(-5)},
		{"3.14", 3.14},
		{`"hello"`, "hello"},
		{"true", true},
		{"false", false},
		{"null", nil},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got, err := ParseValue(tc.source)
			if err != nil {
				t.Fatalf("ParseValue(%q): %v", tc.source, err)
			}
			if got != tc.expected {
				t.Fatalf("ParseValue(%q) = %v, want %v", tc.source, got, tc.expected)
			}
		})
	}
}

func TestParseValue_StructLiteral(t *testing.T) {
	got, err := ParseValue(`Point{x: 1, y: 2}`)
	if err != nil {
		t.Fatalf("ParseValue: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["__struct__"] != "Point" {
		t.Fatalf("expected struct name Point, got %v", m["__struct__"])
	}
	if m["x"] != int64(1) || m["y"] != int64(2) {
		t.Fatalf("expected x=1, y=2, got x=%v, y=%v", m["x"], m["y"])
	}
}

func TestParseValue_Array(t *testing.T) {
	got, err := ParseValue(`[1, 2, 3]`)
	if err != nil {
		t.Fatalf("ParseValue: %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", got)
	}
	if len(arr) != 3 || arr[0] != int64(1) || arr[1] != int64(2) || arr[2] != int64(3) {
		t.Fatalf("expected [1, 2, 3], got %v", arr)
	}
}

func TestParseValue_Map(t *testing.T) {
	got, err := ParseValue(`{"a": 1, "b": 2}`)
	if err != nil {
		t.Fatalf("ParseValue: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["a"] != int64(1) || m["b"] != int64(2) {
		t.Fatalf("expected {a:1, b:2}, got %v", m)
	}
}

func TestParseArgs(t *testing.T) {
	args, err := ParseArgs(`1, "hello", Point{x: 1, y: 2}`)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if args[0] != int64(1) {
		t.Fatalf("expected arg[0]=1, got %v", args[0])
	}
	if args[1] != "hello" {
		t.Fatalf("expected arg[1]=hello, got %v", args[1])
	}
	m, ok := args[2].(map[string]any)
	if !ok || m["__struct__"] != "Point" {
		t.Fatalf("expected arg[2]=Point struct, got %v", args[2])
	}
}

func TestParseCallableDesc(t *testing.T) {
	desc, err := ParseCallableDesc("fun add(a: int, b: int): int")
	if err != nil {
		t.Fatalf("ParseCallableDesc: %v", err)
	}
	if desc.Name != "add" {
		t.Fatalf("expected name add, got %q", desc.Name)
	}
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 params, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Name != "a" || SporeSyntax(desc.Parameters[0].Type) != "int" {
		t.Fatalf("unexpected param[0]: %+v", desc.Parameters[0])
	}
	if len(desc.Returns) != 1 || SporeSyntax(desc.Returns[0]) != "int" {
		t.Fatalf("unexpected returns: %+v", desc.Returns)
	}

	stream, err := ParseCallableDesc("stream fun chat(prompt: string): string")
	if err != nil {
		t.Fatalf("ParseCallableDesc stream: %v", err)
	}
	if stream.Name != "chat" || stream.Mode != schema.CallableModeStreaming {
		t.Fatalf("unexpected stream callable: %+v", stream)
	}
}

func TestParseObjectDesc(t *testing.T) {
	desc, err := ParseObjectDesc("struct Point { x: int, y: int }")
	if err != nil {
		t.Fatalf("ParseObjectDesc: %v", err)
	}
	if desc.Name != "Point" {
		t.Fatalf("expected name Point, got %q", desc.Name)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Name != "x" || SporeSyntax(desc.Fields[0].Type) != "int" {
		t.Fatalf("unexpected field[0]: %+v", desc.Fields[0])
	}
}

func TestParseObjectDesc_MediaField(t *testing.T) {
	desc, err := ParseObjectDesc("struct Avatar { photo: media, gallery: array<media> }")
	if err != nil {
		t.Fatalf("ParseObjectDesc: %v", err)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Type.Kind != schema.TypeKindMedia {
		t.Fatalf("expected media kind on photo, got %s", desc.Fields[0].Type.Kind)
	}
	if desc.Fields[1].Type.Element == nil || desc.Fields[1].Type.Element.Kind != schema.TypeKindMedia {
		t.Fatalf("expected array<media> on gallery, got %+v", desc.Fields[1].Type)
	}
}

func TestParseTypeDesc(t *testing.T) {
	cases := []struct {
		source   string
		expected string
	}{
		{"int", "int"},
		{"string", "string"},
		{"array<int>", "array<int>"},
		{"map<string, int>", "map<string, int>"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			desc, err := ParseTypeDesc(tc.source)
			if err != nil {
				t.Fatalf("ParseTypeDesc: %v", err)
			}
			got := SporeSyntax(desc)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestDebug_NodeKind(t *testing.T) {
	prog, err := parseModule(`export struct Node { value: int }
export struct Edge { source: Node, target: Node }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	meta := declarationsFromProgram(prog)
	for _, obj := range meta.Objects {
		t.Logf("Object %s: Kind=%v", obj.Name, obj.Kind)
	}
	for _, obj := range meta.ExportedObjects {
		t.Logf("ExportedObject %s: Kind=%v", obj.Name, obj.Kind)
	}
	if len(meta.Errors) > 0 {
		for _, e := range meta.Errors {
			t.Logf("Error: %v", e)
		}
	}
}

package strings_test

import (
	stdstrings "strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/strings"
)

func TestStringsModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	functions := []string{
		"strings.contains", "strings.hasPrefix", "strings.hasSuffix",
		"strings.index", "strings.lastIndex", "strings.toLower",
		"strings.toUpper", "strings.trimSpace", "strings.repeat",
		"strings.replace", "strings.join", "strings.builder",
		"strings.append", "strings.build",
	}
	for _, name := range functions {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestStringsModule_Contains(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strings.contains", Stage: binding.InvocationStageUnary, Args: []any{"hello world", "world"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}
}

func TestStringsModule_ToUpper(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strings.toUpper", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "HELLO" {
		t.Fatalf("expected HELLO, got %+v", outcome.Payload)
	}
}

func TestStringsModule_Replace(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strings.replace", Stage: binding.InvocationStageUnary, Args: []any{"hello world", "world", "spore", 1}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello spore" {
		t.Fatalf("expected hello spore, got %+v", outcome.Payload)
	}
}

func TestStringsModule_Join(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strings.join", Stage: binding.InvocationStageUnary, Args: []any{[]string{"a", "b", "c"}, "-"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "a-b-c" {
		t.Fatalf("expected a-b-c, got %+v", outcome.Payload)
	}
}

func TestStringsModule_Builder(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strings.builder", Stage: binding.InvocationStageUnary})
	if err != nil {
		t.Fatalf("builder Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("expected builder payload, got nil")
	}
	parts, ok := outcome.Payload.Value.([]string)
	if !ok && outcome.Payload.Value != nil {
		t.Fatalf("expected []string builder, got %T %v", outcome.Payload.Value, outcome.Payload.Value)
	}
	if len(parts) != 0 {
		t.Fatalf("expected empty builder, got %v", parts)
	}

	for _, part := range []string{"a", "b", "c"} {
		outcome, err = sb.Invoke(binding.InvocationRequest{Callable: "strings.append", Stage: binding.InvocationStageUnary, Args: []any{parts, part}})
		if err != nil {
			t.Fatalf("append Invoke: %v", err)
		}
		if outcome.Payload == nil {
			t.Fatalf("expected append payload, got nil")
		}
		parts, ok = outcome.Payload.Value.([]string)
		if !ok {
			t.Fatalf("expected []string append result, got %T %v", outcome.Payload.Value, outcome.Payload.Value)
		}
	}

	outcome, err = sb.Invoke(binding.InvocationRequest{Callable: "strings.build", Stage: binding.InvocationStageUnary, Args: []any{parts}})
	if err != nil {
		t.Fatalf("build Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "abc" {
		t.Fatalf("expected abc, got %+v", outcome.Payload)
	}

	outcome, err = sb.Invoke(binding.InvocationRequest{Callable: "strings.build", Stage: binding.InvocationStageUnary, Args: []any{[]string{}}})
	if err != nil {
		t.Fatalf("empty build Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "" {
		t.Fatalf("expected empty string, got %+v", outcome.Payload)
	}
}

func TestStringsModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import toUpper from "strings"
import trimSpace from "strings"
import contains from "strings"
fun greet(): string { return toUpper(trimSpace("  hello  ")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("greet", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "HELLO" {
		t.Fatalf("expected HELLO, got %+v", outcome.Payload)
	}
}

func TestStringsModule_VMBuilderLinearString(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import builder from "strings"
import append from "strings"
import build from "strings"
fun buildString(n: int): string {
  var parts: array<string> = builder()
  for (var i: int = 0; i < n; i = i + 1) { parts = append(parts, "x") }
  return build(parts)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("buildString", []any{50})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Error != nil {
		t.Fatalf("invocation error: code=%s msg=%s", outcome.Result.Error.DiagnosticCode, outcome.Result.Error.Message)
	}
	if outcome.Payload == nil {
		t.Fatalf("expected payload, got nil")
	}
	got, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string payload, got %T %v", outcome.Payload.Value, outcome.Payload.Value)
	}
	if len(got) != 50 {
		t.Fatalf("expected 50-char string, got %d-char %q", len(got), got)
	}
}

func TestStringsModule_VMJoinWithPushBuiltsLinearString(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import join from "strings"
fun build(n: int): string {
  var parts: array<string> = []
  for (var i: int = 0; i < n; i = i + 1) { push(parts, "x") }
  return join(parts, "")
}
fun dashed(): string {
  var parts: array<string> = []
  push(parts, "a")
  push(parts, "b")
  push(parts, "c")
  return join(parts, "-")
}
fun empty(): string {
  var parts: array<string> = []
  return join(parts, ",")
}
fun large(n: int): string {
  var parts: array<string> = []
  for (var i: int = 0; i < n; i = i + 1) { push(parts, "xxxxxxxxxx") }
  return join(parts, "")
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("build", []any{50})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Error != nil {
		t.Fatalf("invocation error: code=%s msg=%s", outcome.Result.Error.DiagnosticCode, outcome.Result.Error.Message)
	}
	if outcome.Payload == nil {
		t.Fatalf("expected payload, got nil")
	}
	got, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string payload, got %T %v", outcome.Payload.Value, outcome.Payload.Value)
	}
	if len(got) != 50 {
		t.Fatalf("expected 50-char string, got %d-char %q", len(got), got)
	}

	outcome, err = f.Invoke("dashed", nil)
	if err != nil {
		t.Fatalf("dashed Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "a-b-c" {
		t.Fatalf("expected a-b-c, got %+v", outcome.Payload)
	}

	outcome, err = f.Invoke("empty", nil)
	if err != nil {
		t.Fatalf("empty Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "" {
		t.Fatalf("expected empty string, got %+v", outcome.Payload)
	}

	outcome, err = f.Invoke("large", []any{30})
	if err != nil {
		t.Fatalf("large Invoke: %v", err)
	}
	want := stdstrings.Repeat("xxxxxxxxxx", 30)
	if outcome.Payload == nil || outcome.Payload.Value != want {
		t.Fatalf("expected %d-char large join, got %+v", len(want), outcome.Payload)
	}
}

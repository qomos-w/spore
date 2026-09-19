package json_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/json"
)

func TestJsonModule_VMEndToEnd_Encode(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
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

	if err := f.LoadSource(`import encode from "json"
	import decode from "json"
	fun roundtrip(): string { return decode(encode("hello")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("roundtrip", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %+v", outcome.Payload)
	}
}

func TestJsonModule_VMEndToEnd_DecodeReturnsMap(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
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

	if err := f.LoadSource(`import decode from "json"
	export fun getPerson(json: string): any {
		return decode(json)
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("getPerson", []any{`{"name":"alice","age":30}`})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind == binding.InvocationResultError {
		t.Fatalf("invocation error: %+v", outcome.Result.Error)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	m, ok := outcome.Payload.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", outcome.Payload.Value)
	}
	if m["name"] != "alice" {
		t.Fatalf("expected name=alice, got %v", m["name"])
	}
	if m["age"] != 30.0 {
		t.Fatalf("expected age=30.0, got %v", m["age"])
	}
}

func TestJsonModule_VMEndToEnd_DecodeStructViaAs(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
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

	if err := f.LoadSource(`import decode from "json"
	struct Person { name: string, age: int }
	export fun getPerson(json: string): Person {
		return decode(json) as Person
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("getPerson", []any{`{"name":"alice","age":30}`})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind == binding.InvocationResultError {
		t.Fatalf("invocation error: %+v", outcome.Result.Error)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	p, ok := outcome.Payload.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", outcome.Payload.Value)
	}
	if p["name"] != "alice" {
		t.Fatalf("expected name=alice, got %v", p["name"])
	}
	if p["age"] != 30.0 {
		t.Fatalf("expected age=30.0, got %v", p["age"])
	}
}

func TestJsonModule_VMEndToEnd_DecodeNestedStructViaAs(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := json.Register(sb); err != nil {
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

	if err := f.LoadSource(`import decode from "json"
	struct Address { city: string, zip: string }
	struct Person { name: string, age: int, addr: Address }
	export fun getPerson(json: string): Person {
		return decode(json) as Person
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("getPerson", []any{`{"name":"alice","age":30,"addr":{"city":"NYC","zip":"10001"}}`})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind == binding.InvocationResultError {
		t.Fatalf("invocation error: %+v", outcome.Result.Error)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	p, ok := outcome.Payload.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", outcome.Payload.Value)
	}
	if p["name"] != "alice" {
		t.Fatalf("expected name=alice, got %v", p["name"])
	}
	if p["age"] != 30.0 {
		t.Fatalf("expected age=30.0, got %v", p["age"])
	}
	addr, ok := p["addr"].(map[string]any)
	if !ok {
		t.Fatalf("expected addr map[string]any, got %T", p["addr"])
	}
	if addr["city"] != "NYC" {
		t.Fatalf("expected city=NYC, got %v", addr["city"])
	}
	if addr["zip"] != "10001" {
		t.Fatalf("expected zip=10001, got %v", addr["zip"])
	}
}

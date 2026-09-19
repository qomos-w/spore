package bytes_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/bytes"
)

func TestBytesModule_VMEndToEnd_BindFunc(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
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

	if err := f.LoadSource(`import { length, concat, equal } from "bytes"
	export fun measure(a: bytes, b: bytes): int {
		if equal(a, b) {
			return length(a)
		}
		return length(concat(a, b))
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("measure", []any{[]byte("hello"), []byte("hello")})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected 5 for equal bytes, got %+v", outcome.Payload)
	}

	outcome, err = f.Invoke("measure", []any{[]byte("hi"), []byte("there")})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected 7 for concat bytes, got %+v", outcome.Payload)
	}
}

func TestBytesModule_VMEndToEnd_Roundtrip(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
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

	if err := f.LoadSource(`import { slice } from "bytes"
	export fun extract(data: bytes): bytes {
		return slice(data, 2, 5)
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("extract", []any{[]byte("abcdefgh")})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	got, ok := outcome.Payload.Value.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", outcome.Payload.Value)
	}
	if string(got) != "cde" {
		t.Fatalf("expected cde, got %s", string(got))
	}
}

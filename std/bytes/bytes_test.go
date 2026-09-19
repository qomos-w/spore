package bytes_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/bytes"
)

func TestBytesModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{
		"bytes.length", "bytes.slice", "bytes.concat", "bytes.compare",
		"bytes.contains", "bytes.index", "bytes.equal", "bytes.hasPrefix",
		"bytes.hasSuffix", "bytes.repeat", "bytes.replace", "bytes.toLower",
		"bytes.toUpper", "bytes.trimSpace",
	} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestBytesModule_Length(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "bytes.length", Stage: binding.InvocationStageUnary, Args: []any{[]byte("hello")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	if outcome.Payload.Value != 5 {
		t.Fatalf("expected 5, got %v", outcome.Payload.Value)
	}
}

func TestBytesModule_Concat(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "bytes.concat", Stage: binding.InvocationStageUnary, Args: []any{[]byte("hello"), []byte(" world")}})
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
	if string(got) != "hello world" {
		t.Fatalf("expected 'hello world', got %s", string(got))
	}
}

func TestBytesModule_Equal(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "bytes.equal", Stage: binding.InvocationStageUnary, Args: []any{[]byte("a"), []byte("a")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}
}

func TestBytesModule_Index(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := bytes.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "bytes.index", Stage: binding.InvocationStageUnary, Args: []any{[]byte("hello world"), []byte("world")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 6 {
		t.Fatalf("expected 6, got %+v", outcome.Payload)
	}
}

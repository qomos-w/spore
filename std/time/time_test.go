package time_test

import (
	"testing"
	"time"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	stdtime "github.com/qomos-w/spore/std/time"
)

func TestTimeModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := stdtime.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	functions := []string{"time.nowUnix", "time.now", "time.format"}
	for _, name := range functions {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestTimeModule_NowUnix(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := stdtime.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	before := time.Now().Unix()
	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "time.nowUnix", Stage: binding.InvocationStageUnary})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	after := time.Now().Unix()

	if outcome.Payload == nil {
		t.Fatal("expected payload")
	}
	v, ok := outcome.Payload.Value.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", outcome.Payload.Value)
	}
	if v < before || v > after {
		t.Fatalf("expected timestamp between %d and %d, got %d", before, after, v)
	}
}

func TestTimeModule_Now_StructReturn(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := stdtime.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "time.now", Stage: binding.InvocationStageUnary})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload")
	}

	result, ok := outcome.Payload.Value.(stdtime.TimeInfo)
	if !ok {
		t.Fatalf("expected TimeInfo, got %T", outcome.Payload.Value)
	}
	if result.Unix == 0 {
		t.Fatalf("expected non-zero unix timestamp")
	}
	if result.RFC3339 == "" {
		t.Fatalf("expected non-empty rfc3339")
	}
}

func TestTimeModule_Format(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := stdtime.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "time.format", Stage: binding.InvocationStageUnary, Args: []any{int64(0), "2006-01-02"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "1970-01-01" {
		t.Fatalf("expected 1970-01-01, got %+v", outcome.Payload)
	}
}

func TestTimeModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := stdtime.Register(sb); err != nil {
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

	if err := f.LoadSource(`import nowUnix from "time"
import format from "time"
fun demo(): string { return format(nowUnix(), "2006-01-02") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("demo", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload")
	}
	result, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if result != time.Now().Format("2006-01-02") {
		t.Fatalf("expected today's date, got %s", result)
	}
}

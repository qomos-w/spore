package std_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
)

type nativeErrProbeResp struct {
	Status int `json:"status"`
}

func newFailingNativeFrontend(t *testing.T, src string) *frontend.Frontend {
	t.Helper()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("failmod", "module")
	must := func(err error) {
		if err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	must(builder.AddFreeFunction("noargs", func() error { return errors.New("boom") }))
	must(builder.AddFreeFunction("strInt", func(s string, n int) error { return errors.New("boom") }))
	must(builder.AddFreeFunction("retStructPtr", func(s string) (*nativeErrProbeResp, error) {
		return nil, errors.New("boom")
	}))
	must(builder.AddFreeFunction("noop", func(s string) error { return nil }))
	cap, err := builder.Build()
	must(err)
	must(sb.RegisterCapability(cap))
	must(sb.ExposeCapabilityCallables("failmod"))

	f, err := frontend.New(sb)
	must(err)
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	must(f.LoadSource(src))
	return f
}

func invokeCatching(t *testing.T, f *frontend.Frontend, fn string) string {
	t.Helper()
	outcome, err := f.Invoke(fn, nil)
	if err != nil {
		t.Fatalf("%s: Invoke: %v", fn, err)
	}
	if outcome.Payload == nil {
		t.Fatalf("%s: no payload, result=%+v err=%+v", fn, outcome.Result, outcome.Result.Error)
	}
	return fmt.Sprint(outcome.Payload.Value)
}

// Native callable failures (function-body errors) must be catchable by
// try/catch so scripts can handle host I/O failures.
func TestNativeErrorIsCatchable(t *testing.T) {
	for _, fn := range []string{"noargs", "strInt", "retStructPtr"} {
		call := fn + `("x", 1)`
		if fn == "noargs" {
			call = fn + "()"
		} else if fn == "retStructPtr" {
			call = fn + `("x")`
		}
		src := "import " + fn + ` from "failmod"` + `
fun guarded(): string {
    try {
        var v: any = ` + call + `
        return "no-error"
    } catch (e) {
        return "caught"
    }
}`
		f := newFailingNativeFrontend(t, src)
		if got := invokeCatching(t, f, "guarded"); got != "caught" {
			t.Fatalf("%s: expected caught, got %s", fn, got)
		}
	}
}

// Void natives (payload-less success) must resolve as handled callables,
// not undefined functions.
func TestNativeVoidCallableWorks(t *testing.T) {
	src := `import noop from "failmod"
fun callVoid(): string {
    noop("x")
    return "after-void"
}`
	f := newFailingNativeFrontend(t, src)
	if got := invokeCatching(t, f, "callVoid"); got != "after-void" {
		t.Fatalf("callVoid: %s", got)
	}
}

// std/http module: a connection failure must be catchable from script.
func TestHTTPErrorIsCatchableFromScript(t *testing.T) {
	f := newVMFrontend(t, `
import get from "http"
fun guarded(): string {
    try {
        var resp: map = get("http://127.0.0.1:1/x", 1000)
        return "no-error"
    } catch (e) {
        return "caught"
    }
}`)
	if got := invokeCatching(t, f, "guarded"); got != "caught" {
		t.Fatalf("expected caught, got %s", got)
	}
}

package std_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std"
)

func newVMFrontend(t *testing.T, src string) *frontend.Frontend {
	t.Helper()
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	if err := f.LoadSource(src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	return f
}

func TestHTTPVM_ResponseProjectedAsMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "body")
	}))
	defer srv.Close()

	f := newVMFrontend(t, `
import get from "http"
fun status(): int {
    var resp: map = get("`+srv.URL+`/x", 5000)
    return resp["status"]
}
fun ok(): bool {
    var resp: map = get("`+srv.URL+`/x", 5000)
    return resp["ok"]
}
fun statusText(): string {
    var resp: map = get("`+srv.URL+`/x", 5000)
    return resp["statusText"]
}
fun header(): string {
    var resp: map = get("`+srv.URL+`/x", 5000)
    return resp["headers"]["Content-Type"]
}`)

	cases := []struct {
		fn   string
		want any
	}{
		{"status", "202"},
		{"ok", "true"},
		{"statusText", "202 Accepted"},
		{"header", "text/plain"},
	}
	for _, tc := range cases {
		outcome, err := f.Invoke(tc.fn, nil)
		if err != nil {
			t.Fatalf("%s: Invoke: %v", tc.fn, err)
		}
		if outcome.Payload == nil || fmt.Sprint(outcome.Payload.Value) != tc.want {
			t.Fatalf("%s: got %#v, want %v", tc.fn, outcome.Payload, tc.want)
		}
	}
}

func TestHTTPVM_PostStringBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "spore" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	f := newVMFrontend(t, `
import post from "http"
fun created(): bool {
    var resp: map = post("`+srv.URL+`/echo", "spore", "text/plain", 5000)
    return resp["status"] == 201
}`)
	outcome, err := f.Invoke("created", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("created: no payload, result=%+v err=%+v", outcome.Result, outcome.Result.Error)
	}
	if got := fmt.Sprint(outcome.Payload.Value); got != "true" {
		t.Fatalf("created: %v", got)
	}
}

func TestHTTPVM_ErrorCatchable(t *testing.T) {
	f := newVMFrontend(t, `
import get from "http"
fun guarded(): string {
    try {
        var resp: map = get("http://127.0.0.1:1/unreachable", 1000)
        return "no-error"
    } catch (e) {
        return "caught"
    }
}`)
	outcome, err := f.Invoke("guarded", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("expected payload, result=%+v err=%+v", outcome.Result, outcome.Result.Error)
	}
	if got, _ := outcome.Payload.Value.(string); got != "caught" {
		t.Fatalf("expected caught, got %v", outcome.Payload.Value)
	}
}

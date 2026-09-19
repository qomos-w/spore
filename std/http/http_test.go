package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	stdhttp "github.com/qomos-w/spore/std/http"
)

func newHTTPBinding(t *testing.T) *binding.ScriptBinding {
	t.Helper()
	sb := binding.NewScriptBinding()
	if err := stdhttp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return sb
}

func invokeHTTP(t *testing.T, sb *binding.ScriptBinding, callable string, args ...any) (binding.InvocationOutcome, error) {
	t.Helper()
	return sb.Invoke(binding.InvocationRequest{Callable: callable, Stage: binding.InvocationStageUnary, Args: args})
}

func TestHTTP_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Multi", "a")
		w.Header().Add("X-Multi", "b")
		if r.URL.Query().Get("q") != "spore" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	sb := newHTTPBinding(t)
	outcome, err := invokeHTTP(t, sb, "http.get", srv.URL+"/echo?q=spore", int64(5000))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if outcome.Result.Error != nil {
		t.Fatalf("result error: %+v", outcome.Result.Error)
	}
	resp, ok := outcome.Payload.Value.(*stdhttp.Response)
	if !ok {
		t.Fatalf("expected *Response, got %T", outcome.Payload.Value)
	}
	if resp.Status != 200 || !resp.OK || resp.StatusText != "200 OK" {
		t.Fatalf("unexpected status block: %+v", resp)
	}
	if string(resp.Body) != "hello" || resp.ContentType != "text/plain" {
		t.Fatalf("unexpected body: %q ct=%q", resp.Body, resp.ContentType)
	}
	if resp.Headers["X-Multi"] != "a, b" {
		t.Fatalf("expected folded header, got %q", resp.Headers["X-Multi"])
	}

	// 400 keeps OK=false and surfaces the status.
	outcome, err = invokeHTTP(t, sb, "http.get", srv.URL+"/echo", int64(5000))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	resp, _ = outcome.Payload.Value.(*stdhttp.Response)
	if resp == nil || resp.Status != 400 || resp.OK {
		t.Fatalf("expected 400 not-ok, got %+v", resp)
	}
}

func TestHTTP_Post(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(append([]byte(r.Header.Get("Content-Type")+"|"), body...))
	}))
	defer srv.Close()

	sb := newHTTPBinding(t)
	outcome, err := invokeHTTP(t, sb, "http.post", srv.URL+"/submit", []byte("payload"), "application/json", int64(5000))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	resp, ok := outcome.Payload.Value.(*stdhttp.Response)
	if !ok {
		t.Fatalf("expected *Response, got %T", outcome.Payload.Value)
	}
	if resp.Status != 201 {
		t.Fatalf("expected 201, got %d", resp.Status)
	}
	if string(resp.Body) != "application/json|payload" {
		t.Fatalf("unexpected echo: %q", resp.Body)
	}
}

func TestHTTP_RequestWithHeadersAndMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Auth", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, r.Method)
	}))
	defer srv.Close()

	sb := newHTTPBinding(t)
	headers := map[string]any{"Authorization": "Bearer tok"}
	outcome, err := invokeHTTP(t, sb, "http.request", "DELETE", srv.URL+"/x", headers, []byte(nil), int64(5000))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	resp, _ := outcome.Payload.Value.(*stdhttp.Response)
	if resp == nil || resp.Status != 200 || string(resp.Body) != "DELETE" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Headers["X-Echo-Auth"] != "Bearer tok" {
		t.Fatalf("auth header not echoed: %+v", resp.Headers)
	}
}

func TestHTTP_Errors(t *testing.T) {
	sb := newHTTPBinding(t)

	// Bad URL surfaces as invocation error descriptor.
	outcome, err := invokeHTTP(t, sb, "http.get", "http://127.0.0.1:1/nope", int64(2000))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if outcome.Result.Error == nil {
		t.Fatalf("expected error descriptor, got %+v", outcome)
	}
	if !strings.Contains(outcome.Result.Error.Message, "127.0.0.1:1") {
		t.Fatalf("error should mention target: %q", outcome.Result.Error.Message)
	}

	// Empty url rejected before dialing.
	outcome, _ = invokeHTTP(t, sb, "http.get", "", int64(1000))
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "url is required") {
		t.Fatalf("expected url-required error, got %+v", outcome.Result.Error)
	}
	// Empty method rejected.
	outcome, _ = invokeHTTP(t, sb, "http.request", "", srvURL(t), map[string]any{}, []byte{}, int64(1000))
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "method is required") {
		t.Fatalf("expected method-required error, got %+v", outcome.Result.Error)
	}
}

func srvURL(t *testing.T) string {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).URL
}

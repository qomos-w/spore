package ws

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qomos-w/spore/binding"
)

// startWSEchoServer runs a real WebSocket echo server: HTTP upgrade handshake
// followed by echoing every data frame back as the same opcode.
func startWSEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		accept := ComputeAcceptKey(r.Header.Get("Sec-WebSocket-Key"))
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := rw.Write([]byte(resp)); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}

		br := bufio.NewReader(conn)
		for {
			f, err := ReadFrame(br)
			if err != nil {
				return
			}
			if err := WriteFrame(conn, f.Opcode, f.Fin, f.Payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func newWSBinding(t *testing.T) *binding.ScriptBinding {
	t.Helper()
	sb := binding.NewScriptBinding()
	if err := Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return sb
}

func invokeWS(t *testing.T, sb *binding.ScriptBinding, callable string, args ...any) (binding.InvocationOutcome, error) {
	t.Helper()
	return sb.Invoke(binding.InvocationRequest{Callable: callable, Stage: binding.InvocationStageUnary, Args: args})
}

func TestWS_ModuleRegistration(t *testing.T) {
	sb := newWSBinding(t)
	for _, name := range []string{"ws.connect", "ws.send", "ws.sendText", "ws.read", "ws.close"} {
		if _, ok := sb.Executors.Lookup(name); !ok {
			t.Fatalf("expected %s registered", name)
		}
	}
}

func TestWS_ConnectEchoRoundTrip(t *testing.T) {
	srv := startWSEchoServer(t)
	sb := newWSBinding(t)

	outcome, err := invokeWS(t, sb, "ws.connect", wsURL(srv)+"/chat", int64(5000))
	if err != nil || outcome.Result.Error != nil {
		t.Fatalf("connect: err=%v result=%+v", err, outcome.Result.Error)
	}
	id, ok := outcome.Payload.Value.(int64)
	if !ok || id <= 0 {
		t.Fatalf("expected positive handle, got %v", outcome.Payload.Value)
	}

	// Text round trip.
	if _, err := invokeWS(t, sb, "ws.sendText", id, "hello ws"); err != nil {
		t.Fatalf("sendText: %v", err)
	}
	outcome, err = invokeWS(t, sb, "ws.read", id, int64(5000))
	if err != nil || outcome.Result.Error != nil {
		t.Fatalf("read: %v %+v", err, outcome.Result.Error)
	}
	msg, ok := outcome.Payload.Value.(*Message)
	if !ok {
		t.Fatalf("expected *Message, got %T", outcome.Payload.Value)
	}
	if msg.Type != "text" || string(msg.Data) != "hello ws" {
		t.Fatalf("unexpected echo: %+v", msg)
	}

	// Binary round trip.
	if _, err := invokeWS(t, sb, "ws.send", id, []byte{1, 2, 3}); err != nil {
		t.Fatalf("send: %v", err)
	}
	outcome, _ = invokeWS(t, sb, "ws.read", id, int64(5000))
	msg, _ = outcome.Payload.Value.(*Message)
	if msg == nil || msg.Type != "binary" || len(msg.Data) != 3 || msg.Data[2] != 3 {
		t.Fatalf("unexpected binary echo: %+v", msg)
	}

	// Close with code.
	outcome, err = invokeWS(t, sb, "ws.close", id, int64(1000), "done")
	if err != nil || outcome.Result.Error != nil {
		t.Fatalf("close: %v %+v", err, outcome.Result.Error)
	}
}

func TestWS_ReadTimeoutSurfacesError(t *testing.T) {
	srv := startWSEchoServer(t)
	sb := newWSBinding(t)

	outcome, err := invokeWS(t, sb, "ws.connect", wsURL(srv), int64(5000))
	if err != nil || outcome.Result.Error != nil {
		t.Fatalf("connect: %v %+v", err, outcome.Result.Error)
	}
	id := outcome.Payload.Value.(int64)

	outcome, err = invokeWS(t, sb, "ws.read", id, int64(300))
	if err != nil {
		t.Fatalf("read invoke: %v", err)
	}
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "timeout") {
		t.Fatalf("expected timeout error, got %+v", outcome.Result.Error)
	}
	_, _ = invokeWS(t, sb, "ws.close", id, int64(1000), "")
}

func TestWS_UnknownHandleAndBadScheme(t *testing.T) {
	sb := newWSBinding(t)

	outcome, _ := invokeWS(t, sb, "ws.read", int64(9999), int64(100))
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "unknown connection handle") {
		t.Fatalf("expected unknown-handle error, got %+v", outcome.Result.Error)
	}

	outcome, err := invokeWS(t, sb, "ws.connect", "http://example.invalid/", int64(500))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "unsupported scheme") {
		t.Fatalf("expected scheme error, got %+v", outcome.Result.Error)
	}

	outcome, _ = invokeWS(t, sb, "ws.connect", "ws://127.0.0.1:1/", int64(1000))
	if outcome.Result.Error == nil {
		t.Fatal("expected dial failure for unroutable port")
	}
}

func TestWS_ConcurrentConnectionsIsolated(t *testing.T) {
	srv := startWSEchoServer(t)
	sb := newWSBinding(t)

	var ids []int64
	for i := 0; i < 3; i++ {
		outcome, _ := invokeWS(t, sb, "ws.connect", wsURL(srv), int64(5000))
		id := outcome.Payload.Value.(int64)
		if _, err := invokeWS(t, sb, "ws.sendText", id, "tag"+strings.Repeat("x", i+1)); err != nil {
			t.Fatalf("sendText %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	for i, id := range ids {
		outcome, err := invokeWS(t, sb, "ws.read", id, int64(5000))
		if err != nil || outcome.Result.Error != nil {
			t.Fatalf("read %d: %v %+v", i, err, outcome.Result.Error)
		}
		msg := outcome.Payload.Value.(*Message)
		if msg.Type != "text" || string(msg.Data) != "tag"+strings.Repeat("x", i+1) {
			t.Fatalf("conn %d cross-talk: %+v", i, msg)
		}
	}
	for _, id := range ids {
		_, _ = invokeWS(t, sb, "ws.close", id, int64(1000), "")
	}
}

// TestWS_EchoLatency sanity-checks the handshake + round-trip stays cheap.
func TestWS_EchoLatency(t *testing.T) {
	srv := startWSEchoServer(t)
	sb := newWSBinding(t)
	outcome, _ := invokeWS(t, sb, "ws.connect", wsURL(srv), int64(5000))
	id := outcome.Payload.Value.(int64)
	defer func() { _, _ = invokeWS(t, sb, "ws.close", id, int64(1000), "") }()

	start := time.Now()
	for i := 0; i < 20; i++ {
		if _, err := invokeWS(t, sb, "ws.sendText", id, "m"); err != nil {
			t.Fatalf("send: %v", err)
		}
		if outcome, err := invokeWS(t, sb, "ws.read", id, int64(5000)); err != nil || outcome.Result.Error != nil {
			t.Fatalf("read: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("20 round trips took %v", elapsed)
	}
}

package std_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qomos-w/spore/std/ws"
)

// startWSEchoServerVM runs a WebSocket echo server using the exported ws codec.
func startWSEchoServerVM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		accept := ws.ComputeAcceptKey(r.Header.Get("Sec-WebSocket-Key"))
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := rw.Write([]byte(resp)); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}

		br := bufio.NewReader(conn)
		for {
			f, err := ws.ReadFrame(br)
			if err != nil {
				return
			}
			if err := ws.WriteFrame(conn, f.Opcode, f.Fin, f.Payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWSVM_EchoFromScript(t *testing.T) {
	srv := startWSEchoServerVM(t)

	f := newVMFrontend(t, `
import { connect, sendText, read, close } from "ws"

fun roundTrip(): string {
    var id: int = connect("`+"ws"+strings.TrimPrefix(srv.URL, "http")+`/vm", 5000)
    sendText(id, "ping")
    var msg: map = read(id, 5000)
    close(id, 1000, "done")
    if msg["type"] == "text" {
        return "echo-ok"
    }
    return "bad-type"
}`)

	outcome, err := f.Invoke("roundTrip", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("roundTrip: result=%+v err=%+v", outcome.Result, outcome.Result.Error)
	}
	if got, _ := outcome.Payload.Value.(string); got != "echo-ok" {
		t.Fatalf("round trip: %v", outcome.Payload.Value)
	}
}

func TestWSVM_ReadTimeoutCatchable(t *testing.T) {
	srv := startWSEchoServerVM(t)

	f := newVMFrontend(t, `
import { connect, read, close } from "ws"

fun waitThenReport(): string {
    var id: int = connect("`+"ws"+strings.TrimPrefix(srv.URL, "http")+`", 5000)
    try {
        var msg: map = read(id, 200)
        close(id, 1000, "")
        return "got:" + msg["type"]
    } catch (e) {
        close(id, 1000, "")
        return "timeout"
    }
}`)

	outcome, err := f.Invoke("waitThenReport", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("waitThenReport: result=%+v err=%+v", outcome.Result, outcome.Result.Error)
	}
	if got, _ := outcome.Payload.Value.(string); got != "timeout" {
		t.Fatalf("expected timeout catch, got %v", outcome.Payload.Value)
	}
}

func TestWSVM_BinarySendFromScript(t *testing.T) {
	srv := startWSEchoServerVM(t)

	f := newVMFrontend(t, `
import { connect, send, read, close } from "ws"

fun echoType(): string {
    var id: int = connect("`+"ws"+strings.TrimPrefix(srv.URL, "http")+`", 5000)
    send(id, "raw-bytes-as-string")
    var msg: map = read(id, 5000)
    close(id, 1000, "")
    return msg["type"]
}`)

	outcome, err := f.Invoke("echoType", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatalf("echoType: result=%+v err=%+v", outcome.Result, outcome.Result.Error)
	}
	if got, _ := outcome.Payload.Value.(string); got != "binary" {
		t.Fatalf("binary type: %v", outcome.Payload.Value)
	}
}

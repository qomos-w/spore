package ws

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestComputeAcceptKey_RFCVector(t *testing.T) {
	// RFC 6455 §1.3 example vector.
	got := ComputeAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	if got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("accept key mismatch: %q", got)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	cases := [][]byte{
		bytes.Repeat([]byte("x"), 1),          // small
		bytes.Repeat([]byte("y"), 125),         // boundary: inline length
		bytes.Repeat([]byte("z"), 126),         // 16-bit extended length
		bytes.Repeat([]byte("w"), 0xFFFF),      // 16-bit max
		bytes.Repeat([]byte("v"), 0x10000),     // 64-bit extended length
	}
	for i, payload := range cases {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, OpBinary, true, payload); err != nil {
			t.Fatalf("case %d: writeFrame: %v", i, err)
		}
		f, err := ReadFrame(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("case %d: readFrame: %v", i, err)
		}
		if !f.Fin || f.Opcode != OpBinary || !bytes.Equal(f.Payload, payload) {
			t.Fatalf("case %d: round-trip mismatch len=%d", i, len(f.Payload))
		}
	}
}

func TestFrameFragmentedFlag(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, OpText, false, []byte("frag")); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	f, err := ReadFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if f.Fin || string(f.Payload) != "frag" {
		t.Fatalf("expected unfinished frame, got %+v", f)
	}
}

func TestFrameMaskApplied(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, OpText, true, []byte("secret")); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	raw := buf.Bytes()
	// Masked flag must be set and on-wire payload must differ from plaintext.
	if raw[1]&0x80 == 0 {
		t.Fatal("client frame must set MASK bit")
	}
	if bytes.Contains(raw[6:], []byte("secret")) {
		t.Fatal("on-wire payload must be masked")
	}
}

func TestFrameTooLargeRejected(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x82, 0x7F})
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], maxFramePayload+1)
	buf.Write(ext[:])
	if _, err := ReadFrame(bufio.NewReader(&buf)); err != errFrameTooLarge {
		t.Fatalf("expected errFrameTooLarge, got %v", err)
	}
}

func TestCloseCode(t *testing.T) {
	var payload []byte
	payload = binary.BigEndian.AppendUint16(payload, 1008)
	payload = append(payload, "policy"...)
	code, reason := closeCode(payload)
	if code != 1008 || reason != "policy" {
		t.Fatalf("got %d %q", code, reason)
	}
	if code, reason := closeCode(nil); code != 1005 || reason != "" {
		t.Fatalf("empty payload: %d %q", code, reason)
	}
}

// pipeConn adapts a net.Pipe for direct Conn tests.
func newTestConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return newConn(client, bufio.NewReader(client)), server
}

func TestConnWriteReadEcho(t *testing.T) {
	conn, server := newTestConn(t)

	go func() {
		// Read one frame (unmask) and echo it back unmasked as text.
		f, err := ReadFrame(bufio.NewReader(server))
		if err != nil {
			return
		}
		_ = WriteFrame(server, f.Opcode, true, f.Payload)
	}()

	if err := conn.WriteMessage(OpText, []byte("ping-pong")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	opcode, data, err := conn.ReadMessage(3000)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if opcode != OpText || string(data) != "ping-pong" {
		t.Fatalf("unexpected echo: op=%X data=%q", opcode, data)
	}
}

func TestConnReadAnswersPing(t *testing.T) {
	conn, server := newTestConn(t)

	go func() {
		_ = WriteFrame(server, OpPing, true, []byte("hb"))
		// Expect a pong with the same payload.
		f, err := ReadFrame(bufio.NewReader(server))
		if err != nil || f.Opcode != OpPong || string(f.Payload) != "hb" {
			return
		}
		_ = WriteFrame(server, OpText, true, []byte("after-ping"))
	}()

	opcode, data, err := conn.ReadMessage(3000)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if opcode != OpText || string(data) != "after-ping" {
		t.Fatalf("expected data after transparent ping, got %X %q", opcode, data)
	}
}

func TestConnReadFragmented(t *testing.T) {
	conn, server := newTestConn(t)

	go func() {
		w := server
		_ = WriteFrame(w, OpText, false, []byte("hello, "))
		_ = WriteFrame(w, OpContinuation, false, []byte("world"))
		_ = WriteFrame(w, OpContinuation, true, []byte("!"))
	}()

	opcode, data, err := conn.ReadMessage(3000)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if opcode != OpText || string(data) != "hello, world!" {
		t.Fatalf("unexpected reassembly: %q", data)
	}
}

func TestConnReadSurfacesClose(t *testing.T) {
	conn, server := newTestConn(t)

	go func() {
		var payload []byte
		payload = binary.BigEndian.AppendUint16(payload, 1000)
		payload = append(payload, "bye"...)
		_ = WriteFrame(server, OpClose, true, payload)
		// Drain the echoed close frame.
		_, _ = ReadFrame(bufio.NewReader(server))
	}()

	opcode, payload, err := conn.ReadMessage(3000)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if opcode != OpClose {
		t.Fatalf("expected close frame, got %X", opcode)
	}
	code, reason := closeCode(payload)
	if code != 1000 || reason != "bye" {
		t.Fatalf("unexpected close payload: %d %q", code, reason)
	}
}

func TestConnReadTimeout(t *testing.T) {
	conn, _ := newTestConn(t)
	if _, _, err := conn.ReadMessage(150); err != ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestConnClosedGuards(t *testing.T) {
	conn, _ := newTestConn(t)
	if err := conn.Close(1000, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := conn.Close(1000, ""); err != ErrClosed {
		t.Fatalf("second close should be ErrClosed, got %v", err)
	}
	if err := conn.WriteMessage(OpText, []byte("x")); err != ErrClosed {
		t.Fatalf("write after close should be ErrClosed, got %v", err)
	}
	if _, _, err := conn.ReadMessage(100); err != ErrClosed {
		t.Fatalf("read after close should be ErrClosed, got %v", err)
	}
}

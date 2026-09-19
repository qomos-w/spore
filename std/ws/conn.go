package ws

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	defaultConnectTimeoutMs int64 = 10000
	defaultReadTimeoutMs    int64 = 30000
)

// ErrTimeout is returned by ReadMessage when the read deadline elapsed
// before a complete message arrived. It maps to a catchable script error.
var ErrTimeout = errors.New("timeout")

// ErrClosed is returned once the connection has been closed locally.
var ErrClosed = errors.New("closed")

// Conn is a client-side WebSocket connection. Reads and writes are
// serialized internally; incoming ping control frames are answered with
// pong transparently during ReadMessage.
type Conn struct {
	raw net.Conn
	br  *bufio.Reader

	writeMu sync.Mutex
	readMu  sync.Mutex
	closeMu sync.Mutex
	closed  bool
}

func newConn(raw net.Conn, br *bufio.Reader) *Conn {
	return &Conn{raw: raw, br: br}
}

// ReadMessage blocks until one complete data message is available. Ping
// frames are answered inline; pong frames are skipped. A close frame from
// the peer is returned as (OpClose, payload) without an error.
func (c *Conn) ReadMessage(timeoutMs int64) (byte, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.isClosed() {
		return 0, nil, ErrClosed
	}
	deadline := deadlineFromMs(timeoutMs, defaultReadTimeoutMs)
	if err := c.raw.SetReadDeadline(deadline); err != nil {
		return 0, nil, fmt.Errorf("ws: set read deadline: %w", err)
	}

	var (
		msgOpcode byte
		msg       []byte
		inMessage bool
	)
	for {
		f, err := ReadFrame(c.br)
		if err != nil {
			if isTimeout(err) {
				return 0, nil, ErrTimeout
			}
			return 0, nil, err
		}
		switch f.Opcode {
		case OpPing:
			if err := c.writeControl(OpPong, f.Payload); err != nil {
				return 0, nil, err
			}
		case OpPong:
			// Unsolicited pong: ignore.
		case OpClose:
			// Echo the close handshake then surface the payload.
			_ = c.writeControl(OpClose, f.Payload)
			return OpClose, f.Payload, nil
		case OpText, OpBinary:
			if inMessage {
				return 0, nil, fmt.Errorf("ws: new data frame inside unfinished message")
			}
			msgOpcode, msg, inMessage = f.Opcode, append([]byte(nil), f.Payload...), true
		case OpContinuation:
			if !inMessage {
				return 0, nil, fmt.Errorf("ws: continuation frame without initial frame")
			}
			msg = append(msg, f.Payload...)
		default:
			return 0, nil, fmt.Errorf("ws: unknown opcode 0x%X", f.Opcode)
		}
		if inMessage && f.Fin {
			if len(msg) > maxFramePayload {
				return 0, nil, errFrameTooLarge
			}
			return msgOpcode, msg, nil
		}
	}
}

// WriteMessage sends one complete masked data message.
func (c *Conn) WriteMessage(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.isClosed() {
		return ErrClosed
	}
	if err := c.raw.SetWriteDeadline(time.Now().Add(time.Duration(defaultReadTimeoutMs) * time.Millisecond)); err != nil {
		return fmt.Errorf("ws: set write deadline: %w", err)
	}
	if err := WriteFrame(c.raw, opcode, true, payload); err != nil {
		return err
	}
	return nil
}

func (c *Conn) writeControl(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.isClosed() {
		return ErrClosed
	}
	if err := c.raw.SetWriteDeadline(time.Now().Add(time.Duration(defaultReadTimeoutMs) * time.Millisecond)); err != nil {
		return err
	}
	return WriteFrame(c.raw, opcode, true, payload)
}

// Close performs the closing handshake (best effort) and closes the socket.
func (c *Conn) Close(code int, reason string) error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return ErrClosed
	}
	c.closed = true
	c.closeMu.Unlock()

	payload := make([]byte, 2, 2+len(reason))
	payload[0] = byte(code >> 8)
	payload[1] = byte(code)
	payload = append(payload, []byte(reason)...)
	c.writeMu.Lock()
	_ = c.raw.SetWriteDeadline(time.Now().Add(time.Second))
	_ = WriteFrame(c.raw, OpClose, true, payload)
	c.writeMu.Unlock()
	return c.raw.Close()
}

func (c *Conn) isClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

func isTimeout(err error) bool {
	if errors.Is(err, ErrTimeout) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

func durationFromMs(ms, def int64) time.Duration {
	if ms <= 0 {
		ms = def
	}
	return time.Duration(ms) * time.Millisecond
}

func deadlineFromMs(ms, def int64) time.Time {
	return time.Now().Add(durationFromMs(ms, def))
}

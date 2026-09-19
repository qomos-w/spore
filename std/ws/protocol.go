// Package ws implements the WebSocket client protocol (RFC 6455) on top of
// the Go standard library only: HTTP/1.1 upgrade handshake, frame codec with
// client-side masking, fragmentation, and the ping/pong + close handshakes.
package ws

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Frame opcodes (RFC 6455 §5.2).
const (
	OpContinuation byte = 0x0
	OpText         byte = 0x1
	OpBinary       byte = 0x2
	OpClose        byte = 0x8
	OpPing         byte = 0x9
	OpPong         byte = 0xA
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var (
	errFrameTooLarge = errors.New("ws: frame payload exceeds limit")
	errBadFrame      = errors.New("ws: malformed frame header")
)

const maxFramePayload = 16 << 20 // 16 MiB per (reassembled) message

// Frame is one WebSocket frame (RFC 6455 §5.2). The codec is exported so
// hosts and tests can run the server side of the protocol as well.
type Frame struct {
	Fin     bool
	Opcode  byte
	Payload []byte
}

// ComputeAcceptKey derives the Sec-WebSocket-Accept value for a
// Sec-WebSocket-Key (RFC 6455 §4.2.2).
func ComputeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// newRequestKey generates a random Sec-WebSocket-Key value.
func newRequestKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("ws: key generation: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// ReadFrame reads one frame. Server-to-client frames must be unmasked.
func ReadFrame(r io.Reader) (Frame, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Frame{}, fmt.Errorf("ws: read frame header: %w", err)
	}
	fin := head[0]&0x80 != 0
	rsv := head[0] & 0x70
	if rsv != 0 {
		return Frame{}, fmt.Errorf("%w: rsv bits set (0x%X)", errBadFrame, rsv)
	}
	opcode := head[0] & 0x0F
	masked := head[1]&0x80 != 0
	length := uint64(head[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read extended length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read extended length: %w", err)
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > maxFramePayload {
		return Frame{}, errFrameTooLarge
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read mask key: %w", err)
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, fmt.Errorf("ws: read payload: %w", err)
	}
	if masked {
		applyMask(payload, maskKey)
	}
	return Frame{Fin: fin, Opcode: opcode, Payload: payload}, nil
}

// WriteFrame writes one frame; client-to-server frames must be masked.
func WriteFrame(w io.Writer, opcode byte, fin bool, payload []byte) error {
	if len(payload) > maxFramePayload {
		return errFrameTooLarge
	}
	var head bytes.Buffer
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	head.WriteByte(b0)

	length := len(payload)
	var maskKey [4]byte
	if _, err := rand.Read(maskKey[:]); err != nil {
		return fmt.Errorf("ws: mask generation: %w", err)
	}

	switch {
	case length < 126:
		head.WriteByte(0x80 | byte(length))
	case length <= 0xFFFF:
		head.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(length))
		head.Write(ext[:])
	default:
		head.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		head.Write(ext[:])
	}
	head.Write(maskKey[:])

	masked := make([]byte, length)
	copy(masked, payload)
	applyMask(masked, maskKey)

	if _, err := w.Write(head.Bytes()); err != nil {
		return fmt.Errorf("ws: write frame header: %w", err)
	}
	if _, err := w.Write(masked); err != nil {
		return fmt.Errorf("ws: write frame payload: %w", err)
	}
	return nil
}

func applyMask(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}

// closeCode extracts the close code and reason from a close payload.
func closeCode(payload []byte) (int, string) {
	if len(payload) < 2 {
		return 1005, ""
	}
	code := int(binary.BigEndian.Uint16(payload[:2]))
	reason := ""
	if len(payload) > 2 {
		reason = string(payload[2:])
	}
	return code, reason
}

// dial opens and handshakes a WebSocket connection to rawURL.
// Only ws:// and wss:// schemes are accepted; wss uses TLS with SNI.
func dial(rawURL string, timeoutMs int64) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws: invalid url: %w", err)
	}
	var useTLS bool
	switch strings.ToLower(u.Scheme) {
	case "ws":
	case "wss":
		useTLS = true
	default:
		return nil, fmt.Errorf("ws: unsupported scheme %q (want ws or wss)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("ws: url has no host")
	}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	port := u.Port()
	if port == "" {
		if useTLS {
			port = "443"
		} else {
			port = "80"
		}
	}

	var raw net.Conn
	if useTLS {
		raw, err = tls.Dial("tcp", net.JoinHostPort(u.Hostname(), port), &tls.Config{
			ServerName: u.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
	} else {
		raw, err = net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), durationFromMs(timeoutMs, 10000))
	}
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", u.Host, err)
	}
	if useTLS {
		if err := raw.SetDeadline(deadlineFromMs(timeoutMs, 10000)); err != nil {
			raw.Close()
			return nil, fmt.Errorf("ws: set deadline: %w", err)
		}
	}

	key, err := newRequestKey()
	if err != nil {
		raw.Close()
		return nil, err
	}
	req := &http.Request{
		Method: http.MethodGet,
		URL:    u,
		Host:   u.Host,
		Header: http.Header{},
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Sec-WebSocket-Version", "13")

	var handshake bytes.Buffer
	writeRequestLine(&handshake, path, u.Host)
	if err := req.Header.Write(&handshake); err != nil {
		raw.Close()
		return nil, fmt.Errorf("ws: encode handshake: %w", err)
	}
	handshake.WriteString("\r\n")
	if _, err := raw.Write(handshake.Bytes()); err != nil {
		raw.Close()
		return nil, fmt.Errorf("ws: send handshake: %w", err)
	}

	br := bufio.NewReader(raw)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("ws: read handshake response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw.Close()
		return nil, fmt.Errorf("ws: handshake failed with status %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		raw.Close()
		return nil, fmt.Errorf("ws: handshake response missing Upgrade: websocket")
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != ComputeAcceptKey(key) {
		raw.Close()
		return nil, fmt.Errorf("ws: handshake Sec-WebSocket-Accept mismatch")
	}
	// Clear the handshake deadline; subsequent I/O uses per-call deadlines.
	_ = raw.SetDeadline(time.Time{})
	return newConn(raw, br), nil
}

func writeRequestLine(w *bytes.Buffer, path, host string) {
	fmt.Fprintf(w, "GET %s HTTP/1.1\r\nHost: %s\r\n", path, host)
}

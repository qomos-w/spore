// ws module: WebSocket client surface for Spore scripts. Connections are
// represented by integer handles managed in a package-level registry, which
// fits the runtime's synchronous cooperative model: inbound messages are
// consumed via blocking ws.read calls bounded by deadlines.
package ws

import (
	"fmt"
	"sync"

	"github.com/qomos-w/spore/binding"
)

// Message is the script-facing read result shape.
type Message struct {
	Type   string `json:"type"`   // "text" | "binary" | "close"
	Data   []byte `json:"data"`   // payload (text: UTF-8 bytes)
	Code   int    `json:"code"`   // close frame status code
	Reason string `json:"reason"` // close frame reason
}

type connRegistry struct {
	mu     sync.Mutex
	conns  map[int64]*Conn
	nextID int64
}

var registry = &connRegistry{conns: make(map[int64]*Conn)}

func (r *connRegistry) add(c *Conn) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.conns[r.nextID] = c
	return r.nextID
}

func (r *connRegistry) get(id int64) (*Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[id]
	if !ok {
		return nil, fmt.Errorf("ws: unknown connection handle %d", id)
	}
	return c, nil
}

func (r *connRegistry) remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, id)
}

// Register registers the ws standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("ws", "module")

	if err := builder.AddFreeFunction("connect", func(url string, timeoutMs int64) (int64, error) {
		conn, err := dial(url, timeoutMs)
		if err != nil {
			return 0, err
		}
		return registry.add(conn), nil
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("sendText", func(id int64, text string) error {
		conn, err := registry.get(id)
		if err != nil {
			return err
		}
		return conn.WriteMessage(OpText, []byte(text))
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("send", func(id int64, data any) error {
		conn, err := registry.get(id)
		if err != nil {
			return err
		}
		switch v := data.(type) {
		case string:
			return conn.WriteMessage(OpBinary, []byte(v))
		case []byte:
			return conn.WriteMessage(OpBinary, v)
		default:
			return fmt.Errorf("ws.send: unsupported data type %T", data)
		}
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("read", func(id int64, timeoutMs int64) (*Message, error) {
		conn, err := registry.get(id)
		if err != nil {
			return nil, err
		}
		opcode, payload, err := conn.ReadMessage(timeoutMs)
		if err != nil {
			return nil, err
		}
		msg := &Message{Data: payload}
		switch opcode {
		case OpText:
			msg.Type = "text"
		case OpBinary:
			msg.Type = "binary"
		case OpClose:
			msg.Type = "close"
			msg.Code, msg.Reason = closeCode(payload)
			msg.Data = nil
		default:
			return nil, fmt.Errorf("ws: unexpected opcode 0x%X", opcode)
		}
		return msg, nil
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("close", func(id int64, code int64, reason string) error {
		conn, err := registry.get(id)
		if err != nil {
			return err
		}
		if code <= 0 {
			code = 1000
		}
		err = conn.Close(int(code), reason)
		registry.remove(id)
		if err == ErrClosed {
			return nil
		}
		return err
	}); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("ws")
}

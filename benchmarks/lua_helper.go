package benchmarks

import (
	lua "github.com/yuin/gopher-lua"
)

// LuaRunner loads a Lua script once into a fresh VM (the named functions go
// into the global table). Each Run resolves the function by name and calls
// it through the canonical Push / PCall protocol.
type LuaRunner struct {
	L *lua.LState
}

func NewLuaRunner(source string) (*LuaRunner, error) {
	L := lua.NewState()
	if err := L.DoString(source); err != nil {
		L.Close()
		return nil, err
	}
	return &LuaRunner{L: L}, nil
}

func (r *LuaRunner) Run(name string, n int) (int64, error) {
	fn := r.L.GetGlobal(name)
	r.L.Push(fn)
	r.L.Push(lua.LNumber(n))
	if err := r.L.PCall(1, 1, nil); err != nil {
		return 0, err
	}
	v := r.L.Get(-1)
	r.L.Pop(1)
	switch x := v.(type) {
	case lua.LNumber:
		return int64(x), nil
	case lua.LString:
		return int64(len(string(x))), nil
	}
	return 0, nil
}

func (r *LuaRunner) Close() {
	if r.L != nil {
		r.L.Close()
	}
}

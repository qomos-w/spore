package benchmarks

import (
	"github.com/dop251/goja"
)

// GojaRunner compiles a JS program once, runs it once to install the named
// function in the VM globals, then asserts a callable handle for repeated
// invocation. b.N hits only the AssertFunction call, which is goja's
// fastest sustained call path.
type GojaRunner struct {
	vm *goja.Runtime
	fn goja.Callable
}

func NewGojaRunner(source string) (*GojaRunner, error) {
	prog, err := goja.Compile("bench.js", source, false)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, err
	}
	return &GojaRunner{vm: vm}, nil
}

func (r *GojaRunner) bind(name string) error {
	if r.fn != nil {
		return nil
	}
	fn, ok := goja.AssertFunction(r.vm.Get(name))
	if !ok {
		return errFnNotFound(name)
	}
	r.fn = fn
	return nil
}

func (r *GojaRunner) Run(name string, n int) (int64, error) {
	if err := r.bind(name); err != nil {
		return 0, err
	}
	v, err := r.fn(goja.Undefined(), r.vm.ToValue(n))
	if err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return v.ToInteger(), nil
}

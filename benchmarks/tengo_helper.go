package benchmarks

import (
	"github.com/d5/tengo/v2"
)

// TengoRunner pre-compiles a tengo script that exposes named functions.
// Each Run sets the input variable, executes the compiled script, and reads
// the result variable back.
type TengoRunner struct {
	compiled *tengo.Compiled
}

// NewTengoRunner takes a script that, after the function definitions, ends
// with `__result := <name>(__n)` so a single Run produces the value the
// benchmark wants.
func NewTengoRunner(source string) (*TengoRunner, error) {
	script := tengo.NewScript([]byte(source))
	if err := script.Add("__n", 0); err != nil {
		return nil, err
	}
	if err := script.Add("__result", 0); err != nil {
		return nil, err
	}
	compiled, err := script.Compile()
	if err != nil {
		return nil, err
	}
	return &TengoRunner{compiled: compiled}, nil
}

func (r *TengoRunner) Run(_ string, n int) (int64, error) {
	if err := r.compiled.Set("__n", n); err != nil {
		return 0, err
	}
	if err := r.compiled.Run(); err != nil {
		return 0, err
	}
	v := r.compiled.Get("__result")
	if v == nil {
		return 0, nil
	}
	return v.Int64(), nil
}

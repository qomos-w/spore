package benchmarks

import (
	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/strings"
)

// SporeRunner wraps a Spore Frontend that has loaded a script.
// Compile-once / invoke-many — exactly what the benchmark loop needs.
type SporeRunner struct {
	f      *frontend.Frontend
	vmEval *bytecode.VMEvaluator
}

func NewSporeRunner(source string) (*SporeRunner, error) {
	sb := binding.NewScriptBinding()
	if err := strings.Register(sb); err != nil {
		return nil, err
	}
	f, err := frontend.New(sb)
	if err != nil {
		return nil, err
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	if err := f.LoadSource(source); err != nil {
		return nil, err
	}
	return &SporeRunner{f: f, vmEval: vmEval}, nil
}

func (r *SporeRunner) Run(name string, n int) (int64, error) {
	return r.vmEval.EvaluateUnaryInt(name, n)
}

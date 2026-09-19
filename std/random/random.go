package random

import (
	"math/rand"
	"time"

	"github.com/qomos-w/spore/binding"
)

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// Register registers the random standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("random", "module")

	if err := builder.AddFreeFunction("intn", func(n int64) int64 {
		return rng.Int63n(n)
	}); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("float64", func() float64 {
		return rng.Float64()
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
	return sb.ExposeCapabilityCallables("random")
}

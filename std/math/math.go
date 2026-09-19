package math

import (
	"math"

	"github.com/qomos-w/spore/binding"
)

// Register registers the math standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("math", "module")

	if err := builder.AddFreeFunction("abs", math.Abs); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("floor", math.Floor); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("ceil", math.Ceil); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("round", math.Round); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("sqrt", math.Sqrt); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("max", math.Max); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("min", math.Min); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("pow", math.Pow); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("math")
}

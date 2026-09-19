package strconv

import (
	stdstr "strconv"

	"github.com/qomos-w/spore/binding"
)

// Register registers the strconv standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("strconv", "module")

	// parseInt converts a decimal string to a long integer.
	if err := builder.AddFreeFunction("parseInt", func(s string) (int64, error) {
		return stdstr.ParseInt(s, 10, 64)
	}); err != nil {
		return err
	}

	// parseFloat converts a string to a double.
	if err := builder.AddFreeFunction("parseFloat", func(s string) (float64, error) {
		return stdstr.ParseFloat(s, 64)
	}); err != nil {
		return err
	}

	// formatInt converts a long integer to a decimal string.
	if err := builder.AddFreeFunction("formatInt", func(n int64) string {
		return stdstr.FormatInt(n, 10)
	}); err != nil {
		return err
	}

	// formatFloat converts a double to a string.
	if err := builder.AddFreeFunction("formatFloat", func(f float64) string {
		return stdstr.FormatFloat(f, 'f', -1, 64)
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
	return sb.ExposeCapabilityCallables("strconv")
}

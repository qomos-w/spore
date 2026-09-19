package strings

import (
	"strings"

	"github.com/qomos-w/spore/binding"
)

func newBuilder() []string {
	return nil
}

func appendBuilder(parts []string, value string) []string {
	return append(parts, value)
}

func buildBuilder(parts []string) string {
	return strings.Join(parts, "")
}

// Register registers the strings standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("strings", "module")

	if err := builder.AddFreeFunction("contains", strings.Contains); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("hasPrefix", strings.HasPrefix); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("hasSuffix", strings.HasSuffix); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("index", strings.Index); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("lastIndex", strings.LastIndex); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("toLower", strings.ToLower); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("toUpper", strings.ToUpper); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("trimSpace", strings.TrimSpace); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("repeat", strings.Repeat); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("replace", strings.Replace); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("join", strings.Join); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("builder", newBuilder); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("append", appendBuilder); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("build", buildBuilder); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("strings")
}

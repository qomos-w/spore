package strings

import (
	"strings"

	"github.com/qomos-w/spore/binding"
)

// RegisterStd registers the "std" string module using the sporemind host
// binding names (snake_case: join/split/contains/has_prefix/has_suffix/
// trim/to_upper/to_lower/parse_int/parse_float/err_text/now_ms). It is
// name-aligned with the std host module shipped in sporemind so scripts move
// between hosts without renaming calls. The legacy "strings" module
// (camelCase) remains registered for existing consumers.
func RegisterStd(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("std", "module")

	register := func(name string, fn any) error {
		return builder.AddFreeFunction(name, fn)
	}
	for _, f := range []struct {
		name string
		fn   any
	}{
		{"join", strings.Join},
		{"split", strings.Split},
		{"contains", strings.Contains},
		{"has_prefix", strings.HasPrefix},
		{"has_suffix", strings.HasSuffix},
		{"trim", strings.TrimSpace},
		{"to_upper", strings.ToUpper},
		{"to_lower", strings.ToLower},
		{"parse_int", parseStdInt},
		{"parse_float", parseStdFloat},
		{"err_text", errText},
		{"now_ms", nowMillis},
	} {
		if err := register(f.name, f.fn); err != nil {
			return err
		}
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("std")
}
package strings

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

// parseStdInt mirrors the std.parse_int contract: decimal-only, rejects
// trailing junk (unlike Go's ParseInt with base 0). Returns int (the spore
// scalar default) to match the host binding's schema.
func parseStdInt(s string) (int, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse_int: %q is not a valid int", s)
	}
	return int(n), nil
}

func parseStdFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parse_float: %q is not a valid float", s)
	}
	return f, nil
}

// errText renders a caught error value to its message text. It accepts the
// standard error map produced by catch ({"message": ...}) or any string.
func errText(v any) string {
	switch e := v.(type) {
	case nil:
		return ""
	case string:
		return e
	case map[string]any:
		if msg, ok := e["message"].(string); ok {
			return msg
		}
		return fmt.Sprint(v)
	case error:
		return e.Error()
	default:
		return fmt.Sprint(v)
	}
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
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

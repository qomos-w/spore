package std

import (
	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/base64"
	"github.com/qomos-w/spore/std/bytes"
	"github.com/qomos-w/spore/std/hash"
	"github.com/qomos-w/spore/std/hex"
	stdhttp "github.com/qomos-w/spore/std/http"
	"github.com/qomos-w/spore/std/json"
	"github.com/qomos-w/spore/std/math"
	"github.com/qomos-w/spore/std/random"
	"github.com/qomos-w/spore/std/regexp"
	"github.com/qomos-w/spore/std/strconv"
	"github.com/qomos-w/spore/std/strings"
	"github.com/qomos-w/spore/std/time"
	"github.com/qomos-w/spore/std/url"
	"github.com/qomos-w/spore/std/uuid"
	"github.com/qomos-w/spore/std/ws"
)

// moduleEntry holds a standard library module name and its registration function.
type moduleEntry struct {
	name     string
	register func(*binding.ScriptBinding) error
}

var modules = []moduleEntry{
	{"base64", base64.Register},
	{"bytes", bytes.Register},
	{"hash", hash.Register},
	{"hex", hex.Register},
	{"json", json.Register},
	{"math", math.Register},
	{"random", random.Register},
	{"regexp", regexp.Register},
	{"strconv", strconv.Register},
	{"strings", strings.Register},
	{"time", time.Register},
	{"url", url.Register},
	{"uuid", uuid.Register},
	{"http", stdhttp.Register},
	{"ws", ws.Register},
}

// NewScriptBindingWithStd creates a ScriptBinding with all standard library modules
// pre-registered. This is the convenience path for hosts that want standard library
// functions available without calling RegisterAll explicitly.
func NewScriptBindingWithStd() *binding.ScriptBinding {
	sb := binding.NewScriptBinding()
	_ = RegisterAll(sb)
	return sb
}

// NewScriptBindingWithStdExcept creates a ScriptBinding with all standard library
// modules except the excluded ones. Hosts can use this to omit specific modules
// (e.g. to provide a custom implementation).
func NewScriptBindingWithStdExcept(exclude ...string) *binding.ScriptBinding {
	sb := binding.NewScriptBinding()
	_ = RegisterAllExcept(sb, exclude...)
	return sb
}

// RegisterAll registers all standard library modules into the given ScriptBinding.
// Returns an error if any module fails to register (including duplicate names).
func RegisterAll(sb *binding.ScriptBinding) error {
	for _, m := range modules {
		if err := m.register(sb); err != nil {
			return err
		}
	}
	return nil
}

// RegisterAllIfAbsent registers all standard library modules that are not already
// present in the ScriptBinding. If a module name already exists (e.g. a host-provided
// custom implementation was pre-registered), it is skipped silently.
func RegisterAllIfAbsent(sb *binding.ScriptBinding) error {
	for _, m := range modules {
		if _, ok := sb.DescribeCapability(m.name); ok {
			continue
		}
		if err := m.register(sb); err != nil {
			return err
		}
	}
	return nil
}

// RegisterAllExcept registers all standard library modules except those named in
// the exclude list. Hosts can use this to omit specific modules and then register
// their own custom implementations.
func RegisterAllExcept(sb *binding.ScriptBinding, exclude ...string) error {
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		excludeSet[name] = struct{}{}
	}
	for _, m := range modules {
		if _, skip := excludeSet[m.name]; skip {
			continue
		}
		if err := m.register(sb); err != nil {
			return err
		}
	}
	return nil
}

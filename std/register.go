package std

import (
	"sync"

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

// Module is the interface constraint a standard library module adapter must
// satisfy. Its surface mirrors exactly what the registry consumers use: a
// stable module name (the capability namespace, also used for exclude and
// if-absent matching) and a registration hook that installs the module's
// callables into a ScriptBinding.
type Module interface {
	// Name returns the module name (e.g. "math"). It doubles as the capability
	// namespace the module registers into the ScriptBinding.
	Name() string
	// Register installs the module's callables into sb.
	Register(sb *binding.ScriptBinding) error
}

// moduleFunc is the default Module adapter: a name paired with a registration
// function. It is what NewModule returns and what the built-in registry holds.
type moduleFunc struct {
	name     string
	register func(*binding.ScriptBinding) error
}

var _ Module = moduleFunc{}

// Name implements Module.
func (m moduleFunc) Name() string { return m.name }

// Register implements Module.
func (m moduleFunc) Register(sb *binding.ScriptBinding) error { return m.register(sb) }

// NewModule adapts a module name and registration function into a Module. It
// lets hosts and tests inject ad-hoc modules without declaring a named type.
func NewModule(name string, register func(*binding.ScriptBinding) error) Module {
	return moduleFunc{name: name, register: register}
}

// registryMu guards modules, the package-level module registry. The registry is
// a package global so hosts get a ready-made standard library, but it can be
// snapshotted and replaced through Registry and SetRegistry. That pair is the
// test-isolation seam: a test swaps in its own modules, then restores the
// built-in set so the global is left unpolluted for every other test.
var (
	registryMu sync.RWMutex
	modules    = builtinModules()
)

// builtinModules returns the default registry contents: every standard library
// module paired with its Register function.
func builtinModules() []Module {
	return []Module{
		NewModule("base64", base64.Register),
		NewModule("bytes", bytes.Register),
		NewModule("hash", hash.Register),
		NewModule("hex", hex.Register),
		NewModule("json", json.Register),
		NewModule("math", math.Register),
		NewModule("random", random.Register),
		NewModule("regexp", regexp.Register),
		NewModule("strconv", strconv.Register),
		NewModule("strings", strings.Register),
		NewModule("time", time.Register),
		NewModule("url", url.Register),
		NewModule("uuid", uuid.Register),
		NewModule("http", stdhttp.Register),
		NewModule("ws", ws.Register),
	}
}

// Registry returns a copy of the current module registry in registration order.
// Pair it with SetRegistry to save and restore the registry around a test that
// substitutes modules.
func Registry() []Module {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Module, len(modules))
	copy(out, modules)
	return out
}

// SetRegistry replaces the current module registry with mods. Passing a value
// previously returned by Registry restores that snapshot; passing a custom set
// injects it. The slice is copied, so the caller may reuse it afterwards.
func SetRegistry(mods []Module) {
	registryMu.Lock()
	defer registryMu.Unlock()
	modules = append([]Module(nil), mods...)
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
	for _, m := range Registry() {
		if err := m.Register(sb); err != nil {
			return err
		}
	}
	return nil
}

// RegisterAllIfAbsent registers all standard library modules that are not already
// present in the ScriptBinding. If a module name already exists (e.g. a host-provided
// custom implementation was pre-registered), it is skipped silently.
func RegisterAllIfAbsent(sb *binding.ScriptBinding) error {
	for _, m := range Registry() {
		if _, ok := sb.DescribeCapability(m.Name()); ok {
			continue
		}
		if err := m.Register(sb); err != nil {
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
	for _, m := range Registry() {
		if _, skip := excludeSet[m.Name()]; skip {
			continue
		}
		if err := m.Register(sb); err != nil {
			return err
		}
	}
	return nil
}

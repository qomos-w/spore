package std_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std"
)

var expectedFunctions = []string{
	"base64.encode", "base64.decode",
	"hash.md5", "hash.sha256",
	"hex.encode", "hex.decode",
	"json.encode", "json.decode", "json.prettyEncode",
	"math.abs", "math.max", "math.pow",
	"random.intn", "random.float64",
	"regexp.match", "regexp.find", "regexp.findAll", "regexp.replace", "regexp.split",
	"strconv.parseInt", "strconv.parseFloat", "strconv.formatInt", "strconv.formatFloat",
	"strings.contains", "strings.toUpper", "strings.replace",
	"time.nowUnix", "time.now", "time.format",
	"url.encode", "url.decode", "url.parse",
	"uuid.v4", "uuid.nil",
}

func TestRegisterAll_RegistersAllModules(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := std.RegisterAll(sb); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range expectedFunctions {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestNewScriptBindingWithStd_RegistersAllModules(t *testing.T) {
	sb := std.NewScriptBindingWithStd()

	for _, name := range expectedFunctions {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestRegisterAllIfAbsent_SkipsExisting(t *testing.T) {
	sb := binding.NewScriptBinding()

	// Pre-register a custom "math" module with a unique function.
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("customAdd", func(a, b float64) float64 {
		return a + b + 999 // distinguishable from standard math
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("math"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}

	// RegisterAllIfAbsent should skip the existing "math" module.
	if err := std.RegisterAllIfAbsent(sb); err != nil {
		t.Fatalf("RegisterAllIfAbsent: %v", err)
	}

	// Verify custom math is still present.
	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "math.customAdd", Stage: binding.InvocationStageUnary, Args: []any{1.0, 2.0}})
	if err != nil {
		t.Fatalf("Invoke customAdd: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 1002.0 {
		t.Fatalf("expected customAdd to return 1002, got %+v", outcome.Payload)
	}

	// Verify other standard modules were registered.
	for _, name := range []string{"hex.encode", "strings.contains", "time.nowUnix"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}

	// Standard math functions should NOT be present (custom math took precedence).
	_, ok := sb.Executors.Lookup("math.abs")
	if ok {
		t.Fatalf("expected math.abs to NOT be registered (custom math took precedence)")
	}
}

func TestRegisterAllExcept_OmitsExcluded(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := std.RegisterAllExcept(sb, "math", "hex"); err != nil {
		t.Fatalf("RegisterAllExcept: %v", err)
	}

	// math and hex should not be present.
	for _, name := range []string{"math.abs", "math.max", "hex.encode", "hex.decode"} {
		_, ok := sb.Executors.Lookup(name)
		if ok {
			t.Fatalf("expected %s to NOT be registered", name)
		}
	}

	// Other modules should be present.
	for _, name := range []string{"strings.contains", "random.intn", "time.nowUnix"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestNewScriptBindingWithStdExcept_OmitsExcluded(t *testing.T) {
	sb := std.NewScriptBindingWithStdExcept("math")

	// math should not be present.
	_, ok := sb.Executors.Lookup("math.abs")
	if ok {
		t.Fatalf("expected math.abs to NOT be registered")
	}

	// Other modules should be present.
	for _, name := range []string{"hex.encode", "strings.contains", "random.intn", "time.nowUnix"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

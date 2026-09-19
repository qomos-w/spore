package vm

import (
	"strings"
	"testing"
)

func TestCallStackPushPop(t *testing.T) {
	cs := newCallStack(16)

	cs.push("main", "test.spore", 1)
	cs.push("foo", "test.spore", 5)
	cs.push("bar", "lib.spore", 10)

	if cs.depth() != 3 {
		t.Errorf("depth = %d, want 3", cs.depth())
	}

	cs.pop()
	if cs.depth() != 2 {
		t.Errorf("depth after pop = %d, want 2", cs.depth())
	}
}

func TestCallStackPopOnEmpty(t *testing.T) {
	cs := newCallStack(16)
	// Should not panic.
	cs.pop()
	if cs.depth() != 0 {
		t.Errorf("depth after pop on empty = %d, want 0", cs.depth())
	}
}

func TestCallStackClear(t *testing.T) {
	cs := newCallStack(16)
	cs.push("a", "", 1)
	cs.push("b", "", 2)
	cs.clear()
	if cs.depth() != 0 {
		t.Errorf("depth after clear = %d, want 0", cs.depth())
	}
}

func TestCallStackFormatEmpty(t *testing.T) {
	cs := newCallStack(16)
	result := cs.formatStackTrace()
	if !strings.Contains(result, "empty") {
		t.Errorf("empty stack trace should mention 'empty', got: %q", result)
	}
}

func TestCallStackFormatNonEmpty(t *testing.T) {
	cs := newCallStack(16)
	cs.push("main", "test.spore", 1)
	cs.push("foo", "test.spore", 10)
	cs.push("bar", "lib.spore", 20)

	result := cs.formatStackTrace()

	// Stack is printed bottom-up, so bar (most recent) appears first.
	if !strings.Contains(result, "bar") {
		t.Error("trace should contain 'bar'")
	}
	if !strings.Contains(result, "foo") {
		t.Error("trace should contain 'foo'")
	}
	if !strings.Contains(result, "main") {
		t.Error("trace should contain 'main'")
	}
	if !strings.Contains(result, "lib.spore:20") {
		t.Error("trace should contain file:line for bar")
	}
}

func TestCallStackFormatNoFile(t *testing.T) {
	cs := newCallStack(16)
	cs.push("anonymous", "", 0)

	result := cs.formatStackTrace()
	if !strings.Contains(result, "anonymous") {
		t.Error("trace should contain function name even without file")
	}
}

func TestCallStackOverflow(t *testing.T) {
	cs := newCallStack(4)
	cs.push("f1", "", 1)
	cs.push("f2", "", 2)
	cs.push("f3", "", 3)
	cs.push("f4", "", 4)

	// 5th push should panic.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on stack overflow")
		}
	}()
	cs.push("f5", "", 5)
}

func TestRuntimeErrorFormat(t *testing.T) {
	err := &runtimeError{
		message:    "division by zero",
		stackTrace: "  [0] div (math.spore:5)\n",
	}
	s := err.Error()
	if !strings.Contains(s, "division by zero") {
		t.Error("error string should contain message")
	}
	if !strings.Contains(s, "div") {
		t.Error("error string should contain stack trace")
	}
}

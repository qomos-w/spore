package vm

import (
	"fmt"
	"runtime"
	"strings"
)

// stackFrame represents a call stack entry.
type stackFrame struct {
	functionName string
	fileName     string
	lineNumber   int
}

// callStack tracks function call frames for error reporting.
type callStack struct {
	frames   []stackFrame
	maxDepth int
}

func newCallStack(maxDepth int) *callStack {
	return &callStack{
		frames:   make([]stackFrame, 0, maxDepth),
		maxDepth: maxDepth,
	}
}

func (cs *callStack) push(functionName, fileName string, lineNumber int) {
	if len(cs.frames) >= cs.maxDepth {
		panic(fmt.Sprintf("stack overflow: max depth %d exceeded", cs.maxDepth))
	}
	cs.frames = append(cs.frames, stackFrame{
		functionName: functionName,
		fileName:     fileName,
		lineNumber:   lineNumber,
	})
}

func (cs *callStack) pop() {
	if len(cs.frames) > 0 {
		cs.frames = cs.frames[:len(cs.frames)-1]
	}
}

func (cs *callStack) depth() int {
	return len(cs.frames)
}

func (cs *callStack) clear() {
	cs.frames = cs.frames[:0]
}

func (cs *callStack) formatStackTrace() string {
	if len(cs.frames) == 0 {
		return "  (empty stack)"
	}

	var sb strings.Builder
	for i := len(cs.frames) - 1; i >= 0; i-- {
		f := cs.frames[i]
		sb.WriteString(fmt.Sprintf("  [%d] %s", len(cs.frames)-1-i, f.functionName))
		if f.fileName != "" {
			sb.WriteString(fmt.Sprintf(" (%s:%d)", f.fileName, f.lineNumber))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// runtimeError is a structured error produced by the VM.
type runtimeError struct {
	message    string
	stackTrace string
}

func (e *runtimeError) Error() string {
	return fmt.Sprintf("%s\n%s", e.message, e.stackTrace)
}

// vmPanic triggers a runtime error panic.
func vmPanic(message string, context ...interface{}) {
	ctx := make(map[string]interface{})
	for i := 0; i < len(context); i += 2 {
		if i+1 < len(context) {
			key := fmt.Sprintf("%v", context[i])
			ctx[key] = context[i+1]
		}
	}

	var msg strings.Builder
	msg.WriteString(message)
	if len(ctx) > 0 {
		msg.WriteString("\nContext:")
		for k, v := range ctx {
			msg.WriteString(fmt.Sprintf("\n  %s: %v", k, v))
		}
	}

	_, file, line, ok := runtime.Caller(1)
	if ok {
		msg.WriteString(fmt.Sprintf("\nGo Source: %s:%d", file, line))
	}

	panic(&runtimeError{message: msg.String()})
}

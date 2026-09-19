package diagnostics

import "sync"

type CodeInfo struct {
	Code        string
	Category    Category
	Description string
	Hint        string
}

var (
	codeRegistryMu sync.RWMutex
	codeRegistry   = make(map[string]CodeInfo)
)

func RegisterCode(info CodeInfo) {
	if info.Code == "" {
		return
	}
	codeRegistryMu.Lock()
	defer codeRegistryMu.Unlock()
	codeRegistry[info.Code] = info
}

func LookupCode(code string) (CodeInfo, bool) {
	codeRegistryMu.RLock()
	defer codeRegistryMu.RUnlock()
	info, ok := codeRegistry[code]
	return info, ok
}

func HintFor(err error) string {
	if err == nil {
		return ""
	}
	if h, ok := err.(hinter); ok {
		return h.DiagnosticHint()
	}
	if c, ok := err.(coder); ok {
		if info, found := LookupCode(c.DiagnosticCode()); found {
			return info.Hint
		}
	}
	return ""
}

func init() {
	RegisterCode(CodeInfo{Code: "multiple_diagnostics", Category: CategoryRuntime, Description: "Multiple diagnostics were produced", Hint: "查看每个 cause 诊断并逐项修复"})
}

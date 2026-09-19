package diagnostics

import (
	"sort"
	"sync"
)

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

// RegisteredCodes returns a snapshot of every registered diagnostic code,
// sorted by code. It exists so callers and tests can observe the full
// registry — e.g. to pin the exact code set and prove a registration refactor
// changed nothing. Hot-path lookups should use LookupCode instead; this walks
// the whole map and allocates.
func RegisteredCodes() []CodeInfo {
	codeRegistryMu.RLock()
	defer codeRegistryMu.RUnlock()
	codes := make([]CodeInfo, 0, len(codeRegistry))
	for _, info := range codeRegistry {
		codes = append(codes, info)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i].Code < codes[j].Code })
	return codes
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

// builtinCodes are the diagnostic codes the diagnostics package owns itself.
// Kept as a table so init() is the same uniform registration loop every other
// package uses, while the package stays self-contained.
var builtinCodes = []CodeInfo{
	{Code: "multiple_diagnostics", Category: CategoryRuntime, Description: "Multiple diagnostics were produced", Hint: "查看每个 cause 诊断并逐项修复"},
}

func init() {
	for _, info := range builtinCodes {
		RegisterCode(info)
	}
}

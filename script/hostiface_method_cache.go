package script

import (
	"reflect"
	"sync"
)

// Host interface method resolution cache. reflect.Type method sets are
// immutable, so (type, script-side method name) pairs resolve to a stable
// exported name and method index. Caching removes the per-invoke linear
// MethodByName scan and the per-invoke snake_case→CamelCase string rebuild.
//
// Entries are shared across Runtime instances and goroutines: sync.Map
// handles concurrent population idempotently. A negative index marks
// "declared but absent on the Go type" so miss scans are not repeated.
type cachedHostMethod struct {
	exported string
	index    int
}

type hostMethodKey struct {
	typ    reflect.Type
	method string
}

var hostMethodIndex sync.Map // hostMethodKey -> cachedHostMethod

func lookupHostMethod(target any, methodName string) (reflect.Value, bool) {
	t := reflect.TypeOf(target)
	if t == nil {
		return reflect.Value{}, false
	}
	key := hostMethodKey{typ: t, method: methodName}
	if v, ok := hostMethodIndex.Load(key); ok {
		m := v.(cachedHostMethod)
		if m.index < 0 {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(target).Method(m.index), true
	}
	exported := exportedMethodName(methodName)
	tm, ok := t.MethodByName(exported)
	if !ok {
		hostMethodIndex.Store(key, cachedHostMethod{exported: exported, index: -1})
		return reflect.Value{}, false
	}
	hostMethodIndex.Store(key, cachedHostMethod{exported: exported, index: tm.Index})
	return reflect.ValueOf(target).Method(tm.Index), true
}

package script

import (
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// hostInterfaceLedger is the single owner of a Runtime's host-interface proxy
// state. It replaces the four peer maps (classes/objects/handles/bindings) that
// used to live directly on Runtime plus the two ad-hoc VM-tracking fields
// (hostIfaceRootProviderVM/hostIfaceRootProviderID), and makes the authority
// relationship between them explicit:
//
//   - objects is the AUTHORITATIVE index. Object identity (ID), symbol
//     (Namespace/Name), shape (InterfaceDesc) and payload (Target) live here,
//     and nowhere else; every other index is a projection of it.
//   - bindings is a durable symbol -> object-ID projection: a namespace/name
//     resolves to the ID of the object that symbol is bound to. It survives
//     Reset/Clone because the bound symbols and targets do, so it is kept
//     rather than rebuilt from objects.
//   - handles is a VM-scoped projection (proxy handle -> object ID). A handle
//     is only meaningful to the VM it was allocated on; when the execution VM
//     is replaced the whole projection is dropped in one assignment and
//     repopulated lazily as proxies are re-registered against the new VM. This
//     local invalidation replaces the old full-table rewrite of every object's
//     Handle/Pending fields plus the class-map rebuild on every Reset/Clone.
//
// The VM-scoped layer also owns the GC root-provider registration, so the
// provider's VM/ID bookkeeping is invalidated by exactly the act that
// invalidates the handle table — the two can no longer drift apart.
type hostInterfaceLedger struct {
	// Durable layer (survives Reset/Clone) -------------------------------

	// objects is the authoritative index: ID -> record. IDs are never reused.
	objects map[uint64]hostInterfaceObject
	// bindings is the symbol -> primary object-ID projection.
	bindings map[hostInterfaceBindingKey]uint64
	// nextID is the monotonic object identity allocator.
	nextID uint64

	// Live (VM-scoped) layer (dropped wholesale on VM replacement) -------

	// vm is the execution VM the live projection belongs to and the VM the
	// host-interface GC root provider is attached to. nil before a VM is bound.
	vm *vm.VM
	// handles is the live handle -> object ID projection for vm.
	handles map[vm.Handle]uint64
	// rootProviderID is the GC root provider registered on vm.
	rootProviderID int
}

func newHostInterfaceLedger() hostInterfaceLedger {
	return hostInterfaceLedger{
		objects:  make(map[uint64]hostInterfaceObject),
		bindings: make(map[hostInterfaceBindingKey]uint64),
	}
}

// bindVM adopts v as the live execution VM: it invalidates the previous
// VM-scoped projection wholesale (handles and proxy-class IDs belong to the old
// VM and are stale the moment it is discarded) and attaches the host-interface
// GC root provider to the new one. It is invoked from the evaluator's
// VM-replacement subscription, so construction, the compile-time VM swap,
// Reset and Clone all funnel through this single invalidation point; none of
// them needs to walk the durable object set.
func (l *hostInterfaceLedger) bindVM(v *vm.VM) {
	l.vm = v
	l.handles = make(map[vm.Handle]uint64)
	l.rootProviderID = v.AddRootProvider(l.markRoots)
}

// markRoots is the GC root provider that keeps host interface proxy objects
// live. Proxy objects are allocated in the VM heap via CreateObject but are
// only referenced from this ledger's handle table, which the VM GC cannot see —
// so without this provider sustained allocation reclaims a proxy, the handle is
// reused, and method dispatch panics with "object class not found".
func (l *hostInterfaceLedger) markRoots(visit func(vm.Value)) {
	for h := range l.handles {
		visit(vm.EncodeHandle(h))
	}
}

// clear releases every index, durable and live. Used by Runtime.Close.
func (l *hostInterfaceLedger) clear() {
	*l = hostInterfaceLedger{}
}

// clone builds the durable half of a fresh ledger for a new Runtime: object
// records and bindings are copied (descriptors deep-copied, targets shared),
// while the VM-scoped projection starts empty and is populated when the clone's
// own execution VM is bound. Copying nextID keeps object identities unique
// across the two Runtimes.
func (l *hostInterfaceLedger) clone() hostInterfaceLedger {
	out := hostInterfaceLedger{
		nextID:   l.nextID,
		objects:  make(map[uint64]hostInterfaceObject, len(l.objects)),
		bindings: make(map[hostInterfaceBindingKey]uint64, len(l.bindings)),
	}
	for id, obj := range l.objects {
		obj.InterfaceDesc = schema.CloneInterfaceDesc(obj.InterfaceDesc)
		out.objects[id] = obj
	}
	for k, v := range l.bindings {
		out.bindings[k] = v
	}
	return out
}

func (l *hostInterfaceLedger) allocID() uint64 {
	return atomic.AddUint64(&l.nextID, 1)
}

// objectForHandle resolves a proxy handle to its object record. This is the
// method-dispatch hot path.
func (l *hostInterfaceLedger) objectForHandle(handle vm.Handle) (hostInterfaceObject, bool) {
	if l == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := l.handles[handle]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := l.objects[id]
	return obj, ok
}

// liveIDs returns the set of object IDs that currently hold a proxy handle.
// Callers that need to classify many objects build this once rather than
// probing the handle table per object (which would be quadratic).
func (l *hostInterfaceLedger) liveIDs() map[uint64]struct{} {
	live := make(map[uint64]struct{}, len(l.handles))
	for _, id := range l.handles {
		live[id] = struct{}{}
	}
	return live
}

// pending returns a snapshot of the records that must be registered against the
// current VM. Snapshotting (rather than ranging objects directly) keeps
// finalize deterministic when registration mints or adopts records.
func (l *hostInterfaceLedger) pending() []hostInterfaceObject {
	live := l.liveIDs()
	var out []hostInterfaceObject
	for _, obj := range l.objects {
		if _, ok := live[obj.ID]; !ok {
			out = append(out, obj)
		}
	}
	return out
}

// lookupBound returns the record a symbol is bound to.
func (l *hostInterfaceLedger) lookupBound(namespace, name string) (hostInterfaceObject, bool) {
	if l == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := l.bindings[hostInterfaceBindingKey{Namespace: namespace, Name: name}]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := l.objects[id]
	return obj, ok
}

// lookupByTarget finds the (namespace, name) object whose target deep-equals
// target. When requireLive is true only objects holding a proxy on the bound VM
// are considered, and the scan walks the live handle projection (so its cost is
// the live set, not the whole durable set).
func (l *hostInterfaceLedger) lookupByTarget(namespace, name string, target any, requireLive bool) (hostInterfaceObject, bool) {
	if requireLive {
		for _, id := range l.handles {
			obj, ok := l.objects[id]
			if !ok || obj.Namespace != namespace || obj.Name != name {
				continue
			}
			if reflect.DeepEqual(obj.Target, target) {
				return obj, true
			}
		}
		return hostInterfaceObject{}, false
	}
	for _, obj := range l.objects {
		if obj.Namespace != namespace || obj.Name != name {
			continue
		}
		if reflect.DeepEqual(obj.Target, target) {
			return obj, true
		}
	}
	return hostInterfaceObject{}, false
}

// handleForValue returns the live proxy handle bound to value, if any.
func (l *hostInterfaceLedger) handleForValue(value any) (vm.Handle, bool) {
	for h, id := range l.handles {
		obj, ok := l.objects[id]
		if !ok {
			continue
		}
		if reflect.DeepEqual(obj.Target, value) {
			return h, true
		}
	}
	return vm.InvalidHandle, false
}

// deleteObject removes a record and any live handle pointing at it.
func (l *hostInterfaceLedger) deleteObject(id uint64) {
	delete(l.objects, id)
	for h, mapped := range l.handles {
		if mapped == id {
			delete(l.handles, h)
		}
	}
}

// consistencyError returns a description of the first violation of the
// three-index ledger invariant, or "" when the ledger is consistent. The
// invariant is the executable form of the objects/handles/bindings layering:
//
//	(1) every object record is filed under its own ID;
//	(2) every handle resolves to an existing object, and no object holds two
//	    handles;
//	(3) every binding resolves to an existing object.
func (l *hostInterfaceLedger) consistencyError() string {
	for id, obj := range l.objects {
		if obj.ID != id {
			return fmt.Sprintf("object key %d files a record with ID %d", id, obj.ID)
		}
	}
	seen := make(map[uint64]vm.Handle, len(l.handles))
	for h, id := range l.handles {
		if _, ok := l.objects[id]; !ok {
			return fmt.Sprintf("handle %d maps to missing object %d", uint64(h), id)
		}
		if prev, dup := seen[id]; dup {
			return fmt.Sprintf("object %d holds two handles (%d, %d)", id, uint64(prev), uint64(h))
		}
		seen[id] = h
	}
	for key, id := range l.bindings {
		if _, ok := l.objects[id]; !ok {
			return fmt.Sprintf("binding %s/%s maps to missing object %d", key.Namespace, key.Name, id)
		}
	}
	return ""
}

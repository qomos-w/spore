package runtime

import (
	"reflect"
	"strings"
	"testing"
)

// Test component types local to this file. Names are namespaced to avoid
// ambiguity when multiple tables are merged onto one World in a test.
type regPos struct{ X, Y float64 }
type regHp struct{ Value int }

var (
	regPosC = NewComponent[regPos]("reg.Position")
	regHpC  = NewComponent[regHp]("reg.Health")
)

// testTable is a minimal SchemaTable built by hand. IDs must be unique per
// test; a fresh table is constructed per test so nothing is shared.
type testTable struct {
	types map[uint64]reflect.Type
	names map[uint64]string
	comps []uint64
}

func (t testTable) SchemaTypes() map[uint64]reflect.Type { return t.types }
func (t testTable) SchemaIDs() map[uint64]string         { return t.names }
func (t testTable) ComponentIDs() []uint64               { return t.comps }

func tableOf(entries ...tableEntry) testTable {
	tt := testTable{
		types: make(map[uint64]reflect.Type),
		names: make(map[uint64]string),
	}
	for _, e := range entries {
		tt.types[e.id] = e.typ
		tt.names[e.id] = e.name
		if e.comp {
			tt.comps = append(tt.comps, e.id)
		}
	}
	return tt
}

type tableEntry struct {
	id   uint64
	name string
	typ  reflect.Type
	comp bool
}

func compEntry[T any](id uint64, name string) tableEntry {
	return tableEntry{id: id, name: name, typ: reflect.TypeOf((*T)(nil)).Elem(), comp: true}
}

func TestAddRegistry_ResolvesViaFacade(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regPos](100, "reg.Position"), compEntry[regHp](101, "reg.Health")))
	e := w.Create()

	// Descriptor-free write resolves the registered name.
	if err := w.SetT(e, &regPos{X: 1, Y: 2}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
	if got, ok := w.GetT[regPos](e); !ok || got.X != 1 || got.Y != 2 {
		t.Fatalf("GetT after SetT = %v %v", got, ok)
	}
	if !w.HasT[regPos](e) {
		t.Fatal("HasT must see the registered component")
	}

	// Interop with the descriptor API: same storage slot.
	if p, ok := w.Get(e, regPosC); !ok || p.X != 1 {
		t.Fatalf("descriptor Get after SetT = %v %v", p, ok)
	}

	// Mark + remove through the facade.
	w.MarkT[regPos](e)
	if n := len(w.Execute(NewQuery().OnChanged(regPosC))); n != 1 {
		t.Fatalf("MarkT must record change, matched %d", n)
	}
	w.RemoveT[regPos](e)
	if w.HasT[regPos](e) {
		t.Fatal("RemoveT must remove the component")
	}
}

func TestAddRegistry_SecondRegistrationMerges(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regPos](100, "reg.Position")))
	w.AddRegistry(tableOf(compEntry[regPos](100, "reg.Position"), compEntry[regHp](101, "reg.Health")))
	e := w.Create()
	if err := w.SetT(e, &regHp{Value: 7}); err != nil {
		t.Fatalf("SetT after merge: %v", err)
	}
	if v, ok := w.GetT[regHp](e); !ok || v.Value != 7 {
		t.Fatalf("GetT = %v %v", v, ok)
	}
}

func TestLookupComponent_EcsResolvesMappingFromWorldRegistry(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regPos](100, "reg.Position")))

	// ECS code holding only the type (not the generated <Name>C var)
	// resolves the descriptor from the world registry at runtime.
	c, ok := w.LookupComponent[regPos]()
	if !ok {
		t.Fatal("registered type must resolve via LookupComponent")
	}
	if c.Name() != "reg.Position" {
		t.Fatalf("resolved name = %q, want %q", c.Name(), "reg.Position")
	}

	e := w.Create()
	if err := w.Set(e, c, &regPos{X: 3}); err != nil {
		t.Fatalf("Set via looked-up descriptor: %v", err)
	}
	if p, ok2 := w.Get(e, c); !ok2 || p.X != 3 {
		t.Fatalf("Get via looked-up descriptor = %v %v", p, ok2)
	}
	if n := len(w.Execute(NewQuery().With(c))); n != 1 {
		t.Fatalf("query via looked-up descriptor matched %d", n)
	}

	// Unregistered types must not resolve.
	type notInRegistry struct{ A int }
	if _, ok := w.LookupComponent[notInRegistry](); ok {
		t.Fatal("unregistered type must not resolve")
	}
}

func TestAddRegistry_UnregisteredTypeFails(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	type neverRegistered struct{ A int }
	if err := w.SetT(e, &neverRegistered{}); err == nil {
		t.Fatal("SetT on unregistered type must fail")
	}
	if !strings.Contains(w.SetT(e, &neverRegistered{}).Error(), "not registered") {
		t.Fatal("error must explain the missing registration")
	}
	if _, ok := w.GetT[neverRegistered](e); ok {
		t.Fatal("GetT on unregistered type must return false")
	}
	if w.HasT[neverRegistered](e) {
		t.Fatal("HasT on unregistered type must return false")
	}
}

func TestAddRegistry_ConflictingNamePanics(t *testing.T) {
	type regConflict struct{ B int }

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("registering the same type under two names must panic")
		}
	}()
	// Same type regConflict under two different schema IDs/names in one table.
	w := NewWorld()
	w.AddRegistry(tableOf(
		compEntry[regConflict](10, "reg.Conflict.A"),
		compEntry[regConflict](11, "reg.Conflict.B"),
	))
}

func TestAddRegistry_ComponentMustBeSchemaSubset(t *testing.T) {
	// A plain schema struct (no @component) is in SchemaTypes/SchemaIDs but
	// NOT in ComponentIDs: it must not become addressable via the facade.
	type regPlain struct{ S string }

	w := NewWorld()
	tt := tableOf(
		compEntry[regPos](100, "reg.Position"),
		tableEntry{id: 200, name: "reg.Plain", typ: reflect.TypeOf(regPlain{}), comp: false},
	)
	w.AddRegistry(tt)

	if _, ok := w.LookupComponent[regPlain](); ok {
		t.Fatal("non-component schema must not resolve via the facade")
	}

	e := w.Create()
	if err := w.SetT(e, &regPos{}); err != nil {
		t.Fatalf("SetT on component: %v", err)
	}
}

func TestAddRegistry_PointerShapeNormalization(t *testing.T) {
	// SchemaTypes may carry the value type; lookups via *T (SetT stores *T)
	// must still resolve.
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regHp](101, "reg.Health")))
	e := w.Create()
	if err := w.SetT(e, &regHp{Value: 5}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
	if v, ok := w.GetT[regHp](e); !ok || v.Value != 5 {
		t.Fatalf("GetT = %v %v", v, ok)
	}
}

func TestSetT_InteropWithDescriptorsAndRegistry(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regPos](100, "reg.Position")))
	e := w.Create()

	// The facade and the explicit descriptor write the SAME slot: the
	// registered name is the storage key, not the Go type.
	if err := w.SetT(e, &regPos{X: 9}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
	if err := w.Set(e, regPosC, &regPos{X: 10}); err != nil {
		t.Fatalf("descriptor Set: %v", err)
	}
	if p, _ := w.Get(e, regPosC); p.X != 10 {
		t.Fatalf("last write must win on the shared slot, got %v", p.X)
	}
	if n := len(w.Execute(NewQuery().With(regPosC))); n != 1 {
		t.Fatalf("query by schema name must match, got %d", n)
	}
}

func TestAddRegistry_NilTableNoop(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(nil) // must not panic
	e := w.Create()
	if err := w.SetT(e, &regPos{}); err == nil {
		t.Fatal("SetT must still fail after nil AddRegistry")
	}
}

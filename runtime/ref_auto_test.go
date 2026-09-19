package runtime

import (
	"strings"
	"testing"
)

// Auto-recognized component structs for the aggregate host. Exported names so
// they can be embedded anonymously (unexported anonymous fields are skipped by
// the auto-scan).
type AggPos struct{ X, Y float64 }
type AggHp struct{ Value int }
type AggVel struct{ DX float64 }

func aggTable() testTable {
	return tableOf(
		compEntry[AggPos](300, "agg.Position"),
		compEntry[AggHp](301, "agg.Hp"),
		compEntry[AggVel](302, "agg.Vel"),
	)
}

func TestAggregateAuto_AnonymousEmbedRegisters(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(aggTable())

	h := &struct {
		Ref
		AggPos
		AggHp
	}{}
	e, err := Bind(w, h).Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if w.EntityCount() != 1 {
		t.Fatalf("EntityCount = %d", w.EntityCount())
	}
	p, ok := w.GetT[AggPos](e)
	if !ok || p != &h.AggPos {
		t.Fatalf("GetT[AggPos] must alias the anonymous embed field: ptr %p vs %p (ok=%v)", p, &h.AggPos, ok)
	}
	if _, ok := w.GetT[AggHp](e); !ok {
		t.Fatal("GetT[AggHp] must resolve for the embedded AggHp")
	}

	// Zero-copy aliasing: direct field writes are visible through the ECS view.
	h.AggPos.X = 42
	if p.X != 42 {
		t.Fatalf("ECS view must see direct field write, got X=%v", p.X)
	}

	// Registered host is reachable through the host view.
	if got, ok := w.Host(e); !ok || got != any(h) {
		t.Fatalf("Host(e) = %v %v", got, ok)
	}
}

func TestAggregateAuto_NamedFieldsRegister(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(aggTable())

	h := &struct {
		Ref
		Pos AggPos
		HP  AggHp
	}{Pos: AggPos{X: 1}}
	e, err := Bind(w, h).Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, ok := w.GetT[AggPos](e)
	if !ok || p != &h.Pos {
		t.Fatalf("GetT[AggPos] must alias the named field Pos: %p vs %p (ok=%v)", p, &h.Pos, ok)
	}
	if p.X != 1 {
		t.Fatalf("registered value must match the field, got X=%v", p.X)
	}
}

func TestAggregateAuto_NamedAndAnonymousConflict(t *testing.T) {
	// The same component type both embedded anonymously and present as a
	// named field maps to one storage slot twice — Register must error and
	// create nothing.
	w := NewWorld()
	w.AddRegistry(aggTable())

	h := &struct {
		Ref
		AggPos
		Extra AggPos
	}{}
	e, err := Bind(w, h).Register()
	if err == nil {
		t.Fatalf("Register must fail for named+anonymous duplicate, got entity %v", e)
	}
	if !strings.Contains(err.Error(), "agg.Position") || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("error must name the component and the rule, got: %v", err)
	}
	if w.EntityCount() != 0 {
		t.Fatalf("failed Register must not create an entity, count = %d", w.EntityCount())
	}
	if h.IsRegistered() {
		t.Fatal("failed Register must leave the host unregistered")
	}
}

func TestAggregateAuto_TwoNamedSameTypeConflict(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(aggTable())
	h := &struct {
		Ref
		A AggPos
		B AggPos
	}{}
	if _, err := Bind(w, h).Register(); err == nil {
		t.Fatal("Register must fail when two named fields share one component type")
	}
}

func TestAggregateAuto_ExplicitAttachSamePointerMerges(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(aggTable())
	h := &struct {
		Ref
		AggPos
	}{}
	aggPosDesc := NewComponent[AggPos]("agg.Position")
	e, err := Bind(w, h).Attach(aggPosDesc, &h.AggPos).Register()
	if err != nil {
		t.Fatalf("Register must merge identical explicit+auto pointer: %v", err)
	}
	if p, ok := w.GetT[AggPos](e); !ok || p != &h.AggPos {
		t.Fatalf("GetT[AggPos] = %p (ok=%v), want alias %p", p, ok, &h.AggPos)
	}
}

func TestAggregateAuto_PlainFieldsIgnored(t *testing.T) {
	type plainMsg struct{ Note string }
	w := NewWorld()
	w.AddRegistry(aggTable())
	h := &struct {
		Ref
		AggPos
		AggHp
		Tag    string
		Msg    plainMsg // not registered → ignored
		Extra  *AggPos  // pointer field → skipped by auto-scan
		aggPos AggPos   // unexported named field → skipped
	}{Extra: &AggPos{}}
	e, err := Bind(w, h).Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if n := len(w.ComponentNames(e)); n != 2 {
		t.Fatalf("only registered value fields must become components, got %v", w.ComponentNames(e))
	}
}

func TestAggregateAuto_NoRegistryRegistersNothing(t *testing.T) {
	w := NewWorld() // no AddRegistry
	h := &struct {
		Ref
		AggPos
	}{}
	e, err := Bind(w, h).Register()
	if err != nil {
		t.Fatalf("Register without registry must not error: %v", err)
	}
	if w.EntityCount() != 1 {
		t.Fatalf("EntityCount = %d", w.EntityCount())
	}
	if n := len(w.ComponentNames(e)); n != 0 {
		t.Fatalf("no registry → no auto components, got %v", w.ComponentNames(e))
	}
}

func TestAggregateAuto_CoexistsWithDescriptorComponents(t *testing.T) {
	// Auto fields register at Bind; components added later through the
	// descriptor API are peers of the field components on the same entity.
	w := NewWorld()
	w.AddRegistry(aggTable())
	h := &struct {
		Ref
		AggPos
		AggHp
	}{}
	e, err := Bind(w, h).Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	vel := AggVel{DX: 1.5}
	aggVelDesc := NewComponent[AggVel]("agg.Vel")
	if err := w.Set(e, aggVelDesc, &vel); err != nil {
		t.Fatalf("Set extra component: %v", err)
	}
	if v, ok := w.Get(e, aggVelDesc); !ok || v.DX != 1.5 {
		t.Fatalf("Get extra component = %v %v", v, ok)
	}
	// Field components and the later component coexist on one entity.
	if n := len(w.ComponentNames(e)); n != 3 {
		t.Fatalf("ComponentNames = %v, want 3", w.ComponentNames(e))
	}
}

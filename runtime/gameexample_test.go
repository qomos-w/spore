// Package gameexample demonstrates the two authoring paradigms of the
// spore runtime carrier with a minimal game: a field of units that move,
// lose health over time, and die at zero.
//
// It is a test-driven example: both paradigms run the same rules and the
// test asserts identical outcomes, proving the paradigms interoperate on
// one World.
package runtime_test

import (
	"testing"

	"github.com/qomos-w/spore/runtime"
)

// --- Shared component types and descriptors (schema-name keyed) ---

type gPos struct{ X, Y float64 }
type gVel struct{ DX, DY float64 }
type gHealth struct{ HP float64 }
type gDead struct{}

var (
	gPosC  = runtime.NewComponent[gPos]("game.Position")
	gVelC  = runtime.NewComponent[gVel]("game.Velocity")
	gHpC   = runtime.NewComponent[gHealth]("game.Health")
	gDeadC = runtime.NewComponent[gDead]("game.Dead")
)

const (
	hungerDamage = 10.0
	deathHP      = 0.0
)

// --- ECS paradigm: systems as functions over query batches ---

func sysHungerECS(w *runtime.World) {
	w.Each2(runtime.NewQuery().With(gHpC).Without(gDeadC), gHpC, gVelC,
		func(e runtime.Entity, hp *gHealth, vel *gVel) {
			// Only moving units burn energy; systems never see host types.
			if vel.DX != 0 || vel.DY != 0 {
				hp.HP -= hungerDamage
				w.Mark(e, gHpC)
			}
		})
}

func sysDeathECS(w *runtime.World) {
	for _, e := range w.Execute(runtime.NewQuery().With(gHpC).Without(gDeadC)) {
		hp, _ := w.Get(e, gHpC)
		if hp.HP <= deathHP {
			w.Remove(e, gHpC)
			w.Remove(e, gVelC)
			w.Set(e, gDeadC, &gDead{})
		}
	}
}

func runECSTick(w *runtime.World) {
	sysHungerECS(w)
	sysDeathECS(w)
	w.Tick()
}

// --- OOP paradigm: systems as methods on host structs ---

type oopUnit struct {
	runtime.Ref
	Pos     gPos
	Vel     gVel
	Health  gHealth
	alive   bool // plain Go field: never a component, invisible to queries
}

func newOOPUnit(w *runtime.World, x, y float64) *oopUnit {
	u := &oopUnit{
		Pos:    gPos{X: x, Y: y},
		Vel:    gVel{DX: 1},
		Health: gHealth{HP: 25},
		alive:  true,
	}
	if _, err := runtime.Bind(w, u).
		Attach(gHpC, &u.Health).
		Attach(gVelC, &u.Vel).
		Register(); err != nil {
		panic(err) // example code; production code propagates
	}
	return u
}

func (u *oopUnit) HungerTick() {
	if !u.alive {
		return
	}
	// Same rule as the ECS system: only moving units burn energy.
	if u.Vel.DX == 0 && u.Vel.DY == 0 {
		return
	}
	u.Health.HP -= hungerDamage
	u.Mark(gHpC) // direct field write needs an explicit Mark
}

func (u *oopUnit) DeathTick() {
	if !u.alive || u.Health.HP > deathHP {
		return
	}
	u.alive = false
	u.Remove(gHpC)
	u.Remove(gVelC)
	u.Set(gDeadC, gDead{})
}

func runOOPTick(w *runtime.World, units []*oopUnit) {
	for _, u := range units {
		u.HungerTick()
	}
	for _, u := range units {
		u.DeathTick()
	}
	w.Tick()
}

// --- The game: both paradigms interoperate on one World ---

func TestGameParadigms_ECS(t *testing.T) {
	w := runtime.NewWorld()

	// Spawn: ECS builds entities component-by-component.
	mover := w.Create()
	w.Set(mover, gPosC, &gPos{X: 0, Y: 0})
	w.Set(mover, gVelC, &gVel{DX: 1, DY: 0})
	w.Set(mover, gHpC, &gHealth{HP: 25})

	idler := w.Create()
	w.Set(idler, gPosC, &gPos{X: 5, Y: 5})
	w.Set(idler, gVelC, &gVel{}) // DX=0, DY=0: idles, burns nothing
	w.Set(idler, gHpC, &gHealth{HP: 25})

	for i := 0; i < 3; i++ {
		runECSTick(w)
	}

	// The mover burned 10 HP per tick for 3 ticks (25 → -5): dead and
	// stripped of gameplay components. The idler (DX=0) survived intact.
	if !w.Has(mover, gDeadC) {
		t.Fatal("mover must be Dead after 3 ticks (30 damage > 25 HP)")
	}
	if w.Has(mover, gHpC) || w.Has(mover, gVelC) {
		t.Fatal("dead entity must be stripped of Health and Velocity")
	}
	if w.Has(idler, gDeadC) {
		t.Fatal("idler must never be marked Dead (no velocity, no damage)")
	}
	if hp, _ := w.Get(idler, gHpC); hp.HP != 25 {
		t.Fatalf("idler HP = %v, want 25 (untouched)", hp.HP)
	}
}

func TestGameParadigms_OOP(t *testing.T) {
	w := runtime.NewWorld()
	units := []*oopUnit{
		newOOPUnit(w, 0, 0), // mover: Vel{DX:1}
		newOOPUnit(w, 5, 5),
	}
	units[1].Vel.DX = 0 // idler: direct field write, then Mark
	units[1].Mark(gVelC)

	for i := 0; i < 3; i++ {
		runOOPTick(w, units)
	}

	// Same rule as ECS: mover burned 30 of 25 HP → dead; idler intact.
	if units[0].alive || !units[0].Has(gDeadC) {
		t.Fatal("mover must be dead after 3 ticks (30 damage > 25 HP)")
	}
	if !units[1].alive || units[1].Has(gDeadC) {
		t.Fatal("idler must survive (no damage)")
	}
	if units[1].Health.HP != 25 {
		t.Fatalf("idler HP = %v, want 25 (untouched)", units[1].Health.HP)
	}
	// The host's own field went negative before DeathTick stripped the
	// component view; plain Go state (`alive`) is invisible to queries
	// but perfectly usable in methods.
	if units[0].Health.HP != -5 {
		t.Fatalf("mover host field HP = %v, want -5 (host owns its memory)", units[0].Health.HP)
	}
}

func TestGameParadigms_CoexistSameWorld(t *testing.T) {
	w := runtime.NewWorld()

	// One ECS entity and one OOP host in the SAME world.
	ecsEntity := w.Create()
	w.Set(ecsEntity, gPosC, &gPos{X: 1, Y: 1})
	w.Set(ecsEntity, gVelC, &gVel{DX: 1})
	w.Set(ecsEntity, gHpC, &gHealth{HP: 25})

	oop := newOOPUnit(w, 2, 2)

	// One shared query sees both; EachHost sees only the host.
	q := runtime.NewQuery().With(gHpC).With(gVelC)
	if got := len(w.Execute(q)); got != 2 {
		t.Fatalf("shared query matched %d, want 2 (one per paradigm)", got)
	}

	// OOP iteration is typed: system logic calls methods on the host.
	w.EachHost(q, func(e runtime.Entity, host *oopUnit) { host.HungerTick() })

	// ECS entities keep working with plain component iteration.
	w.Each2(q, gHpC, gVelC, func(e runtime.Entity, hp *gHealth, v *gVel) {
		if v.DX != 0 {
			hp.HP -= hungerDamage
			w.Mark(e, gHpC)
		}
	})

	w.Tick()

	// Both systems ran over BOTH entities (shared query): the host lost
	// 10 HP via EachHost→HungerTick and 10 more via Each2, the ECS entity
	// lost 20 the same way. Zero-copy aliasing means the ECS view sees
	// exactly what the host methods wrote.
	hp, ok := w.Get(oop.Entity, gHpC)
	if !ok || hp.HP != 5 {
		t.Fatalf("host HP after both systems = %v, want 5 (aliased across both paradigms)", hp)
	}
	if hp != &oop.Health {
		t.Fatal("World.Get must alias the host field (zero-copy)")
	}
	ecsHP, _ := w.Get(ecsEntity, gHpC)
	if ecsHP.HP != 15 { // 25 - 10 (Each2 only; EachHost visits hosts only)
		t.Fatalf("ECS entity HP = %v, want 15", ecsHP)
	}
}
package runtime

import "testing"

type benchPos struct{ X, Y float64 }
type benchVel struct{ DX, DY float64 }
type benchTag struct{ On bool }

var (
	benchPosC = NewComponent[benchPos]("bench.Position")
	benchVelC = NewComponent[benchVel]("bench.Velocity")
	benchTagC = NewComponent[benchTag]("bench.Tag")
)

func benchSetup(b *testing.B, n int) (*World, *Query) {
	b.Helper()
	w := NewWorld()
	for i := 0; i < n; i++ {
		e := w.Create()
		if err := w.Set(e, benchPosC, &benchPos{X: float64(i), Y: 1}); err != nil {
			b.Fatal(err)
		}
		if err := w.Set(e, benchVelC, &benchVel{DX: 0.5, DY: -0.5}); err != nil {
			b.Fatal(err)
		}
	}
	return w, NewQuery().With(benchPosC).With(benchVelC)
}

// each2Legacy reproduces the pre-optimization path: Execute materializes
// []Entity, then per-entity Get re-resolves the map twice.
func each2Legacy[T1, T2 any](w *World, q *Query, c1 Component[T1], c2 Component[T2], fn func(e Entity, v1 *T1, v2 *T2)) {
	for _, e := range w.Execute(q) {
		v1, ok1 := w.Get(e, c1)
		v2, ok2 := w.Get(e, c2)
		if !ok1 || !ok2 {
			continue
		}
		fn(e, v1, v2)
	}
}

func benchEach(b *testing.B, n int, iterate func(w *World, q *Query)) {
	w, q := benchSetup(b, n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iterate(w, q)
	}
}

func BenchmarkEach2_Optimized_100(b *testing.B) {
	benchEach(b, 100, func(w *World, q *Query) {
		w.Each2(q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

func BenchmarkEach2_Legacy_100(b *testing.B) {
	benchEach(b, 100, func(w *World, q *Query) {
		each2Legacy(w, q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

func BenchmarkEach2_Optimized_1000(b *testing.B) {
	benchEach(b, 1000, func(w *World, q *Query) {
		w.Each2(q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

func BenchmarkEach2_Legacy_1000(b *testing.B) {
	benchEach(b, 1000, func(w *World, q *Query) {
		each2Legacy(w, q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

func BenchmarkEach2_Optimized_10000(b *testing.B) {
	benchEach(b, 10000, func(w *World, q *Query) {
		w.Each2(q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

func BenchmarkEach2_Legacy_10000(b *testing.B) {
	benchEach(b, 10000, func(w *World, q *Query) {
		each2Legacy(w, q, benchPosC, benchVelC, func(e Entity, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	})
}

// Range vs Execute over the same query: Range walks lazily and allocates
// nothing, Execute pays for the materialized []Entity.
func BenchmarkRange_10000(b *testing.B) {
	benchEach(b, 10000, func(w *World, q *Query) {
		for e := range w.Range(q) {
			p, _ := w.Get(e, benchPosC)
			v, _ := w.Get(e, benchVelC)
			p.X += v.DX
			p.Y += v.DY
		}
	})
}

func BenchmarkExecute_10000(b *testing.B) {
	benchEach(b, 10000, func(w *World, q *Query) {
		for _, e := range w.Execute(q) {
			p, _ := w.Get(e, benchPosC)
			v, _ := w.Get(e, benchVelC)
			p.X += v.DX
			p.Y += v.DY
		}
	})
}

// Sparse-match pattern (barcraft-like): 10% of entities carry the queried
// component. Query cost must be proportional to carriers, not table size.
func BenchmarkRange_SparseMatch_10000(b *testing.B) {
	w := NewWorld()
	for i := 0; i < 10000; i++ {
		e := w.Create()
		if err := w.Set(e, benchPosC, &benchPos{X: float64(i), Y: 1}); err != nil {
			b.Fatal(err)
		}
		if i%10 == 0 {
			if err := w.Set(e, benchVelC, &benchVel{DX: 0.5, DY: -0.5}); err != nil {
				b.Fatal(err)
			}
		}
	}
	q := NewQuery().With(benchVelC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for e := range w.Range(q) {
			p, _ := w.Get(e, benchPosC)
			v, _ := w.Get(e, benchVelC)
			p.X += v.DX
			p.Y += v.DY
		}
	}
}

// Tick is the per-frame boundary: its change-tracking reset must be
// allocation-free in steady state.
func BenchmarkTick_10000(b *testing.B) {
	w, _ := benchSetup(b, 10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Tick()
	}
}
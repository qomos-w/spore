package benchmarks

import (
	"strconv"
	"testing"
)

// closureOracles computes the expected result of each closure/higher-order
// workload in plain Go, so the four engine implementations are validated
// against an oracle rather than merely against each other.

func oracleClosureCounter(n int) int64 {
	// count starts at 0 and inc(1) runs n times.
	return int64(n)
}

func oracleMapFilter(n int) int64 {
	// xs = [0..n-1]; doubled = xs * 2; evens = doubled where x%4==0
	// (i.e. original i even); result is the sum of doubled over evens.
	var s int64
	for i := 0; i < n; i++ {
		if 2*i%4 == 0 {
			s += 2 * int64(i)
		}
	}
	return s
}

func oracleNestedClosure(n int) int64 {
	// Each iteration builds fn = outer(i%10) capturing base=1000 and y=i%10,
	// then s += fn(i%100) = 1000 + (i%10) + (i%100).
	var s int64
	for i := 0; i < n; i++ {
		s += 1000 + int64(i%10) + int64(i%100)
	}
	return s
}

// TestClosureWorkloadCorrectness runs every closure workload on every engine
// and checks the result against the oracle at the workload's own sizes, so a
// plain `go test ./...` short-run validates script correctness without
// benchmarking.
func TestClosureWorkloadCorrectness(t *testing.T) {
	engineNames := []string{engSpore, engTengo, engGoja, engLua}
	for _, w := range workloads {
		var oracle func(int) int64
		switch w.name {
		case "ClosureCounter":
			oracle = oracleClosureCounter
		case "MapFilter":
			oracle = oracleMapFilter
		case "NestedClosure":
			oracle = oracleNestedClosure
		default:
			continue
		}
		for _, eng := range engineNames {
			src, ok := w.src[eng]
			if !ok {
				t.Fatalf("workload %q has no source for engine %q", w.name, eng)
			}
			for _, n := range w.sizes {
				// runOnce scopes the deferred Close (lua needs it per VM)
				// and keeps failure attribution in the subtest.
				t.Run(w.name+"/"+eng+"/N="+strconv.Itoa(n), func(t *testing.T) {
					r, err := mkRunner(eng, src)
					if err != nil {
						t.Fatalf("setup %s/%s: %v", eng, w.name, err)
					}
					if closer, ok := r.(interface{ Close() }); ok {
						defer closer.Close()
					}
					got, err := r.Run(w.fn, n)
					if err != nil {
						t.Fatalf("run %s/%s/N=%d: %v", eng, w.name, n, err)
					}
					if want := oracle(n); got != want {
						t.Fatalf("%s/%s/N=%d: got %d, want %d", eng, w.name, n, got, want)
					}
				})
			}
		}
	}
}
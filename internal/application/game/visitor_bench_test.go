package game

import (
	"fmt"
	"testing"

	"rimworld-go/internal/domain/faction"
)

func visitorBenchGame(b *testing.B, side int, visiting bool) *Game {
	b.Helper()
	g := NewColony(Config{Seed: 142, Width: side, Height: side})
	g.DevAddFactions()
	first := true
	for i := range g.ng.factions {
		f := &g.ng.factions[i]
		if !f.Type.Human() {
			continue
		}
		f.Defeated, f.Relation = false, faction.Hostile
		if first {
			f.Relation, first = faction.Neutral, false
		}
	}
	if first {
		b.Fatal("no human faction for visiting")
	}
	if visiting {
		now := uint64(g.clock.Now())
		if !g.visitorsArrive(now) || !g.caravanArrive(now) {
			b.Fatal("visitor or trader arrival failed")
		}
	}
	g.Step(100)
	return g
}

// BenchmarkVisitorTick isolates the overhead of a live party/caravan on
// otherwise identical colonies. Both baseline and feature branches use
// this test with the same scenario, seed and warm-up.
func BenchmarkVisitorTick(b *testing.B) {
	for _, side := range []int{160, 500} {
		for _, visiting := range []bool{false, true} {
			name := fmt.Sprintf("%d/empty", side)
			if visiting {
				name = fmt.Sprintf("%d/visit-trade", side)
			}
			b.Run(name, func(b *testing.B) {
				g := visitorBenchGame(b, side, visiting)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					g.Step(1)
				}
			})
		}
	}
}

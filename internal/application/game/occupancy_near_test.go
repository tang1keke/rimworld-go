package game

import (
	"fmt"
	"testing"

	"rimworld-go/internal/domain/sim/entity"
)

// nearFromCells recounts occupancy.near (blocker cells) from scratch.
func nearFromCells(o *occupancy) []uint8 { return nearFrom(o, o.Cells) }

// pnearFromCells recounts occupancy.pnear (partner cells) from scratch.
func pnearFromCells(o *occupancy) []uint8 { return nearFrom(o, o.Pfirst) }

func nearFrom(o *occupancy, held []entity.EntityID) []uint8 {
	near := make([]uint8, len(held))
	for i, id := range held {
		if id == 0 {
			continue
		}
		x, y := i%o.W, i/o.W
		for ny := max(y-1, 0); ny <= min(y+1, o.H-1); ny++ {
			for nx := max(x-1, 0); nx <= min(x+1, o.W-1); nx++ {
				near[ny*o.W+nx]++
			}
		}
	}
	return near
}

// checkPartnerLayer: every partner cell's count is its first partner plus
// the extras recorded on it, and a cell has a first partner iff it counts
// any.
func checkPartnerLayer(t *testing.T, o *occupancy, when string) {
	t.Helper()
	extra := map[int]int{}
	for _, s := range o.Pextra {
		extra[s.Idx]++
		if s.ID == o.Pfirst[s.Idx] {
			t.Fatalf("%s: %d both first and extra on cell %d", when, s.ID, s.Idx)
		}
	}
	for i, n := range o.Pcount {
		want := extra[i]
		if o.Pfirst[i] != 0 {
			want++
		}
		if int(n) != want || (n == 0) != (o.Pfirst[i] == 0) {
			t.Fatalf("%s cell %d: count %d, first %d, extras %d", when, i, n, o.Pfirst[i], extra[i])
		}
	}
}

func checkNear(t *testing.T, o *occupancy, when string) {
	t.Helper()
	want, pwant := nearFromCells(o), pnearFromCells(o)
	for i := range want {
		if o.Near[i] != want[i] {
			t.Fatalf("%s cell %d: near %d, recount %d", when, i, o.Near[i], want[i])
		}
		if o.Pnear[i] != pwant[i] {
			t.Fatalf("%s cell %d: pnear %d, recount %d", when, i, o.Pnear[i], pwant[i])
		}
	}
}

// TestOccupancyNearMatchesCells: the held-cells-around counts crowdCost
// skips empty ground with stay equal to a recount through rebuilds, moves
// and stack hand-overs of a busy colony, for blockers and partners alike.
func TestOccupancyNearMatchesCells(t *testing.T) {
	t.Parallel()
	g := NewColony(Config{Seed: 7, Width: 48, Height: 40})
	g.Raid(2)
	for tick := 0; tick < 3000; tick++ {
		g.Step(1)
		o := &g.occ
		if o.Cells == nil || o.Dirty {
			continue
		}
		when := fmt.Sprintf("tick %d", tick)
		checkNear(t, o, when)
		checkPartnerLayer(t, o, when)
	}
}

// TestOccupancyPutStacked: a stacked blocker taking a cell over keeps
// near unchanged; the last one leaving clears it.
func TestOccupancyPutStacked(t *testing.T) {
	t.Parallel()
	g := occTestGame()
	a := wildAt(t, g, 5, 5)
	b := wildAt(t, g, 5, 5) // stacked on a
	rebuild(g)
	o := &g.occ
	i := 5*o.W + 5
	if o.Near[i] != 1 {
		t.Fatalf("near = %d with two creatures on one cell, want 1", o.Near[i])
	}
	o.Move(a, false, 5, 5, 6, 5)
	if o.Cells[i] != b || o.Near[i] != 2 {
		t.Fatalf("after a left: cell holds %d, near %d; want %d, 2", o.Cells[i], o.Near[i], b)
	}
	o.Move(b, false, 5, 5, 4, 5)
	if o.Cells[i] != entity.EntityID(0) {
		t.Fatalf("cell still held by %d", o.Cells[i])
	}
	checkNear(t, o, "after both left")
}

// TestOccupancyPartnerLayer: partners on one cell are counted, the lowest
// ID is first after a rebuild, the next one takes over when the first
// leaves, and they never show as blockers.
func TestOccupancyPartnerLayer(t *testing.T) {
	t.Parallel()
	g := occTestGame()
	a := mustPawn(t, g, 5, 5)
	b := mustPawn(t, g, 5, 5)
	dog := animalAt(t, g, "husky", 5, 5, true)
	rebuild(g)
	o := &g.occ
	i := 5*o.W + 5
	if o.Cells[i] != 0 || o.Near[i] != 0 {
		t.Fatalf("partners indexed as blockers: cell %d near %d", o.Cells[i], o.Near[i])
	}
	if o.Pcount[i] != 3 || o.Pfirst[i] != a || o.Pnear[i] != 1 {
		t.Fatalf("partner cell: count %d first %d pnear %d; want 3, %d, 1", o.Pcount[i], o.Pfirst[i], o.Pnear[i], a)
	}
	if id, ok := g.occupantAt(5, 5); !ok || id != a {
		t.Fatalf("occupantAt = %d, want the first partner %d", id, a)
	}
	o.Move(a, true, 5, 5, 6, 5)
	if o.Pcount[i] != 2 || o.Pfirst[i] != b {
		t.Fatalf("after a left: count %d first %d; want 2, %d", o.Pcount[i], o.Pfirst[i], b)
	}
	o.Move(dog, true, 5, 5, 6, 5) // joins a
	o.Move(b, true, 5, 5, 4, 5)
	if o.Pcount[i] != 0 || o.Pfirst[i] != 0 {
		t.Fatalf("emptied cell: count %d first %d", o.Pcount[i], o.Pfirst[i])
	}
	if j := 5*o.W + 6; o.Pcount[j] != 2 || o.Pfirst[j] != a {
		t.Fatalf("joined cell: count %d first %d", o.Pcount[j], o.Pfirst[j])
	}
	checkNear(t, o, "after moves")
	checkPartnerLayer(t, o, "after moves")
	// a rebuild starts over from the positions (unchanged here)
	rebuild(g)
	if o.Pcount[i] != 3 || o.Pfirst[i] != a {
		t.Fatalf("rebuilt: count %d first %d", o.Pcount[i], o.Pfirst[i])
	}
}

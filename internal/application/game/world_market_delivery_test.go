package game

import (
	"testing"

	"rimworld-go/internal/domain/building/construction"
	"rimworld-go/internal/domain/economy/item"
)

func TestReturningCaravanPreservesBookAndCrateIdentities(t *testing.T) {
	g := NewColony(Config{Seed: 2, Width: 40, Height: 40})
	x, y := g.home()
	samples := []item.Stack{
		{Kind: item.Novel, Qty: 1, Book: 1234567, Quality: 4},
		{Kind: item.MinifiedBuilding, Qty: 1, Building: uint8(construction.Urn), Stuff: item.Wood, TradeBought: true},
		{Kind: item.FertileEgg, Qty: 1, Animal: 3, Tame: true, Hatch: 1200},
	}
	for _, st := range samples {
		g.dropStackOf(st, x, y)
		found := false
		for _, id := range g.itemIDs {
			got := g.stacks.Ref(id)
			if got == nil || got.Kind != st.Kind {
				continue
			}
			found = true
			if got.Book != st.Book || got.Building != st.Building ||
				got.Hatch != st.Hatch || got.Tame != st.Tame ||
				got.Animal != st.Animal || got.Stuff != st.Stuff ||
				got.TradeBought != st.TradeBought || got.Quality != st.Quality {
				t.Fatalf("returning %v lost physical attributes: got %+v, want %+v", st.Kind, *got, st)
			}
		}
		if !found {
			t.Fatalf("returning %v was missing from the map", st.Kind)
		}
	}
}

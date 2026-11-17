package game

import (
	"reflect"
	"testing"

	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/sim/entity"
)

func TestWorldMarketPersistWithoutActiveCaravans(t *testing.T) {
	g := worldGame(t)
	tile := nearDest(t, g)
	cid := visitingMarket(t, g, tile, g.pawnIDs[0])
	before, ok := fleetOf(g).MarketAt(tile)
	if !ok {
		t.Fatal("base market not generated")
	}
	g.Enqueue(command.Command{Kind: command.CaravanReturn, Target: cid})
	g.ApplyPending()
	for i := 0; i < 90 && fleetOf(g).At(entity.EntityID(cid)) != nil; i++ {
		worldForward(g, 2500)
	}
	if len(g.worldSt.caravans) != 0 {
		t.Fatal("caravan was not removed after returning home")
	}
	if g.exportWorld() == nil {
		t.Fatal("visited settlement must keep World save block alive after caravan return")
	}
	restored, _ := saveLoadInfo(t, g)
	restored.worldS().planet, restored.worldS().planetTried = testPlanet().Fork(), true
	after, ok := fleetOf(restored).MarketAt(tile)
	if !ok || !reflect.DeepEqual(after, before) || restored.StateHash() != g.StateHash() {
		t.Fatalf("market changed after caravan return and save/load: %+v -> %+v", before, after)
	}
}

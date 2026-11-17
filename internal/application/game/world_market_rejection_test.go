package game

import (
	"reflect"
	"testing"

	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/sim/entity"
)

func TestWorldMarketRejectsMissingSilverAndExpiredVisit(t *testing.T) {
	g := worldGame(t)
	tile := nearDest(t, g)
	pawn := g.pawnIDs[0]
	cid := visitingMarket(t, g, tile, pawn)
	initial, _ := fleetOf(g).MarketAt(tile)
	buy := trade.EncodeOffer([]trade.Delta{{Kind: item.Cloth, Qty: 1}})
	offer := command.Command{Kind: command.WorldCaravanTrade, Target: cid, Pawn: uint64(pawn), Name: buy}
	g.Enqueue(offer)
	g.ApplyPending()
	after, _ := fleetOf(g).MarketAt(tile)
	if !reflect.DeepEqual(initial, after) || len(travelOf(g).Goods(cid)) != 0 {
		t.Fatal("an unfunded purchase changed physical cargo or market stock")
	}
	travelOf(g).Carry(cid, item.Stack{Kind: item.Silver, Qty: 200})
	beforeExpiry, _ := fleetOf(g).MarketAt(tile)
	cargoBefore := travelOf(g).Goods(cid)
	c := fleetOf(g).At(entity.EntityID(cid))
	c.C.VisitUntil = uint64(g.clock.Now())
	g.Enqueue(offer)
	g.ApplyPending()
	afterExpiry, _ := fleetOf(g).MarketAt(tile)
	if !reflect.DeepEqual(beforeExpiry, afterExpiry) || !reflect.DeepEqual(cargoBefore, travelOf(g).Goods(cid)) {
		t.Fatal("an expired settlement visit committed a trade")
	}
	if len(g.WorldMap().Markets) != 0 {
		t.Fatal("the market view remained open after the visit deadline")
	}
}

func TestWorldMarketGiftsTransferPhysicalSilverAndGoodwill(t *testing.T) {
	g := worldGame(t)
	tile := nearDest(t, g)
	pawn := g.pawnIDs[0]
	cid := visitingMarket(t, g, tile, pawn)
	travelOf(g).Carry(cid, item.Stack{Kind: item.Silver, Qty: 500})
	travelOf(g).Carry(cid, item.Stack{Kind: item.Wood, Qty: 10})
	before, _ := fleetOf(g).MarketAt(tile)
	fa := g.factionOf(g.settlementFaction(tile))
	goodwill := fa.Goodwill
	gift := []trade.Delta{{Kind: item.Silver, Qty: -400}, {Kind: item.Wood, Qty: -10}}
	g.Enqueue(command.Command{Kind: command.WorldCaravanTrade, Pawn: uint64(pawn),
		Target: cid, Name: trade.EncodeOffer(gift), Enabled: true})
	g.ApplyPending()
	after, _ := fleetOf(g).MarketAt(tile)
	if after.Stock.Silver != before.Stock.Silver+400 ||
		marketQty(after.Stock, item.Wood) != marketQty(before.Stock, item.Wood)+10 ||
		cargoQty(travelOf(g).Goods(cid), item.Silver) != 100 ||
		cargoQty(travelOf(g).Goods(cid), item.Wood) != 0 {
		t.Fatalf("gift created or lost silver/goods: before %+v, after %+v, cargo %+v",
			before.Stock, after.Stock, travelOf(g).Goods(cid))
	}
	if want := goodwill + faction.GiftGoodwill(trade.GiftValue(gift)); fa.Goodwill != want {
		t.Fatalf("gift goodwill = %d, want %d", fa.Goodwill, want)
	}
}

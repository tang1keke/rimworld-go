package worldview

import (
	"testing"

	"rimworld-go/internal/adapters/inbound/ebiten/inputframe"
	"rimworld-go/internal/application/game/tabview"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/sim/clock"
	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/world/caravan"
)

func marketFixture(t *testing.T) (*fakeGame, *fakeShell, *View) {
	t.Helper()
	g := colonyGame(t)
	c := &g.world.Caravans[0]
	c.State = caravan.Visiting.Label()
	g.world.Markets = []tabview.MarketView{{
		CaravanID: c.ID, Tile: c.Tile, Negotiator: 17,
		RestockAt: 4 * clock.TicksPerDay, Epoch: 2,
		CaravanSilver: 100, TraderSilver: 200,
		Rows: []tabview.MarketRow{
			{Kind: item.Wood.String(), Label: "Wood", Category: "Materials",
				CargoQty: 3, StockQty: 10, BuyC: 250, SellC: 100, WillBuy: true, WillSell: true},
			{Kind: item.Cloth.String(), Label: "Cloth", Category: "Materials",
				StockQty: 5, BuyC: 200, SellC: 80, WillBuy: true},
		},
	}}
	sh, v := newFake(g)
	v.Show("caravan:7")
	frame(sh, v, -1, -1, inputframe.Raw{})
	return g, sh, v
}

func declareMarket(sh *fakeShell, v *View) {
	frame(sh, v, -1, -1, inputframe.Raw{})
	sh.kit.RunModals(nil)
}

func clickMarket(t *testing.T, sh *fakeShell, v *View, id string) {
	t.Helper()
	declareMarket(sh, v)
	w, ok := sh.kit.Find(id)
	if !ok || w.Disabled {
		t.Fatalf("market widget %q unavailable: %+v", id, w)
	}
	w.Click()
	declareMarket(sh, v)
}

func TestWorldMarketGizmoOpensModalAndEmitsTypedOffer(t *testing.T) {
	g, sh, v := marketFixture(t)
	var tradeGizmo *gizmo
	for _, o := range v.objs {
		if o.Kind != KindCaravan {
			continue
		}
		_, _, actions := (inspector{v}).inspectCaravan(&o)
		for i := range actions {
			if actions[i].name == "trade" {
				tradeGizmo = &actions[i]
			}
		}
	}
	if tradeGizmo == nil || tradeGizmo.why != "" {
		t.Fatalf("visiting caravan lacks a trade action: %+v", tradeGizmo)
	}
	tradeGizmo.run()
	if sh.kit.Top() != MarketModalID {
		t.Fatal("trade modal did not open")
	}
	clickMarket(t, sh, v, "world.market.cloth.take.2")
	clickMarket(t, sh, v, "world.market.wood.give.2")
	clickMarket(t, sh, v, "modal."+MarketModalID+".accept")
	want := trade.EncodeOffer([]trade.Delta{{Kind: item.Cloth, Qty: 1}, {Kind: item.Wood, Qty: -1}})
	if len(sh.cmds) != 1 || sh.cmds[0].Kind != command.WorldCaravanTrade ||
		sh.cmds[0].Target != 7 || sh.cmds[0].Pawn != 17 || sh.cmds[0].Name != want {
		t.Fatalf("trade command = %+v, expected encoded %q", sh.cmds, want)
	}
	// The same tick must not submit a second copy of the transaction.
	(marketUI{v}).submitMarket(v.market)
	if len(sh.cmds) != 1 {
		t.Fatalf("duplicate trade submitted on the same tick: %+v", sh.cmds)
	}
	// The server's next snapshot invalidates stale quantities and drafts.
	g.world.Markets[0].Rows[1].StockQty--
	sh.tick = clock.Tick(15)
	declareMarket(sh, v)
	if len(v.market.net) != 0 || v.market.sent {
		t.Fatalf("stale offer retained after stock update: %+v", v.market)
	}
	g.world.Markets = nil
	sh.tick = clock.Tick(30)
	declareMarket(sh, v)
	if v.market != nil || sh.kit.Top() == MarketModalID {
		t.Fatal("ended settlement visit left the market modal open")
	}
}

func TestWorldMarketGiftAndInvalidNegotiator(t *testing.T) {
	g, sh, v := marketFixture(t)
	m := g.world.Markets[0]
	d := newMarketDraft(m)
	d.set(d.rows[0], 2) // buying cloth before gift mode
	d.setGift(true)
	if len(d.net) != 0 {
		t.Fatalf("gift mode kept purchases: %+v", d.net)
	}
	d.set(d.rows[1], -2)
	if d.reason() != "" || d.giftValue() != 2*item.Wood.Value() {
		t.Fatalf("gift draft invalid: %+v", d)
	}
	(marketUI{v}).openMarket(m.CaravanID)
	v.market.setGift(true)
	v.market.set(v.market.rows[1], -2)
	(marketUI{v}).submitMarket(v.market)
	if len(sh.cmds) != 1 || !sh.cmds[0].Enabled || sh.cmds[0].Name != trade.EncodeOffer([]trade.Delta{{Kind: item.Wood, Qty: -2}}) {
		t.Fatalf("gift command = %+v", sh.cmds)
	}
	v.Close()
	if sh.kit.Top() == MarketModalID {
		t.Fatal("closing world view left the trade modal open")
	}
	g.world.Markets[0].Negotiator = 0
	sh.tick = clock.Tick(15)
	v.Show("caravan:7")
	declareMarket(sh, v)
	(marketUI{v}).openMarket(7)
	if sh.kit.Top() == MarketModalID {
		t.Fatal("invalid negotiator opened a trade modal")
	}
}

func TestWorldMarketDraftAffordabilityAndBounds(t *testing.T) {
	m := tabview.MarketView{
		Negotiator: 4, CaravanSilver: 1, TraderSilver: 2,
		Rows: []tabview.MarketRow{{
			Kind: item.Steel.String(), CargoQty: 4, StockQty: 5,
			BuyC: 250, SellC: 100, WillBuy: true, WillSell: true,
		}},
	}
	d := newMarketDraft(m)
	d.set(d.rows[0], 5)
	if d.reason() != "The caravan lacks silver." {
		t.Fatalf("unaffordable purchase passed: %+v", d)
	}
	d.set(d.rows[0], -4)
	if d.reason() != "The settlement cannot pay." {
		t.Fatalf("trader insolvent sale passed: %+v", d)
	}
	d.setGift(true)
	if d.reason() != "" || d.net[item.Steel.String()] != -4 {
		t.Fatalf("gift should allow donation of held steel: %+v", d)
	}
	d.set(d.rows[0], -100)
	if d.net[item.Steel.String()] != -4 {
		t.Fatalf("sell exceeded physically carried goods: %+v", d)
	}
}

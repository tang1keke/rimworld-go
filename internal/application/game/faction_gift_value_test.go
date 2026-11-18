package game

import (
	"testing"

	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/world/caravan"
)

func TestFactionGiftPhysicalCaravanItemSellingValue(t *testing.T) {
	t.Parallel()
	g, c := caravanGame(t)
	neg := openCaravanTrade(t, g, c)
	tradeYard(t, g, neg, 700, 80)
	f := g.factionByID(c.Faction)
	if f == nil {
		t.Fatal("visiting trader has no faction")
	}
	f.Goodwill = 0
	faction.Update(f)
	offer := []trade.Delta{{Kind: item.Components, Qty: -20}}
	terms := g.termsFor(neg, f)
	value := trade.GiftValue(offer, terms.Social)
	if value >= 20*item.Components.Value() {
		t.Fatalf("20 gifted components should use selling value, got %d", value)
	}
	want := faction.GiftGoodwill(*f, value)
	before := totalOf(g, item.Components)
	traderBefore := stockQty(c.Stock, item.Components)
	g.Enqueue(command.Command{Kind: command.ProposeTrade, Name: trade.EncodeOffer(offer), Enabled: true})
	g.ApplyPending()
	view, ok := g.TradeView()
	if !ok || view.Reason != "" {
		t.Fatalf("goods gift refused: %+v", view)
	}
	if f.Goodwill != want ||
		totalOf(g, item.Components) != before-20 ||
		stockQty(c.Stock, item.Components) != traderBefore+20 {
		t.Fatalf("caravan gift goodwill=%d want=%d goods=%d trader=%d",
			f.Goodwill, want, totalOf(g, item.Components), stockQty(c.Stock, item.Components))
	}
}

func TestFactionGiftTransportPodItemSellingValue(t *testing.T) {
	t.Parallel()
	g := worldGame(t)
	dest := nearDest(t, g)
	x, y := ax9Free(t, g)
	pod, _ := ax9PodRig(t, g, x+1, y, 150)
	ax9Stock(g, pod, item.Lung, 1)
	f := g.factionOf(g.settlementFaction(dest))
	if f == nil {
		t.Fatal("no faction at gift destination")
	}
	before := f.Goodwill
	want := faction.GiftGoodwill(*f, 612) // wiki: 1000-market-value lung by pod
	if want <= 0 {
		t.Fatal("fixture cannot benefit from the published gift")
	}
	g.Enqueue(command.Command{Kind: command.LaunchPods, Target: uint64(pod), Count: dest, Mode: command.PodArriveGift})
	g.Step(2*caravan.PodTicksPerTile + 260)
	if got := g.factionOf(g.settlementFaction(dest)).Goodwill - before; got != want {
		t.Fatalf("pod delivery goodwill +%d, want +%d for 612 silver of gift value", got, want)
	}
}

package game

import (
	"reflect"
	"slices"
	"testing"

	"rimworld-go/internal/application/game/visit"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/pawn/health"
	"rimworld-go/internal/domain/sim/clock"
	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/sim/entity"
)

func cargoBalance(t *testing.T, c *traderCaravan) {
	t.Helper()
	got := make(map[trade.Goods]int)
	silver := 0
	for _, hold := range c.Cargo {
		silver += hold.Silver
		for _, ln := range hold.Lines {
			got[ln.Goods()] += ln.Qty
		}
	}
	want := make(map[trade.Goods]int)
	for _, ln := range c.Stock.Lines {
		want[ln.Goods()] += ln.Qty
	}
	if !reflect.DeepEqual(got, want) || silver != c.Stock.Silver {
		t.Fatalf("cargo differs from sale stock: cargo %v, %d silver; stock %v, %d silver", got, silver, want, c.Stock.Silver)
	}
}

func goodsOwner(cargo []visit.Cargo, key trade.Goods) entity.EntityID {
	for _, hold := range cargo {
		for _, ln := range hold.Lines {
			if ln.Goods() == key && ln.Qty > 0 {
				return hold.Owner
			}
		}
	}
	return 0
}

func TestReviewCargoTradeDoesNotReassignRemainingGoods(t *testing.T) {
	g, c := caravanGame(t)
	neg := openCaravanTrade(t, g, c)
	tradeYard(t, g, neg, 300000, 10)
	if len(c.Stock.Lines) < 3 || len(c.Cargo) < 2 {
		t.Fatal("need three goods and multiple physical carriers")
	}
	a, b, d := c.Stock.Lines[0], c.Stock.Lines[1], c.Stock.Lines[2]
	bOwner, dOwner := goodsOwner(c.Cargo, b.Goods()), goodsOwner(c.Cargo, d.Goods())
	cargoBalance(t, c)
	g.Enqueue(command.Command{Kind: command.ProposeTrade, Name: trade.EncodeOffer([]trade.Delta{{
		Kind: a.Kind, Building: a.Building, Qty: a.Qty,
	}})})
	g.ApplyPending()
	if v, ok := g.TradeView(); !ok || v.Reason != "" {
		t.Fatalf("trade refused: %+v", v)
	}
	cargoBalance(t, c)
	if goodsOwner(c.Cargo, b.Goods()) != bOwner || goodsOwner(c.Cargo, d.Goods()) != dOwner {
		t.Fatalf("unsold goods changed carriers: B %d -> %d, C %d -> %d",
			bOwner, goodsOwner(c.Cargo, b.Goods()), dOwner, goodsOwner(c.Cargo, d.Goods()))
	}
}

func TestReviewGroupAccumulatesDepartedLosses(t *testing.T) {
	g := visitorGame(t)
	now := uint64(g.clock.Now())
	ref := g.visitingFactions()[0]
	f := g.factionOf(ref)
	used := make([][2]int, 0, 10)
	members := make([]visit.Member, 0, 10)
	for i := 0; i < 10; i++ {
		x, y, ok := g.insetLandNear(5+i, 5+i, used)
		if !ok {
			t.Fatal("no independent visitor entry")
		}
		used = append(used, [2]int{x, y})
		id := visitsOf(g).Spawn(ref, f, x, y, now)
		role := visit.RoleGuard
		if i == 0 {
			role = visit.RoleLeader
		}
		members = append(members, visit.Member{ID: id, Role: role})
	}
	gid := visitsOf(g).NewGroup(visit.KindCaravan, ref, members, now, now+clock.TicksPerDay)
	first, second := members[8].ID, members[7].ID
	g.die(first, health.CauseInjury)
	g.Step(2)
	group := reviewGroup(t, g, gid)
	if group.Phase == visit.PhaseLeaving {
		t.Fatalf("one of ten losses triggered early retreat: %+v", group)
	}
	if m, ok := slices.BinarySearchFunc(group.Members, first, func(m visit.Member, id entity.EntityID) int {
		if m.ID < id {
			return -1
		}
		if m.ID > id {
			return 1
		}
		return 0
	}); !ok || !group.Members[m].Lost || !group.Members[m].Departed {
		t.Fatalf("first loss was not saved: %+v", group.Members)
	}
	h, _ := saveLoadInfo(t, g)
	if g.StateHash() != h.StateHash() {
		t.Fatal("first departure did not persist")
	}
	for _, game := range []*Game{g, h} {
		game.die(second, health.CauseInjury)
		game.Step(2)
		gr := reviewGroup(t, game, gid)
		if gr.Phase != visit.PhaseLeaving || gr.Reason != visit.LeaveLoss {
			t.Fatalf("two staggered losses should trigger 20%% retreat: %+v", gr)
		}
	}
	if g.StateHash() != h.StateHash() {
		t.Fatal("saved loss replay diverged")
	}
}

func reviewGroup(t *testing.T, g *Game, id uint64) visit.Group {
	t.Helper()
	for _, gr := range g.worldSt.visitGroups {
		if gr.ID == id {
			return gr
		}
	}
	t.Fatalf("group %d was removed", id)
	return visit.Group{}
}

func TestReviewTraderCargoLossHasNoDuplicateDrops(t *testing.T) {
	g, c := caravanGame(t)
	pack := caravanPack(g, c)
	if pack == 0 {
		t.Fatal("missing pack")
	}
	cargoBalance(t, c)
	g.removeAnimal(pack)
	cargoBalance(t, c)
	before := len(g.itemIDs)
	packCargoLost(g, pack)
	if len(g.itemIDs) != before {
		t.Fatal("repeat death dropped the same cargo twice")
	}
	g.Step(2)
	if gr := reviewGroup(t, g, c.GroupID); gr.Reason != visit.LeaveLoss {
		t.Fatalf("pack loss did not start retreat: %+v", gr)
	}
}

func forbiddenCaravanGoods(g *Game) map[trade.Goods]int {
	out := make(map[trade.Goods]int)
	for _, id := range g.itemIDs {
		st := g.stacks.Ref(id)
		if st == nil || !st.Forbidden || st.Carrier != 0 || st.Kind.IsCorpse() {
			continue
		}
		out[trade.Goods{Kind: st.Kind, Building: st.Building}] += st.Qty
	}
	return out
}

func cargoOf(c *traderCaravan, id entity.EntityID) visit.Cargo {
	for _, hold := range c.Cargo {
		if hold.Owner == id {
			return hold
		}
	}
	return visit.Cargo{}
}

func checkDroppedCargo(t *testing.T, before, after map[trade.Goods]int, held visit.Cargo) {
	t.Helper()
	expected := map[trade.Goods]int{}
	for _, ln := range held.Lines {
		expected[ln.Goods()] += ln.Qty
	}
	if held.Silver > 0 {
		expected[trade.Goods{Kind: item.Silver}] += held.Silver
	}
	for k, n := range after {
		if n != before[k] {
			if n-before[k] != expected[k] {
				t.Fatalf("%s: dropped %d, expected %d", k.Name(), n-before[k], expected[k])
			}
			delete(expected, k)
		}
	}
	for k, n := range expected {
		if n != 0 {
			t.Fatalf("%s: missing %d units after loss", k.Name(), n)
		}
	}
}

func TestReviewTwoPartialTradesPackDeathAndReload(t *testing.T) {
	g, c := caravanGame(t)
	neg := openCaravanTrade(t, g, c)
	tradeYard(t, g, neg, 300000, 10)
	pack := caravanPack(g, c)
	if pack == 0 {
		t.Fatal("missing caravan's real pack animal")
	}
	var target trade.Line
	for _, ln := range cargoOf(c, pack).Lines {
		if ln.Kind != item.MinifiedBuilding && ln.Qty >= 3 {
			target = ln
			break
		}
	}
	if target.Qty < 3 {
		t.Fatal("pack has no item available for two partial purchases")
	}
	others := make(map[trade.Goods]entity.EntityID)
	for _, ln := range c.Stock.Lines {
		if ln.Goods() != target.Goods() {
			others[ln.Goods()] = goodsOwner(c.Cargo, ln.Goods())
		}
	}
	for i := 0; i < 2; i++ {
		offer := []trade.Delta{{Kind: target.Kind, Building: target.Building, Qty: 1}}
		g.Enqueue(command.Command{Kind: command.ProposeTrade, Name: trade.EncodeOffer(offer)})
		g.ApplyPending()
		if view, ok := g.TradeView(); !ok || view.Reason != "" {
			t.Fatalf("partial purchase %d refused: %+v", i, view)
		}
		cargoBalance(t, c)
		for k, owner := range others {
			if goodsOwner(c.Cargo, k) != owner {
				t.Fatalf("partial purchase %d moved unrelated %s from %d to %d",
					i, k.Name(), owner, goodsOwner(c.Cargo, k))
			}
		}
	}
	g.Enqueue(command.Command{Kind: command.CloseTrade})
	g.ApplyPending()
	g.Step(1)
	h, _ := saveLoadInfo(t, g)
	if g.StateHash() != h.StateHash() {
		t.Fatal("post-trade save changed authoritative state")
	}
	for _, sim := range []*Game{g, h} {
		caravan := caravanByTrader(&sim.trade, c.Trader)
		if caravan == nil {
			t.Fatal("post-trade caravan not restored")
		}
		cargoBalance(t, caravan)
		held := cargoOf(caravan, pack)
		before := forbiddenCaravanGoods(sim)
		sim.killAnimal(pack, 0)
		after := forbiddenCaravanGoods(sim)
		checkDroppedCargo(t, before, after, held)
		cargoBalance(t, caravan)
		sim.killAnimal(pack, 0)
		packCargoLost(sim, pack)
		if !reflect.DeepEqual(after, forbiddenCaravanGoods(sim)) {
			t.Fatal("repeated loss signal dropped cargo twice")
		}
		sim.Step(2)
		if gr := reviewGroup(t, sim, caravan.GroupID); gr.Phase != visit.PhaseLeaving || gr.Reason != visit.LeaveLoss {
			t.Fatalf("real pack loss did not trigger caravan departure: %+v", gr)
		}
	}
	if g.StateHash() != h.StateHash() {
		t.Fatal("pack loss diverged across save/load replay")
	}
}

func TestReviewTraderLossExactlyOnceAfterPartialTrade(t *testing.T) {
	for _, how := range []string{"killed", "captured"} {
		t.Run(how, func(t *testing.T) {
			g, c := caravanGame(t)
			held := cargoOf(c, c.Trader)
			before := forbiddenCaravanGoods(g)
			goodwill := g.factionByID(c.Faction).Goodwill
			if how == "killed" {
				g.die(c.Trader, health.CauseInjury)
			} else {
				g.makePrisoner(c.Trader, g.pawnIDs[0])
			}
			g.Step(1)
			checkDroppedCargo(t, before, forbiddenCaravanGoods(g), held)
			cargoBalance(t, c)
			if g.factionByID(c.Faction).Goodwill != goodwill-traderLostGoodwill {
				t.Fatalf("trader loss goodwill mismatch %s", how)
			}
			after := forbiddenCaravanGoods(g)
			g.traderVisitorGone(c.Trader)
			if !reflect.DeepEqual(after, forbiddenCaravanGoods(g)) ||
				g.factionByID(c.Faction).Goodwill != goodwill-traderLostGoodwill {
				t.Fatal("duplicate trader loss dropped inventory or altered goodwill")
			}
		})
	}
}

func TestReviewPeacefulDepartureKeepsSaleStockOffTheGround(t *testing.T) {
	g, c := caravanGame(t)
	initial := cloneCaravan(c)
	forbidden := forbiddenCaravanGoods(g)
	pack := caravanPack(g, c)
	if pack == 0 {
		t.Fatal("missing pack")
	}
	visitsOf(g).LeaveGroup(c.GroupID, visit.LeaveScheduled, uint64(g.clock.Now()))
	for i := 0; i < 16000; i++ {
		if !g.visitor(c.Trader) && !g.animals.Has(pack) {
			break
		}
		g.Step(1)
	}
	g.Step(2)
	if g.visitor(c.Trader) || g.animals.Has(pack) || caravanByGroup(&g.trade, c.GroupID) != nil {
		t.Fatal("caravan did not finish leaving")
	}
	if !reflect.DeepEqual(initial.Stock, c.Stock) || !reflect.DeepEqual(initial.Cargo, c.Cargo) {
		t.Fatal("peaceful departure consumed goods as if carriers had died")
	}
	if !reflect.DeepEqual(forbidden, forbiddenCaravanGoods(g)) {
		t.Fatal("peaceful departure dropped forbidden trade goods")
	}
}

func TestReviewSameTickAndSplitLossesAgree(t *testing.T) {
	makeTen := func(t *testing.T) (*Game, uint64, []entity.EntityID) {
		t.Helper()
		g := visitorGame(t)
		now := uint64(g.clock.Now())
		ref := g.visitingFactions()[0]
		f := g.factionOf(ref)
		var members []visit.Member
		var ids []entity.EntityID
		var used [][2]int
		for i := 0; i < 10; i++ {
			x, y, ok := g.insetLandNear(5+i, 5+i, used)
			if !ok {
				t.Fatal("could not place visitor")
			}
			used = append(used, [2]int{x, y})
			id := visitsOf(g).Spawn(ref, f, x, y, now)
			ids = append(ids, id)
			members = append(members, visit.Member{ID: id, Role: visit.RoleGuard})
		}
		return g, visitsOf(g).NewGroup(visit.KindCaravan, ref, members, now, now+clock.TicksPerDay), ids
	}
	split, id, ids := makeTen(t)
	split.die(ids[8], health.CauseInjury)
	split.Step(2)
	split.die(ids[9], health.CauseInjury)
	split.Step(2)
	grSplit := reviewGroup(t, split, id)
	same, sameID, sameIDs := makeTen(t)
	same.Step(2)
	same.die(sameIDs[8], health.CauseInjury)
	same.die(sameIDs[9], health.CauseInjury)
	same.Step(2)
	grSame := reviewGroup(t, same, sameID)
	if grSplit.Phase != visit.PhaseLeaving || grSame.Phase != visit.PhaseLeaving ||
		grSplit.Reason != visit.LeaveLoss || grSame.Reason != visit.LeaveLoss ||
		!slices.Equal(grSplit.Members, grSame.Members) {
		t.Fatalf("loss spacing changed the outcome: split %+v, simultaneous %+v", grSplit, grSame)
	}

	normal, normalID, normalIDs := makeTen(t)
	normal.setWhere(normalIDs[6], whereOffMap)
	normal.Step(2)
	gr := reviewGroup(t, normal, normalID)
	if !gr.Members[6].Departed || gr.Members[6].Lost || gr.Phase == visit.PhaseLeaving {
		t.Fatalf("normal departure was classified as a casualty: %+v", gr)
	}
	visitsOf(normal).LeaveGroup(normalID, visit.LeaveDismissed, uint64(normal.clock.Now()))
	normal.die(normalIDs[8], health.CauseInjury)
	normal.die(normalIDs[9], health.CauseInjury)
	normal.Step(2)
	if gr = reviewGroup(t, normal, normalID); gr.Reason != visit.LeaveDismissed {
		t.Fatalf("late casualties overwrote the initial leave reason: %+v", gr)
	}
}

func TestReviewGroupGiftClosureIsOnceOnly(t *testing.T) {
	g, c := caravanGame(t)
	for i := range g.worldSt.visitGroups {
		if gr := &g.worldSt.visitGroups[i]; gr.ID == c.GroupID {
			gr.Phase, gr.Reason = visit.PhaseLeaving, visit.LeaveScheduled
			for j := range gr.Members {
				gr.Members[j].Departed = true
			}
		}
	}
	visitsOf(g).TickGroups(uint64(g.clock.Now()))
	if _, ok := visitsOf(g).GroupOf(c.Trader); ok {
		t.Fatal("completed group was not removed")
	}
	if caravanByGroup(&g.trade, c.GroupID) != nil {
		t.Fatal("completed caravan was not removed")
	}
	first := g.StateHash()
	visitsOf(g).TickGroups(uint64(g.clock.Now()))
	if g.StateHash() != first {
		t.Fatal("completed caravan repeated departure or parting gift")
	}
}

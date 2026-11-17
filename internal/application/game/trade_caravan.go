package game

import (
	"fmt"
	"slices"

	"rimworld-go/internal/application/game/letters"
	"rimworld-go/internal/application/game/visit"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/pawn/health"
	"rimworld-go/internal/domain/sim/clock"
	"rimworld-go/internal/domain/sim/entity"
)

// Trader caravans (PLAY-024, agent S2): incident.TraderCaravan (or a
// caravan asked for over the radio) brings a visitor group from a
// non-hostile faction: a trader with the goods and one to three guards.
// They are ordinary visitors (visitors.go) that stroll within
// traderCaravanRange of home() for traderCaravanStay, then walk out.
// Killing or capturing the trader drops its goods (silver too) forbidden
// where it fell and costs the faction traderLostGoodwill. Faction, kind
// and stock draws come from streamTrade; the edge and the strolls reuse
// the visitors' world stream.

// Multiple independent caravans can visit beside ordinary visitors.
func (g *Game) caravanFits(now uint64) bool {
	return len(g.pawnIDs) > 0 && len(g.visitingFactions()) > 0 && len(caravansOf(&g.trade)) < 4
}

// caravanArrive sends a trader caravan in; false when it could not come.
func (g *Game) caravanArrive(now uint64) bool {
	facs := g.visitingFactions()
	if len(facs) == 0 {
		return false
	}
	return g.traderCaravanFrom(facs[g.stream(streamTrade).IntN(len(facs))], now, false)
}

// traderCaravanFrom brings faction ref's (1 + index) trader caravan in
// from a map edge; requested: it was asked for over the radio.
func (g *Game) traderCaravanFrom(ref int, now uint64, requested bool) bool {
	return traderCaravanFromKind(g, ref, now, requested, false)
}

func smallTraderArrive(g *Game, now uint64) bool {
	facs := g.visitingFactions()
	if len(facs) == 0 {
		return false
	}
	ref := facs[g.stream(streamTrade).IntN(len(facs))]
	return traderCaravanFromKind(g, ref, now, false, true)
}

func traderCaravanFromKind(g *Game, ref int, now uint64, requested, small bool) bool {
	f := g.factionOf(ref)
	if f == nil || len(caravansOf(&g.trade)) >= 4 {
		return false
	}
	duration := uint64(traderCaravanStay)
	if small {
		duration = clock.TicksPerDay / 2
	}
	r := g.stream(streamTrade)
	n := 2 + r.IntN(3)
	kind := trade.BulkHauler
	if small {
		n = 1 + r.IntN(2)
	} else if r.IntN(2) == 1 {
		kind = trade.Quartermaster
	}
	group, ok := visitsOf(g).ArriveCaravan(ref, f, now, duration, small, n, nil)
	if !ok {
		return false
	}
	stock := trade.Generate(kind, faction.DefOf(f.Type).Tech, r)
	if small {
		stock.Silver = min(stock.Silver, 500)
		for i := range stock.Lines {
			stock.Lines[i].Qty = min(stock.Lines[i].Qty, 3)
		}
	}
	c := &traderCaravan{GroupID: group.GroupID, Trader: group.Trader, Members: group.Members,
		Faction: f.ID, Kind: kind, Stock: stock, LeaveAt: now + duration}
	if g.trade.caravan == nil {
		g.trade.caravan = c
	} else {
		g.trade.others = append(g.trade.others, c)
	}
	c.Cargo = visit.AssignCargo(c.Stock, caravanCargoOwners(g, c))
	ids := group.Members
	why := "has come"
	if requested {
		why = "has come as you asked"
	}
	if small {
		letterf(g, letters.Info, "Visitors with goods", ids[0], "%s of %s has come to visit and brought a few things to trade.", g.name(ids[0]), f.Name)
	} else {
		letterf(g, letters.Info, "Trader caravan", ids[0], "%s, a %s of %s, %s to trade, with %d guards. They will camp near the colony for most of a day. Right-click the trader with a colonist selected to trade.",
			g.name(ids[0]), lower(trade.DefOf(kind).Label), f.Name, why, len(ids)-1)
	}
	return true
}

// lower is s with its first letter in lower case (labels inside a
// sentence).
func lower(s string) string {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return s
	}
	return string(s[0]+'a'-'A') + s[1:]
}

// traderOpen reports trader id is ready to trade now: the caravan's
// trader, conscious, on the map and not yet leaving.
func (g *Game) traderOpen(id entity.EntityID) bool {
	c := caravanByTrader(&g.trade, id)
	if c == nil || c.Turned || !g.visitor(id) {
		return false
	}
	v, _ := g.vital.Get(id)
	return v.Status == health.Alive
}

// traderAt is the trader standing at x, y (0: none). One that turned
// (MAP-18) still counts, so the order menu says why it won't trade.
func (g *Game) traderAt(x, y int) entity.EntityID {
	for _, c := range caravansOf(&g.trade) {
		if !c.Turned && !g.visitor(c.Trader) {
			continue
		}
		if p, ok := g.positions.Get(c.Trader); ok && p.X == x && p.Y == y {
			return c.Trader
		}
	}
	return 0
}

// traderHeld reports visitor id is the trader while a colonist walks up
// to it or trades with it: it stays put (no new stroll).
func (g *Game) traderHeld(id entity.EntityID) bool {
	c := caravanByTrader(&g.trade, id)
	if c == nil {
		return false
	}
	if s := g.trade.session; s != nil && s.trader == id {
		return true
	}
	_, taken := g.reserve.EntityOwner(id)
	return taken
}

// caravanStrollRange is how far from home() visitor id strolls.
func (g *Game) caravanStrollRange(id entity.EntityID) int {
	for _, c := range caravansOf(&g.trade) {
		if _, ok := slices.BinarySearch(c.Members, id); ok {
			return traderCaravanRange
		}
	}
	return visitorRange
}

// traderVisitorGone: visitor id is no longer a visitor on the map
// (visitorsTick). A trader that walked off takes the caravan with it; one
// killed or taken prisoner drops its goods and angers its faction.
func (g *Game) traderVisitorGone(id entity.EntityID) {
	c := caravanByMember(&g.trade, id)
	if c == nil {
		return
	}
	if i, ok := slices.BinarySearch(c.Members, id); ok {
		c.Members = slices.Delete(c.Members, i, i+1)
	}
	if id != c.Trader {
		return
	}
	retireCaravan(&g.trade, c)
	if g.trade.session != nil && g.trade.session.trader == id {
		g.trade.session = nil
	}
	if p := g.personOf(id); p != nil && p.Where == whereOffMap {
		return // left with its goods
	}
	g.traderLost(c)
}

// traderLost drops caravan c's goods forbidden where its trader fell and
// costs the faction traderLostGoodwill, unless the caravan had turned on the
// colony (MAP-18).
func (g *Game) traderLost(c *traderCaravan) {
	x, y := g.home()
	if p, ok := g.positions.Get(c.Trader); ok {
		x, y = p.X, p.Y
	}
	remaining, lines, silver := visit.LoseCargo(&c.Stock, c.Cargo, c.Trader)
	c.Cargo = remaining
	if len(c.Cargo) == 0 && len(lines) == 0 && silver == 0 {
		lines, silver = c.Stock.Lines, c.Stock.Silver // old saves without a cargo manifest
	}
	for _, l := range lines {
		g.dropGoods(l, x, y, true)
	}
	if silver > 0 {
		g.dropGoods(trade.Line{Kind: item.Silver, Qty: silver}, x, y, true)
	}
	f := g.factionByID(c.Faction)
	if f == nil {
		return
	}
	if c.Turned {
		letterf(g, letters.Info, "Trader lost", c.Trader, "%s, the trader of %s, won't reach home. Its goods lie where it fell.", g.name(c.Trader), f.Name)
		return
	}
	now := uint64(g.clock.Now())
	faction.Adjust(f, -traderLostGoodwill, fmt.Sprintf("Day %d: trader killed or taken (%d goodwill)", g.relDay(now)+1, -traderLostGoodwill))
	letterf(g, letters.Caution, "Trader lost", c.Trader, "%s, the trader of %s, is lost to them. Its goods lie where it fell. %s will not forget it (%d goodwill).",
		g.name(c.Trader), f.Name, f.Name, -traderLostGoodwill)
}

// requestedCaravanDue sends the first requested caravan whose time has
// come; one that can't come yet (visitors or another caravan here) tries
// again later, one whose faction turned hostile or fell never comes.
func (g *Game) requestedCaravanDue(now uint64) {
	t := &g.trade
	req := t.requests[0]
	t.requests = slices.Delete(t.requests, 0, 1)
	f := g.factionByID(req.Faction)
	if f == nil || f.Defeated || f.Relation == faction.Hostile {
		return
	}
	busy := len(caravansOf(t)) >= 4
	if !busy && g.traderCaravanFrom(g.factionRef(req.Faction), now, true) {
		return
	}
	g.addRequest(caravanRequest{Faction: req.Faction, At: now + requestRetry})
}

// addRequest queues r in arrival order (ties: lower faction id first).
func (g *Game) addRequest(r caravanRequest) {
	rs := g.trade.requests
	i, _ := slices.BinarySearchFunc(rs, r, func(a, b caravanRequest) int {
		if a.At != b.At {
			if a.At < b.At {
				return -1
			}
			return 1
		}
		return a.Faction - b.Faction
	})
	g.trade.requests = slices.Insert(rs, i, r)
}

// requestPending reports a caravan from faction id is on its way.
func (g *Game) requestPending(id int) bool {
	for _, r := range g.trade.requests {
		if r.Faction == id {
			return true
		}
	}
	return false
}

// caravansOf combines active and parked caravans in their arrival order.
func caravansOf(t *tradeState) []*traderCaravan { return visit.Caravans(t.caravan, t.others) }

func caravanByTrader(t *tradeState, id entity.EntityID) *traderCaravan {
	return visit.FindCaravan(t.caravan, t.others, func(c *traderCaravan) bool { return c.Trader == id })
}
func caravanByMember(t *tradeState, id entity.EntityID) *traderCaravan {
	return visit.FindCaravan(t.caravan, t.others, func(c *traderCaravan) bool {
		_, found := slices.BinarySearch(c.Members, id)
		return found
	})
}
func caravanByGroup(t *tradeState, group uint64) *traderCaravan {
	return visit.FindCaravan(t.caravan, t.others, func(c *traderCaravan) bool { return c.GroupID == group })
}
func removeCaravan(t *tradeState, c *traderCaravan) {
	visit.RemoveCaravan(&t.caravan, &t.others, c)
}
func retireCaravan(t *tradeState, c *traderCaravan) {
	visit.RetireCaravan(&t.caravan, &t.others, c, func(next *traderCaravan) bool {
		return next.Trader != c.Trader && next.LeaveAt != 0
	})
}

// Every cargo owner is an actual living group member.
func caravanCargoOwners(g *Game, c *traderCaravan) []entity.EntityID {
	out := []entity.EntityID{c.Trader}
	if gr, ok := visitsOf(g).GroupOf(c.Trader); ok {
		for _, m := range gr.Members {
			if m.Role == visit.RolePackAnimal && g.animals.Has(m.ID) {
				out = append(out, m.ID)
			}
		}
	}
	return out
}

// packCargoLost changes sale stock once when a real animal is killed.
// Walking off with its own goods is an ordinary, non-loss departure.
func packCargoLost(g *Game, id entity.EntityID) {
	gr, ok := visitsOf(g).GroupOf(id)
	if !ok || gr.Kind != visit.KindCaravan {
		return
	}
	for _, m := range gr.Members {
		if m.ID == id && m.Departed {
			return
		}
	}
	c := caravanByGroup(&g.trade, gr.ID)
	if c == nil {
		return
	}
	remaining, lines, silver := visit.LoseCargo(&c.Stock, c.Cargo, id)
	c.Cargo = remaining
	pos, _ := g.positions.Get(id)
	for _, l := range lines {
		g.dropGoods(l, pos.X, pos.Y, true)
	}
	if silver > 0 {
		g.dropGoods(trade.Line{Kind: item.Silver, Qty: silver}, pos.X, pos.Y, true)
	}
}

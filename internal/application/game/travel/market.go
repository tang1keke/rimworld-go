package travel

import (
	"slices"

	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/world/caravan"
)

// TradePlan is a preflighted, private replacement for one caravan's cargo.
// The market owner must commit the corresponding settlement stock in the
// same authoritative game command.
type TradePlan struct {
	caravan uint64
	items   []item.Stack
	sold    []item.Stack
	valid   bool
}

// Sold returns the actual stacks offered to the settlement, including
// quality, material, wear and other item attributes.
func (p TradePlan) Sold() []item.Stack { return slices.Clone(p.sold) }

// appendTradeGood joins only identical physical stacks. Goods obtained
// through repeated trades retain their own provenance and metadata.
func appendTradeGood(items []item.Stack, st item.Stack) []item.Stack {
	for i := range items {
		if items[i].Kind != st.Kind {
			continue
		}
		same := items[i]
		same.Qty = st.Qty
		if same == st {
			items[i].Qty += st.Qty
			return items
		}
	}
	return append(items, st)
}

// TradeCounts returns amounts the caravan can physically trade. Forbidden
// stacks remain aboard and cannot satisfy an offer.
func (o Ops) TradeCounts(cid uint64) map[item.Kind]int {
	counts := make(map[item.Kind]int)
	if h := o.s.cargo[cid]; h != nil {
		for _, st := range h.Items {
			if st.Qty > 0 && !st.Forbidden {
				counts[st.Kind] += st.Qty
			}
		}
	}
	return counts
}

// PlanTrade checks the complete new cargo, including payment in silver,
// and its carrying capacity before any authoritative state is changed.
// incoming is the exact merchandise removed from settlement stock.
func (o Ops) PlanTrade(cid uint64, members int, res trade.Result, incoming []item.Stack) (TradePlan, string) {
	if !res.OK() || members <= 0 {
		return TradePlan{}, "The caravan cannot make that trade."
	}
	var next []item.Stack
	capacity := members * caravan.PersonCarryKg
	if h := o.s.cargo[cid]; h != nil {
		next = make([]item.Stack, len(h.Items), len(h.Items)+len(incoming)+1)
		copy(next, h.Items)
		for _, b := range h.Animals {
			capacity += caravan.PackKg(animal.BodySize(b.State.Species))
		}
	}
	plan := TradePlan{caravan: cid, valid: true}
	take := func(kind item.Kind, qty int, selling bool) bool {
		for i := 0; i < len(next) && qty > 0; {
			st := &next[i]
			if st.Kind != kind || st.Forbidden || st.Qty <= 0 {
				i++
				continue
			}
			n := min(qty, st.Qty)
			if selling {
				part := *st
				part.Qty = n
				plan.sold = append(plan.sold, part)
			}
			st.Qty -= n
			qty -= n
			if st.Qty == 0 {
				next = slices.Delete(next, i, i+1)
			} else {
				i++
			}
		}
		return qty == 0
	}
	for _, line := range res.Sell {
		if line.Qty <= 0 || !take(line.Kind, line.Qty, line.Kind != item.Silver) {
			return TradePlan{}, "The caravan lacks goods for that trade."
		}
	}
	if res.Silver < 0 && !take(item.Silver, -res.Silver, false) {
		return TradePlan{}, "The caravan lacks silver for that trade."
	}
	// A gift of silver is already represented by res.Sell.
	if res.Silver > 0 {
		next = appendTradeGood(next, item.Stack{Kind: item.Silver, Qty: res.Silver})
	}
	expected := make(map[trade.Goods]int, len(res.Buy))
	for _, line := range res.Buy {
		expected[line.Goods()] += line.Qty
	}
	for _, st := range incoming {
		k := trade.Goods{Kind: st.Kind, Building: st.Building}
		if st.Qty <= 0 || expected[k] < st.Qty {
			return TradePlan{}, "The settlement cargo is inconsistent."
		}
		expected[k] -= st.Qty
		st.TradeBought = true
		st.X, st.Y, st.Carrier = 0, 0, 0
		next = appendTradeGood(next, st)
	}
	for _, remain := range expected {
		if remain != 0 {
			return TradePlan{}, "The settlement lacks the offered goods."
		}
	}
	var grams int64
	for _, st := range next {
		if st.Qty <= 0 {
			return TradePlan{}, "The trade contains an invalid item stack."
		}
		grams += int64(st.Qty) * int64(st.Kind.MassGrams())
	}
	if grams > int64(capacity)*1000 {
		return TradePlan{}, "The caravan cannot carry the purchased goods."
	}
	plan.items = next
	return plan, ""
}

// CommitTrade installs a previously accepted cargo replacement. It is
// called once, immediately alongside the settlement stock update.
func (o Ops) CommitTrade(cid uint64, plan TradePlan) {
	if !plan.valid || plan.caravan != cid {
		return
	}
	o.s.cargoOf(cid).Items = plan.items
	o.h.Changed()
}

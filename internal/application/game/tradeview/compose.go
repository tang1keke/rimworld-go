package tradeview

import (
	"cmp"
	"slices"

	"rimworld-go/internal/application/game/setupview"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
)

// Market is a resolved trade's read-only inputs, supplied by its owner.
type Market struct {
	Kind        trade.TraderKind
	Stock       trade.Stock
	Terms       trade.Terms
	Colony      map[item.Kind]int
	TraderName  string
	FactionName string
	FactionID   int
	HoursLeft   int
}

// Compose resolves all rows from the live sale stock and the colony's
// eligible stacks. This code only formats a snapshot.
func Compose(v Trade, m Market) Trade {
	v.Trader, v.TraderKind = m.TraderName, trade.DefOf(m.Kind).Label
	v.Faction, v.FactionID, v.Goodwill = m.FactionName, m.FactionID, m.Terms.Goodwill
	v.HoursLeft, v.Social = m.HoursLeft, m.Terms.Social
	centi := m.Terms.Social*trade.SocialStepCenti + m.Terms.Goodwill*trade.BuyGoodwillMilli/10
	v.PriceModPct = min(centi/100, trade.BuyMarkupPct-trade.BuyFloorPct)
	v.ColonySilver, v.TraderSilver = m.Colony[item.Silver], m.Stock.Silver

	theirs := make(map[trade.Goods]int, len(m.Stock.Lines))
	keys := make([]trade.Goods, 0, len(m.Colony)+len(m.Stock.Lines))
	for _, ln := range m.Stock.Lines {
		theirs[ln.Goods()] += ln.Qty
		keys = append(keys, ln.Goods())
	}
	for k := range m.Colony {
		if k != item.Silver && k.Value() > 0 && theirs[trade.Goods{Kind: k}] == 0 {
			keys = append(keys, trade.Goods{Kind: k})
		}
	}
	slices.SortFunc(keys, trade.Goods.Compare)
	for _, goods := range slices.Compact(keys) {
		k, qty := goods.Kind, m.Colony[goods.Kind]
		if goods.Building != 0 {
			qty = 0
		}
		v.Rows = append(v.Rows, Row{
			Kind: goods.Name(), Label: cmp.Or(goods.Label(), setupview.ItemLabel(k)),
			Category: k.Category(), ColonyQty: qty, TraderQty: theirs[goods],
			BuyC:     trade.PriceOf(goods, m.Kind, trade.Buy, m.Terms),
			SellC:    trade.PriceOf(goods, m.Kind, trade.Sell, m.Terms),
			WillBuy:  theirs[goods] > 0,
			WillSell: goods.Building == 0 && trade.Wants(m.Kind, k),
		})
	}
	slices.SortStableFunc(v.Rows, func(a, b Row) int {
		if ra, rb := item.CategoryRank(a.Category), item.CategoryRank(b.Category); ra != rb {
			return ra - rb
		}
		switch {
		case a.Label < b.Label:
			return -1
		case a.Label > b.Label:
			return 1
		}
		return 0
	})
	return v
}

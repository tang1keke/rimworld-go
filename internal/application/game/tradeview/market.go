package tradeview

import (
	"cmp"
	"slices"

	"rimworld-go/internal/application/game/maps"
	"rimworld-go/internal/application/game/tabview"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/entity"
	"rimworld-go/internal/domain/world/caravan"
)

// PriceModPct is the effective buy-markup improvement shown by the
// existing local trade dialog.
func PriceModPct(t trade.Terms) int {
	centi := t.Social*trade.SocialStepCenti + t.Goodwill*trade.BuyGoodwillMilli/10
	return min(centi/100, trade.BuyMarkupPct-trade.BuyFloorPct)
}

// StockRow is one commodity in the union of a trader's sorted stock and
// the caravan's tradeable cargo. It remains a read-only projection.
type StockRow struct {
	Goods              trade.Goods
	CargoQty, StockQty int
	BuyC, SellC        int
	WillBuy, WillSell  bool
}

func StockRows(stock trade.Stock, cargo map[item.Kind]int, terms trade.Terms) []StockRow {
	return stockRows(nil, stock, cargo, terms)
}

func stockRows(rows []StockRow, stock trade.Stock, cargo map[item.Kind]int, terms trade.Terms) []StockRow {
	var keyBuf [32]trade.Goods
	keys := keyBuf[:0]
	for _, line := range stock.Lines {
		keys = append(keys, line.Goods())
	}
	for k := range cargo {
		if k != item.Silver && k.Value() > 0 {
			keys = append(keys, trade.Goods{Kind: k})
		}
	}
	slices.SortFunc(keys, trade.Goods.Compare)
	keys = slices.Compact(keys)
	stockIdx := 0
	for _, k := range keys {
		for stockIdx < len(stock.Lines) && stock.Lines[stockIdx].Goods().Compare(k) < 0 {
			stockIdx++
		}
		qty := 0
		if stockIdx < len(stock.Lines) && stock.Lines[stockIdx].Goods() == k {
			qty = stock.Lines[stockIdx].Qty
		}
		carry := cargo[k.Kind]
		if k.Building != 0 {
			carry = 0
		}
		rows = append(rows, StockRow{
			Goods: k, CargoQty: carry, StockQty: qty,
			BuyC:     trade.PriceOf(k, stock.Kind, trade.Buy, terms),
			SellC:    trade.PriceOf(k, stock.Kind, trade.Sell, terms),
			WillBuy:  qty > 0,
			WillSell: k.Building == 0 && trade.Wants(stock.Kind, k.Kind),
		})
	}
	return rows
}

// LocalRows is the same price and inventory projection used by the
// existing caravan and orbital trade dialogs.
func LocalRows(stock trade.Stock, cargo map[item.Kind]int, terms trade.Terms, label func(item.Kind) string) []Row {
	parts := StockRows(stock, cargo, terms)
	rows := make([]Row, 0, len(parts))
	for _, r := range parts {
		k := r.Goods
		rows = append(rows, Row{
			Kind: k.Name(), Label: cmp.Or(k.Label(), label(k.Kind)),
			Category: k.Kind.Category(), ColonyQty: r.CargoQty, TraderQty: r.StockQty,
			BuyC: r.BuyC, SellC: r.SellC, WillBuy: r.WillBuy, WillSell: r.WillSell,
		})
	}
	slices.SortStableFunc(rows, func(a, b Row) int {
		if ra, rb := item.CategoryRank(a.Category), item.CategoryRank(b.Category); ra != rb {
			return ra - rb
		}
		return cmp.Compare(a.Label, b.Label)
	})
	return rows
}

// VisitingSource supplies the World tab's existing authoritative lookup
// and negotiation rules without giving presentation code simulation state.
type VisitingSource interface {
	MarketAt(int) (maps.SettlementStock, bool)
	FactionAt(int) *faction.Faction
	Eligible(entity.EntityID) bool
	Terms(entity.EntityID, *faction.Faction) trade.Terms
	Cargo(uint64) map[item.Kind]int
}

// Visiting resolves a live caravan, settlement and negotiator into a
// market snapshot; false means the inspector must not offer trading.
func Visiting(now uint64, c *maps.Caravan, src VisitingSource) (tabview.MarketView, bool) {
	if c == nil || c.Back || c.C.State != caravan.Visiting || now >= c.C.VisitUntil {
		return tabview.MarketView{}, false
	}
	m, ok := src.MarketAt(c.C.Tile)
	if !ok {
		return tabview.MarketView{}, false
	}
	fa := src.FactionAt(m.Tile)
	if fa == nil || fa.ID != m.FactionID || fa.Defeated || fa.Relation == faction.Hostile || fa.Besieged {
		return tabview.MarketView{}, false
	}
	var negotiator entity.EntityID
	for _, raw := range c.C.Members {
		id := entity.EntityID(raw)
		if src.Eligible(id) {
			negotiator = id
			break
		}
	}
	terms := src.Terms(negotiator, fa)
	terms.ImproveCenti = 200
	return Settlement(c.C.ID, uint64(negotiator), m, src.Cargo(c.C.ID), terms), true
}

// Settlement creates the World tab's snapshot from the already-validated
// visiting settlement and the current physical caravan cargo.
func Settlement(cid, negotiator uint64, m maps.SettlementStock,
	cargo map[item.Kind]int, terms trade.Terms) tabview.MarketView {
	v := tabview.MarketView{
		CaravanID: cid, Negotiator: negotiator, Tile: m.Tile,
		FactionID: m.FactionID, RestockAt: m.RestockAt, Epoch: m.Epoch,
		CaravanSilver: cargo[item.Silver], TraderSilver: m.Stock.Silver,
	}
	var rowBuf [32]StockRow
	rows := stockRows(rowBuf[:0], m.Stock, cargo, terms)
	v.Rows = make([]tabview.MarketRow, 0, len(rows))
	for _, r := range rows {
		k := r.Goods
		label := k.Kind.String()
		if crate := k.Label(); crate != "" {
			label = crate
		}
		v.Rows = append(v.Rows, tabview.MarketRow{
			Kind: k.Name(), Category: k.Kind.Category(), Label: label,
			Building: k.Building, CargoQty: r.CargoQty, StockQty: r.StockQty,
			BuyC: r.BuyC, SellC: r.SellC, WillBuy: r.WillBuy, WillSell: r.WillSell,
		})
	}
	return v
}

package worldview

import (
	"fmt"
	"slices"
	"strings"

	"rimworld-go/internal/adapters/inbound/ebiten/gfx"
	"rimworld-go/internal/adapters/inbound/ebiten/uikit"
	"rimworld-go/internal/application/game/tabview"
	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/command"
)

// MarketModalID is the World tab's faction-base trade modal.
const MarketModalID = "world.market"

const marketRowH = 30

type marketDraft struct {
	view   tabview.MarketView
	rows   []tabview.MarketRow
	net    map[string]int
	gift   bool
	search string
	sig    string
	sentAt uint64
	sent   bool
}

func marketSig(v tabview.MarketView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d:%d:%d:%d:%d:%d", v.CaravanID, v.Negotiator, v.Epoch, v.RestockAt, v.CaravanSilver, v.TraderSilver)
	for _, r := range v.Rows {
		fmt.Fprintf(&b, "|%s:%d:%d:%d:%d:%t:%t", r.Kind, r.CargoQty, r.StockQty, r.BuyC, r.SellC, r.WillBuy, r.WillSell)
	}
	return b.String()
}

func newMarketDraft(v tabview.MarketView) *marketDraft {
	d := &marketDraft{net: make(map[string]int)}
	d.refresh(v)
	return d
}

func (d *marketDraft) refresh(v tabview.MarketView) {
	if sig := marketSig(v); sig != d.sig {
		clear(d.net)
		d.sig = sig
		d.sent = false
	}
	d.view = v
	d.rows = slices.Clone(v.Rows)
	slices.SortStableFunc(d.rows, func(a, b tabview.MarketRow) int {
		if c := strings.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return strings.Compare(a.Label, b.Label)
	})
	for _, r := range d.rows {
		d.set(r, d.net[r.Kind])
	}
}

func (d *marketDraft) bounds(r tabview.MarketRow) (lo, hi int) {
	if r.WillSell {
		lo = -r.CargoQty
	}
	if r.WillBuy && !d.gift {
		hi = r.StockQty
	}
	return lo, hi
}

func (d *marketDraft) set(r tabview.MarketRow, n int) {
	lo, hi := d.bounds(r)
	n = max(lo, min(hi, n))
	if n == 0 {
		delete(d.net, r.Kind)
		return
	}
	d.net[r.Kind] = n
}

func (d *marketDraft) setGift(on bool) {
	d.gift = on
	for _, r := range d.rows {
		d.set(r, d.net[r.Kind])
	}
}

func (d *marketDraft) totalC() int {
	total := 0
	for _, r := range d.rows {
		n := d.net[r.Kind]
		switch {
		case n > 0:
			total += n * r.BuyC
		case n < 0:
			total += n * r.SellC
		}
	}
	return total
}

func (d *marketDraft) giftValue() int {
	total := 0
	for _, r := range d.rows {
		n := d.net[r.Kind]
		if n >= 0 {
			continue
		}
		if g, ok := trade.ParseGoods(r.Kind); ok {
			total -= n * g.Value()
		}
	}
	return total
}

func (d *marketDraft) offer() []trade.Delta {
	out := make([]trade.Delta, 0, len(d.net))
	for _, r := range d.rows {
		if n := d.net[r.Kind]; n != 0 {
			if g, ok := trade.ParseGoods(r.Kind); ok {
				out = append(out, trade.Delta{Kind: g.Kind, Building: g.Building, Qty: n})
			}
		}
	}
	return out
}

func (d *marketDraft) reason() string {
	switch {
	case d.view.Negotiator == 0:
		return "No caravan member can negotiate."
	case len(d.net) == 0:
		return "Choose goods to buy or sell."
	case d.gift:
		return ""
	}
	t := d.totalC()
	if t > 0 && d.view.CaravanSilver < (t+99)/100 {
		return "The caravan lacks silver."
	}
	if t < 0 && d.view.TraderSilver < -t/100 {
		return "The settlement cannot pay."
	}
	return ""
}

// marketUI owns the trade dialog's projection and actions, separate from
// the world view's map navigation and camera.
type marketUI struct{ *View }

func (v marketUI) marketFor(cid uint64) (tabview.MarketView, bool) {
	for _, m := range v.markets {
		if m.CaravanID == cid {
			return m, true
		}
	}
	return tabview.MarketView{}, false
}

func (v marketUI) closeMarket() {
	v.market = nil
	v.kit().CloseModal(MarketModalID)
}

func (v marketUI) syncMarket() {
	if v.market == nil {
		return
	}
	next, ok := v.marketFor(v.market.view.CaravanID)
	if !ok {
		v.closeMarket()
		return
	}
	v.market.refresh(next)
	if uint64(v.sh.Tick()) > v.market.sentAt {
		v.market.sent = false
	}
}

func (v marketUI) openMarket(cid uint64) {
	m, ok := v.marketFor(cid)
	if !ok || m.Negotiator == 0 {
		return
	}
	d := newMarketDraft(m)
	v.market = d
	v.kit().Push(uikit.Modal{
		ID: MarketModalID, Title: v.sh.Tf("Trade at %s", v.tileName(m.Tile)), W: 900, H: 660,
		Draw: func(body gfx.Rect) { v.drawMarket(d, body) },
		OnClose: func() {
			if v.market == d {
				v.market = nil
			}
		},
		Buttons: []uikit.ModalButton{
			{ID: "accept", Label: v.sh.T("Accept"), Style: uikit.BtnPrimary,
				Disabled: func() string {
					if d.sent && uint64(v.sh.Tick()) == d.sentAt {
						return "Trade is being processed."
					}
					return d.reason()
				}, OnClick: func() { v.submitMarket(d) }},
			{ID: "reset", Label: v.sh.T("Reset"), OnClick: func() { clear(d.net) }},
			{ID: "close", Label: v.sh.T("Close"), Closes: true},
		},
	})
}

func (v marketUI) submitMarket(d *marketDraft) {
	if v.market != d || d.reason() != "" || d.sent && d.sentAt == uint64(v.sh.Tick()) {
		return
	}
	v.send(command.Command{
		Kind: command.WorldCaravanTrade, Target: d.view.CaravanID,
		Pawn: d.view.Negotiator, Name: trade.EncodeOffer(d.offer()), Enabled: d.gift,
	})
	d.sentAt, d.sent = uint64(v.sh.Tick()), true
}

func marketSilver(c int) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

func (v marketUI) drawMarket(d *marketDraft, body gfx.Rect) {
	k := v.kit()
	k.Label(body.X, body.Y, v.sh.Tf("Caravan: %d silver   Settlement: %d silver   Negotiator: %d",
		d.view.CaravanSilver, d.view.TraderSilver, d.view.Negotiator), gfx.Body)
	k.Label(body.X, body.Y+24, v.sh.Tf("Restock in %s", hoursLeft(d.view.RestockAt, uint64(v.sh.Tick()))), uikit.DimText())
	k.TextField("world.market.search", gfx.Rect{X: body.Right() - 210, Y: body.Y + 20, W: 205, H: 26},
		&d.search, uikit.TextFieldSpec{Placeholder: "Search goods", MaxLen: 80})

	top := body.Y + 60
	head := gfx.WithColor(gfx.Small, uikit.TextDim)
	col := [...]float32{0, 215, 275, 408, 480, 618, 675, 752}
	for i, label := range [...]string{"Goods", "Caravan", "Give", "Amount", "Take", "Stock", "Sell for", "Buy for"} {
		k.Label(body.X+col[i], top, v.sh.T(label), head)
	}
	table := gfx.Rect{X: body.X, Y: top + 24, W: body.W, H: body.Bottom() - (top + 24) - 82}
	filtered := make([]tabview.MarketRow, 0, len(d.rows))
	for _, r := range d.rows {
		if d.search == "" || strings.Contains(strings.ToLower(r.Label+" "+r.Category), strings.ToLower(d.search)) {
			filtered = append(filtered, r)
		}
	}
	area := k.ScrollArea("world.market.rows", table, float32(len(filtered)*marketRowH), uikit.ScrollOpts{Step: marketRowH * 2})
	y := area.View.Y - area.Off
	for _, r := range filtered {
		ry := y
		y += marketRowH
		if ry < area.View.Y || ry+marketRowH > area.View.Bottom() {
			continue
		}
		n := d.net[r.Kind]
		k.Label(body.X, ry+6, uikit.FitText(r.Label, gfx.Body, 206), gfx.Body)
		k.Label(body.X+220, ry+6, fmt.Sprint(r.CargoQty), gfx.Small)
		k.Label(body.X+419, ry+6, fmt.Sprintf("%+d", n), gfx.Body)
		k.Label(body.X+621, ry+6, fmt.Sprint(r.StockQty), gfx.Small)
		if r.WillSell {
			k.Label(body.X+682, ry+6, marketSilver(r.SellC), gfx.Small)
		}
		if r.WillBuy {
			k.Label(body.X+759, ry+6, marketSilver(r.BuyC), gfx.Small)
		}
		for i, step := range [...]int{0, 10, 1} {
			nn := n
			lo, hi := d.bounds(r)
			stepQty := step
			if stepQty == 0 {
				stepQty = nn - lo
			}
			why := ""
			if !r.WillSell || r.CargoQty == 0 || nn <= lo {
				why = "No goods available to sell."
			}
			id := fmt.Sprintf("world.market.%s.give.%d", r.Kind, i)
			button := gfx.Rect{X: body.X + 275 + float32(i)*43, Y: ry + 2, W: 40, H: marketRowH - 4}
			if k.Button(id, button, [...]string{"All", "10", "1"}[i], uikit.BtnOpts{Disabled: why != "", Reason: why}) {
				d.set(r, nn-stepQty)
			}
			if step == 0 {
				stepQty = hi - nn
			}
			why = ""
			if !r.WillBuy || d.gift || r.StockQty == 0 || nn >= hi {
				why = "No goods available to buy."
			}
			id = fmt.Sprintf("world.market.%s.take.%d", r.Kind, i)
			button.X = body.X + 480 + float32(i)*43
			if k.Button(id, button, [...]string{"All", "10", "1"}[i], uikit.BtnOpts{Disabled: why != "", Reason: why}) {
				d.set(r, nn+stepQty)
			}
		}
	}
	sumY := body.Bottom() - 65
	if k.Check("world.market.gift", gfx.Rect{X: body.X, Y: sumY, W: 230, H: 22},
		v.sh.T("Gift (no payment)"), &d.gift, uikit.CheckOpts{}) {
		d.setGift(d.gift)
	}
	if d.gift {
		k.Label(body.X+250, sumY+3, v.sh.Tf("Goodwill +%d", faction.GiftGoodwill(d.giftValue())), gfx.Body)
	} else {
		total := d.totalC()
		label := v.sh.T("You pay")
		if total < 0 {
			label = v.sh.T("You receive")
			total = -total
		}
		k.Label(body.X+250, sumY+3, v.sh.Tf("%s %s silver", label, marketSilver(total)), gfx.Body)
	}
	k.Label(body.X, sumY+34, v.sh.Tf("Selected %d kinds. %s", len(d.net), d.reason()), uikit.DimText())
}

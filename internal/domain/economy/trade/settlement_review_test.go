package trade

import (
	"reflect"
	"testing"

	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/faction"
)

func TestSettleGiftOnlyCreditsTransferredGoods(t *testing.T) {
	// Direct callers can pass duplicate keys; a positive and a negative
	// line are a net single sale. The donated value must match that unit.
	offer := []Delta{{Kind: item.Rifle, Qty: 100}, {Kind: item.Rifle, Qty: -101}}
	got := Settle(offer, map[item.Kind]int{item.Rifle: 1}, Stock{Kind: IndustrialSettlement}, Terms{}, true)
	want := faction.GiftGoodwill(item.Rifle.Value())
	if !got.OK() || !reflect.DeepEqual(got.Sell, []Line{{Kind: item.Rifle, Qty: 1}}) || got.Goodwill != want {
		t.Fatalf("gift value was not based on the actual transfer: %+v, want goodwill %d", got, want)
	}
}

func BenchmarkSettleCaravanOffer(b *testing.B) {
	stock := GenerateSettlement(faction.Industrial, 901)
	cargo := map[item.Kind]int{item.Silver: 1500, item.Wood: 40, item.Cloth: 20}
	offers := []struct {
		name  string
		delta []Delta
	}{
		{"single", []Delta{{Kind: item.Cloth, Qty: 1}}},
		{"mixed", []Delta{{Kind: item.Cloth, Qty: 2}, {Kind: item.Wood, Qty: -3}}},
	}
	for _, tc := range offers {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var last Result
			for i := 0; i < b.N; i++ {
				last = Settle(tc.delta, cargo, stock, Terms{Social: 8, Goodwill: 12, ImproveCenti: 200}, false)
			}
			if !last.OK() {
				b.Fatalf("valid offer rejected: %d", last.Reject)
			}
		})
	}
}

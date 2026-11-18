package trade

import (
	"testing"

	"rimworld-go/internal/domain/building/construction"
	"rimworld-go/internal/domain/economy/item"
)

func TestGiftMethodRates(t *testing.T) {
	for _, tc := range []struct {
		kind   item.Kind
		market int
		method GiftMethod
		social int
		centi  int
	}{
		{item.Lung, 1000, GiftVisitor, 0, 60000},
		{item.Lung, 1000, GiftVisitor, 1, 60900},
		{item.Lung, 1000, GiftVisitor, 20, 78000},
		{item.Lung, 1000, GiftVisitor, 21, 78000},
		{item.Lung, 1000, GiftPod, 0, 61200},
		{item.Lung, 1000, GiftPod, 20, 61200},
		{item.Silver, 1000, GiftVisitor, 20, 100000},
		{item.Silver, 1000, GiftPod, 0, 100000},
		{item.Rifle, 120, GiftVisitor, 0, 1440},
		{item.Rifle, 120, GiftPod, 0, 1469},
		{item.Rifle, 120, GiftVisitor, 20, 1872},
		{item.Lung, 0, GiftPod, 0, 0},
	} {
		if got := GiftUnitCenti(tc.kind, tc.market, tc.method, tc.social); got != tc.centi {
			t.Errorf("%s market %d method %d social %d: got %d, want %d centi",
				tc.kind, tc.market, tc.method, tc.social, got, tc.centi)
		}
	}
	if got := GiftBaseCenti(1000, GiftPod, 0); got != 61200 {
		t.Fatalf("pod ordinary item: %d centi, want 61200", got)
	}
}

// A crated 350-silver telescope is furniture: 0.7 seller modifier for
// visiting traders, while sending it minified by pod excludes the 0.7.
func TestGiftMinifiedFurnitureByMethod(t *testing.T) {
	g := Goods{Kind: item.MinifiedBuilding, Building: uint8(construction.Telescope)}
	if got := GiftGoodsCenti(g, GiftVisitor, 0); got != 14700 {
		t.Fatalf("350 market visiting gift: got %d centi, want 14700", got)
	}
	if got := GiftGoodsCenti(g, GiftPod, 0); got != 21420 {
		t.Fatalf("350 market pod gift: got %d centi, want 21420", got)
	}
	if got := GiftValue([]Delta{{Kind: item.MinifiedBuilding, Building: g.Building, Qty: -1}}, 0); got != 147 {
		t.Fatalf("gift offer value %d, want 147", got)
	}
}

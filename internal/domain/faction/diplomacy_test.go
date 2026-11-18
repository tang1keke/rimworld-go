package faction

import (
	"reflect"
	"testing"
)

func TestGiftGoodwillCostAndQuote(t *testing.T) {
	if GiftCostAt(-20) != 40 || GiftCostAt(0) != 41 || GiftCostAt(75) != 160 || GiftCostAt(100) != 160 {
		t.Fatal("public goodwill gift-price endpoints changed")
	}
	for _, tc := range []struct{ target, silver int }{
		{40, 2041}, {60, 3661}, {75, 5539},
	} {
		f := Faction{Type: Settlers, Goodwill: 0}
		if got := GiftSilverFor(f, tc.target); got != tc.silver {
			t.Fatalf("wiki gift to %+d: got %d silver, want %d", tc.target, got, tc.silver)
		}
	}
	for goodwill := -90; goodwill <= 99; goodwill++ {
		f := Faction{Type: Settlers, Goodwill: goodwill}
		for target := goodwill + 1; target <= min(100, goodwill+10); target++ {
			total := GiftSilverFor(f, target)
			if total <= 0 || GiftGoodwill(f, total) < target-goodwill ||
				GiftGoodwill(f, total-1) >= target-goodwill {
				t.Fatalf("from %d to %d: quote %d", goodwill, target, total)
			}
		}
	}
	f := Faction{Type: Settlers, Goodwill: -60}
	if GiftGoodwill(f, 160) <= 4 {
		t.Fatal("natural-goodwill gift bonus was lost")
	}
	for _, ty := range []Type{Freebooters, SavageClans, Swarm, MechHive, AncientHostile, Player} {
		f := Faction{Type: ty, Goodwill: -100}
		if GiftGoodwill(f, 10000) != 0 || GiftSilverFor(f, 0) != 0 {
			t.Fatalf("fixed or hidden faction %v accepted gift", ty)
		}
	}
}

func TestRadioRequestsRespectFactionAndCooldown(t *testing.T) {
	f := Faction{Type: Settlers, Relation: Friendly, Goodwill: 90}
	if ok, why := CanRequestAid(f, Cooldowns{}, 0); !ok || why != "" {
		t.Fatalf("industrial ally aid: %q", why)
	}
	if ok, why := CanRequestCaravan(f, Cooldowns{}, 0); !ok || why != "" {
		t.Fatalf("industrial ally trade: %q", why)
	}
	if ok, why := CanRequestAid(f, Cooldowns{AidUntil: 100}, 99); ok || why != ReasonCoolingOff {
		t.Fatalf("aid cooldown: %t %q", ok, why)
	}
	if ok, why := CanRequestCaravan(f, Cooldowns{CaravanUntil: 100}, 99); ok || why != ReasonCoolingOff {
		t.Fatalf("caravan cooldown: %t %q", ok, why)
	}
	if ok, _ := CanRequestCaravan(f, Cooldowns{CaravanUntil: 100}, 100); !ok {
		t.Fatal("expired cooldown")
	}
	tribe := Faction{Type: Clans, Relation: Friendly, Goodwill: 100}
	if ok, why := CanRequestAid(tribe, Cooldowns{}, 0); ok || why != ReasonNoFighters {
		t.Fatalf("tribal radio aid must be refused: %t %q", ok, why)
	}
	if ok, why := CanRequestCaravan(tribe, Cooldowns{}, 0); !ok || why != "" {
		t.Fatalf("tribal ally trade: %t %q", ok, why)
	}
	for _, tc := range []struct {
		name string
		f    Faction
		want string
	}{
		{"neutral", Faction{Type: Settlers, Relation: Neutral, Goodwill: 50}, ReasonNotFriend},
		{"hostile", Faction{Type: Drifters, Relation: Hostile, Goodwill: -80}, ReasonHostile},
		{"defeated", Faction{Type: Settlers, Relation: Friendly, Goodwill: 90, Defeated: true}, ReasonGone},
		{"permanent", Faction{Type: Freebooters, Relation: Hostile, Goodwill: -100}, ReasonHostile},
	} {
		if ok, why := CanRequestCaravan(tc.f, Cooldowns{}, 0); ok || why != tc.want {
			t.Errorf("%s caravan: %t %q", tc.name, ok, why)
		}
		if ok, why := CanRequestAid(tc.f, Cooldowns{}, 0); ok || why != tc.want {
			t.Errorf("%s aid: %t %q", tc.name, ok, why)
		}
	}
}

// Trading earns goodwill only through actual merchandise and keeps partial
// silver amounts across transactions, including through a saved checkpoint.
func TestTradeGoodwillAccumulatesAcrossTransactions(t *testing.T) {
	f := Faction{Type: Settlers, Goodwill: 73}
	if TradeGoodwill(&f, 300, "trade") != 0 || f.Goodwill != 73 || f.TradeRemainder != 300 {
		t.Fatalf("first trade: %+v", f)
	}
	copy := f
	if TradeGoodwill(&f, 200, "trade") != 1 || TradeGoodwill(&copy, 200, "trade") != 1 ||
		f.Goodwill != 74 || f.TradeRemainder != 0 || !reflect.DeepEqual(copy, f) {
		t.Fatalf("second trade across checkpoint: %+v / %+v", f, copy)
	}
	if TradeGoodwill(&f, 550, "trade") != 1 || f.Goodwill != 75 || f.Relation != Friendly ||
		f.TradeRemainder != 50 {
		t.Fatalf("goodwill trade changed alliance incorrectly: %+v", f)
	}
	underAttack := Faction{Type: Settlers, Goodwill: -50, Besieged: true}
	if earned := TradeGoodwill(&underAttack, 1500, "trading"); earned != 0 ||
		underAttack.Goodwill != -50 || underAttack.TradeRemainder != 0 {
		t.Fatalf("war-time trade changed goodwill: %+v", underAttack)
	}
	fixed := Faction{Type: Freebooters, Goodwill: -100}
	if TradeGoodwill(&fixed, 50000, "trade") != 0 || fixed.TradeRemainder != 0 {
		t.Fatal("fixed hostile faction accepted traded goodwill")
	}
}

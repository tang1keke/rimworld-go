package faction

import "testing"

// Independently observed public Core gift outcomes, not original code.
func TestGiftPublicMeasurements(t *testing.T) {
	t.Parallel()
	f := Faction{Type: Settlers}
	if got := GiftGoodwill(f, 334); got != 8 {
		t.Fatalf("334 silver lump at 0 goodwill: got +%d, measured +8", got)
	}
	first := GiftGoodwill(f, 167)
	f.Goodwill += first
	second := GiftGoodwill(f, 167)
	if first+second != 7 {
		t.Fatalf("two 167 silver gifts: +%d and +%d, measured +7 total", first, second)
	}
	b := Faction{Type: Settlers, Goodwill: -100}
	if got := GiftGoodwill(b, 6770); got != 175 {
		t.Fatalf("-100 base goodwill 0, single 6770 silver gift: got %+d, measured +175", got)
	}
}

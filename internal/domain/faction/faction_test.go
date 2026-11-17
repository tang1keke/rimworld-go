package faction

import (
	"reflect"
	"strings"
	"testing"

	"rimworld-go/pkg/randx"
)

func TestDefTable(t *testing.T) {
	t.Parallel()
	type row struct {
		t             Type
		key           string
		tech          Tech
		min, max      int
		fixed, friend bool
		perTenK       int
		titles        string
		points        int
	}
	rows := []row{
		{Settlers, "settlers", Industrial, -50, 50, false, true, 12, "Warden,Steward", 100},
		{Clans, "clans", Neolithic, -50, 50, false, true, 16, "Elder,Hearth-mother,Hearth-father", 100},
		{Drifters, "drifters", Industrial, -100, -80, false, true, 10, "Boss,Caller", 100},
		{Freebooters, "freebooters", Industrial, -100, -100, true, false, 8, "Captain,Ringleader", 120},
		{Swarm, "swarm", Neolithic, -100, -100, true, false, 0, "", 100},
		{FierceClans, "fierce-clans", Neolithic, -100, -80, false, true, 16, "Elder,Hearth-mother,Hearth-father", 100},
		{SavageClans, "savage-clans", Neolithic, -100, -100, true, false, 16, "Elder,Hearth-mother,Hearth-father", 120},
		{AncientQuiet, "old-kin", Spacer, 0, 0, false, true, 0, "", 100},
		{AncientHostile, "lost-kin", Spacer, -100, -100, true, false, 0, "", 100},
		{MechHive, "iron-host", Spacer, -100, -100, true, false, 0, "", 100},
	}
	for _, r := range rows {
		d := DefOf(r.t)
		if d.Type != r.t || d.Key != r.key || d.Tech != r.tech || d.GoodwillMin != r.min || d.GoodwillMax != r.max ||
			d.Fixed != r.fixed || d.CanBeFriendly != r.friend || d.PerTenK != r.perTenK ||
			strings.Join(d.Leaders, ",") != r.titles || d.PointsPct != r.points {
			t.Errorf("%s: %+v", r.key, d)
		}
		if d.Label == "" {
			t.Errorf("%s has no label", r.key)
		}
		if r.t.String() != r.key {
			t.Errorf("String %q", r.t.String())
		}
	}
	for _, tc := range []struct {
		t      Type
		emblem string
	}{
		{FierceClans, "ui_icon/faction_clans"},
		{SavageClans, "ui_icon/faction_clans"},
		{AncientQuiet, "ui_icon/faction_settlers"},
		{AncientHostile, "ui_icon/faction_freebooters"},
		{MechHive, "ui_icon/faction_swarm"},
	} {
		if got := DefOf(tc.t).Emblem(); got != tc.emblem {
			t.Fatalf("%s icon: %s, want %s", tc.t, got, tc.emblem)
		}
	}
	if !DefOf(Swarm).Hidden || DefOf(Swarm).MaxCount != 1 || !DefOf(Player).Hidden {
		t.Error("swarm and player are hidden")
	}
	if all := All(); len(all) != int(NumTypes) || all[Clans].Key != "clans" {
		t.Errorf("All: %d", len(all))
	}
	if DefOf(NumTypes).Key != "" {
		t.Error("unknown type has an empty def")
	}
	d := DefOf(Clans)
	d.Leaders[0] = "Mutated"
	if DefOf(Clans).Leaders[0] != "Elder" {
		t.Error("DefOf returns a copy")
	}
	slots := DefaultSlots()
	want := []Slot{{Settlers, 1}, {Drifters, 1}, {Clans, 1}, {FierceClans, 1},
		{SavageClans, 1}, {Freebooters, 1}}
	if len(slots) != len(want) {
		t.Fatalf("slots %v", slots)
	}
	for i := range want {
		if slots[i] != want[i] {
			t.Errorf("slot %d: %v", i, slots[i])
		}
	}
	for _, ty := range []Type{Settlers, Clans, Drifters, Freebooters, FierceClans, SavageClans} {
		if !ty.Human() {
			t.Errorf("%v is human", ty)
		}
	}
	if Swarm.Human() || MechHive.Human() || Player.Human() || NumTypes.Human() {
		t.Error("swarm, player and unknown are not human")
	}
}

func TestGenerate(t *testing.T) {
	t.Parallel()
	fs := Generate(DefaultSlots(), randx.New(11))
	if len(fs) != 6 {
		t.Fatalf("%d factions, want 6", len(fs))
	}
	counts := map[Type]int{}
	for i, f := range fs {
		counts[f.Type]++
		d := DefOf(f.Type)
		if f.ID != i || f.Name == "" || f.Leader.Name == "" || f.Leader.Title == "" {
			t.Errorf("faction %d incomplete: %+v", i, f)
		}
		if f.Goodwill < d.GoodwillMin || f.Goodwill > d.GoodwillMax {
			t.Errorf("%s goodwill %d outside %d..%d", f.Name, f.Goodwill, d.GoodwillMin, d.GoodwillMax)
		}
		if len(f.History) != 1 || int(f.History[0]) != f.Goodwill {
			t.Errorf("%s history %v", f.Name, f.History)
		}
		switch f.Type {
		case Settlers, Clans:
			if f.Relation != Neutral {
				t.Errorf("%v starts neutral: %+v", f.Type, f)
			}
		case Drifters, FierceClans, SavageClans, Freebooters:
			if f.Relation != Hostile {
				t.Errorf("%v starts hostile: %+v", f.Type, f)
			}
		}
		if f.Relation == Friendly && !d.CanBeFriendly {
			t.Errorf("%s friendly but cannot be", f.Name)
		}
		has := false
		for _, title := range d.Leaders {
			has = has || title == f.Leader.Title
		}
		if !has {
			t.Errorf("%s: title %q not in %v", f.Name, f.Leader.Title, d.Leaders)
		}
	}
	if counts[Settlers] != 1 || counts[Clans] != 1 || counts[Drifters] != 1 ||
		counts[Freebooters] != 1 || counts[FierceClans] != 1 || counts[SavageClans] != 1 {
		t.Errorf("counts %v", counts)
	}
	// Counts clamp to MaxCount; player slots and unknown types are skipped; swarm works.
	many := Generate([]Slot{{Settlers, 99}, {Player, 3}, {NumTypes, 2}, {Swarm, 5}, {Clans, 0}}, randx.New(2))
	got := map[Type]int{}
	for _, f := range many {
		got[f.Type]++
	}
	if got[Settlers] != 4 || got[Swarm] != 1 || got[Player] != 0 || got[Clans] != 0 || len(many) != 5 {
		t.Errorf("clamped counts %v", got)
	}
	for _, f := range many {
		if f.Type == Swarm && (f.Name != "The Swarm" || f.Leader != (Leader{}) || f.Relation != Hostile) {
			t.Errorf("swarm: %+v", f)
		}
	}
	// Clan leaders titled mother/father get a matching given name.
	for seed := uint64(1); seed <= 40; seed++ {
		for _, f := range Generate([]Slot{{Clans, 4}}, randx.New(seed)) {
			given := strings.Fields(f.Leader.Name)[0]
			switch f.Leader.Title {
			case "Hearth-mother":
				if !contains(femaleNames, given) {
					t.Fatalf("%s %s", f.Leader.Title, f.Leader.Name)
				}
			case "Hearth-father":
				if !contains(maleNames, given) {
					t.Fatalf("%s %s", f.Leader.Title, f.Leader.Name)
				}
			}
		}
	}
	// Deterministic.
	a, b := Generate(DefaultSlots(), randx.New(5)), Generate(DefaultSlots(), randx.New(5))
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Leader != b[i].Leader || a[i].Goodwill != b[i].Goodwill {
			t.Fatal("Generate is not deterministic")
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestFactionHostilityHysteresis(t *testing.T) {
	t.Parallel()
	// wiki: Factions. The state between 0 and an entry threshold
	// depends on which relationship the faction last held.
	cases := []struct {
		start Relation
		gw    int
		want  Relation
	}{
		{Neutral, -74, Neutral}, {Neutral, -75, Hostile},
		{Hostile, -74, Hostile}, {Hostile, -1, Hostile},
		{Hostile, 0, Neutral}, {Hostile, 40, Neutral},
		{Neutral, 74, Neutral}, {Neutral, 75, Friendly},
		{Friendly, 74, Friendly}, {Friendly, 1, Friendly},
		{Friendly, 0, Neutral}, {Friendly, -75, Hostile},
		{Hostile, 75, Friendly}, {Neutral, -100, Hostile},
	}
	for _, tc := range cases {
		if got := RelationFor(tc.start, tc.gw); got != tc.want {
			t.Errorf("start=%d goodwill=%d: got %d want %d", tc.start, tc.gw, got, tc.want)
		}
	}
	rel := Neutral
	for gw := 0; gw >= -100; gw-- {
		rel = RelationFor(rel, gw)
		want := Neutral
		if gw <= -75 {
			want = Hostile
		}
		if rel != want {
			t.Fatalf("descending at %d: %d", gw, rel)
		}
	}
	for gw := -100; gw <= 0; gw++ {
		rel = RelationFor(rel, gw)
		if gw < 0 && rel != Hostile || gw == 0 && rel != Neutral {
			t.Fatalf("ascending from hostility at %d: %d", gw, rel)
		}
	}
	for gw := 1; gw <= 100; gw++ {
		rel = RelationFor(rel, gw)
		if gw < 75 && rel != Neutral || gw >= 75 && rel != Friendly {
			t.Fatalf("ascending from neutral at %d: %d", gw, rel)
		}
	}
	for gw := 100; gw >= 0; gw-- {
		rel = RelationFor(rel, gw)
		if gw > 0 && rel != Friendly || gw == 0 && rel != Neutral {
			t.Fatalf("descending from alliance at %d: %d", gw, rel)
		}
	}
}

func TestDailyDriftAndAdjust(t *testing.T) {
	t.Parallel()
	// wiki: Factions. Above the natural band -2 per five days, below
	// it +1 per five days. The phase has to survive a save.
	f := Faction{Type: Settlers, Goodwill: 60, Relation: Neutral}
	for d := 0; d < 5; d++ {
		DailyDrift(&f)
	}
	if f.Goodwill != 58 || f.DriftDay != 0 {
		t.Fatalf("five high days: %+v", f)
	}
	for d := 0; d < 20; d++ {
		DailyDrift(&f)
	}
	if f.Goodwill != 50 {
		t.Fatalf("natural upper bound: %+v", f)
	}
	for d := 0; d < 65; d++ {
		DailyDrift(&f)
	}
	if f.Goodwill != 50 || len(f.History) != HistoryDays {
		t.Fatalf("stable inside natural band: %+v", f)
	}
	low := Faction{Type: Settlers, Goodwill: -60, Relation: Neutral}
	for d := 0; d < 25; d++ {
		DailyDrift(&low)
	}
	if low.Goodwill != -55 || low.DriftDay != 0 {
		t.Fatalf("five-week low drift: %+v", low)
	}
	resumed := low
	for d := 0; d < 25; d++ {
		DailyDrift(&low)
		DailyDrift(&resumed)
	}
	if !reflect.DeepEqual(low, resumed) {
		t.Fatal("drift phase replay diverged")
	}
	if low.Goodwill != -50 {
		t.Fatalf("natural minimum: %+v", low)
	}
	rough := Faction{Type: Drifters, Goodwill: -20, Relation: Hostile}
	for d := 0; d < 5; d++ {
		DailyDrift(&rough)
	}
	if rough.Goodwill != -22 || rough.Relation != Hostile {
		t.Fatalf("rough faction returns to hostile natural band: %+v", rough)
	}
	for _, ty := range []Type{Freebooters, SavageClans, Swarm, MechHive, AncientHostile} {
		fixed := Faction{Type: ty, Goodwill: -100, Relation: Hostile}
		for d := 0; d < 50; d++ {
			DailyDrift(&fixed)
		}
		Adjust(&fixed, 100, "gift")
		if fixed.Goodwill != -100 || fixed.Relation != Hostile {
			t.Errorf("fixed faction %s moved: %+v", ty, fixed)
		}
	}
	a := Faction{Type: Clans, Goodwill: 0}
	for i := 0; i < 7; i++ {
		Adjust(&a, -5, "raid repelled "+string(rune('a'+i)))
	}
	if a.Goodwill != -35 || len(a.Events) != EventCount || a.Events[0] != "raid repelled c" {
		t.Fatalf("events and goodwill: %+v", a)
	}
	Adjust(&a, -500, "")
	if a.Goodwill != -100 || a.Relation != Hostile {
		t.Fatalf("negative clamp: %+v", a)
	}
	Adjust(&a, 500, "")
	if a.Goodwill != 100 || a.Relation != Friendly {
		t.Fatalf("positive clamp: %+v", a)
	}
}

func TestFactionNamesUniqueAndOriginal(t *testing.T) {
	t.Parallel()
	if len(adjectives) < 60 || len(nouns) < 60 || PlaceCapacity() < 80 {
		t.Fatalf("word lists too short: %d adjectives, %d nouns, %d places", len(adjectives), len(nouns), PlaceCapacity())
	}
	for name, list := range map[string][]string{
		"adjectives": adjectives, "nouns": nouns, "bodyParts": bodyParts, "placeStarts": placeStarts,
		"placeEnds": placeEnds, "female": femaleNames, "male": maleNames, "surnames": surnames,
	} {
		seen := map[string]bool{}
		for _, w := range list {
			if w == "" || seen[w] {
				t.Errorf("%s: %q empty or duplicated", name, w)
			}
			seen[w] = true
			if p, bad := Blocked(w); bad {
				t.Errorf("%s: %q hits blocklist %q", name, w, p)
			}
		}
	}
	// Two thousand worlds' worth of names: unique inside each world, none
	// on the blocklist, and each follows its grammar.
	for seed := uint64(1); seed <= 300; seed++ {
		fs := Generate([]Slot{{Settlers, 4}, {Clans, 4}, {Drifters, 3}, {Freebooters, 3}, {Swarm, 1}}, randx.New(seed))
		seen := map[string]bool{}
		for _, f := range fs {
			if seen[f.Name] {
				t.Fatalf("seed %d: duplicate name %q", seed, f.Name)
			}
			seen[f.Name] = true
			for _, text := range []string{f.Name, f.Leader.Name, f.Leader.Title} {
				if p, bad := Blocked(text); bad {
					t.Fatalf("seed %d: %q hits blocklist %q", seed, text, p)
				}
			}
			ok := false
			switch f.Type {
			case Settlers:
				ok = strings.HasSuffix(f.Name, " Compact") && strings.HasPrefix(f.Name, "the ") || strings.HasSuffix(f.Name, " Commonwealth")
			case Clans:
				ok = strings.HasSuffix(f.Name, " Clan") || strings.HasPrefix(f.Name, "People of the ")
			case Drifters:
				ok = strings.HasSuffix(f.Name, " Drift") && strings.HasPrefix(f.Name, "the ") || strings.HasSuffix(f.Name, " Wanderers")
			case Freebooters:
				ok = strings.HasSuffix(f.Name, " Reavers") && strings.HasPrefix(f.Name, "the ") || strings.HasSuffix(f.Name, " Knives")
			case Swarm:
				ok = f.Name == "The Swarm"
			}
			if !ok {
				t.Fatalf("%v name %q breaks its grammar", f.Type, f.Name)
			}
		}
	}
	// Unique names survive an exhausted pool (numbered).
	used := map[string]bool{}
	rng := randx.New(1)
	for i := 0; i < PlaceCapacity()+30; i++ {
		n := uniquePlace(rng, used)
		if n == "" {
			t.Fatal("empty place name")
		}
	}
	if len(used) != PlaceCapacity()+30 {
		t.Errorf("place names repeat: %d distinct", len(used))
	}
	// Faction names also number past exhaustion.
	allUsed := map[string]bool{"The Swarm": true}
	if n := uniqueName(Swarm, randx.New(1), allUsed); n != "The Swarm II" {
		t.Errorf("clash numbering: %q", n)
	}
	if roman(4) != "IV" || roman(14) != "XIV" || roman(1994) != "MCMXCIV" {
		t.Error("roman numerals")
	}
}

// TestNamesAvoidBlocklist checks the originality lint (§0.4) itself and
// that the examples of §3.4 pass it.
func TestNamesAvoidBlocklist(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"Outlander Union", "the Outlander Union", "Pirate Gang", "Pirates of the Salt", "Savage Tribe", "Wild Tribe",
		"New Arrivals", "New Tribe", "Lost Tribe", "Rich Explorer", "Crashlanded", "Naked Brutality",
		"Cassandra Vane", "Phoebe Quay", "Randy Holloway", "Mechanoid Hive", "Thrumbo Clan", "tribe",
	} {
		if _, hit := Blocked(bad); !hit {
			t.Errorf("%q should be blocked", bad)
		}
	}
	for _, ok := range []string{
		"Orla Vane", "Bram Holloway", "Kestrel Quay", "Broken Hull", "Ember Clan", "Gilded Expedition", "Bare Hands",
		"the Ashen Compact", "Thornwick Commonwealth", "Kestrel-Eye Clan", "the Salt Drift", "the Rust Reavers", "Brandy Lane",
		"Randall", "Pirouette",
	} {
		if p, hit := Blocked(ok); hit {
			t.Errorf("%q wrongly blocked by %q", ok, p)
		}
	}
	if len(Blocklist()) < 30 {
		t.Error("blocklist shrank")
	}
	bl := Blocklist()
	bl[0] = "mutated"
	if Blocklist()[0] == "mutated" {
		t.Error("Blocklist returns a copy")
	}
}

func TestPickRaiderHostileOnly(t *testing.T) {
	t.Parallel()
	rng := randx.New(3)
	if PickRaider(nil, rng) != -1 {
		t.Error("no factions, no raider")
	}
	fs := []Faction{
		{Type: Settlers, Relation: Friendly, Settlements: []int{1, 2}},
		{Type: Clans, Relation: Neutral, Settlements: []int{3}},
		{Type: Swarm, Relation: Hostile, Settlements: []int{4}},
		{Type: Player, Relation: Hostile},
		{Type: Freebooters, Relation: Hostile, Settlements: []int{5}, Defeated: true},
	}
	for i := 0; i < 50; i++ {
		if got := PickRaider(fs, rng); got != -1 {
			t.Fatalf("picked %d: only hostile, living, human factions can raid", got)
		}
	}
	// Two hostile factions weighted 3:1 by settlements; a third without
	// settlements still counts as one when it is all there is.
	fs = append(fs,
		Faction{Type: Drifters, Relation: Hostile, Settlements: []int{6, 7, 8}},
		Faction{Type: Freebooters, Relation: Hostile, Settlements: []int{9}})
	hits := map[int]int{}
	for i := 0; i < 4000; i++ {
		got := PickRaider(fs, rng)
		if got != 5 && got != 6 {
			t.Fatalf("picked %d, a faction that cannot raid", got)
		}
		hits[got]++
	}
	if hits[5] < 2800 || hits[5] > 3200 || hits[6] < 800 || hits[6] > 1200 {
		t.Errorf("weights 3:1 expected, got %v", hits)
	}
	bare := []Faction{{Type: Clans, Relation: Hostile}}
	if PickRaider(bare, rng) != 0 {
		t.Error("a hostile faction with no placed settlements can still raid")
	}
	// Becoming friendly removes a faction from the pool.
	fs[5].Relation, fs[6].Relation = Neutral, Neutral
	if PickRaider(fs, rng) != -1 {
		t.Error("calmed factions do not raid")
	}
}

func TestSpacingAndCount(t *testing.T) {
	t.Parallel()
	for pop, want := range map[int]int{50: 8, 60: 8, 100: 6, 139: 6, 140: 4, 160: 4} {
		if got := Spacing(pop); got != want {
			t.Errorf("Spacing(%d) = %d, want %d", pop, got, want)
		}
	}
	// round(land x density x pop / 10k): 25,000 land tiles, normal.
	for _, c := range []struct {
		t               Type
		land, pop, want int
	}{
		{Settlers, 25000, 100, 3}, {Clans, 25000, 100, 4}, {Drifters, 25000, 100, 3},
		{FierceClans, 25000, 100, 4}, {SavageClans, 25000, 100, 4},
		{Freebooters, 25000, 100, 2}, {Clans, 25000, 160, 6}, {Clans, 25000, 50, 2},
		{Clans, 100, 100, 1}, {Swarm, 25000, 100, 0},
		{AncientQuiet, 25000, 100, 0}, {MechHive, 25000, 100, 0}, {Player, 25000, 100, 0},
	} {
		if got := SettlementCount(c.t, c.land, c.pop); got != c.want {
			t.Errorf("SettlementCount(%v, %d, %d) = %d, want %d", c.t, c.land, c.pop, got, c.want)
		}
	}
}

// TestBesiegedGoodwillCannotRise: during an attack goodwill only falls (MAP-16).
func TestBesiegedGoodwillCannotRise(t *testing.T) {
	f := Faction{Type: Settlers, Goodwill: -50, Besieged: true}
	Adjust(&f, 20, "a gift")
	DailyDrift(&f)
	if f.Goodwill != -50 || len(f.Events) != 0 {
		t.Fatalf("goodwill %d events %v, want -50 and none", f.Goodwill, f.Events)
	}
	Adjust(&f, -10, "an insult")
	if f.Goodwill != -60 {
		t.Fatalf("goodwill %d, want -60", f.Goodwill)
	}
	f.Besieged = false
	for day := 0; day < 5; day++ {
		DailyDrift(&f)
	}
	if f.Goodwill != -59 {
		t.Fatalf("goodwill %d, want natural drift +1 in five days", f.Goodwill)
	}
}

func TestEnemyBonusAt(t *testing.T) {
	f := Faction{ID: 1, Enemies: []int{3, 5}}
	if f.EnemyBonusAt(5) != EnemyBonus || f.EnemyBonusAt(4) != 0 || (Faction{}).EnemyBonusAt(3) != 0 {
		t.Fatal("EnemyBonusAt: only an enemy's people earn the bonus")
	}
}

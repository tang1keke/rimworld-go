package difficulty

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"
)

func TestPresetValues(t *testing.T) {
	t.Parallel()
	// One row per §3.3 column: scale, big, peaceful, adapt on/strength,
	// crop, mine, butcher, research, mood, infection, disease, revenge,
	// predators, weather, pain.
	type row struct {
		p                      Preset
		scale                  int
		big                    bool
		peaceful               int
		adapt                  bool
		strength               int
		crop, mine, butch, res int
		mood, inf, dis, rev    int
		predators, weather     bool
		pain                   int
	}
	rows := []row{
		{Wanderer, 10, false, 0, false, 100, 120, 120, 100, 120, 10, 30, 33, 25, false, false, 10},
		{Gentle, 30, true, 12, true, 100, 120, 120, 100, 120, 10, 50, 40, 100, false, false, 5},
		{Settled, 60, true, 8, true, 100, 100, 100, 100, 100, 5, 75, 67, 100, true, true, 0},
		{Seasoned, 100, true, 6, true, 90, 100, 100, 100, 100, 0, 100, 100, 100, true, true, 0},
		{Grim, 155, true, 4, true, 70, 95, 95, 90, 95, -5, 100, 105, 100, true, true, 0},
		{Merciless, 220, true, 3, true, 40, 80, 80, 80, 90, -10, 110, 111, 100, true, true, -5},
	}
	if len(Presets()) != len(rows) {
		t.Fatalf("Presets() = %d rows, want %d", len(Presets()), len(rows))
	}
	for _, r := range rows {
		s := Of(r.p)
		got := row{r.p, s.ThreatScale, s.BigThreats, s.PeacefulDays, s.Adaptation, s.AdaptStrength,
			s.CropYield, s.MineYield, s.ButcherYield, s.ResearchSpeed, s.MoodOffset, s.Infection,
			s.DiseaseFreq, s.AnimalRevenge, s.PredatorsHuntColonists, s.ExtremeWeather, s.PainTolerance}
		if got != r {
			t.Errorf("%s:\n got %+v\nwant %+v", r.p, got, r)
		}
		if s.TimeEscalation || s.IgnoreWealth {
			t.Errorf("%s: time escalation and ignore-wealth are off in every preset", r.p)
		}
		if bad := Valid(s); len(bad) != 0 {
			t.Errorf("%s outside custom ranges: %v", r.p, bad)
		}
		if Match(s) != r.p {
			t.Errorf("Match(%s) = %s", r.p, Match(s))
		}
	}
	for _, n := range Presets() {
		if n.Key == "" || n.Label == "" || n.Desc == "" {
			t.Errorf("preset %v lacks text: %+v", n.Preset, n)
		}
		if p, ok := Parse(n.Key); !ok || p != n.Preset {
			t.Errorf("Parse(%q) = %v %v", n.Key, p, ok)
		}
		if p, ok := Parse(strings.ToUpper(n.Key)); !ok || p != n.Preset {
			t.Errorf("Parse is case-insensitive: %q", n.Key)
		}
	}
	if p, ok := Parse("custom"); !ok || p != Custom {
		t.Error("custom parses")
	}
	if _, ok := Parse("easy"); ok {
		t.Error("unknown keys do not parse")
	}
	if Of(Custom) != Of(Seasoned) || Of(NumPresets) != Of(Seasoned) {
		t.Error("custom starts from seasoned")
	}
	if Match(Settings{ThreatScale: 123}) != Custom {
		t.Error("odd settings are custom")
	}
	if Preset(99).Key() != "" || Preset(99).String() != "" {
		t.Error("out-of-range preset has no key")
	}
}

func TestThreatPointsMonotonicInWealth(t *testing.T) {
	t.Parallel()
	for _, p := range []Preset{Gentle, Seasoned, Merciless} {
		s := Of(p)
		prev := 0
		for w := 0; w <= 400_000; w += 500 {
			got := ThreatPoints(s, w, 4, 0, 0)
			if got < prev {
				t.Fatalf("%s: points fell from %d to %d at wealth %d", p, prev, got, w)
			}
			prev = got
		}
	}
}

func TestThreatPointsFormula(t *testing.T) {
	t.Parallel()
	s := Of(Seasoned)
	cases := []struct {
		name         string
		s            Settings
		wealth, cols int
		years        float64
		adapt        int
		want         int
	}{
		{"floor", Of(Wanderer), 0, 1, 0, 0, 35},
		{"low wealth", s, 4000, 3, 0, 0, 80 + 63},        // 4000/50 + 3*(20+1); EAI-013: colonists by wealth
		{"knee one", s, 20000, 0, 0, 0, 400},             // 20000/50
		{"mid curve", s, 60000, 2, 0, 0, 400 + 500 + 65}, // 400 + 40000/80 + 2*32.5
		{"knee two", s, 100000, 0, 0, 0, 1400},
		{"top curve", s, 250000, 0, 0, 0, 1400 + 1000},
		{"rich colonists", s, 400000, 2, 0, 0, 1400 + 2000 + 120},  // past the last knee: 60 a colonist
		{"scale", Of(Merciless), 4000, 3, 0, 0, 315},               // 143 * 2.2
		{"adapt up", s, 4000, 3, 0, 40, 200},                       // 143 * 1.4 = 200.2
		{"adapt down", s, 4000, 3, 0, -30, 100},                    // 143 * 0.7 = 100.1
		{"adapt off ignored", Of(Wanderer), 100000, 0, 0, 40, 140}, // 1400 * 10% (no adapt)
		{"time replaces wealth", func() Settings { x := s; x.IgnoreWealth = true; return x }(), 999999, 2, 1.5, 0, 120 + 50},
		{"escalation", func() Settings { x := s; x.TimeEscalation = true; x.EscPerYear = 50; return x }(), 4000, 3, 2, 0, 286},
	}
	for _, c := range cases {
		if got := ThreatPoints(c.s, c.wealth, c.cols, c.years, c.adapt); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	if ThreatPoints(s, -5, -2, -1, 0) != 35 {
		t.Error("negative inputs hit the floor")
	}
}

func TestScale(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ pct, base, want int }{
		{100, 7, 7}, {130, 10, 13}, {50, 5, 3}, {50, 3, 2}, {80, 1, 1}, {20, 1, 0}, {0, 9, 0}, {-5, 9, 0}, {120, -3, 0},
	} {
		if got := Scale(c.pct, c.base); got != c.want {
			t.Errorf("Scale(%d, %d) = %d, want %d", c.pct, c.base, got, c.want)
		}
	}
}

func TestAdapterRisesAndFallsAndIsBounded(t *testing.T) {
	t.Parallel()
	s := Of(Seasoned)
	var a Adapter
	for i := 0; i < 400; i++ {
		a.Day(s, Setback{})
	}
	// With 90% adaptation impact, the 40-point target becomes 36.
	if a.Days != 120 || a.Value < 34 || a.Value > 36 {
		t.Fatalf("after a long quiet spell: %+v, want days 120 value 34..36", a)
	}
	a.Day(s, Setback{Deaths: 1})
	if a.Days != 95 {
		t.Errorf("a death takes 25 days: %d", a.Days)
	}
	for i := 0; i < 10; i++ {
		a.Day(s, Setback{Deaths: 1})
	}
	if a.Days != -40 {
		t.Errorf("days floor at -40: %d", a.Days)
	}
	for i := 0; i < 60; i++ {
		a.Day(s, Setback{Deaths: 1})
	}
	if a.Value > -25 {
		t.Errorf("value falls toward -27: %d", a.Value)
	}
	if a.Value < -27 {
		t.Errorf("value never below -27: %d", a.Value)
	}
	// Strength scales the target; off stops movement.
	weak := Of(Merciless) // 40% impact
	var b Adapter
	for i := 0; i < 400; i++ {
		b.Day(weak, Setback{})
	}
	if b.Value < 14 || b.Value > 16 {
		t.Errorf("40%% strength settles near 16, got %d", b.Value)
	}
	off := Of(Wanderer)
	var c Adapter
	for i := 0; i < 100; i++ {
		c.Day(off, Setback{})
	}
	if c.Value != 0 {
		t.Errorf("adaptation off keeps value at 0, got %d", c.Value)
	}
}

// directionChanges counts reversals of Value's movement inside every
// sliding window of w days and returns the worst window.
func worstReversals(vals []int8, w int) int {
	var dirs []int // sign of each day's change, 0 for no move
	for i := 1; i < len(vals); i++ {
		switch d := int(vals[i]) - int(vals[i-1]); {
		case d > 0:
			dirs = append(dirs, 1)
		case d < 0:
			dirs = append(dirs, -1)
		default:
			dirs = append(dirs, 0)
		}
	}
	worst := 0
	for i := 0; i+w <= len(dirs); i++ {
		changes, last := 0, 0
		for _, d := range dirs[i : i+w] {
			if d == 0 {
				continue
			}
			if last != 0 && d != last {
				changes++
			}
			last = d
		}
		worst = max(worst, changes)
	}
	return worst
}

func TestAdaptationNoFlap(t *testing.T) {
	t.Parallel()
	s := Of(Seasoned)
	patterns := map[string]func(day int) bool{
		"alternate":      func(d int) bool { return d%2 == 1 },
		"every third":    func(d int) bool { return d%3 == 0 },
		"every 10":       func(d int) bool { return d%10 == 0 },
		"every 25":       func(d int) bool { return d%25 == 24 },
		"every 50":       func(d int) bool { return d%50 == 49 },
		"two on two off": func(d int) bool { return d%4 < 2 },
	}
	for name, lost := range patterns {
		for _, days := range []int16{-40, 0, 20, 40, 60, 90, 120} {
			for _, val := range []int8{-30, -10, 0, 10, 25, 40} {
				a := Adapter{Days: days, Value: val}
				vals := []int8{a.Value}
				for d := 0; d < 60; d++ {
					a.Day(s, setbackForDeath(lost(d)))
					vals = append(vals, a.Value)
				}
				if w := worstReversals(vals, 6); w > 1 {
					t.Errorf("%s from days=%d value=%d: %d reversals inside 6 days: %v", name, days, val, w, vals)
				}
			}
		}
	}
	// The boundary itself: Value already within the dead band of its target
	// never moves however the daily result alternates around it.
	a := Adapter{Days: 40, Value: 10} // target = 10
	for d := 0; d < 60; d++ {
		a.Day(s, setbackForDeath(d%2 == 0))
	}
	if a.Value < -30 || a.Value > 40 {
		t.Errorf("value escaped its bounds: %d", a.Value)
	}
}

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()
	for _, n := range Presets() {
		got, err := Decode(Encode(n.Settings))
		if err != nil || got != n.Settings {
			t.Errorf("%s round trip: %+v %v", n.Key, got, err)
		}
	}
	custom := Of(Grim)
	custom.TimeEscalation, custom.IgnoreWealth, custom.EscPerYear, custom.MoodOffset = true, true, 150, -17
	if got, err := Decode(Encode(custom)); err != nil || got != custom {
		t.Errorf("custom round trip: %+v %v", got, err)
	}
	if got, err := Decode(""); err != nil || got != Of(Seasoned) {
		t.Errorf("empty decodes to seasoned: %+v %v", got, err)
	}
	if got, err := Decode("ts=77"); err != nil || got.ThreatScale != 77 || got.CropYield != 100 {
		t.Errorf("partial string keeps defaults: %+v %v", got, err)
	}
	bad := []string{"zz=1", "ts", "ts=abc", "ts=501", "bt=2", "mo=-21", "ts=1,ts=2", "ts=1,zz=2,yy=3"}
	for _, v := range bad {
		if _, err := Decode(v); err == nil {
			t.Errorf("Decode(%q) should fail", v)
		}
	}
	if _, err := Decode("zz=1,yy=2"); err == nil || !strings.Contains(err.Error(), "yy, zz") {
		t.Errorf("unknown keys are named: %v", err)
	}
}

func TestCustomDifficultyFiveHundredPercentLimits(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"ts", "cy", "my", "by", "rs", "if", "df", "ar"} {
		limit, ok := Limit(key)
		if !ok || limit.Max != 500 {
			t.Fatalf("%s max = %+v, ok %v; want 500", key, limit, ok)
		}
		if got, err := Decode(key + "=500"); err != nil || len(Valid(got)) != 0 {
			t.Fatalf("%s=500 was rejected: %+v, %v", key, got, err)
		}
		if _, err := Decode(key + "=501"); err == nil {
			t.Fatalf("%s=501 was accepted", key)
		}
	}
}

func TestClampValidLimit(t *testing.T) {
	t.Parallel()
	wild := Settings{ThreatScale: 9999, PeacefulDays: -4, CropYield: 1, MoodOffset: 99, ResearchSpeed: 0}
	c := Clamp(wild)
	if bad := Valid(c); len(bad) != 0 {
		t.Errorf("clamped settings are valid: %v", bad)
	}
	if c.ThreatScale != 500 || c.PeacefulDays != 0 || c.CropYield != 20 || c.MoodOffset != 20 || c.ResearchSpeed != 25 {
		t.Errorf("clamp: %+v", c)
	}
	if len(Valid(wild)) == 0 {
		t.Error("wild settings are invalid")
	}
	if l, ok := Limit("ts"); !ok || l.Min != 0 || l.Max != 500 {
		t.Errorf("ts limit: %+v %v", l, ok)
	}
	if _, ok := Limit("bt"); ok {
		t.Error("toggles have no range")
	}
	if _, ok := Limit("nope"); ok {
		t.Error("unknown key has no range")
	}
}

func TestSaveModeString(t *testing.T) {
	t.Parallel()
	if ReloadAnytime.String() != "reload_anytime" || Commitment.String() != "commitment" {
		t.Error("save mode strings are stable keys")
	}
}

// TestThreatPointsCapped (EAI-013): however rich, crowded, adapted and
// escalated a colony is, a threat never passes PointsCap, and the cap
// leaves an ordinary colony's points alone.
func TestThreatPointsCapped(t *testing.T) {
	t.Parallel()
	s := Of(Merciless)
	s.Adaptation, s.TimeEscalation, s.EscPerYear = true, true, 100
	if got := ThreatPoints(s, 50_000_000, 60, 10, 40); got != PointsCap {
		t.Fatalf("a huge colony gets %d points, want the cap %d", got, PointsCap)
	}
	prev := 0
	for w := 0; w <= 5_000_000; w += 50_000 {
		got := ThreatPoints(s, w, 20, 3, 40)
		if got > PointsCap || got < prev {
			t.Fatalf("wealth %d: %d points (after %d), want rising and at most %d", w, got, prev, PointsCap)
		}
		prev = got
	}
	if got := ThreatPoints(Of(Seasoned), 60000, 2, 0, 0); got >= PointsCap {
		t.Fatalf("an ordinary colony is capped at %d", got)
	}
}

// TestColonistPointsByWealth (EAI-013): the same people draw a bigger
// threat in a richer colony, beyond the wealth's own share, and never a
// smaller one; with wealth ignored a colonist counts flat.
func TestColonistPointsByWealth(t *testing.T) {
	t.Parallel()
	s := Of(Seasoned)
	share := func(w int) int { return ThreatPoints(s, w, 5, 0, 0) - ThreatPoints(s, w, 0, 0, 0) }
	poor, mid, rich := ThreatPoints(s, 0, 5, 0, 0), share(100_000), share(300_000) // no wealth: below the floor alone
	if poor != 100 || mid != 200 || rich != 300 {
		t.Fatalf("five colonists add %d, %d, %d at wealth 0, 100k, 300k; want 100, 200, 300", poor, mid, rich)
	}
	prev := 0
	for w := 20_000; w <= 600_000; w += 1000 {
		if got := share(w); got < prev {
			t.Fatalf("five colonists add %d at wealth %d, less than %d before", got, w, prev)
		}
		prev = share(w)
	}
	flat := s
	flat.IgnoreWealth = true
	if a, b := ThreatPoints(flat, 0, 4, 1, 0), ThreatPoints(flat, 900_000, 4, 1, 0); a != b || a != 4*25+80 {
		t.Fatalf("wealth ignored: %d and %d, want both %d", a, b, 4*25+80)
	}
}

func TestLegacyAndExplicitZeroDifficultyControls(t *testing.T) {
	t.Parallel()
	legacy := Settings{Adaptation: true, AdaptStrength: 100}
	if legacy.AdaptGrowthPercent() != 100 || legacy.FoodPoisonPercent() != 100 {
		t.Fatal("legacy settings must preserve the original neutral multipliers")
	}
	upgraded := Clamp(legacy)
	if upgraded.BalanceVersion != 1 || upgraded.AdaptGrowth != 100 || upgraded.FoodPoisonChance != 100 {
		t.Fatalf("legacy editor upgrade %+v", upgraded)
	}
	custom := Of(Seasoned)
	custom.AdaptGrowth, custom.FoodPoisonChance = 0, 0
	if custom.AdaptGrowthPercent() != 0 || custom.FoodPoisonPercent() != 0 {
		t.Fatal("explicit zero settings were treated as legacy")
	}
	got, err := Decode(Encode(custom))
	if err != nil || got != custom {
		t.Fatalf("custom zero setting round trip %+v: %v", got, err)
	}
	for _, v := range []string{"ag=101", "fp=501"} {
		if _, err := Decode(v); err == nil {
			t.Fatalf("%q should fail", v)
		}
	}
}

func TestAdaptationGrowthAndImpactIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		growth    int
		wantDays  int16
		wantCarry uint8
	}{
		{"stopped", 0, 0, 0},
		{"quarter", 25, 1, 0},
		{"threequarters", 75, 3, 0},
		{"full", 100, 4, 0},
		{"half fraction", 50, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Of(Seasoned)
			s.AdaptGrowth = tc.growth
			a := Adapter{}
			for i := 0; i < 4; i++ {
				a.Day(s, Setback{})
			}
			if a.Days != tc.wantDays || a.GrowthCarry != tc.wantCarry {
				t.Fatalf("4 days: %+v want days=%d carry=%d", a, tc.wantDays, tc.wantCarry)
			}
		})
	}
	s := Of(Seasoned)
	s.AdaptGrowth = 25
	a := Adapter{}
	a.Day(s, Setback{})
	saved := a
	for i := 0; i < 3; i++ {
		a.Day(s, Setback{})
		saved.Day(s, Setback{})
	}
	if a.Days != 1 || a.GrowthCarry != 0 || a != saved {
		t.Fatalf("fractional progress diverged on resume: %+v / %+v", a, saved)
	}
	for _, tc := range []struct{ impact, want int }{{0, 0}, {40, 16}, {100, 40}} {
		s := Of(Seasoned)
		s.AdaptStrength = tc.impact
		a := Adapter{Days: 120}
		for i := 0; i < 35; i++ {
			a.Day(s, Setback{})
		}
		if diff := int(a.Value) - tc.want; diff < -2 || diff > 2 {
			t.Fatalf("impact %d produced %d, want %d", tc.impact, a.Value, tc.want)
		}
	}
}

func BenchmarkAdaptationGrowthFraction(b *testing.B) {
	s := Of(Gentle)
	a := Adapter{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		a.Day(s, Setback{})
	}
	benchAdapt = a
}

func TestDifficultySaveVersionPreservesZeroAndLegacyDefaults(t *testing.T) {
	t.Parallel()
	type legacySettings struct {
		ThreatScale   int
		Adaptation    bool
		AdaptStrength int
	}
	encodeDecode := func(source any) Settings {
		t.Helper()
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(source); err != nil {
			t.Fatal(err)
		}
		var restored Settings
		if err := gob.NewDecoder(&buf).Decode(&restored); err != nil {
			t.Fatal(err)
		}
		return restored
	}
	legacy := encodeDecode(legacySettings{ThreatScale: 100, Adaptation: true, AdaptStrength: 100})
	if legacy.BalanceVersion != 0 || legacy.AdaptGrowthPercent() != 100 || legacy.FoodPoisonPercent() != 100 {
		t.Fatalf("legacy save %+v", legacy)
	}
	explicitZero := Of(Seasoned)
	explicitZero.AdaptGrowth = 0
	explicitZero.FoodPoisonChance = 0
	restored := encodeDecode(explicitZero)
	if restored != explicitZero || restored.AdaptGrowthPercent() != 0 || restored.FoodPoisonPercent() != 0 {
		t.Fatalf("explicit zero save changed: %+v", restored)
	}
	a := Adapter{Days: 4, Value: 2, GrowthCarry: 75}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(a); err != nil {
		t.Fatal(err)
	}
	var decoded Adapter
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != a {
		t.Fatalf("adaptation credit lost: %+v", decoded)
	}
}

func TestFieldKeysStayInSyncWithCodec(t *testing.T) {
	t.Parallel()
	for _, f := range fields {
		got, ok := findField(f.key)
		if !ok || got.key != f.key || got.min != f.min || got.max != f.max {
			t.Fatalf("field lookup out of sync for %s", f.key)
		}
	}
	if _, ok := findField("unknown"); ok {
		t.Fatal("unknown field accepted")
	}
}

func setbackForDeath(death bool) Setback {
	if death {
		return Setback{Deaths: 1}
	}
	return Setback{}
}

func TestAdaptationDistinguishesInjuriesDownsAndDeaths(t *testing.T) {
	t.Parallel()
	s := Of(Seasoned)
	cases := []struct {
		name string
		loss Setback
		days int
	}{
		{"quiet", Setback{}, 41},
		{"injury", Setback{Wounds: 16}, 38},
		{"downed", Setback{Downed: 1}, 32},
		{"one death", Setback{Deaths: 1}, 15},
		{"two deaths", Setback{Deaths: 2}, -10},
		{"combined", Setback{Wounds: 16, Downed: 1, Deaths: 1}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Adapter{Days: 40}
			a.Day(s, c.loss)
			if int(a.Days) != c.days {
				t.Fatalf("%s: %d want %d", c.name, a.Days, c.days)
			}
		})
	}
}

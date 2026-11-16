package game

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/difficulty"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/statehash"
	"rimworld-go/internal/domain/world/biome"
	"rimworld-go/internal/domain/world/planet"
)

// Save format v2 (NG-1 §5.8): v1 saves still load, a Setup colony's
// setup, factions, storyteller, adaptation and climate survive a save, and
// the header describes the colony.

const ng1V2Fixture = "testdata/ng1_v2.sav"

// saveLoadInfo saves g and loads it back.
func saveLoadInfo(t *testing.T, g *Game) (*Game, SaveInfo) {
	t.Helper()
	var buf bytes.Buffer
	if err := g.Save(&buf, "round-trip"); err != nil {
		t.Fatal(err)
	}
	h, info, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return h, info
}

// commitSetup is a small Broken Hull colony in commitment mode, landed and
// a few hours in.
func commitSetup(t *testing.T) *Game {
	t.Helper()
	g := buildSetup(t, setupFor(t, "broken_hull", func(s *Setup) {
		s.MapSize, s.SaveMode = 60, difficulty.Commitment
	}))
	stepUntilLanded(t, g)
	g.Step(3000)
	return g
}

func TestLoadV1MigratesBios(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/premotion_v1.sav")
	if err != nil {
		t.Fatal(err)
	}
	g, info, err := Load(bytes.NewReader(data))
	if err != nil || info.Version != 1 {
		t.Fatalf("load v1: %v (info %+v)", err, info)
	}
	if info.Commitment || info.Storyteller != "" || info.MapSize != 0 {
		t.Fatalf("a v1 header has no v2 fields: %+v", info)
	}
	if g.hasSetup() || len(g.ng.factions) != 0 || g.Story().Commitment {
		t.Fatal("a v1 save is a legacy colony: no setup, factions or commitment")
	}
	if len(g.pawnIDs) == 0 {
		t.Fatal("fixture has no colonists")
	}
	for _, id := range g.pawnIDs {
		name, _ := g.names.Get(id)
		if got, want := g.sk.bios[id], legacyBio(id, name); !reflect.DeepEqual(got, want) {
			t.Fatalf("pawn %d bio %+v, want the legacy bio %+v", id, got, want)
		}
	}
	// Saved again it is v2 and loads to the same state.
	h, info2 := saveLoadInfo(t, g)
	if info2.Version != SaveVersion || h.StateHash() != g.StateHash() {
		t.Fatalf("v1 -> v2 re-save: version %d, same state %v", info2.Version, h.StateHash() == g.StateHash())
	}
}

func TestSaveV2RoundTripSetup(t *testing.T) {
	t.Parallel()
	g := commitSetup(t)
	h, info := saveLoadInfo(t, g)
	if info.Version != SaveVersion {
		t.Fatalf("version %d", info.Version)
	}
	if !reflect.DeepEqual(h.exportSetupState(), g.exportSetupState()) {
		t.Fatal("setup state (setup, factions, story, plan, adaptation, climate, pods) changed in the round trip")
	}
	if h.climate != g.climate || !slices.Equal(h.grid.BlockedBits(), g.grid.BlockedBits()) {
		t.Fatal("climate or blocked tiles changed in the round trip")
	}
	if !reflect.DeepEqual(h.Story(), g.Story()) || !reflect.DeepEqual(h.Factions(), g.Factions()) {
		t.Fatal("story or faction read models changed")
	}
	// v2 bookkeeping the state hash leaves out: learning, need and research
	// fractions, passion work, drip counters, the postponed-work sweep.
	if !reflect.DeepEqual(h.exportSkillLive(), g.exportSkillLive()) || g.exportSkillLive() == nil {
		t.Fatal("skill state beyond the bios was not saved")
	}
	if !reflect.DeepEqual(h.stains.dripN, g.stains.dripN) || h.postponed.SweepAt != g.postponed.SweepAt {
		t.Fatal("drip counters or the postponed sweep were not saved")
	}
	// The loaded game continues exactly as the original.
	for i := 0; i < 8; i++ {
		if a, b := g.StateHash(), h.StateHash(); a != b {
			t.Fatalf("step %d: state hash %#x (continued) vs %#x (loaded)", i, a, b)
		}
		g.Step(250)
		h.Step(250)
	}
	sameState(t, "after 2000 ticks", g, h)
}

func TestWorldRegeneratedOnLoadMatches(t *testing.T) {
	t.Parallel()
	params := planet.Params{SeedText: "save-v2", Coverage: 30, Rain: 2, Temp: 2, Pop: 1, Factions: faction.DefaultSlots()}
	p := planet.Build(params)
	tile := p.RandomLanding(0)
	if tile < 0 {
		t.Fatal("no landing tile")
	}
	g := buildSetup(t, setupFor(t, "broken_hull", func(s *Setup) {
		s.MapSize, s.World, s.Tile, s.Site = 60, params, tile, p.Site(tile)
		s.Factions = p.Factions
	}))
	h, _ := saveLoadInfo(t, g)
	got := h.ng.setup
	if !reflect.DeepEqual(got.World, params) || got.Tile != tile {
		t.Fatalf("world params %+v tile %d, want %+v tile %d", got.World, got.Tile, params, tile)
	}
	q := planet.Build(got.World)
	if statehash.Of(*q) != statehash.Of(*p) {
		t.Fatal("the planet regenerated from the saved params differs")
	}
	if !reflect.DeepEqual(q.Site(got.Tile), got.Site) {
		t.Fatalf("regenerated site %+v, saved %+v", q.Site(got.Tile), got.Site)
	}
}

func TestClimateNotRederivedForSetup(t *testing.T) {
	t.Parallel()
	g := buildSetup(t, setupFor(t, "broken_hull", func(s *Setup) {
		s.MapSize = 60
		s.Site.Climate.YearMean -= 70 // a colder site than the biome's default
		s.Site.Climate.Amplitude += 30
	}))
	def := biome.DefOf(g.biome).Climate
	if g.climate == def {
		t.Fatal("the test site should not have the biome's default climate")
	}
	h, _ := saveLoadInfo(t, g)
	if h.climate != g.climate {
		t.Fatalf("climate %+v after load, want the site's %+v (not the biome's %+v)", h.climate, g.climate, def)
	}
}

func TestSaveInfoFields(t *testing.T) {
	t.Parallel()
	legacy := NewColony(Config{Seed: 3, Width: 40, Height: 30})
	li := legacy.Info("old")
	if li.Version != SaveVersion || li.MapSize != 40 || li.Biome != biome.DefOf(legacy.biome).Name ||
		li.Commitment || li.Storyteller != "" || li.Difficulty != "" {
		t.Fatalf("legacy info %+v", li)
	}
	g := commitSetup(t)
	want := SaveInfo{Version: SaveVersion, Name: "c", Date: g.Date().String(), Day: int(g.Tick()) / 60000,
		Colonists: 3, Commitment: true, Storyteller: g.Story().Storyteller, Difficulty: "Seasoned",
		MapSize: 60, Biome: "Temperate forest"}
	if got := g.Info("c"); !reflect.DeepEqual(got, want) {
		t.Fatalf("info\n got %+v\nwant %+v", got, want)
	}
	var buf bytes.Buffer
	if err := g.Save(&buf, "c"); err != nil {
		t.Fatal(err)
	}
	// Save stamps only the name; reading names the colony from it (v16).
	named := want
	named.Colony, named.ColonyName, named.Base = "slot:c", "c", "c"
	if got, err := ReadSaveInfo(bytes.NewReader(buf.Bytes())); err != nil || !reflect.DeepEqual(got, named) {
		t.Fatalf("header %+v, %v", got, err)
	}
	// EncodeSaveAs writes the client's stamp into the header as given.
	stamp := savefmt.Stamp{Name: "c-autosave-2", Colony: "col-7", ColonyName: "New Hope", Base: "c", Autosave: true,
		Season: "Aprimay", Build: "v9.9.9", Content: savefmt.Content{Hash: 42, Plugins: []string{"core"}}}
	var raw, file bytes.Buffer
	if err := EncodeSaveAs(g, &raw, stamp); err != nil {
		t.Fatal(err)
	}
	if err := savefmt.Compress(&file, raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	stamped := want
	stamp.Apply(&stamped)
	if got, err := ReadSaveInfo(bytes.NewReader(file.Bytes())); err != nil || !reflect.DeepEqual(got, stamped) {
		t.Fatalf("stamped header\n got %+v, %v\nwant %+v", got, err, stamped)
	}
	if h, info, err := Load(bytes.NewReader(file.Bytes())); err != nil || !reflect.DeepEqual(info, stamped) || h.StateHash() != g.StateHash() {
		t.Fatalf("a stamped save loads with header %+v, %v", info, err)
	}
	r := buildSetup(t, setupFor(t, "broken_hull", func(s *Setup) { s.MapSize = 60 }))
	if r.Info("r").Commitment {
		t.Fatal("a reload-anytime colony is not a commitment save")
	}
}

func TestSupportedSaveVersions(t *testing.T) {
	t.Parallel()
	for v, ok := range map[int]bool{0: false, 1: false, 2: false, 3: false, 4: false, 5: false, 6: false, 7: false, 8: false, 9: false, 10: false, 11: false, 12: false, 13: false, 14: false, 15: false, 16: false, 17: false, 18: true, 19: false} { // v18: canonical 12-skill model; v1-v17 require an offline converter
		if SupportedSaveVersion(v) != ok {
			t.Errorf("version %d supported = %v", v, !ok)
		}
	}
}

// TestNG1V2Fixture confirms the v2 header stays inspectable while the
// incompatible v2 body is rejected before gob tries to decode skill arrays.
func TestNG1V2Fixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(ng1V2Fixture)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ReadSaveInfo(bytes.NewReader(data))
	if err != nil || info.Version != 2 {
		t.Fatalf("header: %+v, %v", info, err)
	}
	if _, _, err := Load(bytes.NewReader(data)); !errors.Is(err, ErrSaveVersion) {
		t.Fatalf("old v2 save must be rejected as incompatible: %v", err)
	}
}

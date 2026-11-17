package modloader_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	modloaderadapter "rimworld-go/internal/adapters/outbound/modloader"
	"rimworld-go/internal/application/game"
	"rimworld-go/internal/application/game/contentid"
	"rimworld-go/internal/application/game/savefmt"
	appmodloader "rimworld-go/internal/application/modloader"
	"rimworld-go/internal/domain/economy/item"
)

func TestModExampleSaveLoadAndMismatch(t *testing.T) {
	t.Parallel()
	modDir := filepath.Join("testdata", "example.tools")
	registry, err := appmodloader.Load(modloaderadapter.SemanticVersions{}, modDir)
	if err != nil {
		t.Fatal(err)
	}
	g := game.NewWithModRuntime(game.Config{Width: 25, Height: 25, Seed: 11}, registry)
	if registry.ItemValue(item.Wood) != 17 || item.Wood.Value() == 17 {
		t.Fatalf("patch did not reach the game registry or mutated vanilla table: %d", registry.ItemValue(item.Wood))
	}
	if d, ok := registry.Lookup("example.tools:glowing_ore"); !ok || d.Value != 13 {
		t.Fatalf("game lost mod item: %+v %v", d, ok)
	}
	vanilla := game.New(game.Config{Width: 25, Height: 25, Seed: 11})
	if _, err := g.SpawnStack(item.Wood, 3, 5, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := vanilla.SpawnStack(item.Wood, 3, 5, 5); err != nil {
		t.Fatal(err)
	}
	// Upstream materializes its derived room grid on the first simulation
	// tick. Compare save/reload state after the same native initialization.
	g.Step(1)
	vanilla.Step(1)
	modWealth, _ := g.WealthOf(0, 1)
	vanillaWealth, _ := vanilla.WealthOf(0, 1)
	if modWealth != 3*17 || vanillaWealth != 3*item.Wood.Value() {
		t.Fatalf("actual ground-item wealth failed to use content registry: mod=%d, vanilla=%d", modWealth, vanillaWealth)
	}
	if g.StateHash() == vanilla.StateHash() {
		t.Fatal("active mod definitions must be bound to the authoritative state hash")
	}
	before := registry.Content()
	if before.Hash == contentid.Current().Hash || !slices.Equal(g.Info("modded").Content.Plugins, before.Plugins) {
		t.Fatalf("missing content stamp %+v", g.Info("modded"))
	}
	var buf bytes.Buffer
	if err := g.Save(&buf, "modded"); err != nil {
		t.Fatal(err)
	}
	info, err := game.ReadSaveInfo(bytes.NewReader(buf.Bytes()))
	if err != nil || !reflect.DeepEqual(info.Content, before) {
		t.Fatalf("save header content %+v / %v", info.Content, err)
	}
	var raw, spoofed bytes.Buffer
	if err := game.EncodeSaveAs(g, &raw, savefmt.Stamp{Name: "spoof", Content: savefmt.Content{Hash: 1, Plugins: []string{"bogus"}}}); err != nil {
		t.Fatal(err)
	}
	if err := savefmt.Compress(&spoofed, raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	spoofedInfo, err := game.ReadSaveInfo(bytes.NewReader(spoofed.Bytes()))
	if err != nil || !reflect.DeepEqual(spoofedInfo.Content, before) {
		t.Fatalf("caller replaced authoritative mod stamp %+v: %v", spoofedInfo.Content, err)
	}
	h, read, err := game.LoadWithModRuntime(bytes.NewReader(buf.Bytes()), registry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read.Content, before) || registry.ItemValue(item.Wood) != 17 {
		t.Fatalf("loaded mod mismatch, save=%+v, game=%+v, wood=%d", read.Content, registry.Content(), registry.ItemValue(item.Wood))
	}
	if gh, hh := g.StateHash(), h.StateHash(); gh != hh {
		t.Fatalf("modded save/load state hash differed: before=%016x, after=%016x, native modded wood=%d", gh, hh, registry.ItemValue(item.Wood))
	}
	var again bytes.Buffer
	if err := h.Save(&again, "modded"); err != nil {
		t.Fatal(err)
	}
	restamped, err := game.ReadSaveInfo(bytes.NewReader(again.Bytes()))
	if err != nil || !reflect.DeepEqual(restamped.Content, before) {
		t.Fatalf("resave lost active mod set %+v / %v", restamped.Content, err)
	}
	if _, _, err := game.Load(bytes.NewReader(buf.Bytes())); !errors.Is(err, appmodloader.ErrContentMismatch) {
		t.Fatalf("vanilla load did not refuse modded save: %v", err)
	}
	tmp := t.TempDir()
	for _, filename := range []string{"manifest.json", "definitions.json", "patches.json"} {
		raw, err := os.ReadFile(filepath.Join(modDir, filename))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, filename), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(tmp, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	asset, err := os.ReadFile(filepath.Join(modDir, "assets", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "assets", "note.txt"), asset, 0o600); err != nil {
		t.Fatal(err)
	}
	sameRegistry, err := appmodloader.Load(modloaderadapter.SemanticVersions{}, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := game.LoadWithModRuntime(bytes.NewReader(buf.Bytes()), sameRegistry); err != nil {
		t.Fatalf("identical mod data moved to another directory changed compatibility: %v", err)
	}
	patchFile := filepath.Join(tmp, "patches.json")
	patches, err := os.ReadFile(patchFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patchFile, bytes.ReplaceAll(patches, []byte("17"), []byte("18")), 0o600); err != nil {
		t.Fatal(err)
	}
	changedRegistry, err := appmodloader.Load(modloaderadapter.SemanticVersions{}, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := game.LoadWithModRuntime(bytes.NewReader(buf.Bytes()), changedRegistry); !errors.Is(err, appmodloader.ErrContentMismatch) {
		t.Fatalf("changed file did not refuse save: %v", err)
	}
}

func TestVanillaSaveRemainsUnchangedWithoutMods(t *testing.T) {
	t.Parallel()
	g := game.New(game.Config{Width: 25, Height: 25, Seed: 23})
	h := game.New(game.Config{Width: 25, Height: 25, Seed: 23})
	if g.StateHash() != h.StateHash() || appmodloader.ItemValue(nil, item.Wood) != item.Wood.Value() ||
		g.Info("plain").Content.Hash != 0 || len(g.Info("plain").Content.Plugins) != 0 {
		t.Fatal("core-only constructor or save header changed")
	}
	g.Step(1)
	h.Step(1)
	var buf bytes.Buffer
	if err := g.Save(&buf, "plain"); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := game.Load(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("vanilla save broke: %v", err)
	}
	if g.StateHash() != reloaded.StateHash() {
		t.Fatal("vanilla save/load changed authoritative state (including empty roof layer)")
	}
	moddedRegistry, err := appmodloader.Load(modloaderadapter.SemanticVersions{}, filepath.Join("testdata", "example.tools"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := game.LoadWithModRuntime(bytes.NewReader(buf.Bytes()), moddedRegistry); !errors.Is(err, appmodloader.ErrContentMismatch) {
		t.Fatalf("vanilla save with newly enabled mods was silently accepted: %v", err)
	}
	core, err := appmodloader.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := core.Lookup("core:wood")
	if !ok || definition.Value != item.Wood.Value() || definition.ID != "core:wood" {
		t.Fatalf("core definition lookup failed: %+v", definition)
	}
}

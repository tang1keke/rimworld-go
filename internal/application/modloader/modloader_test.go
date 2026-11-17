package modloader

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rimworld-go/internal/application/game/contentid"
	"rimworld-go/internal/domain/economy/item"
)

type testVersions struct{}

func (testVersions) Valid(version string) bool {
	if strings.HasPrefix(version, "v") {
		return false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if n, err := strconv.Atoi(part); err != nil || n < 0 {
			return false
		}
	}
	return true
}

func (testVersions) Compare(left, right string) int {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func TestExampleModRegistersAndPatchesCore(t *testing.T) {
	t.Parallel()
	core, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := core.Content(); !reflect.DeepEqual(got, contentid.Current()) {
		t.Fatalf("vanilla fingerprint changed: %+v", got)
	}
	if core.ItemValue(item.Wood) != item.Wood.Value() {
		t.Fatal("core-only market value changed")
	}
	r, err := Load(testVersions{}, filepath.Join("testdata", "example.tools"))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Active(); !slices.Equal(got, []string{"example.tools"}) {
		t.Fatalf("active mods %v", got)
	}
	if got := r.ItemValue(item.Wood); got != 17 {
		t.Fatalf("wood patch result %d, want 17", got)
	}
	ore, ok := r.Lookup("example.tools:glowing_ore")
	if !ok || ore.Value != 13 || ore.Source != "example.tools" {
		t.Fatalf("example mod did not register its item: %+v", ore)
	}
	wood, ok := r.Lookup("core:wood")
	if !ok || wood.Value != 17 || wood.Source != "core" || !slices.Equal(wood.PatchedBy, []string{"example.tools"}) {
		t.Fatalf("core patch provenance %+v", wood)
	}
	if len(r.Definitions()) != len(core.Definitions())+1 {
		t.Fatal("mod definition missing or overwrote a core item")
	}
	if r.Content().Hash == core.Content().Hash || !strings.HasPrefix(r.Content().Plugins[1], "example.tools@1.0.0#") {
		t.Fatalf("save content stamp %+v", r.Content())
	}
	b, err := r.Resource("example.tools", "assets/note.txt")
	if err != nil || !bytes.Contains(b, []byte("example data-only asset")) {
		t.Fatalf("resource %q: %v", b, err)
	}
	// The frozen registry must not leak mutable metadata to callers.
	wood.PatchedBy[0] = "tampered"
	entries := r.Definitions()
	entries[0].PatchedBy = append(entries[0].PatchedBy, "changed")
	manifest, _ := r.ManifestOf("example.tools")
	manifest.Capabilities[0] = "tampered"
	again, _ := r.Lookup("core:wood")
	if !slices.Equal(again.PatchedBy, []string{"example.tools"}) {
		t.Fatalf("definition alias escaped: %+v", again)
	}
	m, _ := r.ManifestOf("example.tools")
	if m.Capabilities[0] != CapabilityDefinitions {
		t.Fatalf("manifest alias escaped: %+v", m)
	}
}

type testMod struct {
	Manifest Manifest
	Defs     []itemInput
	Patches  []Patch
}

func tempMod(t *testing.T, root string, spec testMod) string {
	t.Helper()
	dir := filepath.Join(root, spec.Manifest.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", spec.Manifest)
	if spec.Defs != nil {
		write("definitions.json", spec.Defs)
	}
	if spec.Patches != nil {
		write("patches.json", spec.Patches)
	}
	return dir
}

func m(id string) Manifest {
	return Manifest{ID: id, Name: id, Version: "1.0.0", APIVersion: APIVersion}
}

func TestDeterministicDependencyOrderAndPatchPrecedence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ma := m("example.alpha")
	ma.Capabilities = []string{CapabilityDefinitions, CapabilityPatches}
	mb := m("example.beta")
	mb.Dependencies = []Dependency{{ID: ma.ID, MinVersion: "1.0.0"}}
	mb.Capabilities = []string{CapabilityPatches}
	mc := m("example.gamma")
	mc.LoadAfter = []string{"example.beta"}
	a := tempMod(t, root, testMod{Manifest: ma,
		Defs:    []itemInput{{ID: ma.ID + ":one", Label: "One", Value: 5}},
		Patches: []Patch{{Target: "core:wood", Op: "replace", Field: "value", Value: json.RawMessage("4")}}})
	b := tempMod(t, root, testMod{Manifest: mb,
		Patches: []Patch{{Target: "core:wood", Op: "replace", Field: "value", Value: json.RawMessage("9")}}})
	c := tempMod(t, root, testMod{Manifest: mc})
	first, err := Load(testVersions{}, c, b, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(testVersions{}, a, c, b)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first.Active(), []string{ma.ID, mb.ID, mc.ID}) {
		t.Fatalf("dependency order %v", first.Active())
	}
	if first.Content().Hash != second.Content().Hash ||
		!slices.Equal(first.Content().Plugins, second.Content().Plugins) ||
		!reflect.DeepEqual(first.Definitions(), second.Definitions()) {
		t.Fatal("filesystem/selection order affected content, indexes or provenance")
	}
	if first.ItemValue(item.Wood) != 9 {
		t.Fatalf("patch order did not use dependencies: %d", first.ItemValue(item.Wood))
	}
	bad := m("example.missing")
	bad.Dependencies = []Dependency{{ID: "example.absent"}}
	dir := tempMod(t, root, testMod{Manifest: bad})
	if _, err := Load(testVersions{}, dir); err == nil || !strings.Contains(err.Error(), "missing dependency") {
		t.Fatalf("missing dependency: %v", err)
	}
	ma.Dependencies = []Dependency{{ID: mb.ID}}
	tempMod(t, root, testMod{Manifest: ma, Defs: []itemInput{{ID: ma.ID + ":one", Label: "One", Value: 5}}})
	if _, err := Load(testVersions{}, a, b); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("dependency cycle: %v", err)
	}
}

func TestInvalidDefinitionsAndPathsAreRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mod  testMod
		want string
	}{
		{"namespace", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityDefinitions}}, Defs: []itemInput{{ID: "core:wood", Label: "Wrong", Value: 1}}}, "out-of-namespace"},
		{"duplicate def", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityDefinitions}}, Defs: []itemInput{{ID: "example.bad:one", Label: "One", Value: 1}, {ID: "example.bad:one", Label: "One", Value: 2}}}, "duplicate definition"},
		{"negative value", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityDefinitions}}, Defs: []itemInput{{ID: "example.bad:one", Label: "One", Value: -2}}}, "invalid item"},
		{"missing capability", testMod{Manifest: m("example.bad"), Defs: []itemInput{{ID: "example.bad:one", Label: "One", Value: 2}}}, "missing capability"},
		{"bad patch target", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityPatches}}, Patches: []Patch{{Op: "replace", Field: "value", Target: "core:nonexistent", Value: json.RawMessage("1")}}}, "does not exist"},
		{"bad patch field", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityPatches}}, Patches: []Patch{{Op: "replace", Field: "mass", Target: "core:wood", Value: json.RawMessage("1")}}}, "unsupported patch field"},
		{"null value", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityPatches}}, Patches: []Patch{{Op: "replace", Field: "value", Target: "core:wood", Value: json.RawMessage("null")}}}, "null patch"},
		{"api version", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: "2.0"}}, "unsupported API"},
		{"mod version", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "broken", APIVersion: APIVersion}}, "invalid version"},
		{"duplicate patch", testMod{Manifest: Manifest{ID: "example.bad", Name: "Bad", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityPatches}}, Patches: []Patch{{Op: "replace", Field: "value", Target: "core:wood", Value: json.RawMessage("1")}, {Op: "replace", Field: "value", Target: "core:wood", Value: json.RawMessage("2")}}}, "ambiguous duplicate patch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tempMod(t, t.TempDir(), tc.mod)
			if _, err := Load(testVersions{}, dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() = %v, want %s", err, tc.want)
			}
		})
	}
	root := t.TempDir()
	dir := tempMod(t, root, testMod{Manifest: Manifest{ID: "example.safe", Name: "Safe", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityResources}}})
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "assets", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(testVersions{}, dir); err == nil {
		t.Fatal("mod loader accepted an asset symlink escaping its root")
	}
	if err := os.Remove(filepath.Join(dir, "assets", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "safe.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(testVersions{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"assets/../manifest.json", "/etc/passwd", "assets/../../etc/passwd", "assets//safe.txt"} {
		if _, err := r.Resource("example.safe", path); err == nil {
			t.Errorf("accepted unsafe resource path %q", path)
		}
	}
	if _, err := r.Resource("example.safe", "assets/safe.txt"); err != nil {
		t.Fatalf("safe resource rejected: %v", err)
	}
	if _, err := Load(testVersions{}, dir, dir); err == nil || !strings.Contains(err.Error(), "duplicate mod ID") {
		t.Fatalf("duplicate mod ID: %v", err)
	}
}

func TestManifestRejectsIncompatibleAndWrongDependencyVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := m("example.a")
	b := m("example.b")
	a.Incompatible = []string{b.ID}
	dira := tempMod(t, root, testMod{Manifest: a})
	dirb := tempMod(t, root, testMod{Manifest: b})
	if _, err := Load(testVersions{}, dira, dirb); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("incompatible mods: %v", err)
	}
	a.Incompatible = nil
	a.Dependencies = []Dependency{{ID: b.ID, MinVersion: "2.0.0"}}
	tempMod(t, root, testMod{Manifest: a})
	if _, err := Load(testVersions{}, dira, dirb); err == nil || !strings.Contains(err.Error(), "needs >=") {
		t.Fatalf("version mismatch: %v", err)
	}
}

func TestLoadBeforeAndConditionalPatchValidation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := m("example.first")
	first.LoadBefore = []string{"example.second", "example.notselected"}
	first.Capabilities = []string{CapabilityPatches}
	second := m("example.second")
	second.Capabilities = []string{CapabilityPatches}
	firstDir := tempMod(t, root, testMod{Manifest: first, Patches: []Patch{{
		Target: "core:wood", Op: "replace", Field: "value", Value: json.RawMessage("5"), WhenMod: second.ID,
	}}})
	secondDir := tempMod(t, root, testMod{Manifest: second, Patches: []Patch{{
		Target: "core:wood", Op: "replace", Field: "value", Value: json.RawMessage("9"),
	}}})
	r, err := Load(testVersions{}, secondDir, firstDir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Active(), []string{first.ID, second.ID}) || r.ItemValue(item.Wood) != 9 {
		t.Fatalf("load-before ordering/conditional patch failed: %v / %d", r.Active(), r.ItemValue(item.Wood))
	}
	withoutSecond, err := Load(testVersions{}, firstDir)
	if err != nil {
		t.Fatal(err)
	}
	if withoutSecond.ItemValue(item.Wood) != item.Wood.Value() {
		t.Fatalf("absent optional patch condition applied: %d", withoutSecond.ItemValue(item.Wood))
	}
	bad := m("example.invalid")
	bad.Capabilities = []string{CapabilityPatches}
	badDir := tempMod(t, root, testMod{Manifest: bad, Patches: []Patch{{
		Target: "core:wood", Op: "replace", Field: "value", Value: json.RawMessage(`"broken"`), WhenMod: "example.notselected",
	}}})
	if _, err := Load(testVersions{}, badDir); err == nil || !strings.Contains(err.Error(), "invalid value patch") {
		t.Fatalf("inactive conditional patch bypassed schema validation: %v", err)
	}
}

func TestChangingAssetsChangesContentHash(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "example.tools")
	base, err := Load(testVersions{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	temp := tempMod(t, t.TempDir(), testMod{Manifest: Manifest{ID: "example.tools", Name: "Example Tools", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []string{CapabilityResources}}})
	if err := os.Mkdir(filepath.Join(temp, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "assets", "note.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Load(testVersions{}, temp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "assets", "note.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Resource("example.tools", "assets/note.txt"); err == nil || !strings.Contains(err.Error(), "changed after loading") {
		t.Fatalf("changed resource escaped frozen content hash: %v", err)
	}
	b, err := Load(testVersions{}, temp)
	if err != nil {
		t.Fatal(err)
	}
	if a.Content().Hash == b.Content().Hash || slices.Equal(a.Content().Plugins, b.Content().Plugins) || base.Content().Hash == a.Content().Hash {
		t.Fatal("content hashes ignored changed assets")
	}
	// Manifest syntax violations also fail closed, not ignored.
	if err := os.WriteFile(filepath.Join(temp, "manifest.json"), []byte(`{"id":"bad","version":"1.0.0","api_version":"1.0","name":"bad","unknown":123}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(testVersions{}, temp)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown manifest field was accepted: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("unexpected missing fixture")
	}
}

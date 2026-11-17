package modloader

import (
	"bytes"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"rimworld-go/internal/application/game/contentid"
	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/economy/item"
)

// Registry is frozen before the game starts. All public accessors return
// copies. Mod-defined items get compact indexes, not legacy item.Kind enum
// values: simulation support for spawning new kinds is a separate migration.
type Registry struct {
	items   map[string]ItemDefinition
	ordered []ItemDefinition
	coreIDs map[item.Kind]string
	mods    map[string]modRecord
	load    []string
	content savefmt.Content
}

// Load validates selected mod directories and builds a deterministic
// registry. An empty selection retains the core content fingerprint.
func Load(versions VersionPolicy, directories ...string) (*Registry, error) {
	if len(directories) > 0 && versions == nil {
		return nil, fmt.Errorf("semantic version policy is required")
	}
	selected := make(map[string]modRecord)
	for _, dir := range directories {
		rec, err := readMod(dir, versions)
		if err != nil {
			return nil, err
		}
		if _, ok := selected[rec.manifest.ID]; ok {
			return nil, fmt.Errorf("duplicate mod ID %q", rec.manifest.ID)
		}
		selected[rec.manifest.ID] = rec
	}
	order, err := resolve(selected, versions)
	if err != nil {
		return nil, err
	}
	r := &Registry{items: make(map[string]ItemDefinition), coreIDs: make(map[item.Kind]string), mods: selected, load: order}
	for _, d := range item.Definitions() {
		if _, exists := r.items[d.ID]; exists {
			return nil, fmt.Errorf("duplicate core item ID %q", d.ID)
		}
		r.items[d.ID] = ItemDefinition{ID: d.ID, Label: d.Label, Value: d.Value, Source: "core"}
		r.coreIDs[d.Kind] = d.ID
	}
	// Register all declarations before applying any patches, so a patch can
	// target a dependency even if the dependency itself declares a patch.
	for _, id := range order {
		rec := selected[id]
		for _, def := range rec.defs {
			if !defIDRE.MatchString(def.ID) || !strings.HasPrefix(def.ID, id+":") {
				return nil, fmt.Errorf("mod %s: invalid/out-of-namespace item ID %q", id, def.ID)
			}
			if def.Label == "" || def.Value < 0 || def.Value > 1_000_000_000 {
				return nil, fmt.Errorf("mod %s: invalid item definition %q", id, def.ID)
			}
			if _, exists := r.items[def.ID]; exists {
				return nil, fmt.Errorf("duplicate definition ID %q (mod %s)", def.ID, id)
			}
			r.items[def.ID] = ItemDefinition{ID: def.ID, Label: def.Label, Value: def.Value, Source: id}
		}
	}
	for _, id := range order {
		rec := selected[id]
		seen := make(map[string]bool)
		for _, p := range rec.patches {
			// Validate every patch even if its conditional mod is absent.
			// Otherwise an invalid declaration silently becomes active later.
			if p.Op != "replace" || !defIDRE.MatchString(p.Target) {
				return nil, fmt.Errorf("mod %s: unsupported patch operation/target %q/%q", id, p.Op, p.Target)
			}
			if bytes.Equal(bytes.TrimSpace(p.Value), []byte("null")) {
				return nil, fmt.Errorf("mod %s: null patch value for %q", id, p.Target)
			}
			var value int
			var label string
			switch p.Field {
			case "value":
				if err := json.Unmarshal(p.Value, &value); err != nil || value < 0 || value > 1_000_000_000 {
					return nil, fmt.Errorf("mod %s: invalid value patch for %q", id, p.Target)
				}
			case "label":
				if err := json.Unmarshal(p.Value, &label); err != nil || strings.TrimSpace(label) == "" {
					return nil, fmt.Errorf("mod %s: invalid label patch for %q", id, p.Target)
				}
			default:
				return nil, fmt.Errorf("mod %s: unsupported patch field %q", id, p.Field)
			}
			if p.WhenMod != "" {
				if !modIDRE.MatchString(p.WhenMod) {
					return nil, fmt.Errorf("mod %s: invalid patch condition %q", id, p.WhenMod)
				}
				if _, present := selected[p.WhenMod]; !present {
					continue
				}
			}
			target, exists := r.items[p.Target]
			if !exists {
				return nil, fmt.Errorf("mod %s: patch target %q does not exist", id, p.Target)
			}
			key := p.Target + ":" + p.Field
			if seen[key] {
				return nil, fmt.Errorf("mod %s: ambiguous duplicate patch for %q", id, key)
			}
			seen[key] = true
			if p.Field == "value" {
				target.Value = value
			} else {
				target.Label = label
			}
			target.PatchedBy = append(target.PatchedBy, id)
			r.items[p.Target] = target
		}
	}
	names := make([]string, 0, len(r.items))
	for id := range r.items {
		names = append(names, id)
	}
	slices.Sort(names)
	for _, id := range names {
		def := r.items[id]
		def.Index = uint32(len(r.ordered))
		r.items[id] = def
		r.ordered = append(r.ordered, def)
	}
	base := contentid.Current()
	if len(order) == 0 {
		r.content = base
		return r, nil
	}
	h := sha512.New()
	fmt.Fprintf(h, "core:%016x:", base.Hash)
	plugins := []string{"core"}
	for _, id := range order {
		rec := selected[id]
		entry := id + "@" + rec.manifest.Version + "#" + rec.hash
		plugins = append(plugins, entry)
		fmt.Fprintf(h, "mod:%s;", entry)
	}
	for _, def := range r.ordered {
		fmt.Fprintf(h, "item:%s:%s:%d;", def.ID, def.Label, def.Value)
	}
	r.content = savefmt.Content{Hash: binary.BigEndian.Uint64(h.Sum(nil)[:8]), Plugins: plugins}
	return r, nil
}

// resolve uses stable topological ordering: ID tie-breaks, and every
// dependency or present ordering hint becomes a directed edge.
func resolve(all map[string]modRecord, versions VersionPolicy) ([]string, error) {
	edges := make(map[string]map[string]bool, len(all))
	in := make(map[string]int, len(all))
	for id := range all {
		edges[id] = make(map[string]bool)
		in[id] = 0
	}
	add := func(a, b string) {
		if !edges[a][b] {
			edges[a][b] = true
			in[b]++
		}
	}
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		m := all[id]
		for _, dep := range m.manifest.Dependencies {
			other, ok := all[dep.ID]
			if !ok {
				return nil, fmt.Errorf("mod %s: missing dependency %q", id, dep.ID)
			}
			if dep.MinVersion != "" && versions.Compare(other.manifest.Version, dep.MinVersion) < 0 {
				return nil, fmt.Errorf("mod %s: dependency %s needs >= %s (have %s)", id, dep.ID, dep.MinVersion, other.manifest.Version)
			}
			add(dep.ID, id)
		}
		for _, other := range m.manifest.LoadAfter {
			if _, ok := all[other]; ok {
				add(other, id)
			}
		}
		for _, other := range m.manifest.LoadBefore {
			if _, ok := all[other]; ok {
				add(id, other)
			}
		}
		for _, other := range m.manifest.Incompatible {
			if _, ok := all[other]; ok {
				return nil, fmt.Errorf("mod %s incompatible with %s", id, other)
			}
		}
	}
	var order []string
	for len(order) < len(all) {
		var ready []string
		for id, n := range in {
			if n == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			remaining := make([]string, 0, len(in))
			for id := range in {
				remaining = append(remaining, id)
			}
			slices.Sort(remaining)
			return nil, fmt.Errorf("mod dependency/load-order cycle: %s", strings.Join(remaining, ", "))
		}
		slices.Sort(ready)
		id := ready[0]
		order = append(order, id)
		delete(in, id)
		for to := range edges[id] {
			in[to]--
		}
	}
	return order, nil
}

// Lookup returns a detached definition (including its patch provenance).
func (r *Registry) Lookup(id string) (ItemDefinition, bool) {
	if r == nil {
		return ItemDefinition{}, false
	}
	d, ok := r.items[id]
	d.PatchedBy = slices.Clone(d.PatchedBy)
	return d, ok
}

// Definitions returns immutable-by-copy definitions in runtime index order.
func (r *Registry) Definitions() []ItemDefinition {
	if r == nil {
		return nil
	}
	out := make([]ItemDefinition, len(r.ordered))
	for i, d := range r.ordered {
		d.PatchedBy = slices.Clone(d.PatchedBy)
		out[i] = d
	}
	return out
}

// ItemValue resolves the game's saved Kind through the same item registry
// that receives patches. An unknown Kind has its native zero value.
func (r *Registry) ItemValue(kind item.Kind) int {
	if r == nil {
		return kind.Value()
	}
	id, ok := r.coreIDs[kind]
	if !ok {
		return kind.Value()
	}
	return r.items[id].Value
}

// Content records the active mod set, versions and SHA-512 content hashes,
// in resolved order. Hash also covers the final patched definitions.
func (r *Registry) Content() savefmt.Content {
	if r == nil {
		return contentid.Current()
	}
	return savefmt.Content{Hash: r.content.Hash, Plugins: slices.Clone(r.content.Plugins)}
}

// Active returns mod IDs in resolved load order (without "core").
func (r *Registry) Active() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.load)
}

// ManifestOf returns the parsed, detached manifest for an active mod.
func (r *Registry) ManifestOf(id string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	rec, ok := r.mods[id]
	m := rec.manifest
	m.Dependencies = slices.Clone(m.Dependencies)
	m.LoadBefore = slices.Clone(m.LoadBefore)
	m.LoadAfter = slices.Clone(m.LoadAfter)
	m.Incompatible = slices.Clone(m.Incompatible)
	m.Authors = slices.Clone(m.Authors)
	m.Capabilities = slices.Clone(m.Capabilities)
	return m, ok
}

// Resource returns a mod-scoped asset with traversal and symlink checks.
// No access to a mod's manifest, another mod, or generated core resources.
func (r *Registry) Resource(id, relative string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("no mods loaded")
	}
	rec, ok := r.mods[id]
	if !ok {
		return nil, fmt.Errorf("unknown mod %q", id)
	}
	if !hasCapability(rec.manifest, CapabilityResources) {
		return nil, fmt.Errorf("mod %s has no resource capability", id)
	}
	if !strings.HasPrefix(relative, "assets/") {
		return nil, fmt.Errorf("asset path must be under assets/")
	}
	expected, ok := rec.assets[relative]
	if !ok {
		return nil, fmt.Errorf("asset %q not registered at mod load", relative)
	}
	data, err := boundedRead(rec.root, relative, 4<<20)
	if err != nil {
		return nil, err
	}
	if sha512.Sum512(data) != expected {
		return nil, fmt.Errorf("mod %s asset %q changed after loading", id, relative)
	}
	return data, nil
}

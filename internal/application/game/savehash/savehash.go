// Package savehash normalises a game save snapshot for deterministic hashing.
package savehash

import (
	"maps"
	"reflect"
	"slices"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/sim/statehash"
	"rimworld-go/internal/domain/world/temperature"
)

// CanonicalSliceValues replaces empty non-nil map slice values with nil,
// matching gob's decoded representation without mutating the source map.
func CanonicalSliceValues[K comparable, V ~[]E, E any](src map[K]V) map[K]V {
	var out map[K]V
	for key, value := range src {
		if value != nil && len(value) == 0 {
			if out == nil {
				out = maps.Clone(src)
			}
			out[key] = nil
		}
	}
	if out != nil {
		return out
	}
	return src
}

// Options contains the game-owned projections that cannot be read from the
// exported save fields alone.
type Options struct {
	Blocked      []int
	ReplacePrios bool
	Prios        any
	RNGStreams   map[string][]byte
	ExtraMaps    []ExtraMap
}

// ExtraMap is a saved map whose state is hashed recursively.
type ExtraMap struct {
	ID      uint32
	Tile    int
	Climate temperature.Climate
	State   any
	Options Options
}

type extraHash struct {
	ID      uint32
	Tile    int
	Climate temperature.Climate
	State   uint64
}

type infoV1 struct {
	Version        int
	Name, Date     string
	Day, Colonists int
}

// Fields returns the canonical, format-independent fields of a save snapshot.
func Fields(info savefmt.Info, state any, opts Options, digest func([]byte) uint64) map[string]any {
	v := reflect.ValueOf(state).Elem()
	t := v.Type()
	fields := map[string]any{"Info": infoV1{1, info.Name, info.Date, info.Day, info.Colonists}}
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			fields[t.Field(i).Name] = v.Field(i).Interface()
		}
	}
	research := ints(fields["ResearchAll"])
	if research == nil {
		research = ints(fields["Research"])
	}
	delete(fields, "ResearchAll")
	for len(research) > 0 && research[len(research)-1] == 0 {
		research = research[:len(research)-1]
	}
	fields["Research"] = research
	fields["Blocked"] = opts.Blocked
	delete(fields, "BlockedBits")
	for _, name := range [...]string{"SkillLive", "StainDrip", "PostSweep", "DoorsOriented", "View"} {
		delete(fields, name)
	}
	if opts.ReplacePrios {
		delete(fields, "PrioList")
		fields["Prios"] = opts.Prios
	}
	if opts.RNGStreams != nil {
		fields["RNGStreams"] = opts.RNGStreams
	}
	if len(opts.ExtraMaps) > 0 {
		hashes := make([]extraHash, len(opts.ExtraMaps))
		for i, m := range opts.ExtraMaps {
			hashes[i] = extraHash{
				ID: m.ID, Tile: m.Tile, Climate: m.Climate,
				State: statehash.OfFieldsWith(Fields(savefmt.Info{}, m.State, m.Options, digest), digest),
			}
		}
		fields["ExtraMaps"] = hashes
	}
	return fields
}

func ints(x any) []int {
	v := reflect.ValueOf(x)
	if !v.IsValid() || (v.Kind() != reflect.Array && v.Kind() != reflect.Slice) || v.Type().Elem().Kind() != reflect.Int {
		return nil
	}
	out := make([]int, v.Len())
	for i := range out {
		out[i] = int(v.Index(i).Int())
	}
	return slices.Clip(out)
}

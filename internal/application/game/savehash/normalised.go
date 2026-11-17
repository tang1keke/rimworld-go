// Package savehash normalises game-save fields before the deterministic
// simulation hash is calculated.
package savehash

import (
	"maps"
	"reflect"
	"slices"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/world/temperature"
)

// Header is the version-one subset of a save header used by state hashes.
type Header struct {
	Version        int
	Name, Date     string
	Day, Colonists int
}

// ExtraMap is an additional open map represented by its own state hash.
type ExtraMap struct {
	ID      uint32
	Tile    int
	Climate temperature.Climate
	State   uint64
}

// Input contains the save fields that require format-independent
// normalisation. State is a pointer to the encoded save-state struct.
type Input struct {
	Header        Header
	State         any
	Blocked       []int
	Content       savefmt.Content
	HasPrioList   bool
	Prios         any
	RNGStreams    map[string][]byte
	StreamSeedKey string
	ExtraMaps     []ExtraMap
}

// Normalised returns named save fields with legacy encodings folded into
// their current equivalents and derived/presentation fields omitted.
func Normalised(input Input) map[string]any {
	state := reflect.ValueOf(input.State).Elem()
	typ := state.Type()
	fields := map[string]any{"Info": input.Header}
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			fields[typ.Field(i).Name] = state.Field(i).Interface()
		}
	}
	if len(input.Content.Plugins) > 1 {
		fields["ModContent"] = input.Content
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
	fields["Blocked"] = input.Blocked
	delete(fields, "BlockedBits")
	if roofs := state.FieldByName("Roofs"); roofs.IsValid() && (roofs.Kind() == reflect.Slice || roofs.Kind() == reflect.Array) && roofs.Len() > 0 {
		empty := true
		for i := 0; i < roofs.Len(); i++ {
			if !roofs.Index(i).IsZero() {
				empty = false
				break
			}
		}
		if empty {
			delete(fields, "Roofs")
		}
	}
	for _, key := range [...]string{"SkillLive", "StainDrip", "PostSweep"} {
		delete(fields, key)
	}
	delete(fields, "DoorsOriented")
	if input.HasPrioList {
		delete(fields, "PrioList")
		fields["Prios"] = input.Prios
	}
	if streams := maps.Clone(input.RNGStreams); streams != nil {
		delete(streams, input.StreamSeedKey)
		fields["RNGStreams"] = streams
	}
	delete(fields, "View")
	if len(input.ExtraMaps) > 0 {
		fields["ExtraMaps"] = input.ExtraMaps
	}
	return fields
}

func ints(value any) []int {
	v := reflect.ValueOf(value)
	if !v.IsValid() || (v.Kind() != reflect.Array && v.Kind() != reflect.Slice) || v.Type().Elem().Kind() != reflect.Int {
		return nil
	}
	out := make([]int, v.Len())
	for i := range out {
		out[i] = int(v.Index(i).Int())
	}
	return slices.Clip(out)
}

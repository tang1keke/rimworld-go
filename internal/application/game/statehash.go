package game

import (
	"fmt"
	"maps"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/application/game/savehash"
	"rimworld-go/internal/domain/sim/statehash"
)

// CheckpointDigest is the application-owned port for hashing checkpoint bytes.
// Platform adapters supply the algorithm so the deterministic core stays dependency-free.
type CheckpointDigest func([]byte) uint64

// StateHash fingerprints the complete authoritative state: everything Save
// writes, hashed in the same normalised, format-independent form as a decoded save
// (statehash.OfFields: fields by name, zero-valued ones skipped, map
// entries ordered by key — Go's random map order makes raw gob bytes vary
// run to run). Research is hashed as one progress list without trailing
// zeros, whichever save fields carry it. So an additive save field (at its
// zero value) or a re-encoding does not move a golden pin; a change of
// behaviour does. It reads the state only.
func (g *Game) StateHash() uint64 {
	info, s, err := snapshotForSave(g, savefmt.Stamp{Name: "statehash"})
	if err != nil {
		panic(fmt.Errorf("state hash: %w", err))
	}
	return statehash.OfFields(normalisedFields(info, s))
}

// CheckpointHash returns the digest of the saved state using the supplied
// non-cryptographic algorithm. It uses the same canonical fields as StateHash.
func CheckpointHash(g *Game, digest CheckpointDigest) uint64 {
	if digest == nil {
		panic("checkpoint hash: digest is required")
	}
	info, s, err := snapshotForSave(g, savefmt.Stamp{Name: "statehash"})
	if err != nil {
		panic(fmt.Errorf("checkpoint hash: %w", err))
	}
	return statehash.OfFieldsWith(normalisedFieldsWith(info, s, digest), digest)
}

// normalisedFields is the save as named fields, with research folded into
// one "Research" progress list (the newest field that holds it).
//
// The header is hashed as its v1 shape (Version pinned to 1): its newer
// fields (map size, biome, storyteller, difficulty, save mode) are derived
// from state hashed below, and the version is format. The blocked tiles
// are hashed as the v1 index list whichever field carries them.
func normalisedFields(info SaveInfo, s *saveState) map[string]any {
	return normalisedFieldsWith(info, s, statehash.Bytes)
}

func normalisedFieldsWith(info SaveInfo, s *saveState, digest CheckpointDigest) map[string]any {
	opts := savehash.Options{Blocked: s.blockedTiles()}
	if s.PrioList != nil {
		opts.ReplacePrios, opts.Prios = true, importPrios(s.PrioList)
	}
	if streams := maps.Clone(s.RNGStreams); streams != nil {
		delete(streams, streamSeedKey)
		opts.RNGStreams = streams
	}
	if len(s.ExtraMaps) > 0 {
		opts.ExtraMaps = make([]savehash.ExtraMap, len(s.ExtraMaps))
		for i := range s.ExtraMaps {
			m := &s.ExtraMaps[i]
			opts.ExtraMaps[i] = savehash.ExtraMap{
				ID: m.ID, Tile: m.Tile, Climate: m.Climate, State: &m.State,
				Options: savehash.Options{Blocked: m.State.blockedTiles()},
			}
		}
	}
	return savehash.Fields(info, s, opts, digest)
}

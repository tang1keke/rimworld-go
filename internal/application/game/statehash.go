package game

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"rimworld-go/internal/application/game/savehash"
	"rimworld-go/internal/application/game/saveutil"
	"rimworld-go/internal/domain/sim/statehash"
)

// StateHash fingerprints the complete authoritative state: everything Save
// writes, decoded and hashed in a normalised, format-independent form
// (statehash.OfFields: fields by name, zero-valued ones skipped, map
// entries ordered by key — Go's random map order makes raw gob bytes vary
// run to run). Research is hashed as one progress list without trailing
// zeros, whichever save fields carry it. So an additive save field (at its
// zero value) or a re-encoding does not move a golden pin; a change of
// behaviour does. It reads the state only.
func (g *Game) StateHash() uint64 {
	var buf bytes.Buffer
	if err := EncodeSave(g, &buf, "statehash"); err != nil { // raw gob: no compression to undo
		panic(fmt.Errorf("state hash: %w", err))
	}
	dec := gob.NewDecoder(&buf)
	var info SaveInfo
	var s saveState
	if err := dec.Decode(&info); err != nil {
		panic(fmt.Errorf("state hash: %w", err))
	}
	if err := dec.Decode(&s); err != nil {
		panic(fmt.Errorf("state hash: %w", err))
	}
	return statehash.OfFields(normalisedFields(info, &s))
}

// normalisedFields keeps the game-facing save decode at the boundary while
// delegating canonical field handling to the small savehash package.
func normalisedFields(info SaveInfo, s *saveState) map[string]any {
	extra := make([]savehash.ExtraMap, len(s.ExtraMaps))
	for i := range s.ExtraMaps {
		m := &s.ExtraMaps[i]
		extra[i] = savehash.ExtraMap{ID: m.ID, Tile: m.Tile, Climate: m.Climate,
			State: statehash.OfFields(normalisedFields(SaveInfo{}, &m.State))}
	}
	var prios any
	if s.PrioList != nil {
		prios = saveutil.ImportPrios(s.PrioList)
	}
	return savehash.Normalised(savehash.Input{
		Header: savehash.Header{Version: 1, Name: info.Name, Date: info.Date, Day: info.Day, Colonists: info.Colonists},
		State:  s, Blocked: s.blockedTiles(), Content: info.Content,
		HasPrioList: s.PrioList != nil, Prios: prios,
		RNGStreams: s.RNGStreams, StreamSeedKey: streamSeedKey, ExtraMaps: extra,
	})
}

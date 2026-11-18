package animalview

import (
	"rimworld-go/internal/application/game/visit"
	"rimworld-go/internal/domain/faction"
)

// Visiting annotates a physical caravan animal with its saved owner.
func (v *View) Visiting(groups []visit.Group, fs []faction.Faction) {
	ref := visit.PackFaction(groups, v.ID)
	if ref <= 0 || ref > len(fs) {
		return
	}
	f := fs[ref-1]
	v.Faction, v.Emblem = f.Name, faction.DefOf(f.Type).Emblem()
}

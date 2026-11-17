package dossierview

import (
	"rimworld-go/internal/application/game/animalview"
	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/faction"
)

// AnimalFaction sets the card's relationship and origin from the
// already cached species facts; ownership stays in the animal state.
func (c *Card) AnimalFaction(species animal.Species, tame, hostile bool) {
	info := animalview.Of(species)
	switch {
	case tame:
		c.Relation, c.FactionEmblem = RelationOwn, faction.DefOf(faction.Player).Emblem()
	case info.Faction != "":
		c.Relation, c.Faction, c.FactionEmblem = RelationFoe, info.Faction, info.Emblem
	case hostile:
		c.Relation = RelationFoe
	default:
		c.Relation = RelationWild
	}
}

// HiddenOrigin fills the saved origin of a human without a world faction.
func (c *Card) HiddenOrigin(origin faction.Type) {
	if origin == faction.Player || origin >= faction.NumTypes {
		return
	}
	d := faction.DefOf(origin)
	if d.Hidden {
		c.Faction, c.FactionEmblem = d.Label, d.Emblem()
	}
}

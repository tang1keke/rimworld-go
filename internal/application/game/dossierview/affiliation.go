package dossierview

import (
	"rimworld-go/internal/application/game/animalview"
	"rimworld-go/internal/application/game/visit"
	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/entity"
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

// CaravanAnimal reflects the visiting pack's real foreign allegiance.
// The animal remains untamed by the colony.
func (c *Card) CaravanAnimal(f faction.Faction) {
	c.Faction = f.Name
	c.FactionEmblem = faction.DefOf(f.Type).Emblem()
	c.Role = "pack animal"
	switch f.Relation {
	case faction.Hostile:
		c.Relation = RelationFoe
	case faction.Friendly:
		c.Relation = RelationAlly
	default:
		c.Relation = RelationNone
	}
}

// PackFromVisit applies the existing saved group affiliation to an animal
// card without confusing a foreign carrier with player-owned livestock.
func (c *Card) PackFromVisit(groups []visit.Group, factions []faction.Faction, id entity.EntityID) {
	ref := visit.PackFaction(groups, id)
	if ref > 0 && ref <= len(factions) {
		c.CaravanAnimal(factions[ref-1])
	}
}

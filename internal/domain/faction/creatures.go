package faction

import "rimworld-go/internal/domain/animal"

// Creature returns the hidden faction a non-human species belongs to.
// Species is already authoritative and saved. Wildlife has no faction.
// Core: Factions, Mechanoid hive, Insectoids (wiki).
func Creature(s animal.Species) (Type, bool) {
	switch {
	case animal.IsMech(s):
		return MechHive, true
	case animal.DefOf(s).Hive:
		return Swarm, true
	default:
		return Player, false
	}
}

// HiddenEnemies is the permanent hostility of the two non-human Core
// factions towards all other factions (wiki: Mechanoid hive; Insectoids).
// Human-to-human wars remain on Faction.Enemies.
func HiddenEnemies(a, b Type) bool {
	return a != b && (a == MechHive || a == Swarm || b == MechHive || b == Swarm)
}

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

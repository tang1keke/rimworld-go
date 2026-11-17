package tabview

import (
	"rimworld-go/internal/application/game/maps"
	"rimworld-go/internal/application/game/travel"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/sim/entity"
)

// BuildWorldMap projects the authoritative caravans into a detached read
// model. The game supplies its name, health, cargo and market views.
func BuildWorldMap(tile int, caravans []maps.Caravan,
	name, health func(entity.EntityID) string,
	cargo func(uint64) []item.Stack,
	market func(*maps.Caravan) (MarketView, bool)) WorldMap {
	v := WorldMap{Tile: tile}
	for i := range caravans {
		c := &caravans[i]
		cv := travel.CaravanView{
			ID: c.C.ID, Tile: c.C.Tile, Dest: c.C.Dest,
			Progress: c.C.Progress / 10, State: c.C.State.Label(),
		}
		if c.Back {
			cv.Dest = c.Home
		}
		for _, raw := range c.C.Members {
			id := entity.EntityID(raw)
			cv.Members = append(cv.Members, name(id))
			cv.Health = append(cv.Health, health(id))
		}
		cv.Cargo = cargo(c.C.ID)
		v.Caravans = append(v.Caravans, cv)
		if m, ok := market(c); ok {
			v.Markets = append(v.Markets, m)
		}
	}
	return v
}

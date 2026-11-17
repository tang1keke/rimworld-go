package behaviorhost

import (
	"rimworld-go/internal/application/game"
	"rimworld-go/internal/application/game/behavior"
	"rimworld-go/internal/domain/building/zone"
	"rimworld-go/internal/domain/world/terrain"
	"slices"
)

// world snapshots every open map through public read models, never exposing
// an entity store, mapState, allocator, scheduler or authoritative pointer.
func (h *Runner) world() behavior.World {
	g := h.g
	out := behavior.World{Tick: uint64(g.Tick())}
	selected := g.ViewedMap()
	defer g.ViewMap(selected)
	for _, location := range g.Maps() {
		g.ViewMap(location.ID)
		v := g.World()
		m := behavior.Map{ID: uint32(location.ID), Width: g.Width(), Height: g.Height()}
		for _, p := range g.Snapshot() {
			m.Pawns = append(m.Pawns, behavior.Pawn{ID: uint64(p.ID), Name: p.Name, X: p.X, Y: p.Y,
				Alive: p.Status != "dead", Colonist: true, Job: p.Job,
				CanConstruct: p.WorkConstruct, CanMine: p.WorkMine, CanHaul: p.WorkHaul, CanCraft: p.WorkCook})
		}
		for _, b := range v.Buildings {
			m.Buildings = append(m.Buildings, behavior.Building{ID: uint64(b.ID), Build: b.Kind, Stuff: b.Stuff,
				Facing: b.Facing, X: b.X, Y: b.Y, Deconstruct: b.Deconstruct, Ruin: b.Ruin, Hostile: b.Hostile})
		}
		for _, f := range v.Frames {
			m.Frames = append(m.Frames, behavior.Frame{ID: uint64(f.ID), Build: f.Kind, Stuff: f.Stuff,
				X: f.X, Y: f.Y, Need: f.Need, Delivered: f.Wood, WorkLeft: f.WorkLeft})
		}
		for _, it := range v.Items {
			m.Items = append(m.Items, behavior.Item{ID: uint64(it.ID), Kind: it.Kind, Count: it.Qty,
				X: it.X, Y: it.Y, Forbidden: it.Forbidden, Unclaimed: it.Unclaimed, HaulMarked: it.HaulMarked})
		}
		for _, z := range v.Zones {
			prio := 0
			for _, p := range zone.Priorities {
				if p.String() == z.Priority {
					prio = int(p)
					break
				}
			}
			m.Stockpiles = append(m.Stockpiles, behavior.Stockpile{ID: uint64(z.ID), Kind: z.Kind,
				Priority: prio, Cells: slices.Clone(z.Cells)})
		}
		mark := make(map[[2]int]bool, len(v.Mines))
		for _, cell := range v.Mines {
			mark[cell] = true
		}
		stones, ores := game.StoneNames(), game.OreNames()
		for y := 0; y < m.Height; y++ {
			for x := 0; x < m.Width; x++ {
				idx := y*m.Width + x
				if idx >= len(v.Terrain) || !terrain.IsRockWall(terrain.Type(v.Terrain[idx])) {
					continue
				}
				stone, ore := "", ""
				if idx < len(v.StoneType) && int(v.StoneType[idx]) < len(stones) {
					stone = stones[v.StoneType[idx]]
				}
				if idx < len(v.Ore) && int(v.Ore[idx]) < len(ores) {
					ore = ores[v.Ore[idx]]
				}
				m.Rocks = append(m.Rocks, behavior.Rock{X: x, Y: y, Stone: stone, Ore: ore, MineMarked: mark[[2]int{x, y}]})
			}
		}
		for _, room := range v.Rooms {
			m.Rooms = append(m.Rooms, behavior.Room{ID: room.ID, Cells: room.Cells, Role: room.Role, Roofed: room.Roofed})
		}
		for _, p := range v.Research {
			m.Research = append(m.Research, behavior.Research{Name: p.Name, Done: p.Done, Available: p.Available, Selected: p.Current})
		}
		out.Maps = append(out.Maps, m)
	}
	return out
}

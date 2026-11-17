package pathstage

import (
	"rimworld-go/internal/domain/ai/pathing"
	"rimworld-go/internal/domain/world/region"
)

// PlainQuery hides a query's optional reach and bitset extensions.
type PlainQuery struct{ pathing.Query }

// CheckReach proves a negative region answer with an unextended A* query
// when the caller's self-check says to sample it.
func CheckReach[Q pathing.Query](q Q, sx, sy, tx, ty int, got, verify bool) bool {
	if verify {
		if _, ok := pathing.Find(PlainQuery{q}, sx, sy, tx, ty); ok {
			panic("regions: Reach said no, A* found a path")
		}
	}
	return got
}

// MoverLayer is the region layer used by a pawn's path query.
func MoverLayer(sapper, hostile bool) region.Layer {
	switch {
	case sapper:
		return region.Sapper
	case hostile:
		return region.Hostile
	}
	return region.Colonist
}

// ReachRegions answers whether the start can reach a target using region
// labels. A blocked target is reachable when the start is beside it or a
// walkable neighbour belongs to the start's component.
func ReachRegions(m *region.Map, layer region.Layer, sx, sy, tx, ty int) bool {
	if sx == tx && sy == ty {
		return true
	}
	if m.Label(layer, tx, ty) >= 0 {
		return m.Reach(layer, sx, sy, tx, ty)
	}
	if pathing.Adjacent(sx, sy, tx, ty) {
		return true
	}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if (dx != 0 || dy != 0) && m.Label(layer, tx+dx, ty+dy) >= 0 && m.Reach(layer, sx, sy, tx+dx, ty+dy) {
				return true
			}
		}
	}
	return false
}

// SameBacking reports whether two cached slices are the same backing set.
func SameBacking[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

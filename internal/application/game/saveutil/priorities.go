package saveutil

import (
	"slices"

	"rimworld-go/internal/domain/ai/work"
	"rimworld-go/internal/domain/sim/entity"
)

// ExportPrios persists work priorities in work-type order with detached values.
func ExportPrios(m map[entity.EntityID]work.Priorities) map[entity.EntityID][]uint8 {
	out := make(map[entity.EntityID][]uint8, len(m))
	for id, p := range m {
		out[id] = slices.Clone(p[:])
	}
	return out
}

// ImportPrios loads legacy short lists without losing the Grow/PlantCut split.
func ImportPrios(m map[entity.EntityID][]uint8) map[entity.EntityID]work.Priorities {
	out := make(map[entity.EntityID]work.Priorities, len(m))
	for id, l := range m {
		var p work.Priorities
		copy(p[:], l)
		if len(l) <= int(work.PlantCut) {
			p[work.PlantCut] = p[work.Grow] // FOOD-2: split from growing
		}
		out[id] = p
	}
	return out
}

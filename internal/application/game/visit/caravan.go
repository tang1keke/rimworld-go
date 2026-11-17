package visit

import (
	"slices"

	"rimworld-go/internal/domain/economy/trade"
	"rimworld-go/internal/domain/sim/entity"
)

// Cargo records which physical carrier owns a share of a caravan's
// virtual sale stock. Stock is the sole authority on sale quantities.
type Cargo struct {
	Owner  entity.EntityID
	Lines  []trade.Line
	Silver int
}

// AssignCargo partitions sale stock among living carriers in sorted order.
func AssignCargo(st trade.Stock, owners []entity.EntityID) []Cargo {
	if len(owners) == 0 {
		return nil
	}
	owners = slices.Clone(owners)
	slices.Sort(owners)
	owners = slices.Compact(owners)
	out := make([]Cargo, len(owners))
	for i, id := range owners {
		out[i].Owner = id
	}
	for i, ln := range st.Lines {
		out[i%len(out)].Lines = append(out[i%len(out)].Lines, ln)
	}
	out[0].Silver = st.Silver
	return out
}

// LoseCargo removes a dead carrier's quantities from both the assignments
// and the authoritative stock exactly once.
func LoseCargo(st *trade.Stock, cargo []Cargo, owner entity.EntityID) (remaining []Cargo, lines []trade.Line, silver int) {
	for i := range cargo {
		if cargo[i].Owner != owner {
			continue
		}
		lines, silver = slices.Clone(cargo[i].Lines), cargo[i].Silver
		for _, ln := range lines {
			st.AddGoods(ln.Goods(), -ln.Qty)
		}
		st.Silver -= silver
		return slices.Delete(cargo, i, i+1), lines, silver
	}
	return cargo, nil, 0
}

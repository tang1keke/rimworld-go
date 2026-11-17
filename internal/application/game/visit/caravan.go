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

// Caravans returns active and parked groups in their saved arrival order.
func Caravans[T any](current *T, others []*T) []*T {
	if current == nil {
		return others
	}
	out := make([]*T, 0, 1+len(others))
	out = append(out, current)
	return append(out, others...)
}

// FindCaravan checks the current seller first, then the parked groups.
func FindCaravan[T any](current *T, others []*T, match func(*T) bool) *T {
	if current != nil && match(current) {
		return current
	}
	for _, c := range others {
		if match(c) {
			return c
		}
	}
	return nil
}

func RemoveCaravan[T any](current **T, others *[]*T, c *T) {
	if *current == c {
		*current = nil
		if len(*others) > 0 {
			*current, *others = (*others)[0], slices.Delete(*others, 0, 1)
		}
		return
	}
	for i, other := range *others {
		if other == c {
			*others = slices.Delete(*others, i, i+1)
			return
		}
	}
}

// RetireCaravan stops selling while keeping the party's physical carriers
// accountable for their stock until the group is fully off-map.
func RetireCaravan[T any](current **T, others *[]*T, c *T, nextOK func(*T) bool) {
	if *current != c {
		return
	}
	*current = nil
	for i, next := range *others {
		if nextOK(next) {
			*current = next
			*others = slices.Delete(*others, i, i+1)
			break
		}
	}
	*others = append(*others, c)
}

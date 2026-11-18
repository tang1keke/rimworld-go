package visit

import (
	"slices"

	"rimworld-go/internal/domain/economy/item"
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

// SettleCargo applies a validated trade to the existing carrier manifest.
// Goods leave in ascending owner-ID order; newly received goods and silver
// belong to the trader. All unaffected lines remain with their owners.
// The caller applies the same trade.Result to Stock after this succeeds.
func SettleCargo(cargo []Cargo, trader entity.EntityID, res trade.Result) bool {
	traderIndex := slices.IndexFunc(cargo, func(c Cargo) bool { return c.Owner == trader })
	if traderIndex < 0 {
		return false
	}
	for _, bought := range res.Buy {
		available := 0
		for _, hold := range cargo {
			for _, line := range hold.Lines {
				if line.Goods() == bought.Goods() {
					available += line.Qty
				}
			}
		}
		if available < bought.Qty {
			return false
		}
	}
	if res.Silver > 0 {
		available := 0
		for _, hold := range cargo {
			available += hold.Silver
		}
		if available < res.Silver {
			return false
		}
	}
	for _, bought := range res.Buy {
		left := bought.Qty
		for i := range cargo {
			for j := 0; j < len(cargo[i].Lines) && left > 0; {
				line := &cargo[i].Lines[j]
				if line.Goods() != bought.Goods() {
					j++
					continue
				}
				n := min(left, line.Qty)
				line.Qty -= n
				left -= n
				if line.Qty == 0 {
					cargo[i].Lines = slices.Delete(cargo[i].Lines, j, j+1)
				} else {
					j++
				}
			}
			if left == 0 {
				break
			}
		}
	}
	for _, sold := range res.Sell {
		if sold.Kind == item.Silver {
			cargo[traderIndex].Silver += sold.Qty
			continue
		}
		lines := cargo[traderIndex].Lines
		i, found := slices.BinarySearchFunc(lines, sold.Goods(), func(l trade.Line, g trade.Goods) int {
			return l.Goods().Compare(g)
		})
		if found {
			lines[i].Qty += sold.Qty
		} else {
			lines = slices.Insert(lines, i, sold)
		}
		cargo[traderIndex].Lines = lines
	}
	if res.Silver < 0 {
		cargo[traderIndex].Silver -= res.Silver
	} else if res.Silver > 0 {
		left := res.Silver
		for i := range cargo {
			n := min(left, cargo[i].Silver)
			cargo[i].Silver -= n
			left -= n
			if left == 0 {
				break
			}
		}
	}
	return true
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

// CargoOwners lists real carriers for an active trade group. The trader
// always owns its own portion; departed or missing pack members own none.
func CargoOwners(group Group, trader entity.EntityID, alive func(entity.EntityID) bool) []entity.EntityID {
	owners := []entity.EntityID{trader}
	for _, m := range group.Members {
		if m.Role == RolePackAnimal && !m.Departed && alive(m.ID) {
			owners = append(owners, m.ID)
		}
	}
	return owners
}

// CargoLossGroup returns the caravan owning an active pack member. An
// already departed carrier must never drop its manifest a second time.
func CargoLossGroup(groups []Group, id entity.EntityID) uint64 {
	for _, g := range groups {
		if g.Kind != KindCaravan {
			continue
		}
		for _, m := range g.Members {
			if m.ID == id && m.Role == RolePackAnimal && !m.Departed {
				return g.ID
			}
		}
	}
	return 0
}

// SmallStock caps goods carried by a small visiting trader.
func SmallStock(stock *trade.Stock) {
	stock.Silver = min(stock.Silver, 500)
	for i := range stock.Lines {
		stock.Lines[i].Qty = min(stock.Lines[i].Qty, 3)
	}
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

package visit

import (
	"cmp"
	"slices"

	"rimworld-go/internal/domain/ai/job"
)

// A visitor tries every map edge before breaking a wall. The search runs
// only when its previous move failed or its retry expires.
type edgeGoal struct {
	x, y int
	ok   bool
}

type edgeSide struct {
	side, distance int
}

const (
	northEdge = iota
	westEdge
	eastEdge
	southEdge
)

// escape runs the normal walk-out job, or the same building-damage job
// used by raiders when every walkable exit is cut off.
func (v Visits) escape(r *Rec, x, y int, j job.Record, now uint64) bool {
	h := v.h
	w, ht := h.Size()
	if min(x, y, w-1-x, ht-1-y) <= Inset || j.Kind == job.Move && j.State == job.Complete {
		v.goHome(r.ID)
		return false
	}
	if j.Kind == job.Breach && j.Active() {
		h.Breach(r.ID, j)
		return true
	}
	if j.Active() || now < r.RetryAt {
		return true
	}

	sides := [4]edgeSide{
		{northEdge, y}, {westEdge, x},
		{eastEdge, w - 1 - x}, {southEdge, ht - 1 - y},
	}
	slices.SortStableFunc(sides[:], func(a, b edgeSide) int {
		return cmp.Compare(a.distance, b.distance)
	})

	var goals [4]edgeGoal
	for i, side := range sides {
		tx, ty, ok := v.edgeSpot(side.side, x, y, w, ht)
		goals[i] = edgeGoal{tx, ty, ok}
		if ok && h.StartJob(r.ID, job.Record{Kind: job.Move, TX: tx, TY: ty}) {
			return true
		}
	}
	for _, goal := range goals {
		if goal.ok && h.StartBreach(r.ID, goal.x, goal.y) {
			return true
		}
	}
	// Impassable terrain can isolate a pawn even on a map with no
	// breakable buildings. Preserve the old terminal cleanup only after
	// every border goal has no structural breach route.
	if now >= r.LeaveAt+giveUp {
		for _, goal := range goals {
			if goal.ok && h.BreachRoute(r.ID, goal.x, goal.y) {
				r.RetryAt = now + retry
				return true
			}
		}
		v.goHome(r.ID)
		return false
	}
	r.RetryAt = now + retry
	return true
}

// edgeSpot finds free ground on the actual departure band, nearest the
// pawn's projection on this side. It never accepts an inland destination.
func (v Visits) edgeSpot(side, x, y, w, h int) (int, int, bool) {
	var center, length int
	if side == northEdge || side == southEdge {
		center, length = x, w
	} else {
		center, length = y, h
	}
	for band := Inset; band >= 0; band-- {
		for offset := 0; offset < length; offset++ {
			for _, at := range [2]int{center + offset, center - offset} {
				if at < 0 || at >= length {
					continue
				}
				tx, ty := at, band
				switch side {
				case westEdge:
					tx, ty = band, at
				case eastEdge:
					tx, ty = w-1-band, at
				case southEdge:
					tx, ty = at, h-1-band
				}
				if v.h.Free(tx, ty) {
					return tx, ty, true
				}
			}
		}
	}
	return 0, 0, false
}

package spatial

import (
	"slices"

	"rimworld-go/internal/domain/ai/pathing"
	"rimworld-go/internal/domain/sim/entity"
)

// occupancy is the movement-phase index of standing creatures. It is
// transient: rebuilt from the mover lists every movement phase and never
// saved.
type Occupancy struct {
	W, H    int
	Cells   []entity.EntityID      // standing blocker (wild animal, raider) per cell, 0 = none
	Near    []uint8                // per cell: blocker cells in its 3×3 (crowdCost's fast path)
	Marked  []int                  // cells written since the last rebuild
	Stacked []StackedRec           // blockers sharing a cell with a lower ID
	Carried IDSet[entity.EntityID] // patients being carried, as of the last rebuild
	// partner layer (PLAY-010): colonists and tame animals, several per
	// cell. pfirst is the lowest ID of them, pcount how many, pextra the
	// others (in no particular order: every read is order-free, so the
	// index is a function of who stands where, as after a load); pnear
	// counts partner cells in each cell's 3×3.
	Pfirst []entity.EntityID
	Pcount []uint16
	Pnear  []uint8
	Pextra []StackedRec
	// rebuild scratch: cells holding a blocker and partners, cells holding
	// two or more partners, creatures to move off, one cell's partners
	Mixed, Shared []int
	MoveOff, Here []entity.EntityID
	// repath cache: the path enter() planned with a stepRepath verdict,
	// handed out by pathAround in the same tick.
	RpID   entity.EntityID
	RpAt   uint64
	RpPath []pathing.Point
	RpMiss entity.EntityID // repathEvery's inline search for it failed (a pawn's: tests count searches)
	// dirty: the movement phase started and the index is not rebuilt yet
	// (it is rebuilt on first use, or by the periodic stack sweep); builds
	// counts the rebuilds (at most one a map a tick: occupancy_builds_test.go).
	Dirty  bool
	Builds uint64
	// hostile caches the wild-animal / raider walkability for the phase
	// (boxed once: handing it out allocates nothing).
	Ground     Layer // groundExtra's cached terrain and plant cost (water.go)
	Hostile    pathing.Query
	HostileOK  bool
	HostileGen uint64 // bumped whenever hostile is rebuilt
}

type StackedRec struct {
	ID  entity.EntityID
	Idx int
}

// partnersOn lists the partners on cell i (in the scratch buffer here).
func (o *Occupancy) PartnersOn(i int) []entity.EntityID {
	out := o.Here[:0]
	if o.Pfirst[i] != 0 {
		out = append(out, o.Pfirst[i])
	}
	if o.Pcount[i] > 1 {
		for _, s := range o.Pextra {
			if s.Idx == i {
				out = append(out, s.ID)
			}
		}
	}
	o.Here = out
	return out
}

// addPartner indexes partner id on cell i.
func (o *Occupancy) AddPartner(i int, id entity.EntityID) {
	switch first := o.Pfirst[i]; {
	case first == 0:
		o.Pput(i, id)
	case id < first: // the lowest ID is first
		o.Pfirst[i] = id
		o.Pextra = append(o.Pextra, StackedRec{ID: first, Idx: i})
	default:
		o.Pextra = append(o.Pextra, StackedRec{ID: id, Idx: i})
	}
	o.Pcount[i]++
	o.Marked = append(o.Marked, i)
}

// dropPartner unindexes partner id from cell fi (inMap: fi is on the
// map); it reports whether id was indexed as a partner.
func (o *Occupancy) DropPartner(id entity.EntityID, fi int, inMap bool) bool {
	if inMap && o.Pfirst[fi] == id {
		o.Pcount[fi]--
		k := -1
		if o.Pcount[fi] > 0 {
			for n, s := range o.Pextra {
				if s.Idx == fi && (k < 0 || s.ID < o.Pextra[k].ID) {
					k = n
				}
			}
		}
		if k < 0 {
			o.Pput(fi, 0)
			o.Pcount[fi] = 0
			return true
		}
		o.Pfirst[fi] = o.Pextra[k].ID // the next lowest takes over
		o.Pextra = slices.Delete(o.Pextra, k, k+1)
		return true
	}
	for n, s := range o.Pextra {
		if s.ID == id {
			o.Pcount[s.Idx]--
			o.Pextra = slices.Delete(o.Pextra, n, n+1)
			return true
		}
	}
	return false
}

// pput sets cell i's first partner, keeping pnear in step when the cell
// turns held or free.
func (o *Occupancy) Pput(i int, id entity.EntityID) {
	was := o.Pfirst[i] != 0
	o.Pfirst[i] = id
	if was != (id != 0) {
		bump(o.Pnear, o.W, o.H, i, was)
	}
}

func (o *Occupancy) Index(x, y int) (int, bool) {
	if x < 0 || y < 0 || x >= o.W || y >= o.H {
		return 0, false
	}
	return y*o.W + x, true
}

// put sets cell i's blocker, keeping near (the blocker cells around each
// cell) in step when the cell turns held or free.
func (o *Occupancy) Put(i int, id entity.EntityID) {
	was := o.Cells[i] != 0
	o.Cells[i] = id
	if was != (id != 0) {
		bump(o.Near, o.W, o.H, i, was)
	}
}

// bump adds one (or, with down, removes one) to near across cell i's 3×3.
func bump(near []uint8, w, h, i int, down bool) {
	var d uint8 = 1
	if down {
		d = 255 // -1
	}
	x, y := i%w, i/w
	for ny := max(y-1, 0); ny <= min(y+1, h-1); ny++ {
		for nx := max(x-1, 0); nx <= min(x+1, w-1); nx++ {
			near[ny*w+nx] += d
		}
	}
}

// at is the indexed standing creature on (x, y), 0 when none: its blocker,
// else its first partner.
func (o *Occupancy) At(x, y int) entity.EntityID {
	if i, ok := o.Index(x, y); ok {
		if b := o.Cells[i]; b != 0 {
			return b
		}
		return o.Pfirst[i]
	}
	return 0
}

// blocker is the indexed blocker on (x, y), 0 when none.
func (o *Occupancy) Blocker(x, y int) entity.EntityID {
	if i, ok := o.Index(x, y); ok {
		return o.Cells[i]
	}
	return 0
}

// move re-indexes id from -> to, in the partner layer when partner. A
// blocker stacked on from takes the cell over when id leaves it.
func (o *Occupancy) Move(id entity.EntityID, partner bool, fromX, fromY, toX, toY int) {
	fi, inMap := o.Index(fromX, fromY)
	if !o.DropBlocker(id, fi, inMap) {
		o.DropPartner(id, fi, inMap)
	}
	ti, ok := o.Index(toX, toY)
	if !ok {
		return
	}
	if partner {
		o.AddPartner(ti, id)
		return
	}
	o.Put(ti, id)
	o.Marked = append(o.Marked, ti)
}

// dropBlocker unindexes blocker id from cell fi; it reports whether id was
// indexed as a blocker.
func (o *Occupancy) DropBlocker(id entity.EntityID, fi int, inMap bool) bool {
	if inMap && o.Cells[fi] == id {
		o.Put(fi, 0)
		for k, s := range o.Stacked {
			if s.Idx == fi {
				o.Put(fi, s.ID)
				o.Stacked = append(o.Stacked[:k], o.Stacked[k+1:]...)
				break
			}
		}
		return true
	}
	for k, s := range o.Stacked {
		if s.ID == id {
			o.Stacked = append(o.Stacked[:k], o.Stacked[k+1:]...)
			return true
		}
	}
	return false
}

package game

import (
	"cmp"
	"slices"

	"rimworld-go/internal/application/game/pathstage"
	"rimworld-go/internal/application/game/spatial"
	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/ai/pathing"
	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/pawn/health"
	"rimworld-go/internal/domain/sim/entity"
)

// stepVerdict is the occupancy decision for one step (see SIM-014).
type stepVerdict int

const (
	stepGo         stepVerdict = iota // the cell is free: move
	stepWait                          // stay this tick, retry next tick
	stepRepath                        // replan around the creatures in the way
	stepSidestep                      // move to alt instead, then replan
	stepArriveNear                    // end the walk at alt, next to the target
)

// Blocking rules (SIM-014, PLAY-010). Colonists and tame animals are
// partners: they walk through each other and may share a cell, except
// that a colonist never ends a walk on another colonist's cell while
// either is in combat (inCombat). Wild animals and raiders own their cell
// against everyone. Downed, sleeping and carried pawns, corpses and items
// never block.
const (
	// yieldRetry: a creature that has to give way retries its sidestep
	// this often while it stays blocked.
	yieldRetry = 15
	// idleYieldAfter: an idle creature in someone's way steps aside once
	// that walker has waited this long.
	idleYieldAfter = 30
	// repathAfter: a walker held up by another walker replans around it
	// every repathAfter blocked ticks (at most one search per period).
	repathAfter = 45
	// repathCap (45·2⁴, the first past blockedFailTicks): four failed
	// searches end an animal's tries until it steps or gives up.
	repathCap = 720
	// occupiedCost / besideCost: extra path cost of a cell holding a
	// creature, and of a cell next to a creature standing still.
	occupiedCost = 40
	besideCost   = 20
	// asideRadius bounds the search for a cell to step aside into;
	// asideLinger is how long a colonist that made way idles there.
	asideRadius = 5
	asideLinger = 60
	// cycleDepth bounds the walk along a chain of blocked walkers.
	cycleDepth = 8
	// avoidAhead is how much of a walker's path a yielder keeps clear of.
	avoidAhead = 10
	// stuckFailTicks: a colonist held up this long by a creature that
	// stands still and won't make way (drafted, in a mental break, a
	// raider, a worker at an exact cell), with no way round, gives up with
	// job.Blocked and its work is postponed (SIM-027). Behind a walker or
	// a creature that makes way it keeps blockedFailTicks.
	stuckFailTicks = 120
)

// The movement index is owned by game/spatial.
type occupancy = spatial.Occupancy
type stackedRec = spatial.StackedRec

// arrival is how a walk may end when its last cell is taken.
type arrival uint8

const (
	arriveAdjacent arrival = iota // any free cell next to the work cell
	arriveExact                   // only that cell: wait for it
	arriveNearest                 // the nearest free cell (move orders, animals)
)

// buildOccupancy starts a movement phase: the index of every standing
// creature's cell goes stale and is rebuilt on its first use this phase
// (most ticks no one is due to step, and nothing is built). Every
// yieldRetry ticks it is rebuilt anyway, to sweep for stacks.
func (g *Game) buildOccupancy() {
	o := &g.occ
	o.Dirty, o.RpID, o.HostileOK = true, 0, false
	if uint64(g.clock.Now())%yieldRetry == 0 {
		g.rebuildOccupancy()
	}
}

// rebuildOccupancy indexes every standing creature's cell. O(movers): only
// the cells written last time are cleared. Blockers sharing a cell
// (spawned, ejected or stood up there) and blocker–partner pairs are
// stacked: the higher ID of the pair, if it stands still, is given a step
// to a free cell now, so it leaves on its next due move. Partners may
// share a cell; two colonists settled on one cell are split the same way
// only while either is in combat.
func (g *Game) rebuildOccupancy() {
	o := &g.occ
	w, h := g.grid.Width(), g.grid.Height()
	if o.W != w || o.H != h || len(o.Cells) != w*h {
		o.W, o.H = w, h
		o.Cells = make([]entity.EntityID, w*h)
		o.Near = make([]uint8, w*h)
		o.Pfirst = make([]entity.EntityID, w*h)
		o.Pcount = make([]uint16, w*h)
		o.Pnear = make([]uint8, w*h)
		o.Marked = o.Marked[:0]
	}
	for _, i := range o.Marked {
		o.Put(i, 0)
		o.Pput(i, 0)
		o.Pcount[i] = 0
	}
	o.Marked = o.Marked[:0]
	o.Stacked = o.Stacked[:0]
	o.Pextra = o.Pextra[:0]
	o.Mixed, o.Shared = o.Mixed[:0], o.Shared[:0]
	o.Dirty, o.Builds = false, o.Builds+1
	o.Carried.Reset()
	g.care.Carry.Each(func(_, p entity.EntityID) { o.Carried.Add(p) })
	for list, ids := range [3][]entity.EntityID{g.pawnIDs, g.raiderIDs, g.animalIDs} {
		for _, id := range ids {
			if list < 2 && !g.standingWalker(id) || list == 2 && !g.standing(id) {
				continue
			}
			pos, _ := g.positions.Get(id)
			i, ok := o.Index(pos.X, pos.Y)
			if !ok {
				continue
			}
			if g.partner(id) {
				o.AddPartner(i, id)
				if o.Cells[i] != 0 {
					o.Mixed = append(o.Mixed, i)
				}
				if o.Pcount[i] == 2 {
					o.Shared = append(o.Shared, i)
				}
				continue
			}
			if o.Pcount[i] != 0 {
				o.Mixed = append(o.Mixed, i)
			}
			switch held := o.Cells[i]; {
			case held == 0:
				o.Put(i, id)
				o.Marked = append(o.Marked, i)
			case id < held: // the lower ID keeps the cell
				o.Put(i, id)
				o.Stacked = append(o.Stacked, stackedRec{ID: held, Idx: i})
			default:
				o.Stacked = append(o.Stacked, stackedRec{ID: id, Idx: i})
			}
		}
	}
	slices.SortFunc(o.Stacked, func(a, b stackedRec) int { return cmp.Compare(a.ID, b.ID) })
	g.unstack()
}

// unstack gives every stacked creature that stands still a step off its
// cell, in ID order: the higher ID of each blocking pair, and of each pair
// of settled colonists where either is in combat.
func (g *Game) unstack() {
	o := &g.occ
	o.MoveOff = o.MoveOff[:0]
	for _, s := range o.Stacked {
		o.MoveOff = append(o.MoveOff, s.ID)
	}
	for _, i := range o.Mixed {
		if b := o.Cells[i]; b > o.Pfirst[i] {
			o.MoveOff = append(o.MoveOff, b) // a partner below it keeps the cell
		} else {
			o.MoveOff = append(o.MoveOff, o.PartnersOn(i)...)
		}
	}
	for _, i := range o.Shared {
		g.combatStack(i)
	}
	if len(o.MoveOff) == 0 {
		return
	}
	slices.Sort(o.MoveOff)
	o.MoveOff = slices.Compact(o.MoveOff)
	for _, id := range o.MoveOff {
		if _, walking := g.walkNext(id); !walking {
			g.makeWay(id, 0)
		}
	}
}

// combatStack adds to moveOff the colonists settled on partner cell i that
// must leave it: going up by ID, each one that would share the cell with a
// staying colonist while either of them is in combat. Colonists passing
// through (walking) are left alone.
func (g *Game) combatStack(i int) {
	o := &g.occ
	ids := o.PartnersOn(i)
	slices.Sort(ids)
	stayed, combat := false, false
	for _, id := range ids {
		if !g.colonist(id) {
			continue
		}
		if _, walking := g.walkNext(id); walking {
			continue
		}
		c := g.combat.Guard.InCombat(id)
		if stayed && (c || combat) {
			o.MoveOff = append(o.MoveOff, id)
			continue
		}
		stayed, combat = true, combat || c
	}
}

// partner reports a creature that walks through other partners: a
// colonist or a tame animal (PLAY-010).
func (g *Game) partner(id entity.EntityID) bool {
	if a := g.animals.Ref(id); a != nil {
		return a.Tame
	}
	return g.colonist(id)
}

// standing reports a creature that owns its cell: a living animal, or a
// colonist or raider on its feet who is neither asleep nor carried.
func (g *Game) standing(id entity.EntityID) bool {
	if a := g.animals.Ref(id); a != nil {
		return !a.Dead()
	}
	return g.standingWalker(id)
}

// standingWalker is standing for a colonist or raider.
func (g *Game) standingWalker(id entity.EntityID) bool {
	if v := g.vital.Ref(id); v == nil || v.Status != health.Alive {
		return false
	}
	if j := g.jobs.Ref(id); j != nil && j.Kind == job.Sleep && (j.State == job.Do || j.State == job.Complete) {
		return false // lying down: can be stepped over
	}
	return !g.occ.Carried.Has(id)
}

// enter decides whether id may step from -> to this tick, and keeps the
// index current when it does (stepGo, stepSidestep, stepArriveNear). alt is
// the cell for stepSidestep and stepArriveNear.
func (g *Game) enter(id entity.EntityID, from, to Position) (stepVerdict, Position) {
	o := g.occIndex()
	mp := g.partner(id)
	occ := g.holderFor(id, mp, to)
	if occ == 0 || occ == id {
		if mp && g.meleeOn(id, to) {
			return stepWait, from // a partner closing on a partner stops next to it
		}
		o.Move(id, mp, from.X, from.Y, to.X, to.Y)
		return stepGo, to
	}
	m := g.motionOf(id)
	if g.targetOf(id) == occ {
		// a hostile closing on its target stops next to it (melee reach)
		return stepWait, from
	}
	if _, walking := g.walkNext(occ); walking {
		if yielder, cycle := g.blockCycle(id, occ); cycle {
			// head-on swap (a 2-cycle) or a longer ring: the highest ID
			// in the ring gives way, everyone else waits for it
			if yielder != id {
				return g.repathEvery(id, from, to, m, true)
			}
			return g.giveWay(id, mp, from, to, occ, m.BlockedTicks)
		}
		// the occupant is moving on: wait for it, replan if it is stuck
		return g.repathEvery(id, from, to, m, true)
	}
	// the occupant stands still: an idler makes way, and so does a
	// colonist at work that can do it from another cell (for colonists)
	if blocked := m.BlockedTicks; m.BlockedBy == occ && blocked >= idleYieldAfter && blocked%yieldRetry == 0 &&
		(g.idleYieldable(occ) || g.colonist(id) && g.workYieldable(occ)) {
		g.makeWay(occ, id)
	}
	if g.finalStep(id, to) {
		switch g.arrivalOf(id) {
		case arriveExact:
			return stepWait, from
		case arriveNearest:
			if alt, ok := g.nearestFreeStep(id, from, to); ok {
				o.Move(id, mp, from.X, from.Y, alt.X, alt.Y)
				return stepArriveNear, alt
			}
		default:
			if alt, ok := g.freeNextTo(id, from, g.workCell(id, to)); ok {
				o.Move(id, mp, from.X, from.Y, alt.X, alt.Y)
				return stepArriveNear, alt
			}
		}
	}
	return g.repathEvery(id, from, to, m, false)
}

// holderFor is the creature that keeps mover id (a partner when mp) out of
// cell c, 0 when none: a blocker always; for a wild animal or raider also
// any partner; for a colonist's last step, a colonist there when either is
// in combat (decision 7). Other partners pass through.
func (g *Game) holderFor(id entity.EntityID, mp bool, c Position) entity.EntityID {
	o := &g.occ
	i, ok := o.Index(c.X, c.Y)
	if !ok {
		return 0
	}
	if b := o.Cells[i]; b != 0 && b != id {
		return b
	}
	if o.Pcount[i] == 0 {
		return 0
	}
	if !mp {
		return o.Pfirst[i]
	}
	if g.colonist(id) && g.finalStep(id, c) {
		return g.combatHolder(id, i)
	}
	return 0
}

// combatHolder is a colonist on partner cell i, other than id, that id may
// not end its walk on: any colonist when id is in combat, else one in
// combat; 0 when none.
func (g *Game) combatHolder(id entity.EntityID, i int) entity.EntityID {
	o := &g.occ
	self := g.combat.Guard.InCombat(id)
	keeps := func(p entity.EntityID) bool {
		return p != 0 && p != id && g.colonist(p) && (self || g.combat.Guard.InCombat(p))
	}
	if p := o.Pfirst[i]; keeps(p) {
		return p // the lowest ID
	}
	var best entity.EntityID
	if o.Pcount[i] > 1 {
		for _, s := range o.Pextra {
			if s.Idx == i && (best == 0 || s.ID < best) && keeps(s.ID) {
				best = s.ID
			}
		}
	}
	return best
}

// meleeOn reports that id's attack target is a partner on cell c.
func (g *Game) meleeOn(id entity.EntityID, c Position) bool {
	o := &g.occ
	i, ok := o.Index(c.X, c.Y)
	if !ok || o.Pcount[i] == 0 {
		return false
	}
	t := g.targetOf(id)
	if t == 0 {
		return false
	}
	if o.Pfirst[i] == t {
		return true
	}
	for _, s := range o.Pextra {
		if s.Idx == i && s.ID == t {
			return true
		}
	}
	return false
}

// stopOK reports that id may end a walk on its own cell c: no colonist
// shares it that combatHolder would keep it off (a partner may stand on
// someone's cell while passing through).
func (g *Game) stopOK(id entity.EntityID, c Position) bool {
	o := &g.occ
	i, ok := o.Index(c.X, c.Y)
	if !ok || o.Pcount[i] < 2 || !g.colonist(id) {
		return true
	}
	return g.combatHolder(id, i) == 0
}

// occIndex is the index, built on first use (e.g. a hook called before
// any movement phase).
func (g *Game) occIndex() *occupancy {
	if g.occ.Dirty || g.occ.Cells == nil {
		g.rebuildOccupancy()
	}
	return &g.occ
}

// repathEvery waits, replanning around the creature in the way at most
// once per repathAfter blocked ticks, the first time after repathAfter
// ticks. A colonist held up by a creature standing still replans at once
// (players notice a colonist idling behind a worker; raiders and animals
// keep the cheaper rule). An animal's search is queued on the path stage
// (paths.go runRepaths: it waits this tick either way); each failed one
// doubles its wait (motionRec.RepathWait).
func (g *Game) repathEvery(id entity.EntityID, from, to Position, m motionRec, behindWalker bool) (stepVerdict, Position) {
	eager := !behindWalker && g.isPawn(id) && !g.isRaider(id)
	if blocked := m.BlockedTicks; blocked%max(m.RepathWait, repathAfter) != 0 || blocked == 0 && !eager {
		return stepWait, from
	}
	if a := g.animals.Ref(id); a != nil && a.PathI < len(a.Path) && g.regs.paths.Queue(pathstage.Request{ID: id,
		From: pathing.Point(from), To: pathing.Point(to), Goal: a.Path[len(a.Path)-1], Token: pathstage.TokenOf(a.Path, a.PathI, pathing.Point(from))}) {
		return stepWait, from
	}
	if g.planAround(id, from, to) {
		return stepRepath, from
	}
	g.occ.RpMiss = id
	return stepWait, from
}

// planAround searches a costed route from 'from' to id's walk goal that
// does not start into 'to' or another creature; on success it caches it
// for pathAround.
func (g *Game) planAround(id entity.EntityID, from, to Position) bool {
	goal, ok := g.walkGoal(id)
	if !ok {
		return false
	}
	o := &g.occ
	path, ok := g.findPath(id, g.costedQuery(id), from.X, from.Y, goal.X, goal.Y) // paths.go: long ones in the region corridor
	if !ok || len(path) == 0 {
		return false
	}
	first := path[0]
	if first.X == to.X && first.Y == to.Y || g.holderFor(id, g.partner(id), Position{X: first.X, Y: first.Y}) != 0 {
		return false
	}
	o.RpID, o.RpAt, o.RpPath = id, uint64(g.clock.Now()), path
	return true
}

// giveWay is the yielder's move in a head-on swap or a blocked ring:
// sidestep off both paths towards its goal; else replan around; else back
// off along the other's path (a 1-wide corridor); else wait. It retries
// every yieldRetry blocked ticks.
func (g *Game) giveWay(id entity.EntityID, mp bool, from, to Position, occ entity.EntityID, blocked int) (stepVerdict, Position) {
	if blocked%yieldRetry != 0 {
		return stepWait, from
	}
	o := &g.occ
	q := g.queryFor(id)
	goal, _ := g.walkGoal(id)
	avoid := g.pathAhead(occ, nil)
	avoid = g.pathAhead(id, avoid)
	best, found := Position{}, false
	bestD := 0
	for _, d := range stepDirs {
		c := Position{X: from.X + d[0], Y: from.Y + d[1]}
		if !g.stepOpen(q, from, c) || onPath(avoid, c) {
			continue
		}
		dist := pathing.Octile(c.X, c.Y, goal.X, goal.Y)
		if !found || dist < bestD || dist == bestD && spatial.Before(c.X, c.Y, best.X, best.Y) {
			best, bestD, found = c, dist, true
		}
	}
	if found {
		o.Move(id, mp, from.X, from.Y, best.X, best.Y)
		return stepSidestep, best
	}
	if g.planAround(id, from, to) {
		return stepRepath, from
	}
	// back off: the free cell farthest from the other walker
	op, _ := g.positions.Get(occ)
	for _, d := range stepDirs {
		c := Position{X: from.X + d[0], Y: from.Y + d[1]}
		if !g.stepOpen(q, from, c) {
			continue
		}
		dist := pathing.Octile(c.X, c.Y, op.X, op.Y)
		if !found || dist > bestD || dist == bestD && spatial.Before(c.X, c.Y, best.X, best.Y) {
			best, bestD, found = c, dist, true
		}
	}
	if found {
		o.Move(id, mp, from.X, from.Y, best.X, best.Y)
		return stepSidestep, best
	}
	return stepWait, from
}

// stepDirs is the fixed neighbour order (deterministic scans).
var stepDirs = [8][2]int{
	{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

// stepOpen retains the caller's Position convention for map movement.
func (g *Game) stepOpen(q pathing.Query, from, c Position) bool {
	return spatial.StepOpen(q, &g.occ, from.X, from.Y, c.X, c.Y)
}

// onPath reports a cell blocked by an existing walking route.
func onPath(p []Position, c Position) bool {
	for _, q := range p {
		if q == c {
			return true
		}
	}
	return false
}

// blockCycle follows the chain of walkers each waiting on the next one's
// cell (holderFor: as each one sees it), starting at occ. It reports
// whether the chain closes back on id and, if so, the highest ID in the
// ring (the one that gives way).
func (g *Game) blockCycle(id, occ entity.EntityID) (entity.EntityID, bool) {
	top, cur := max(id, occ), occ
	for range cycleDepth {
		n, walking := g.walkNext(cur)
		if !walking {
			return 0, false
		}
		nxt := g.holderFor(cur, g.partner(cur), n)
		switch nxt {
		case 0, cur:
			return 0, false
		case id:
			return top, true
		}
		top, cur = max(top, nxt), nxt
	}
	return 0, false
}

// walkNext is the next cell on id's current walk.
func (g *Game) walkNext(id entity.EntityID) (Position, bool) {
	if a := g.animals.Ref(id); a != nil {
		if a.PathI < len(a.Path) {
			p := a.Path[a.PathI]
			return Position{X: p.X, Y: p.Y}, true
		}
		return Position{}, false
	}
	j, _ := g.jobs.Get(id)
	if j.State == job.Goto && j.PathI < len(j.Path) {
		p := j.Path[j.PathI]
		return Position{X: p.X, Y: p.Y}, true
	}
	return Position{}, false
}

// walkGoal is the last cell of id's current walk.
func (g *Game) walkGoal(id entity.EntityID) (Position, bool) {
	if a := g.animals.Ref(id); a != nil {
		if a.PathI < len(a.Path) {
			p := a.Path[len(a.Path)-1]
			return Position{X: p.X, Y: p.Y}, true
		}
		return Position{}, false
	}
	j, _ := g.jobs.Get(id)
	if j.PathI < len(j.Path) {
		p := j.Path[len(j.Path)-1]
		return Position{X: p.X, Y: p.Y}, true
	}
	return Position{}, false
}

// pathAhead appends id's cell and the next avoidAhead cells of its walk.
func (g *Game) pathAhead(id entity.EntityID, out []Position) []Position {
	pos, _ := g.positions.Get(id)
	out = append(out, pos)
	var path []pathing.Point
	var i int
	if a := g.animals.Ref(id); a != nil {
		path, i = a.Path, a.PathI
	} else if j, ok := g.jobs.Get(id); ok && j.State == job.Goto {
		path, i = j.Path, j.PathI
	}
	for k := i; k < len(path) && k < i+avoidAhead; k++ {
		out = append(out, Position{X: path[k].X, Y: path[k].Y})
	}
	return out
}

// finalStep reports that 'to' is the last cell of id's walk.
func (g *Game) finalStep(id entity.EntityID, to Position) bool {
	goal, ok := g.walkGoal(id)
	return ok && goal == to
}

// targetOf is the creature id is attacking (0 when none).
func (g *Game) targetOf(id entity.EntityID) entity.EntityID {
	if a := g.animals.Ref(id); a != nil {
		if a.Mode == animal.Attack {
			return a.Target
		}
		return 0
	}
	if j, _ := g.jobs.Get(id); j.Kind == job.Attack || j.Kind == job.Hunt {
		return j.Target
	}
	return 0
}

// arrivalOf is how id's walk may end when its last cell is taken.
// Exact-cell jobs: sleeping (bed or spot), relaxing in a chair, a meal
// carried to its seat, bench work (bills, research), the horseshoes
// throwing spot, and carrying a load or a patient to its cell or bed.
// Move orders (player and raider) and animals take the nearest free cell;
// every other job works from any free adjacent cell.
func (g *Game) arrivalOf(id entity.EntityID) arrival {
	if _, ok := g.animals.Get(id); ok {
		return arriveNearest
	}
	j, _ := g.jobs.Get(id)
	switch j.Kind {
	case job.Move:
		return arriveNearest
	case job.Sleep, job.Relax, job.Bill, job.Research, job.Horseshoes, job.OperateDrill, job.ScanGround, job.ScanMinerals, job.Recreate, job.WatchTelevision, job.Stargaze:
		return arriveExact
	case job.Eat, job.Haul, job.FillHopper, job.Rescue:
		if j.Carry != 0 && j.PickupI >= len(j.Pickups) || g.care.Carry.Patient(id) != 0 {
			return arriveExact
		}
	}
	return arriveAdjacent
}

// exactCellKinds lists the job kinds arrivalOf always holds to their cell
// (Eat, Haul and Rescue only while carrying).
var exactCellKinds = []job.Kind{job.Sleep, job.Relax, job.Bill, job.Research, job.Horseshoes, job.OperateDrill, job.ScanGround, job.ScanMinerals, job.Recreate, job.WatchTelevision, job.Stargaze}

// workCell is the tile id's job works on: the rock for mining, the tile
// being swept for cleaning, else the job target, else the walk's last cell.
func (g *Game) workCell(id entity.EntityID, to Position) Position {
	j, ok := g.jobs.Get(id)
	if !ok {
		return to
	}
	if j.Kind == job.Mine || j.Kind == job.Smooth || j.Kind == job.Clean {
		return Position{X: j.DestX, Y: j.DestY}
	}
	if j.TX == 0 && j.TY == 0 {
		return to
	}
	return Position{X: j.TX, Y: j.TY}
}

// freeNextTo chooses a legal work-adjacent stop.
func (g *Game) freeNextTo(id entity.EntityID, from, wc Position) (Position, bool) {
	q := g.queryFor(id)
	x, y, ok := spatial.FreeNextTo(from.X, from.Y, wc.X, wc.Y, g.stopOK(id, from),
		func(x, y int) bool { return g.stepOpen(q, from, Position{X: x, Y: y}) })
	return Position{X: x, Y: y}, ok
}

// nearestFreeStep prefers the current cell on a distance tie.
func (g *Game) nearestFreeStep(id entity.EntityID, from, to Position) (Position, bool) {
	q := g.queryFor(id)
	x, y, ok := spatial.NearestFreeStep(from.X, from.Y, to.X, to.Y, g.stopOK(id, from),
		func(x, y int) bool { return g.stepOpen(q, from, Position{X: x, Y: y}) })
	return Position{X: x, Y: y}, ok
}

// idleYieldable: a creature that steps aside for a walker who has waited
// on it: a colonist with nothing to do or idling (not working, not
// drafted, not in a mental break), or a calm animal standing about.
// Raiders never make way.
func (g *Game) idleYieldable(id entity.EntityID) bool {
	if a := g.animals.Ref(id); a != nil {
		// grazing or bedded-down animals step aside too, then carry on
		calm := a.Mode == animal.Wander || a.Mode == animal.Feed || a.Mode == animal.Rest
		return calm && a.PathI >= len(a.Path) && !g.beingTamed(id)
	}
	if !g.isPawn(id) || g.isRaider(id) || g.drafted(id) {
		return false
	}
	if _, breaking := g.breaks[id]; breaking {
		return false
	}
	j, _ := g.jobs.Get(id)
	return j.Kind == job.None || j.Kind.IsIdle() || j.State == job.Idle || j.State == job.Complete || j.State == job.Fail
}

// colonist: a player colonist (not a raider).
func (g *Game) colonist(id entity.EntityID) bool { return g.isPawn(id) && !g.isRaider(id) }

// workYieldable: a colonist at work (not drafted, not in a mental break)
// whose job is done from any cell next to its work: it shuffles to a free
// cell still touching the work and keeps the job (makeWay). Bench, bed and
// carrying jobs (exact cells) and fights hold their cell.
func (g *Game) workYieldable(id entity.EntityID) bool {
	if !g.colonist(id) || g.drafted(id) || g.breaking(id) {
		return false
	}
	j, _ := g.jobs.Get(id)
	if j.Kind == job.None || j.State != job.Do || j.Kind.IsIdle() || j.Kind == job.Hunt || j.Kind == job.Attack {
		return false
	}
	return g.arrivalOf(id) == arriveAdjacent
}

// blockedLimit is how long walker id waits on 'by' (the creature in its
// way; v this tick's verdict) before it gives up: stuckFailTicks behind a
// colonist-blocking creature that stands still, won't make way and has no
// way round it; else blockedFailTicks.
func (g *Game) blockedLimit(id, by entity.EntityID, v stepVerdict) int {
	if by == 0 || v == stepRepath || !g.colonist(id) || g.targetOf(id) == by {
		return blockedFailTicks
	}
	if _, walking := g.walkNext(by); walking || g.idleYieldable(by) || g.workYieldable(by) {
		return blockedFailTicks // it moves on, or makes way
	}
	return stuckFailTicks
}

// makeWay gives a creature standing still a short walk to the nearest
// free cell off forID's way (forID 0: just off its own cell), so it leaves
// on its next due move. A working pawn keeps its job and resumes it next
// to the work; an idle colonist idles there for a moment; a drafted pawn
// or a raider without work just moves.
func (g *Game) makeWay(id, forID entity.EntityID) bool {
	pos, _ := g.positions.Get(id)
	var avoid []Position
	if forID != 0 {
		avoid = g.pathAhead(forID, nil)
	}
	j, _ := g.jobs.Get(id)
	working := !g.isAnimal(id) && j.Kind != job.None && j.State == job.Do && !j.Kind.IsIdle()
	wc := pos
	if working {
		wc = g.workCell(id, pos)
	}
	path, ok := g.asideRoute(id, pos, avoid, wc, working)
	if !ok {
		return false
	}
	dest := path[len(path)-1]
	if working && forID != 0 && pathing.Dist(dest.X, dest.Y, wc.X, wc.Y) > 1 {
		return false // a worker only shuffles to a cell it can still work from
	}
	if a, ok := g.animals.Get(id); ok {
		a.Path, a.PathI, a.GoalX, a.GoalY = path, 0, dest.X, dest.Y
		g.animals.Set(id, a)
		return true
	}
	switch {
	case working, j.State == job.Do && g.breaking(id):
		j.Path, j.PathI, j.State = path, 0, job.Goto
		g.jobs.Set(id, j)
	case g.isRaider(id) || g.drafted(id):
		g.jobs.Set(id, job.Record{Kind: job.Move, State: job.Goto, TX: dest.X, TY: dest.Y, Path: path})
	default:
		if j.Kind != job.None && j.State != job.Idle {
			g.interrupt(id, job.Interrupted) // frees a chair or spot it held
		}
		g.jobs.Set(id, job.Record{Kind: job.Wander, State: job.Goto, TX: dest.X, TY: dest.Y, Path: path, WorkLeft: asideLinger})
	}
	return true
}

func (g *Game) isAnimal(id entity.EntityID) bool {
	_, ok := g.animals.Get(id)
	return ok
}

func (g *Game) breaking(id entity.EntityID) bool {
	_, ok := g.breaks[id]
	return ok
}

// asideRoute searches a bounded neighbourhood for a safe detour.
func (g *Game) asideRoute(id entity.EntityID, pos Position, avoid []Position, wc Position, working bool) ([]pathing.Point, bool) {
	q := g.queryFor(id)
	return spatial.AsideRoute(pos.X, pos.Y, wc.X, wc.Y, working,
		func(fx, fy, x, y int) bool {
			return g.stepOpen(q, Position{X: fx, Y: fy}, Position{X: x, Y: y})
		},
		func(x, y int) bool {
			c := Position{X: x, Y: y}
			for _, a := range avoid {
				if a == c {
					return true
				}
			}
			if g.bedCovering(x, y) {
				return true
			}
			owner, taken := g.reserve.TileOwner(x, y)
			return taken && owner != id
		})
}

// queryFor is the walkability id moves with: wild animals keep out of
// doors, tame ones use them; pawns plan as pathTo does.
func (g *Game) queryFor(id entity.EntityID) pathing.Query {
	if a := g.animals.Ref(id); a != nil {
		if g.gatesOpen(id, a) { // rope.go: livestock only when led
			return g.animalPathQuery(true, false) // fences.go: gates open, fences shut
		}
		return g.animalPathQuery(false, true)
	}
	if g.isRaider(id) && !g.sapper(id) {
		return g.phaseHostileMap() // what pathQuery gives a raider
	}
	return g.pathQuery(id)
}

// phaseHostileMap is hostileMap, built once per movement phase (doors do
// not change while creatures step).
func (g *Game) phaseHostileMap() pathing.Query {
	o := &g.occ
	if !o.HostileOK || o.Dirty {
		o.Hostile, o.HostileOK = g.hostileMap(), true
		o.HostileGen++
	}
	return o.Hostile
}

// crowdCost adds blocking costs to a walkability query: a cell holding a
// creature that blocks the mover costs occupiedCost, one next to such a
// creature standing still besideCost. A partner mover (colonist, tame
// animal) prices only blockers: other partners are free ground.
type crowdCost struct {
	pathing.Query
	g       *Game
	self    entity.EntityID
	partner bool
}

func (c crowdCost) ExtraCost(x, y int) int {
	o := &c.g.occ
	ground := c.g.groundExtra(x, y) + softExtra(c.Query, x, y) // water.go: slow ground costs extra, so A* wades only when it pays; area_soft.go
	i, ok := o.Index(x, y)
	if !ok || o.Near[i] == 0 && (c.partner || o.Pnear[i] == 0) {
		return ground // no creature in the way on the cell or beside it
	}
	if id := c.in(x, y); id != 0 && id != c.self {
		return occupiedCost + ground
	}
	for _, d := range stepDirs {
		id := c.in(x+d[0], y+d[1])
		if id == 0 || id == c.self {
			continue
		}
		if _, walking := c.g.walkNext(id); !walking {
			return besideCost + ground
		}
	}
	return ground
}

// in is the creature on (x, y) that the mover prices.
func (c crowdCost) in(x, y int) entity.EntityID {
	if c.partner {
		return c.g.occ.Blocker(x, y)
	}
	return c.g.occ.At(x, y)
}

func (g *Game) costedQuery(id entity.EntityID) pathing.Query {
	g.occIndex()
	return crowdCost{Query: g.softAnimalQuery(id, g.queryFor(id)), g: g, self: id, partner: g.partner(id)} // area_soft.go
}

// pathAround replans from id's cell to (tx, ty), treating cells taken by
// other creatures as extra cost. Right after enter() answered stepRepath
// it returns the route enter() planned (to the walk's old last cell).
// Otherwise it searches like pathTo: to the cell itself when walkable,
// else to the cheapest free-standing neighbour.
func (g *Game) pathAround(id entity.EntityID, tx, ty int) ([]pathing.Point, bool) {
	o := &g.occ
	if o.RpID == id && o.RpAt == uint64(g.clock.Now()) {
		p := o.RpPath
		o.RpID, o.RpPath = 0, nil
		return p, true
	}
	pos, _ := g.positions.Get(id)
	if pos.X == tx && pos.Y == ty {
		return nil, true
	}
	q := g.costedQuery(id)
	if q.Walkable(tx, ty) {
		return g.findPath(id, q, pos.X, pos.Y, tx, ty) // paths.go: long ones in the region corridor
	}
	if pathing.Adjacent(pos.X, pos.Y, tx, ty) {
		return nil, true
	}
	var best []pathing.Point
	bestOK, bestCost := false, 0
	for _, d := range stepDirs {
		ax, ay := tx+d[0], ty+d[1]
		if !q.Walkable(ax, ay) {
			continue
		}
		p, ok := g.findPath(id, q, pos.X, pos.Y, ax, ay)
		if !ok {
			continue
		}
		if c := spatial.RouteCost(q, pos.X, pos.Y, p); !bestOK || c < bestCost {
			best, bestOK, bestCost = p, true, c
		}
	}
	return best, bestOK
}

// occupantAt reports the standing creature on (x, y), if any, as of the
// current movement phase's index: its blocker, else its first partner.
func (g *Game) occupantAt(x, y int) (entity.EntityID, bool) {
	id := g.occIndex().At(x, y)
	return id, id != 0
}

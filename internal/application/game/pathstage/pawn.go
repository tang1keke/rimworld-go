package pathstage

import (
	"cmp"
	"slices"

	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/ai/pathing"
	"rimworld-go/internal/domain/sim/entity"
	"rimworld-go/internal/domain/world/region"
)

// Guard is the job and area policy that must still match when a plan is used.
type Guard struct {
	Kind     job.Kind
	Forced   bool
	Area     int
	AreaFree bool
}

type Mover struct {
	ID     entity.EntityID
	Animal bool
}

type Candidate struct {
	Eligible                   bool
	Job                        job.Record
	Guard                      Guard
	From, Next, Goal           pathing.Point
	NextWalkable, GoalWalkable bool
}

type PawnPlan struct {
	MoverCount                 int
	MoverAt                    func(int) Mover
	Candidate                  func(Mover) Candidate
	Enabled                    bool
	Pool                       Pool
	Warm                       func()
	Plan                       func(Request) (pathing.Query, Kind, []uint64)
	GridVersion, StructVersion uint64
	Searcher                   *pathing.Searcher
}

// PawnStage adds the map versions to the generic request queue.
type PawnStage struct {
	Stage
	gridVersion, structVersion uint64
}

// Preplan collects the due blocked-colonist requests, prepares their read-only
// queries and searches them in the movement phase's pool.
func (s *PawnStage) Preplan(p PawnPlan) {
	s.Open()
	if !p.Enabled {
		s.Close()
		return
	}
	for i := range p.MoverCount {
		m := p.MoverAt(i)
		if m.Animal {
			continue
		}
		c := p.Candidate(m)
		if !c.Eligible || c.Job.State != job.Goto || c.Job.PathI >= len(c.Job.Path) ||
			c.NextWalkable || !c.GoalWalkable || c.From == c.Goal {
			continue
		}
		s.Queue(Request{
			ID: m.ID, From: c.From, To: c.Next, Goal: c.Goal,
			Token: TokenOf(c.Job.Path, c.Job.PathI, c.From), Guard: c.Guard,
		})
	}
	if len(s.Requests()) < 8 {
		s.Open()
		s.Close()
		return
	}
	p.Warm()
	for i, r := range s.Requests() {
		q, k, allow := p.Plan(r)
		s.SetPlan(i, q, k, allow)
	}
	s.gridVersion, s.structVersion = p.GridVersion, p.StructVersion
	s.SetPool(p.Pool)
	s.Close()
	s.Compute(p.Searcher)
}

// Lookup returns a staged result only while its job, walk, guard and map are
// unchanged. A false ready result asks the owner to use its serial fallback.
func (s *PawnStage) Lookup(id entity.EntityID, guard Guard, from, to, goal pathing.Point, token Token, grid, structure uint64, fire bool) (Result, bool) {
	if fire || s.gridVersion != grid || s.structVersion != structure {
		return Result{}, false
	}
	rs := s.Requests()
	i, found := slices.BinarySearchFunc(rs, id, func(r Request, id entity.EntityID) int {
		return cmp.Compare(r.ID, id)
	})
	if !found {
		return Result{}, false
	}
	r := rs[i]
	if r.Guard != guard || r.From != from || r.To != to || r.Goal != goal || r.Token != token ||
		r.Kind == Failed && !r.Query.Walkable(goal.X, goal.Y) {
		return Result{}, false
	}
	return s.Result(i), true
}

// ReachCheck is a region pre-check with the optional game's index self-check.
type ReachCheck struct {
	m     *region.Map
	layer region.Layer
	from  pathing.Point
	check func(x, y int, ok bool)
}

func NewReachCheck(m *region.Map, layer region.Layer, from pathing.Point, check func(x, y int, ok bool)) ReachCheck {
	return ReachCheck{m: m, layer: layer, from: from, check: check}
}

func (r ReachCheck) OK(x, y int) bool {
	if r.m == nil {
		return true
	}
	ok := r.m.Reach(r.layer, r.from.X, r.from.Y, x, y)
	if r.check != nil {
		r.check(x, y, ok)
	}
	return ok
}

// PlanSearch chooses the exact or region-corridor search for a query.
func PlanSearch(q pathing.Query, sx, sy, tx, ty, longPath int, noRegions, exactLong, startInBounds bool, layer region.Layer, regions func() *region.Map, checkNoCorridor func()) (Kind, []uint64) {
	switch {
	case pathing.Octile(sx, sy, tx, ty) <= longPath || noRegions || exactLong:
		return Exact, nil
	case !q.Walkable(tx, ty) || !startInBounds:
		return Failed, nil
	case sx == tx && sy == ty:
		return Here, nil
	}
	allow, ok := regions().Corridor(layer, sx, sy, tx, ty)
	if !ok {
		checkNoCorridor()
		return Failed, nil
	}
	return Corridor, allow
}

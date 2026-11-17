package visit

import (
	"slices"

	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/sim/entity"
)

// Kind is the purpose of a group on the colony map.
type Kind uint8

const (
	KindVisitor Kind = iota
	KindTraveler
	KindSmallTrader
	KindCaravan
	KindAid
	KindQuest
	KindManaged
)

// Role determines what a member does for its group.
type Role uint8

const (
	RoleVisitor Role = iota
	RoleLeader
	RoleTrader
	RoleGuard
	RolePackAnimal
	RoleFighter
)

// Phase is a group's durable visit state.
type Phase uint8

const (
	PhaseEntering Phase = iota
	PhaseStaying
	PhaseLeaving
)

// LeaveReason is the first decisive reason for departure.
type LeaveReason uint8

const (
	LeaveScheduled LeaveReason = iota
	LeaveThreat
	LeaveLoss
	LeaveCaptured
	LeaveHostile
	LeaveDismissed
	LeaveQuest
	LeaveRecall
)

// Member represents a person's or pack animal's role in a visit.
type Member struct {
	ID       entity.EntityID
	Role     Role
	Lost     bool
	Departed bool
}

// Group owns a shared visit goal. Per-pawn movement and combat remain in job.Record.
type Group struct {
	ID                               uint64
	Kind                             Kind
	Phase                            Phase
	Faction                          int
	Leader                           entity.EntityID
	Members                          []Member
	CampX, CampY                     int
	ExitX, ExitY                     int
	ArrivedAt, LeaveAt, DefenseUntil uint64
	Reason                           LeaveReason
	GiftDone                         bool
}

// Clone makes saved groups independent of the live member slice.
func (g Group) Clone() Group {
	g.Members = slices.Clone(g.Members)
	return g
}

// NewGroup records a group and assigns existing visitor records to it.
// Animal members are recorded here but have no visit.Rec.
func (v Visits) NewGroup(kind Kind, faction int, members []Member, now, leaveAt uint64) uint64 {
	if len(members) == 0 {
		return 0
	}
	slices.SortFunc(members, func(a, b Member) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	id := v.h.GroupID()
	leader := members[0].ID
	for _, m := range members {
		if m.Role == RoleLeader || m.Role == RoleTrader {
			leader = m.ID
			break
		}
	}
	g := Group{ID: id, Kind: kind, Faction: faction, Leader: leader,
		Members: slices.Clone(members), ArrivedAt: now, LeaveAt: leaveAt}
	g.CampX, g.CampY = v.h.Home()
	w, ht := v.h.Size()
	x, y := v.h.Pos(leader)
	if min(x, w-1-x) <= min(y, ht-1-y) {
		if x < w/2 {
			g.ExitX, g.ExitY = Inset, y
		} else {
			g.ExitX, g.ExitY = w-1-Inset, y
		}
	} else if y < ht/2 {
		g.ExitX, g.ExitY = x, Inset
	} else {
		g.ExitX, g.ExitY = x, ht-1-Inset
	}
	*v.h.Groups(true) = append(*v.h.Groups(true), g)
	for i := range *v.h.Records(true) {
		r := &(*v.h.Records(true))[i]
		if _, ok := slices.BinarySearchFunc(g.Members, r.ID, func(m Member, id entity.EntityID) int {
			switch {
			case m.ID < id:
				return -1
			case m.ID > id:
				return 1
			}
			return 0
		}); ok {
			r.GroupID = id
		}
	}
	return id
}

// GroupOf reports the saved group of a visitor or pack animal.
func (v Visits) GroupOf(id entity.EntityID) (Group, bool) {
	gs := v.h.Groups(false)
	if gs == nil {
		return Group{}, false
	}
	for _, g := range *gs {
		if _, ok := slices.BinarySearchFunc(g.Members, id, func(m Member, id entity.EntityID) int {
			switch {
			case m.ID < id:
				return -1
			case m.ID > id:
				return 1
			}
			return 0
		}); ok {
			return g, true
		}
	}
	return Group{}, false
}

// LeaveGroup changes the goal once, then interrupts idle members.
// An active fight or rescue can finish before the member walks out.
func (v Visits) LeaveGroup(id uint64, reason LeaveReason, now uint64) {
	gs := v.h.Groups(false)
	if gs == nil {
		return
	}
	for i := range *gs {
		g := &(*gs)[i]
		if g.ID != id || g.Phase == PhaseLeaving {
			return
		}
		g.Phase, g.Reason = PhaseLeaving, reason
		for j := range *v.h.Records(true) {
			r := &(*v.h.Records(true))[j]
			if r.GroupID != id || r.Leaving {
				continue
			}
			r.Leaving, r.RetryAt = true, 0
			v.h.Release(r.ID)
			v.h.SetJob(r.ID, job.Record{})
		}
		return
	}
}

// TickGroups advances shared goals, then prunes groups only after all members
// are accounted for. A dismissed member never resumes a visit.
func (v Visits) TickGroups(now uint64) {
	gs := v.h.Groups(false)
	if gs == nil {
		return
	}
	kept := (*gs)[:0]
	for i := range *gs {
		g := &(*gs)[i]
		present := 0
		lost := 0
		for j := range g.Members {
			m := &g.Members[j]
			if m.Departed {
				continue
			}
			gone, harmed := v.h.MemberGone(m.ID, m.Role == RolePackAnimal)
			if gone {
				m.Departed = true
			}
			m.Lost = m.Lost || harmed
			if m.Lost {
				lost++
			}
			if !m.Departed {
				present++
				if m.Role == RolePackAnimal {
					v.h.PackStep(m.ID, *g, now)
				}
			}
		}
		if present == 0 {
			if g.Kind == KindCaravan && g.Phase == PhaseLeaving && g.Reason == LeaveScheduled && !g.GiftDone {
				eligible := true
				for _, m := range g.Members {
					eligible = eligible && !m.Lost
				}
				if eligible {
					v.h.GroupGift(*g)
				}
				g.GiftDone = true
			}
			v.h.GroupClosed(g.ID)
			continue
		}
		switch {
		case g.Kind == KindQuest || g.Kind == KindManaged:
			// The owner, rather than the incident clock, decides departure.
		case g.Phase == PhaseLeaving:
		case (g.Kind == KindCaravan || g.Kind == KindSmallTrader) && (lost*5 >= len(g.Members) && lost > 0):
			v.LeaveGroup(g.ID, LeaveLoss, now)
		case (g.Kind == KindCaravan || g.Kind == KindVisitor || g.Kind == KindSmallTrader) &&
			(now+g.ID)%250 == 0 && v.h.GroupDanger(g.Leader):
			v.LeaveGroup(g.ID, LeaveThreat, now)
		case now >= g.LeaveAt:
			v.LeaveGroup(g.ID, LeaveScheduled, now)
		default:
			g.Phase = PhaseStaying
		}
		kept = append(kept, *g)
	}
	clear((*gs)[len(kept):])
	*gs = kept
}

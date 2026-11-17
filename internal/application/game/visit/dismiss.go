package visit

import (
	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/sim/entity"
)

// Dismiss starts a real conversation job with the group leader.
func (v Visits) Dismiss(pawn, target entity.EntityID) {
	g, ok := v.GroupOf(target)
	if !ok || g.Leader != target || g.Phase == PhaseLeaving ||
		(g.Kind != KindVisitor && g.Kind != KindCaravan && g.Kind != KindSmallTrader) ||
		!v.h.CanAsk(pawn) || !v.h.Present(target) {
		return
	}
	x, y := v.h.Pos(target)
	tx, ty, ok := v.h.Beside(pawn, x, y)
	if !ok {
		return
	}
	v.h.Interrupt(pawn)
	v.h.StartJob(pawn, job.Record{Kind: job.DismissVisitorGroup, Target: target,
		TX: tx, TY: ty, WorkLeft: 30, Forced: true})
}

// DismissJob completes the conversation once the visitor is adjacent.
func (v Visits) DismissJob(pawn entity.EntityID, j job.Record, now uint64) {
	g, ok := v.GroupOf(j.Target)
	if !ok || g.Leader != j.Target || g.Phase == PhaseLeaving || !v.h.Present(j.Target) {
		j.State = job.Fail
		v.h.SetJob(pawn, j)
		return
	}
	x, y := v.h.Pos(j.Target)
	px, py := v.h.Pos(pawn)
	if max(x-px, px-x, y-py, py-y) > 1 {
		tx, ty, ok := v.h.Beside(pawn, x, y)
		if ok && (tx != j.TX || ty != j.TY) {
			v.h.StartJob(pawn, job.Record{Kind: j.Kind, Target: j.Target,
				TX: tx, TY: ty, WorkLeft: j.WorkLeft, Forced: true})
			return
		}
		j.State = job.Fail
		v.h.SetJob(pawn, j)
		return
	}
	if j.WorkLeft > 1 {
		j.WorkLeft--
		v.h.SetJob(pawn, j)
		return
	}
	v.LeaveGroup(g.ID, LeaveDismissed, now)
	j.State = job.Complete
	v.h.SetJob(pawn, j)
}

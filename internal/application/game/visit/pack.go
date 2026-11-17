package visit

import "rimworld-go/internal/domain/sim/entity"

// packStep follows the caravan's leader while staying, then leaves via the
// saved exit. Its member flag must be set before removal: cargo leaves with
// the animal and is never mistakenly counted as lost.
func (v Visits) packStep(g *Group, m *Member, now uint64) {
	h := v.h
	x, y, ready, ok := h.PackState(m.ID, now)
	if !ok {
		return
	}
	tx, ty := g.CampX, g.CampY
	moving := false
	if g.Phase == PhaseLeaving {
		tx, ty = g.ExitX, g.ExitY
		if max(x-tx, tx-x, y-ty, ty-y) <= 1 {
			m.Departed = true
			h.PackLeave(m.ID)
			return
		}
		moving = ready
	} else {
		if h.Present(g.Leader) {
			tx, ty = h.Pos(g.Leader)
		}
		moving = ready && max(x-tx, tx-x, y-ty, ty-y) > 4
	}
	h.PackMove(m.ID, tx, ty, moving, now)
}

type PackSupport interface {
	PackState(id entity.EntityID, now uint64) (x, y int, ready, ok bool)
	PackMove(id entity.EntityID, tx, ty int, move bool, now uint64)
	PackLeave(id entity.EntityID)
}

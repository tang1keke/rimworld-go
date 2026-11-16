package game

import (
	"slices"

	"rimworld-go/internal/application/game/cadence"
	"rimworld-go/internal/application/game/letters"
	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/pawn/body"
	"rimworld-go/internal/domain/pawn/health"
	"rimworld-go/internal/domain/pawn/mentalbreak"
	"rimworld-go/internal/domain/sim/clock"
	"rimworld-go/internal/domain/sim/entity"
	"rimworld-go/pkg/msgfmt"
)

// MED-08: deterministic daily onset and hourly condition progression.

const (
	// dementiaWanderDays is the mean days between a demented colonist's
	// confused wanders. wiki: Ailments (MTB 5 days).
	dementiaWanderDays = 5
	// dementiaWanderTicks is how long a confused wander lasts (original tuning).
	dementiaWanderTicks = 4 * clock.TicksPerHour
)

// stepAging runs chronic disease progression and onset.
func (g *Game) stepAging(now uint64) {
	g.stepAgingConds(now)
	if cadence.Due(now, clock.TicksPerDay, cadence.OffMedical, 0) {
		g.rollAging(now)
	}
}

// agingSalt fixes per-pawn disease progression.
func (g *Game) agingSalt(id entity.EntityID) uint64 {
	return g.streams.seed ^ uint64(id)*0x9e3779b97f4a7c15
}

// stepAgingConds updates chronic patients.
func (g *Game) stepAgingConds(now uint64) {
	if len(g.med.patients) == 0 {
		return
	}
	for _, id := range ownPatients(g) { // patients.go: this map's, a copy (a death touches the index)
		b := g.bodies[id]
		if !b.HasAging() {
			continue
		}
		v, ok := g.vital.Get(id)
		if !ok || v.Status == health.Dead {
			continue
		}
		st := b.StepAging(now, g.agingSalt(id), g.stream(streamMedical).IntN)
		pos, _ := g.positions.Get(id)
		if st.Died != body.CondNone {
			cause := health.CauseHeartAttack
			switch st.Died {
			case body.CondCarcinoma:
				cause = health.CauseCarcinoma
				g.message(pos.X, pos.Y, "%s died of a carcinoma in the %s.", g.name(id), st.DiedOn.Label())
			case body.CondArteryBlockage:
				g.message(pos.X, pos.Y, "%s's blocked arteries stopped the heart.", g.name(id))
			default:
				g.message(pos.X, pos.Y, "%s died of a heart attack.", g.name(id))
			}
			g.dieMedical(id, cause) // caravan_medical.go
			g.clearPatient(id)
			continue
		}
		for _, k := range st.Ended {
			if k == body.CondHeartAttack {
				g.message(pos.X, pos.Y, "%s has come through the heart attack.", g.name(id))
			} else {
				g.message(pos.X, pos.Y, "%s's carcinoma has faded away.", g.name(id))
			}
		}
		for _, p := range st.Lost {
			g.message(pos.X, pos.Y, "A carcinoma destroyed %s's %s.", g.name(id), p.Label())
		}
		if st.Changed {
			g.bodyChanged(id, b, &v)
			g.updateDowned(id, b, &v)
		}
		if c, ok := b.Cond(body.CondAlzheimers); ok {
			// wiki: Alzheimer's; confusion and memory loss increase by stage.
			r := g.stream(streamMedical)
			wander := [...]int{12, 9, 7, 4}
			forget := [...]int{7 * 24, 4 * 24, 2 * 24, 19}
			stage := min(int(c.Stage), 3)
			if r.IntN(wander[stage]*24) == 0 {
				confusedWander(g, id, now, "alzheimers")
			}
			if r.IntN(forget[stage]) == 0 {
				forgetThought(g, id, now, r.IntN)
			}
		}
	}
}

// agingCandidate finds eligible living colonists.
func (g *Game) agingCandidate(id entity.EntityID) bool {
	v, ok := g.vital.Get(id)
	if !ok || v.Status == health.Dead || g.prison.Is(id) || g.isRaider(id) || g.visitor(id) {
		return false
	}
	_, aged := g.sk.bios[id]
	return aged
}

// rollAging performs deterministic daily onset checks.
func (g *Game) rollAging(now uint64) {
	ids := slices.Clone(g.pawnIDs)
	slices.Sort(ids)
	r := g.stream(streamMedical)
	for _, id := range ids {
		if !g.agingCandidate(id) {
			continue
		}
		age := g.sk.bios[id].AgeYears()
		for _, ch := range body.Chronics() {
			if ch.Kind == body.CondAsthma {
				// wiki: Hediffs/Core/Hediff_Giver_Sets/OrganicStandard.
				// One birthday roll uses biological age / 80-year lifespan.
				ageDays := int(g.sk.bios[id].AgeDays) + int(now/clock.TicksPerDay)
				if body.AsthmaBirthday(ageDays, 80*clock.DaysPerYear, r.IntN) {
					v, _ := g.vital.Get(id)
					b := g.ensureBody(id, &v)
					if b != nil && body.DevelopAsthma(b, now) {
						g.bodyChanged(id, b, &v)
						g.updateDowned(id, b, &v)
						pos, _ := g.positions.Get(id)
						g.message(pos.X, pos.Y, "%s has developed asthma.", g.name(id))
					}
				}
				continue
			}
			chance := body.OnsetPerDay(ch.Kind, age)
			if chance <= 0 {
				continue
			}
			if ch.Parts == nil { // a carcinoma: one roll, then a part by coverage
				if int32(r.IntN(body.OnsetScale)) < chance {
					g.developAging(id, ch.Kind, -1, now)
				}
				continue
			}
			for _, p := range ch.Parts {
				if !g.bodies[id].CanDevelop(ch.Kind, p) {
					continue
				}
				if int32(r.IntN(body.OnsetScale)) < chance {
					g.developAging(id, ch.Kind, int(p), now)
				}
			}
		}
		if b := g.bodies[id]; b != nil {
			if mtb := b.HeartAttackMTB(); mtb > 0 && r.IntN(int(mtb)) == 0 {
				g.developAging(id, body.CondHeartAttack, int(body.Heart), now)
			}
			if _, dem := b.Cond(body.CondDementia); dem && r.IntN(dementiaWanderDays) == 0 {
				g.dementiaWander(id, now)
			}
		}
	}
}

// developAging records an ailment and reports its onset.
func (g *Game) developAging(id entity.EntityID, k body.CondKind, p int, now uint64) {
	v, _ := g.vital.Get(id)
	b := g.ensureBody(id, &v)
	if b == nil {
		return
	}
	part := body.Part(p)
	if p < 0 {
		var ok bool
		if part, ok = b.PickPart(g.stream(streamMedical).IntN); !ok {
			return
		}
	}
	if !b.Develop(k, part, now) {
		return
	}
	if k == body.CondCarcinoma && g.stream(streamMedical).IntN(100) < 30 {
		c, _ := b.Cond(body.CondCarcinoma)
		c.Rate = -1 // wiki: Carcinoma; 30% are painless, persisted in Rate
	}
	g.bodyChanged(id, b, &v)
	g.updateDowned(id, b, &v)
	pos, _ := g.positions.Get(id)
	switch k {
	case body.CondHeartAttack:
		g.post(MessageView{Tick: now, X: pos.X, Y: pos.Y, Focus: id, Letter: true, Title: "Heart attack", Kind: letters.Threat,
			Msg: msgfmt.Of("%s is having a heart attack. Untreated it is usually fatal within hours: get a doctor to tend %s every hour, each tend may ease it. A pump or valve heart prevents heart attacks.",
				g.personName(id), g.personName(id))})
	case body.CondCarcinoma:
		g.post(MessageView{Tick: now, X: pos.X, Y: pos.Y, Focus: id, Letter: true, Title: "Carcinoma", Kind: letters.Caution,
			Msg: msgfmt.Of("%s has a carcinoma in the %s. If it grows to full size it destroys the %s. A doctor with Medicine 10 can excise it with 4 industrial medicine or better; a failed excision kills one time in four.",
				g.personName(id), part.Label(), part.Label())})
	default:
		g.message(pos.X, pos.Y, "%s has developed %s.", g.name(id), body.AgingOnsetText(k, part))
	}
}

// dementiaWander shares the confusion behavior.
func forgetThought(g *Game, id entity.EntityID, now uint64, roll func(int) int) {
	memories := g.memories[id].Prune(now)
	if len(memories) == 0 {
		return
	}
	i := roll(len(memories))
	memories = append(memories[:i], memories[i+1:]...)
	g.memories[id] = memories
}

func (g *Game) dementiaWander(id entity.EntityID, now uint64) {
	confusedWander(g, id, now, "dementia")
}

func confusedWander(g *Game, id entity.EntityID, now uint64, reason string) {
	if v, ok := g.vital.Get(id); !ok || v.Status != health.Alive {
		return
	}
	if _, breaking := g.breaks[id]; breaking || g.draft[id] {
		return
	}
	g.interrupt(id, job.Cancelled)
	delete(g.orderQueue, id)
	g.breaks[id] = breakState{kind: mentalbreak.SadWander, until: now + dementiaWanderTicks}
	pos, _ := g.positions.Get(id)
	g.message(pos.X, pos.Y, "%s is wandering around, confused (%s).", g.name(id), reason)
}

// --- Health tab -------------------------------------------------------
